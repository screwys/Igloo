package com.screwy.igloo.sync

import androidx.test.core.app.ApplicationProvider
import com.screwy.igloo.data.IglooDatabase
import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.data.RoomTestSupport
import com.screwy.igloo.data.entity.AndroidSyncAssetEntity
import com.screwy.igloo.data.entity.AndroidSyncHeadEntity
import com.screwy.igloo.data.entity.AndroidSyncStateEntity
import com.screwy.igloo.data.entity.ChannelSettingEntity
import com.screwy.igloo.data.entity.FeedItemEntity
import com.screwy.igloo.data.entity.FeedLikeEntity
import com.screwy.igloo.data.entity.FeedRankEntity
import com.screwy.igloo.data.entity.FeedSeenEntity
import com.screwy.igloo.data.entity.MomentsCursorEntity
import com.screwy.igloo.data.entity.OutboxEntity
import com.screwy.igloo.data.entity.RetweetSourceEntity
import com.screwy.igloo.log.InMemoryLogSink
import com.screwy.igloo.log.Logger
import com.screwy.igloo.media.ForegroundPromoter
import com.screwy.igloo.net.AndroidSyncApi
import com.screwy.igloo.net.AndroidSyncAssetDto
import com.screwy.igloo.net.AndroidSyncChangeDto
import com.screwy.igloo.net.AndroidSyncPageResponse
import com.screwy.igloo.net.AndroidSyncReconcileResponse
import com.screwy.igloo.net.AndroidSyncRetentionRequest
import com.screwy.igloo.net.Reachability
import com.screwy.igloo.net.ServerBaseUrlProvider
import com.screwy.igloo.net.iglooJson
import com.screwy.igloo.outbox.OutboxKind
import com.screwy.igloo.outbox.OutboxRejectedMutation
import com.screwy.igloo.outbox.applyOptimisticMutation
import io.ktor.client.HttpClient
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.MockRequestHandleScope
import io.ktor.client.engine.mock.respond
import io.ktor.client.plugins.contentnegotiation.ContentNegotiation
import io.ktor.http.ContentType
import io.ktor.http.HttpStatusCode
import io.ktor.http.HttpHeaders
import io.ktor.http.headersOf
import io.ktor.serialization.kotlinx.json.json
import java.io.File
import java.io.IOException
import java.util.Collections
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.put
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], manifest = Config.NONE)
class AndroidSyncMirrorTest {
    @get:Rule val temporaryFolder = TemporaryFolder()

    private lateinit var db: IglooDatabase
    private lateinit var scope: CoroutineScope
    private lateinit var client: HttpClient
    private lateinit var logger: Logger
    private var nowMs = 1_000_000_000L

    @Before
    fun setUp() {
        db = RoomTestSupport.freshDb()
        scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val prefs = PreferencesRepo(db.preferenceDao(), scope, nowMsProvider = { nowMs })
        logger = Logger(prefs, InMemoryLogSink(), scope, nowMsProvider = { nowMs })
    }

    @After
    fun tearDown() {
        if (::client.isInitialized) client.close()
        RoomTestSupport.closeAfterScope(scope, db)
    }

    @Test
    fun enrichedPostSyncRemainsAvailableOfflineWithoutChangingLocalOptOuts() = runBlocking {
        db.preferenceDao().put(PreferencesRepo.Keys.SHOW_X_ACCOUNT_REGION, "false", nowMs)
        db.preferenceDao().put(PreferencesRepo.Keys.SHOW_X_COMMUNITY_NOTES, "false", nowMs)
        val pollJson = """{"choices":[{"label":"Sample","count":1,"percentage":100}],"total_votes":1,"captured_at":100}"""
        val detailsJson = """{"source":"App Store","location_accurate":false,"username_changes":0,"user_id":"12345"}"""
        val article = feedChange("sample_article", channelId = "twitter_sample")
        val item = article.payload!!.jsonObject.getValue("item").jsonObject
        val profile = profileOnlyChannelChange("twitter_sample")
        val profilePayload = profile.payload!!.jsonObject.getValue("profile").jsonObject
        val changes = listOf(
            article.copy(payload = buildJsonObject {
                put("item", JsonObject(item + buildJsonObject {
                    put("article_title", "Sample article")
                    put("body_text", "First paragraph.\n\nSecond paragraph.")
                    put("quote_article_title", "Quoted article")
                    put("quote_body_text", "Quoted full text.")
                    put("poll_json", pollJson)
                    put("quote_poll_json", pollJson)
                    put("community_note", "Context https://example.com/source")
                    put("quote_community_note", "Quoted context https://example.com/quote")
                }))
            }),
            profile.copy(payload = buildJsonObject {
                put("channel", JsonNull)
                put("profile", JsonObject(profilePayload + buildJsonObject {
                    put("account_region", "United States")
                    put("account_details_json", detailsJson)
                }))
            }),
            upsertChange(
                ownerKind = "setting",
                ownerId = PreferencesRepo.Keys.X_COMMUNITY_NOTES_ENABLED,
                payload = buildJsonObject {
                    put("key", PreferencesRepo.Keys.X_COMMUNITY_NOTES_ENABLED)
                    put("value", "false")
                },
            ),
            upsertChange(
                ownerKind = "setting",
                ownerId = PreferencesRepo.Keys.X_ACCOUNT_REGION_ENABLED,
                payload = buildJsonObject {
                    put("key", PreferencesRepo.Keys.X_ACCOUNT_REGION_ENABLED)
                    put("value", "true")
                },
            ),
        )
        buildMirror(MockEngine { request ->
            when (request.url.encodedPath) {
                "/api/android/sync/bootstrap" -> respondJson(page(changes, "article-cursor"))
                "/api/android/sync/changes" -> respondJson(page(emptyList(), "article-cursor"))
                "/api/android/sync/health" -> respondOk()
                else -> error("Unexpected request ${request.url}")
            }
        }).syncMetadataOnce()

        val saved = db.feedItemDao().getById("sample_article")!!
        assertEquals("Sample article", saved.articleTitle)
        assertEquals("First paragraph.\n\nSecond paragraph.", saved.bodyText)
        assertEquals("Quoted article", saved.quoteArticleTitle)
        assertEquals("Quoted full text.", saved.quoteBodyText)
        assertEquals(pollJson, saved.pollJson)
        assertEquals(pollJson, saved.quotePollJson)
        assertEquals("Context https://example.com/source", saved.communityNote)
        assertEquals("Quoted context https://example.com/quote", saved.quoteCommunityNote)
        assertEquals(detailsJson, db.channelProfileDao().getById("twitter_sample")?.accountDetailsJson)
        assertEquals(detailsJson, db.feedReadDao().getThreadTree("sample_article").single().authorAccountDetailsJson)
        assertEquals("United States", db.channelProfileDao().getById("twitter_sample")?.accountRegion)
        assertEquals("United States", db.feedReadDao().getThreadTree("sample_article").single().authorAccountRegion)
        assertEquals("true", db.preferenceDao().getValue(PreferencesRepo.Keys.X_ACCOUNT_REGION_ENABLED))
        assertEquals("false", db.preferenceDao().getValue(PreferencesRepo.Keys.SHOW_X_ACCOUNT_REGION))
        assertEquals("false", db.preferenceDao().getValue(PreferencesRepo.Keys.X_COMMUNITY_NOTES_ENABLED))
        assertEquals("false", db.preferenceDao().getValue(PreferencesRepo.Keys.SHOW_X_COMMUNITY_NOTES))
    }

    @Test
    fun flatBootstrapPageAppliesDependenciesBeforeOpaqueCursorCommit() = runBlocking {
        val requests = mutableListOf<String>()
        val changes =
            listOf(
                assetChange(missingAsset("sample_asset_post", "sample_post")),
                assetChange(
                    missingAsset(
                        "sample_asset_retweeter",
                        "sample_retweeter",
                        ownerKind = "channel",
                        assetKind = "avatar",
                    )
                ),
                assetChange(
                    missingAsset(
                        "sample_asset_reposter",
                        "sample_reposter",
                        ownerKind = "channel",
                        assetKind = "avatar",
                    )
                ),
                assetChange(
                    missingAsset(
                        "sample_asset_comment_raw",
                        "youtube_UCsample_raw",
                        ownerKind = "comment_author",
                        assetKind = "avatar",
                        bucket = "youtube",
                    )
                ),
                assetChange(
                    missingAsset(
                        "sample_asset_comment_prefixed",
                        "youtube_UCsample_prefixed",
                        ownerKind = "comment_author",
                        assetKind = "avatar",
                        bucket = "youtube",
                    )
                ),
                profileOnlyChannelChange("sample_retweeter"),
                profileOnlyChannelChange("sample_reposter"),
                feedChange("sample_post"),
                retweetSourcesChange("hash-sample_post", "sample_retweeter"),
                videoChange(
                    "sample_video",
                    nowMs,
                    nowMs,
                    reposterChannelId = "sample_reposter",
                    commentAuthorIds = listOf("UCsample_raw", "youtube_UCsample_prefixed"),
                ),
                feedLikeChange("sample_post"),
            )
        val engine = MockEngine { request ->
            when (request.url.encodedPath) {
                "/api/android/sync/bootstrap" -> {
                    requests += "bootstrap:${request.url.parameters["after"]}"
                    respondJson(page(changes, "cursor-1"))
                }
                "/api/android/sync/changes" -> {
                    requests += "changes:${request.url.parameters["after"]}"
                    assertRetentionQuery(request.url.parameters.entries().associate { it.key to it.value.single() })
                    respondJson(page(emptyList(), "cursor-1"))
                }
                "/api/android/sync/health" -> respondOk()
                else -> error("Unexpected request ${request.url}")
            }
        }

        buildMirror(engine).syncOnce()

        assertNotNull(db.feedItemDao().getById("sample_post"))
        assertNotNull(db.videoDao().getById("sample_video"))
        assertEquals(1, db.retweetSourceDao().countForContentHash("hash-sample_post"))
        assertEquals(
            listOf("sample_reposter"),
            db.videoRepostSourceDao().forVideo("sample_video").map { it.reposterChannelId },
        )
        assertEquals(
            setOf("youtube_UCsample_raw", "youtube_UCsample_prefixed"),
            db.videoCommentDao().forVideoFlow("sample_video").first().mapNotNull { it.authorId }.toSet(),
        )
        assertNotNull(db.channelProfileDao().getById("sample_retweeter"))
        assertNotNull(db.channelProfileDao().getById("sample_reposter"))
        assertTrue(db.feedLikeDao().exists("sample_post"))
        listOf(
            "sample_asset_post",
            "sample_asset_retweeter",
            "sample_asset_reposter",
            "sample_asset_comment_raw",
            "sample_asset_comment_prefixed",
        ).forEach { assertNotNull(db.androidSyncDao().asset(it)) }
        assertEquals(11, db.androidSyncDao().headCount())
        assertEquals("cursor-1", db.androidSyncDao().syncState()?.cursor)
        assertEquals(listOf("bootstrap:null", "changes:cursor-1"), requests)
    }

    @Test
    fun priorityStateAppliesUserStateWithoutWaitingForContentCatchUp() = runBlocking {
        db.androidSyncDao().upsertSyncState(changesState("content-cursor"))
        db.feedItemDao().upsert(FeedItemEntity(tweetId = "sample_post"))
        db.momentsCursorDao()
            .upsert(MomentsCursorEntity("all", "local_equal", updatedAtMs = nowMs))
        val requests = mutableListOf<String>()
        val engine = MockEngine { request ->
            when (request.url.encodedPath) {
                "/api/android/sync/state" -> {
                    requests += requireNotNull(request.url.parameters["after"])
                    respondJson(
                        page(
                            listOf(
                                feedLikeChange("sample_post"),
                                upsertChange(
                                    ownerKind = "watch_history",
                                    ownerId = "sample_video",
                                    payload = buildJsonObject {
                                        put("video_id", "sample_video")
                                        put("playback_position", 42.0)
                                        put("duration", 100.0)
                                        put("updated_at_ms", nowMs)
                                    },
                                ),
                                upsertChange(
                                    ownerKind = "moments_cursor",
                                    ownerId = "all",
                                    payload = buildJsonObject {
                                        put("scope", "all")
                                        put("video_id", "sample_moment")
                                        put("position_ms", 42000L)
                                        put("sort_at_ms", nowMs)
                                        put("updated_at_ms", nowMs)
                                    },
                                ),
                            ),
                            "state-cursor",
                        )
                    )
                }
                else -> error("Unexpected request ${request.url}")
            }
        }

        val result = buildMirror(engine).syncPriorityStateOnce()

        assertEquals(listOf("content-cursor"), requests)
        assertTrue(db.feedLikeDao().exists("sample_post"))
        assertEquals(42.0, db.watchHistoryDao().getById("sample_video")?.playbackPosition)
        assertEquals("sample_moment", db.momentsCursorDao().get("all")?.videoId)
        assertEquals(
            "state-cursor",
            db.preferenceDao().getValue("android_sync_priority_state_cursor"),
        )
        assertEquals("content-cursor", db.androidSyncDao().syncState()?.cursor)
        assertTrue(result.metadataRequired)
    }

    @Test
    fun prioritySelectionWideningSurvivesTheNextMainPageAndStartsReplay() = runBlocking {
        db.androidSyncDao().upsertSyncState(changesState("cursor-a"))
        val firstMetadataStarted = CompletableDeferred<Unit>()
        val releaseFirstMetadata = CompletableDeferred<Unit>()
        val laterMetadataStarted = CompletableDeferred<Unit>()
        val releaseLaterMetadata = CompletableDeferred<Unit>()
        val requests = Collections.synchronizedList(mutableListOf<String>())
        val engine = MockEngine { request ->
            when (request.url.encodedPath) {
                "/api/android/sync/changes" -> {
                    val after = requireNotNull(request.url.parameters["after"])
                    requests += "changes:$after"
                    when (after) {
                        "cursor-a" -> {
                            firstMetadataStarted.complete(Unit)
                            releaseFirstMetadata.await()
                            respondJson(page(emptyList(), "cursor-b", end = false))
                        }
                        "cursor-b" -> {
                            laterMetadataStarted.complete(Unit)
                            releaseLaterMetadata.await()
                            respondJson(page(emptyList(), "cursor-c"))
                        }
                        "cursor-replayed" -> respondJson(page(emptyList(), "cursor-replayed"))
                        else -> error("Unexpected changes cursor $after")
                    }
                }
                "/api/android/sync/state" -> {
                    requests += "priority"
                    respondJson(
                        page(listOf(channelFollowChange("sample_channel")), "priority-cursor")
                    )
                }
                "/api/android/sync/bootstrap" -> {
                    requests += "bootstrap"
                    respondJson(
                        page(
                            listOf(channelFollowChange("sample_channel")),
                            "cursor-replayed",
                        )
                    )
                }
                "/api/android/sync/health" -> respondOk()
                else -> error("Unexpected request ${request.url}")
            }
        }
        val mirror = buildMirror(engine)
        val metadata = async { mirror.syncMetadataOnce() }
        firstMetadataStarted.await()
        val priority = async { mirror.syncPriorityStateOnce() }

        releaseFirstMetadata.complete(Unit)
        laterMetadataStarted.await()
        assertTrue(priority.await().metadataRequired)
        releaseLaterMetadata.complete(Unit)
        metadata.await()

        assertTrue("bootstrap" in requests)
        assertEquals("cursor-replayed", db.androidSyncDao().syncState()?.cursor)
        assertTrue(db.channelFollowDao().exists("sample_channel"))
    }

    @Test
    fun pendingOptimisticFollowCannotHideAuthoritativePriorityWidening() = runBlocking {
        db.androidSyncDao().upsertSyncState(changesState("content-cursor"))
        val pending =
            OutboxEntity(
                kind = OutboxKind.CODE_FOLLOW,
                itemId = "sample_channel",
                payloadJson =
                    """{"channel_id":"sample_channel","action":"set","updated_at_ms":$nowMs}""",
                createdAtMs = nowMs,
            )
        db.outboxDao().insert(pending)
        applyOptimisticMutation(db, db.outboxDao().pendingRows().single())
        val engine = MockEngine { request ->
            when (request.url.encodedPath) {
                "/api/android/sync/state" ->
                    respondJson(
                        page(listOf(channelFollowChange("sample_channel")), "priority-cursor")
                    )
                else -> error("Unexpected request ${request.url}")
            }
        }

        val result = buildMirror(engine).syncPriorityStateOnce()

        assertTrue(result.metadataRequired)
        assertTrue(requireNotNull(db.androidSyncDao().syncState()).bootstrapRequired)
        assertTrue(db.channelFollowDao().exists("sample_channel"))
    }

    @Test
    fun unacknowledgedSelectionMarkerDoesNotStartReplay() = runBlocking {
        db.androidSyncDao().upsertSyncState(changesState("content-cursor"))
        db.outboxDao()
            .insert(
                OutboxEntity(
                    kind = OutboxKind.CODE_FOLLOW,
                    itemId = "sample_channel",
                    payloadJson =
                        """{"channel_id":"sample_channel","action":"set","updated_at_ms":$nowMs,"selection_widening":true,"selection_baseline":0}""",
                    createdAtMs = nowMs,
                )
            )
        val requests = mutableListOf<String>()
        val engine = MockEngine { request ->
            requests += request.url.encodedPath
            when (request.url.encodedPath) {
                "/api/android/sync/changes" ->
                    respondJson(page(emptyList(), "content-cursor"))
                "/api/android/sync/health" -> respondOk()
                else -> error("Unexpected request ${request.url}")
            }
        }

        buildMirror(engine).syncMetadataOnce()

        assertFalse(requireNotNull(db.androidSyncDao().syncState()).bootstrapRequired)
        assertFalse(requests.contains("/api/android/sync/bootstrap"))
    }

    @Test
    fun promotedSelectionMarkerStartsExactlyOneReplay() = runBlocking {
        db.androidSyncDao()
            .upsertSyncState(changesState("content-cursor").copy(bootstrapRequired = true))
        var bootstrapCalls = 0
        val engine = MockEngine { request ->
            when (request.url.encodedPath) {
                "/api/android/sync/changes" ->
                    respondJson(
                        page(
                            emptyList(),
                            request.url.parameters["after"] ?: "content-cursor",
                        )
                    )
                "/api/android/sync/bootstrap" -> {
                    bootstrapCalls++
                    respondJson(page(emptyList(), "replayed-cursor"))
                }
                "/api/android/sync/health" -> respondOk()
                else -> error("Unexpected request ${request.url}")
            }
        }
        val mirror = buildMirror(engine)

        mirror.syncMetadataOnce()
        mirror.syncMetadataOnce()

        assertEquals(1, bootstrapCalls)
        assertEquals("replayed-cursor", db.androidSyncDao().syncState()?.cursor)
        assertFalse(requireNotNull(db.androidSyncDao().syncState()).bootstrapRequired)
    }

    @Test
    fun priorityStateRunsBetweenBoundedMetadataPages() = runBlocking {
        db.androidSyncDao().upsertSyncState(changesState("content-cursor"))
        val firstMetadataStarted = CompletableDeferred<Unit>()
        val releaseFirstMetadata = CompletableDeferred<Unit>()
        val laterMetadataStarted = CompletableDeferred<Unit>()
        val releaseLaterMetadata = CompletableDeferred<Unit>()
        val engine = MockEngine { request ->
            when (request.url.encodedPath) {
                "/api/android/sync/changes" ->
                    when (request.url.parameters["after"]) {
                        "content-cursor" -> {
                            firstMetadataStarted.complete(Unit)
                            releaseFirstMetadata.await()
                            respondJson(page(emptyList(), "next-content-cursor", end = false))
                        }
                        "next-content-cursor" -> {
                            laterMetadataStarted.complete(Unit)
                            releaseLaterMetadata.await()
                            respondJson(page(emptyList(), "next-content-cursor"))
                        }
                        else -> error("Unexpected cursor ${request.url}")
                    }
                "/api/android/sync/state" ->
                    respondJson(page(listOf(feedLikeChange("sample_priority_post")), "state-cursor"))
                "/api/android/sync/health" -> respondOk()
                else -> error("Unexpected request ${request.url}")
            }
        }
        val mirror = buildMirror(engine)
        val metadata = async { mirror.syncMetadataOnce() }
        firstMetadataStarted.await()
        val priority = async { mirror.syncPriorityStateOnce() }

        releaseFirstMetadata.complete(Unit)
        laterMetadataStarted.await()

        priority.await()
        assertTrue(db.feedLikeDao().exists("sample_priority_post"))
        assertFalse(releaseLaterMetadata.isCompleted)

        releaseLaterMetadata.complete(Unit)
        metadata.await()
    }

    @Test
    fun delayedPriorityCursorCannotReplaceANewerAcknowledgedCursor() = runBlocking {
        db.androidSyncDao().upsertSyncState(changesState("content-cursor"))
        val requestStarted = CompletableDeferred<Unit>()
        val releaseOldResponse = CompletableDeferred<Unit>()
        val stalePage =
            page(
                listOf(
                    upsertChange(
                        ownerKind = "moments_cursor",
                        ownerId = "all",
                        payload = buildJsonObject {
                            put("scope", "all")
                            put("video_id", "server_older")
                            put("position_ms", 0L)
                            put("sort_at_ms", 100L)
                            put("updated_at_ms", nowMs)
                        },
                    )
                ),
                "state-cursor",
            )
        val engine = MockEngine { request ->
            when (request.url.encodedPath) {
                "/api/android/sync/state" -> {
                    requestStarted.complete(Unit)
                    releaseOldResponse.await()
                    respondJson(stalePage)
                }
                else -> error("Unexpected request ${request.url}")
            }
        }
        val sync = async { buildMirror(engine).syncPriorityStateOnce() }
        requestStarted.await()

        val acknowledgedAt = nowMs + 1L
        val pendingId =
            db.outboxDao()
                .insert(
                    OutboxEntity(
                        kind = OutboxKind.CODE_MOMENTS_CURSOR,
                        itemId = "all",
                        payloadJson =
                            """{"scope":"all","video_id":"acknowledged_newer","position_ms":0,"sort_at_ms":200,"updated_at_ms":$acknowledgedAt}""",
                        createdAtMs = acknowledgedAt,
                    )
                )
        db.momentsCursorDao()
            .upsert(
                MomentsCursorEntity(
                    scope = "all",
                    videoId = "acknowledged_newer",
                    sortAtMs = 200L,
                    updatedAtMs = acknowledgedAt,
                )
            )
        db.outboxDao().completeAndDelete(pendingId)

        releaseOldResponse.complete(Unit)
        sync.await()

        assertTrue(db.outboxDao().pendingRows().isEmpty())
        assertEquals("acknowledged_newer", db.momentsCursorDao().get("all")?.videoId)
        assertEquals(acknowledgedAt, db.momentsCursorDao().get("all")?.updatedAtMs)
    }

    @Test
    fun assetDrainBoundsRetriesAndReturnsChangedMetadataPromptly() = runBlocking {
        db.androidSyncDao().upsertAsset(readyAsset("sample_retry_asset"))
        var attempts = 0
        val retryEngine = MockEngine {
            attempts++
            assertEquals(1, attempts)
            respond("", HttpStatusCode.ServiceUnavailable)
        }
        val retryResult =
            buildAssetDrainer(retryEngine) { nowMs }
                .drain(youtubeCutoffMs = Long.MIN_VALUE)
        assertEquals(1, attempts)
        assertEquals(nowMs + 30_000L, retryResult.nextAttemptAtMs)

        db.androidSyncDao().deleteAsset("sample_retry_asset")
        repeat(65) { index ->
            db.androidSyncDao().upsertAsset(readyAsset("sample_changed_asset_%03d".format(index)))
        }
        val requested = Collections.synchronizedList(mutableListOf<String>())
        val changedEngine = MockEngine { request ->
            val assetId = request.url.encodedPath.substringAfter("/assets/").substringBefore('/')
            requested += assetId
            val status =
                if (assetId == "sample_changed_asset_000") HttpStatusCode.NotFound
                else HttpStatusCode.ServiceUnavailable
            respond("", status)
        }

        val failure =
            runCatching {
                buildAssetDrainer(changedEngine) { nowMs }.drain(youtubeCutoffMs = Long.MIN_VALUE)
            }.exceptionOrNull()

        assertTrue(failure is AndroidSyncAssetChangedException)
        assertEquals(64, requested.size)
        assertTrue("sample_changed_asset_064" !in requested)
    }

    @Test
    fun assetDrainKeepsADeferredWakeWhenTheDeadlinePassesDuringTheBatch() = runBlocking {
        db.androidSyncDao().upsertAsset(readyAsset("sample_slow_retry_asset"))
        var clockReads = 0
        val drainer =
            buildAssetDrainer(MockEngine { respond("", HttpStatusCode.ServiceUnavailable) }) {
                clockReads++
                if (clockReads <= 2) nowMs else nowMs + 31_000L
            }

        val result = drainer.drain(youtubeCutoffMs = Long.MIN_VALUE)

        assertEquals(nowMs + 30_000L, result.nextAttemptAtMs)
    }

    @Test
    fun assetDrainPublishesOnlyAnExactLengthResponse() = runBlocking {
        db.androidSyncDao().upsertAsset(readyAsset("sample_complete_asset", sizeBytes = 3))
        buildAssetDrainer(MockEngine { respond("abc", HttpStatusCode.OK) }) { nowMs }
            .drain(youtubeCutoffMs = Long.MIN_VALUE)

        val downloaded = requireNotNull(db.androidSyncDao().asset("sample_complete_asset"))
        val downloadedFile = File(requireNotNull(downloaded.localPath))
        assertEquals(3L, downloadedFile.length())
        assertEquals("abc", downloadedFile.readText())
        assertFalse(File(downloadedFile.parentFile, downloadedFile.name + ".part").exists())

        db.androidSyncDao().upsertAsset(readyAsset("sample_truncated_asset", sizeBytes = 3))
        buildAssetDrainer(MockEngine { respond("ab", HttpStatusCode.OK) }) { nowMs }
            .drain(youtubeCutoffMs = Long.MIN_VALUE)

        val truncated = requireNotNull(db.androidSyncDao().asset("sample_truncated_asset"))
        assertNull(truncated.localPath)
        assertTrue(truncated.nextAttemptAtMs > nowMs)

        nowMs += 31_000L
        buildAssetDrainer(
            MockEngine { request ->
                assertEquals("bytes=2-", request.headers[HttpHeaders.Range])
                respond(
                    "c",
                    HttpStatusCode.PartialContent,
                    headersOf(HttpHeaders.ContentRange, "bytes 2-2/3"),
                )
            }
        ) { nowMs }.drain(youtubeCutoffMs = Long.MIN_VALUE)

        val resumed = requireNotNull(db.androidSyncDao().asset("sample_truncated_asset"))
        assertEquals("abc", File(requireNotNull(resumed.localPath)).readText())
    }

    @Test
    fun assetDrainUsesOneBoundedPackForSmallPresentationAssets() = runBlocking {
        val first =
            readyAsset("sample_avatar_first", sizeBytes = 3).copy(
                assetKind = "avatar",
                ownerId = "sample_channel_first",
                ownerKind = "channel",
                bucket = "avatars",
            )
        val second =
            readyAsset("sample_avatar_second", sizeBytes = 3).copy(
                assetKind = "avatar",
                ownerId = "sample_channel_second",
                ownerKind = "channel",
                bucket = "avatars",
            )
        db.androidSyncDao().upsertAsset(first)
        db.androidSyncDao().upsertAsset(second)
        var requests = 0
        val engine = MockEngine { request ->
            requests++
            assertEquals("/api/android/sync/assets/pack", request.url.encodedPath)
            respond(
                buildString {
                    append("IGLOO-ASSET-PACK-1\n")
                    append("{\"asset_id\":\"sample_avatar_first\",\"revision\":1,\"size_bytes\":3,\"content_type\":\"image/jpeg\"}\n")
                    append("abc")
                    append("{\"asset_id\":\"sample_avatar_second\",\"revision\":1,\"size_bytes\":3,\"content_type\":\"image/jpeg\"}\n")
                    append("def")
                },
                HttpStatusCode.OK,
            )
        }

        buildAssetDrainer(engine) { nowMs }.drain(youtubeCutoffMs = Long.MIN_VALUE)

        assertEquals(1, requests)
        assertEquals(
            "abc",
            File(requireNotNull(db.androidSyncDao().asset(first.assetId)?.localPath)).readText(),
        )
        assertEquals(
            "def",
            File(requireNotNull(db.androidSyncDao().asset(second.assetId)?.localPath)).readText(),
        )
    }

    @Test
    fun dependencyReplacingUpsertCollectsDepartedIdentitiesAndAssets() = runBlocking {
        val initial =
            listOf(
                assetChange(
                    missingAsset(
                        "sample_asset_reposter",
                        "sample_reposter",
                        ownerKind = "channel",
                        assetKind = "avatar",
                    )
                ),
                assetChange(
                    missingAsset(
                        "sample_asset_comment",
                        "youtube_UCsample_commenter",
                        ownerKind = "comment_author",
                        assetKind = "avatar",
                        bucket = "youtube",
                    )
                ),
                profileOnlyChannelChange("sample_reposter"),
                videoChange(
                    "sample_video",
                    nowMs,
                    nowMs,
                    reposterChannelId = "sample_reposter",
                    commentAuthorIds = listOf("UCsample_commenter"),
                ),
            )
        val engine = MockEngine { request ->
            when (request.url.encodedPath) {
                "/api/android/sync/bootstrap" -> respondJson(page(initial, "cursor-initial"))
                "/api/android/sync/changes" ->
                    respondJson(
                        page(
                            listOf(
                                videoChange(
                                    "sample_video",
                                    nowMs,
                                    nowMs,
                                    reposterChannelId = null,
                                )
                            ),
                            "cursor-updated",
                        )
                    )
                "/api/android/sync/health" -> respondOk()
                else -> error("Unexpected request ${request.url}")
            }
        }

        buildMirror(engine).syncOnce()

        assertTrue(db.videoRepostSourceDao().forVideo("sample_video").isEmpty())
        assertNull(db.channelProfileDao().getById("sample_reposter"))
        assertNull(db.androidSyncDao().asset("sample_asset_reposter"))
        assertNull(db.androidSyncDao().asset("sample_asset_comment"))
    }

    @Test
    fun interruptedBootstrapResumesWithoutHidingCanonicalRows() = runBlocking {
        db.feedItemDao().upsert(FeedItemEntity(tweetId = "sample_old_post"))
        db.androidSyncDao().upsertHead(AndroidSyncHeadEntity("feed", "sample_old_post", "feed", nowMs))
        db.androidSyncDao().upsertAsset(
            readyAsset("sample_old_asset").copy(ownerId = "sample_old_owner")
        )
        var bootstrapCalls = 0
        val firstEngine = MockEngine { request ->
            when (request.url.encodedPath) {
                "/api/android/sync/bootstrap" -> {
                    bootstrapCalls++
                    if (request.url.parameters["after"] == null) {
                        respondJson(page(listOf(feedChange("sample_new_post")), "bootstrap-token", end = false))
                    } else {
                        throw IOException("interrupted")
                    }
                }
                else -> error("Unexpected request ${request.url}")
            }
        }

        assertNotNull(runCatching { buildMirror(firstEngine).syncOnce() }.exceptionOrNull())
        assertEquals("bootstrap", db.androidSyncDao().syncState()?.mode)
        assertEquals("bootstrap-token", db.androidSyncDao().syncState()?.cursor)
        assertNotNull(db.feedItemDao().getById("sample_old_post"))
        assertNotNull(db.feedItemDao().getById("sample_new_post"))
        assertNotNull(db.androidSyncDao().asset("sample_old_asset"))

        var resumedAfter: String? = null
        val secondEngine = MockEngine { request ->
            when (request.url.encodedPath) {
                "/api/android/sync/bootstrap" -> {
                    resumedAfter = request.url.parameters["after"]
                    respondJson(page(emptyList(), "cursor-2"))
                }
                "/api/android/sync/changes" -> respondJson(page(emptyList(), "cursor-2"))
                "/api/android/sync/health" -> respondOk()
                else -> error("Unexpected request ${request.url}")
            }
        }

        buildMirror(secondEngine).syncOnce()

        assertEquals(2, bootstrapCalls)
        assertEquals("bootstrap-token", resumedAfter)
        assertNull(db.feedItemDao().getById("sample_old_post"))
        assertNotNull(db.feedItemDao().getById("sample_new_post"))
        assertEquals("cursor-2", db.androidSyncDao().syncState()?.cursor)
    }

    @Test
    fun v3FeedRankSnapshotAtomicallyReplacesRowsAndLegacyHeads() = runBlocking {
        db.feedItemDao().upsert(FeedItemEntity(tweetId = "sample_rank_old"))
        db.feedItemDao().upsert(FeedItemEntity(tweetId = "sample_rank_first"))
        db.feedItemDao().upsert(FeedItemEntity(tweetId = "sample_rank_second"))
        db.feedRankDao().upsert(listOf(FeedRankEntity("sample_rank_old", 1, nowMs)))
        db.androidSyncDao()
            .upsertHead(AndroidSyncHeadEntity("feed_rank", "sample_rank_old", "feed", nowMs))
        val snapshotAt = nowMs + 10
        val rows =
            listOf(
                FeedRankEntity("sample_rank_first", 1, snapshotAt),
                FeedRankEntity("sample_rank_second", 2, snapshotAt),
            )
        val engine =
            bootstrapEngine(
                listOf(
                    feedChange("sample_rank_first"),
                    feedChange("sample_rank_second"),
                    feedRankSnapshotChange(rows, snapshotAt),
                ),
                "cursor-ranks",
            )

        buildMirror(engine).syncOnce()

        assertEquals(2, db.feedRankDao().count())
        assertEquals(snapshotAt, db.feedRankDao().currentSnapshotAt())
        assertTrue(db.androidSyncDao().headIds("feed_rank").isEmpty())
        assertEquals(
            listOf("main"),
            db.androidSyncDao().headIds("feed_rank_snapshot"),
        )
    }

    @Test
    fun resetBootstrapSweepsAbsentOwnersAndRestoresPendingState() = runBlocking {
        db.androidSyncDao().upsertSyncState(changesState("old-cursor"))
        db.feedItemDao().upsert(FeedItemEntity(tweetId = "sample_deleted_post"))
        db.feedItemDao()
            .upsert(
                FeedItemEntity(
                    tweetId = "sample_existing_post",
                    contentHash = "hash-sample_existing_post",
                )
            )
        db.androidSyncDao().upsertHead(AndroidSyncHeadEntity("feed", "sample_deleted_post", "feed", nowMs))
        db.androidSyncDao().upsertHead(AndroidSyncHeadEntity("feed", "sample_existing_post", "feed", nowMs))
        db.feedRankDao().upsert(listOf(FeedRankEntity("sample_existing_post", 1, nowMs)))
        db.androidSyncDao()
            .upsertHead(AndroidSyncHeadEntity("feed_rank", "sample_existing_post", "feed", nowMs))
        db.retweetSourceDao()
            .upsert(
                listOf(
                    RetweetSourceEntity(
                        contentHash = "hash-sample_existing_post",
                        retweeterChannelId = "sample_reposter",
                        tweetId = "sample_existing_post",
                        publishedAt = nowMs,
                    )
                )
            )
        db.androidSyncDao()
            .upsertHead(
                AndroidSyncHeadEntity(
                    "retweet_sources",
                    "hash-sample_existing_post",
                    "feed",
                    nowMs,
                )
            )
        db.feedLikeDao().upsert(FeedLikeEntity("sample_existing_post", nowMs))
        db.feedLikeDao().upsert(FeedLikeEntity("sample_rejected_post", nowMs))
        val cachedAssetFile = temporaryFolder.newFile("sample_cached_asset.jpg").apply {
            writeBytes(byteArrayOf(1))
        }
        db.androidSyncDao()
            .upsertAsset(
                readyAsset("sample_uncached_asset")
                    .copy(ownerId = "sample_existing_post")
            )
        db.androidSyncDao()
            .upsertAsset(
                readyAsset("sample_cached_asset")
                    .copy(
                        ownerId = "sample_existing_post",
                        localPath = cachedAssetFile.absolutePath,
                        verifiedAtMs = nowMs,
                    )
            )
        db.androidSyncDao()
            .upsertHead(
                AndroidSyncHeadEntity("asset", "sample_uncached_asset", "feed", nowMs)
            )
        db.androidSyncDao()
            .upsertHead(AndroidSyncHeadEntity("asset", "sample_cached_asset", "feed", nowMs))
        db.momentsCursorDao()
            .upsert(MomentsCursorEntity("stories", "sample_story", updatedAtMs = nowMs))
        db.androidSyncDao()
            .upsertHead(AndroidSyncHeadEntity("moments_cursor", "stories", "story", nowMs))
        db.channelSettingDao()
            .upsert(
                ChannelSettingEntity(
                    channelId = "sample_channel",
                    mediaOnly = 1,
                    maxVideos = 7,
                    updatedAt = nowMs,
                )
            )
        db.outboxDao().insert(
            OutboxEntity(
                kind = OutboxKind.CODE_LIKE,
                itemId = "sample_existing_post",
                payloadJson = """{"tweet_id":"sample_existing_post","action":"set","updated_at_ms":$nowMs}""",
                createdAtMs = nowMs,
            )
        )
        db.outboxDao().insert(
            OutboxEntity(
                kind = OutboxKind.CODE_CHANNEL_SETTING,
                itemId = "sample_channel",
                field = "max_videos",
                payloadJson =
                    """{"channel_id":"sample_channel","field":"max_videos","value":7,"updated_at_ms":$nowMs}""",
                createdAtMs = nowMs,
            )
        )
        var resetSent = false
        val engine = MockEngine { request ->
            when (request.url.encodedPath) {
                "/api/android/sync/changes" -> {
                    if (!resetSent) {
                        resetSent = true
                        respond(
                            """{"ok":false,"error_code":"sync_reset_required","error_message":"reset"}""",
                            HttpStatusCode.Conflict,
                            jsonHeaders(),
                        )
                    } else {
                        respondJson(page(emptyList(), "cursor-reset"))
                    }
                }
                "/api/android/sync/bootstrap" ->
                    respondJson(
                        page(
                            listOf(
                                feedChange("sample_existing_post"),
                                channelSettingChange("sample_channel", mediaOnly = 0, maxVideos = 5),
                            ),
                            "cursor-reset",
                        )
                    )
                "/api/android/sync/health" -> respondOk()
                else -> error("Unexpected request ${request.url}")
            }
        }

        buildMirror(engine).syncOnce()

        assertNull(db.feedItemDao().getById("sample_deleted_post"))
        assertNotNull(db.feedItemDao().getById("sample_existing_post"))
        assertEquals(0, db.feedRankDao().count())
        assertEquals(0, db.retweetSourceDao().countForContentHash("hash-sample_existing_post"))
        assertTrue(db.feedLikeDao().exists("sample_existing_post"))
        assertFalse(db.feedLikeDao().exists("sample_rejected_post"))
        assertNull(db.androidSyncDao().asset("sample_uncached_asset"))
        assertEquals(
            cachedAssetFile.absolutePath,
            db.androidSyncDao().asset("sample_cached_asset")?.localPath,
        )
        assertTrue(cachedAssetFile.exists())
        assertEquals("sample_story", db.momentsCursorDao().get("stories")?.videoId)
        assertFalse(db.androidSyncDao().headIds("moments_cursor").contains("stories"))
        assertEquals(0, db.channelSettingDao().getById("sample_channel")?.mediaOnly)
        assertEquals(7, db.channelSettingDao().getById("sample_channel")?.maxVideos)
        assertEquals(2, db.outboxDao().countByState("pending"))
    }

    @Test
    fun ordinaryPruneDropsLegacyStoriesHeadWithoutDeletingLocalCursor() = runBlocking {
        db.momentsCursorDao()
            .upsert(MomentsCursorEntity("stories", "sample_story", updatedAtMs = nowMs))
        db.androidSyncDao()
            .upsertHead(AndroidSyncHeadEntity("moments_cursor", "stories", "story", 0L))
        val engine = MockEngine { request -> error("Unexpected request ${request.url}") }

        buildMirror(engine).prune()

        assertEquals("sample_story", db.momentsCursorDao().get("stories")?.videoId)
        assertFalse(db.androidSyncDao().headIds("moments_cursor").contains("stories"))
    }

    @Test
    fun savedRootKeepsOldGhostThreadContextUntilUnsaved() = runBlocking {
        val oldTime = nowMs - 100L * DAY_MS
        db.androidSyncDao().upsertSyncState(changesState("context-cursor"))
        val rows = listOf(
            FeedItemEntity(tweetId = "context_root", publishedAt = oldTime),
            FeedItemEntity(tweetId = "context_reply", replyToStatus = "context_root", isGhost = true, publishedAt = oldTime),
            FeedItemEntity(tweetId = "context_nested", replyToStatus = "context_reply", isGhost = true, publishedAt = oldTime),
            FeedItemEntity(tweetId = "context_quote", quoteTweetId = "context_root", isGhost = true, publishedAt = oldTime),
            FeedItemEntity(tweetId = "ordinary_reply", replyToStatus = "context_root", publishedAt = oldTime),
            FeedItemEntity(tweetId = "ordinary_quote", quoteTweetId = "context_root", publishedAt = oldTime),
        )
        rows.forEach { row ->
            db.feedItemDao().upsert(row)
            db.androidSyncDao().upsertHead(AndroidSyncHeadEntity("feed", row.tweetId, "feed", oldTime))
        }
        db.feedLikeDao().upsert(FeedLikeEntity("context_root", nowMs))
        val image = File(temporaryFolder.newFolder("sync"), "context-image.jpg").apply { writeText("cached image") }
        db.androidSyncDao().upsertAsset(readyAsset("context_asset").copy(
            ownerId = "context_quote", localPath = image.absolutePath, verifiedAtMs = nowMs,
        ))
        db.androidSyncDao().upsertHead(AndroidSyncHeadEntity("asset", "context_asset", "feed", oldTime))
        val mirror = buildMirror(MockEngine { error("Pruning must stay offline") })

        mirror.prune()

        listOf("context_root", "context_reply", "context_nested", "context_quote").forEach {
            assertNotNull(db.feedItemDao().getById(it))
        }
        assertNull(db.feedItemDao().getById("ordinary_reply"))
        assertNull(db.feedItemDao().getById("ordinary_quote"))
        assertTrue(image.exists())
        assertNotNull(db.androidSyncDao().asset("context_asset"))
        db.feedLikeDao().delete("context_root")

        mirror.prune()

        rows.forEach { assertNull(db.feedItemDao().getById(it.tweetId)) }
        assertFalse(image.exists())
        assertNull(db.androidSyncDao().asset("context_asset"))
    }

    @Test
    fun protectedTombstoneBecomesCollectableAfterProtectionIsRemoved() = runBlocking {
        db.androidSyncDao().upsertSyncState(changesState("cursor-a"))
        db.feedItemDao().upsert(FeedItemEntity(tweetId = "sample_bookmark_post"))
        db.feedLikeDao().upsert(FeedLikeEntity("sample_bookmark_post", nowMs))
        db.androidSyncDao().upsertHead(AndroidSyncHeadEntity("feed", "sample_bookmark_post", "feed", nowMs))
        val engine = MockEngine { request ->
            when (request.url.encodedPath) {
                "/api/android/sync/changes" ->
                    respondJson(page(listOf(deleteChange("feed", "sample_bookmark_post")), "cursor-b"))
                "/api/android/sync/health" -> respondOk()
                else -> error("Unexpected request ${request.url}")
            }
        }
        val mirror = buildMirror(engine)

        mirror.syncOnce()

        assertNotNull(db.feedItemDao().getById("sample_bookmark_post"))
        assertTrue(db.androidSyncDao().headIds("feed").isEmpty())

        db.feedLikeDao().delete("sample_bookmark_post")
        mirror.prune()

        assertNull(db.feedItemDao().getById("sample_bookmark_post"))
    }

    @Test
    fun rootEffectiveRecencyExpiresOldFeedButKeepsFullYoutubeMetadata() = runBlocking {
        val recentRoot = nowMs
        val oldPublished = nowMs - 100L * DAY_MS
        val changes =
            listOf(
                feedChange("sample_ancestor", publishedAt = oldPublished, retainAt = recentRoot),
                feedChange(
                    "sample_reply",
                    replyTo = "sample_ancestor",
                    publishedAt = recentRoot,
                    retainAt = recentRoot,
                ),
                videoChange("sample_reposted_video", oldPublished, recentRoot),
            )
        val engine = bootstrapEngine(changes, "cursor-retained")
        val mirror = buildMirror(engine)

        mirror.syncOnce()

        assertNotNull(db.feedItemDao().getById("sample_ancestor"))
        assertNotNull(db.videoDao().getById("sample_reposted_video"))
        val contexts = db.feedReadDao().getThreadContexts(listOf("sample_reply"))
        assertEquals(listOf("sample_ancestor"), contexts.map { it.ancestorTweetId })

        nowMs += 8L * DAY_MS
        mirror.prune()

        assertNull(db.feedItemDao().getById("sample_ancestor"))
        assertNull(db.feedItemDao().getById("sample_reply"))
        // The full YouTube mirror retains old video metadata. Only its primary binary is
        // eligible for retention pruning, which is covered by AndroidVideoBinaryRetentionTest.
        assertNotNull(db.videoDao().getById("sample_reposted_video"))
    }

    @Test
    fun selectionChangeFinishesChangeStreamBeforeBootstrap() = runBlocking {
        db.androidSyncDao().upsertSyncState(changesState("cursor-a"))
        db.feedSeenDao().upsert(FeedSeenEntity("sample_post", nowMs))
        val order = mutableListOf<String>()
        val engine = MockEngine { request ->
            when (request.url.encodedPath) {
                "/api/android/sync/changes" -> {
                    val after = request.url.parameters["after"]
                    order += "changes:$after"
                    when (after) {
                        "cursor-a" ->
                            respondJson(
                                page(
                                    listOf(channelFollowChange("sample_channel"), channelChange("sample_channel")),
                                    "cursor-b",
                                    end = false,
                                )
                            )
                        "cursor-b" ->
                            respondJson(
                                page(
                                    listOf(deleteChange("feed_seen", "sample_post")),
                                    "cursor-c",
                                )
                            )
                        "cursor-d" -> respondJson(page(emptyList(), "cursor-d"))
                        else -> error("Unexpected changes cursor $after")
                    }
                }
                "/api/android/sync/bootstrap" -> {
                    order += "bootstrap:${request.url.parameters["after"]}"
                    respondJson(
                        page(
                            listOf(channelFollowChange("sample_channel"), channelChange("sample_channel")),
                            "cursor-d",
                        )
                    )
                }
                "/api/android/sync/health" -> respondOk()
                else -> error("Unexpected request ${request.url}")
            }
        }

        buildMirror(engine).syncOnce()

        assertEquals(
            listOf("changes:cursor-a", "changes:cursor-b", "bootstrap:null", "changes:cursor-d"),
            order,
        )
        assertNull(db.feedSeenDao().getById("sample_post"))
        assertTrue(db.channelFollowDao().exists("sample_channel"))
        assertNotNull(db.channelDao().getById("sample_channel"))
    }

    @Test
    fun narrowingRepostSettingDoesNotBootstrap() = runBlocking {
        db.androidSyncDao().upsertSyncState(changesState("cursor-a"))
        val requests = mutableListOf<String>()
        val engine = MockEngine { request ->
            when (request.url.encodedPath) {
                "/api/android/sync/changes" -> {
                    requests += "changes:${request.url.parameters["after"]}"
                    respondJson(
                        page(
                            listOf(
                                channelSettingChange(
                                    "sample_channel",
                                    mediaOnly = 0,
                                    maxVideos = 5,
                                    includeReposts = 0,
                                    includeMemberOnly = 1,
                                )
                            ),
                            "cursor-b",
                        )
                    )
                }
                "/api/android/sync/health" -> respondOk()
                else -> error("Unexpected request ${request.url}")
            }
        }

        buildMirror(engine).syncOnce()

        assertEquals(listOf("changes:cursor-a"), requests)
        assertEquals(0, db.channelSettingDao().getById("sample_channel")?.includeReposts)
        assertEquals(1, db.channelSettingDao().getById("sample_channel")?.includeMemberOnly)
        assertFalse(requireNotNull(db.androidSyncDao().syncState()).bootstrapRequired)
    }

    @Test
    fun rejectedMutationReconcilesItsOwnerBeforeDeletingTheOutboxRow() = runBlocking {
        db.momentsCursorDao()
            .upsert(MomentsCursorEntity("all", "sample_local", positionMs = 10, updatedAtMs = 100))
        val rowId =
            db.outboxDao()
                .insert(
                    OutboxEntity(
                        kind = OutboxKind.CODE_MOMENTS_CURSOR,
                        itemId = "all",
                        payloadJson = "{}",
                        state = "pending",
                        createdAtMs = 100,
                    )
                )
        val authoritative =
            upsertChange(
                ownerKind = "moments_cursor",
                ownerId = "all",
                payload =
                    buildJsonObject {
                        put("scope", "all")
                        put("video_id", "sample_server")
                        put("position_ms", 20)
                        put("sort_at_ms", 200)
                        put("updated_at_ms", 200)
                    },
            )
        val engine =
            MockEngine { request ->
                when (request.url.encodedPath) {
                    "/api/android/sync/reconcile" ->
                        respondJson(AndroidSyncReconcileResponse(listOf(authoritative)))
                    else -> error("Unexpected request ${request.url}")
                }
            }

        buildMirror(engine)
            .reconcileRejected(
                listOf(
                    OutboxRejectedMutation(
                        rowId = rowId,
                        ownerKind = "moments_cursor",
                        ownerId = "all",
                    )
                )
            )

        assertEquals(0, db.outboxDao().countByState("pending"))
        val cursor = db.momentsCursorDao().get("all")
        assertEquals("sample_server", cursor?.videoId)
        assertEquals(200L, cursor?.updatedAtMs)
    }

    private fun buildMirror(engine: MockEngine): AndroidSyncMirror {
        if (::client.isInitialized) client.close()
        client =
            HttpClient(engine) {
                expectSuccess = false
                install(ContentNegotiation) { json(iglooJson) }
            }
        val reachability =
            Reachability(
                scope = scope,
                probe = { true },
                foregroundFlow = MutableSharedFlow(extraBufferCapacity = 1),
            )
        val baseUrlProvider = ServerBaseUrlProvider { BASE_URL }
        return AndroidSyncMirror(
            db = db,
            dao = db.androidSyncDao(),
            api = AndroidSyncApi(client, baseUrlProvider::baseUrl),
            client = client,
            baseUrlProvider = baseUrlProvider,
            reachability = reachability,
            foregroundPromoter =
                ForegroundPromoter(
                    context = ApplicationProvider.getApplicationContext(),
                    logger = logger,
                    startForegroundService = {},
                    stopForegroundService = {},
                ),
            mediaRoot = temporaryFolder.root,
            logger = logger,
            retentionProvider = { RETENTION },
            serverNowMsProvider = { nowMs },
            metadataRetryDelaysMs = emptyList(),
        )
    }

    private fun buildAssetDrainer(
        engine: MockEngine,
        nowMsProvider: () -> Long,
    ): AndroidSyncAssetDrainer {
        if (::client.isInitialized) client.close()
        client =
            HttpClient(engine) {
                expectSuccess = false
                install(ContentNegotiation) { json(iglooJson) }
            }
        val reachability =
            Reachability(
                scope = scope,
                probe = { true },
                foregroundFlow = MutableSharedFlow(extraBufferCapacity = 1),
            )
        return AndroidSyncAssetDrainer(
            dao = db.androidSyncDao(),
            client = client,
            baseUrlProvider = ServerBaseUrlProvider { BASE_URL },
            reachability = reachability,
            foregroundPromoter =
                ForegroundPromoter(
                    context = ApplicationProvider.getApplicationContext(),
                    logger = logger,
                    startForegroundService = {},
                    stopForegroundService = {},
                ),
            mediaRoot = temporaryFolder.root,
            logger = logger,
            nowMsProvider = nowMsProvider,
        )
    }

    private fun readyAsset(
        id: String,
        sizeBytes: Long = 1,
    ) =
        AndroidSyncAssetEntity(
            assetId = id,
            assetKind = "post_media",
            ownerId = "sample_post",
            ownerKind = "tweet",
            bucket = "feed",
            contentType = "image/jpeg",
            sizeBytes = sizeBytes,
            revision = 1,
        )

    private fun bootstrapEngine(changes: List<AndroidSyncChangeDto>, cursor: String) =
        MockEngine { request ->
            when (request.url.encodedPath) {
                "/api/android/sync/bootstrap" -> respondJson(page(changes, cursor))
                "/api/android/sync/changes" -> respondJson(page(emptyList(), cursor))
                "/api/android/sync/health" -> respondOk()
                else -> error("Unexpected request ${request.url}")
            }
        }

    private fun changesState(cursor: String) =
        AndroidSyncStateEntity(
            mode = "changes",
            cursor = cursor,
            feedDays = RETENTION.feedDays,
            youtubeDays = RETENTION.youtubeDays,
            momentsDays = RETENTION.momentsDays,
            storyHours = RETENTION.storyHours,
        )

    private fun page(
        changes: List<AndroidSyncChangeDto>,
        cursor: String,
        end: Boolean = true,
    ) = AndroidSyncPageResponse(changes, cursor, end)

    private fun feedChange(
        id: String,
        channelId: String = "",
        replyTo: String = "",
        publishedAt: Long = nowMs,
        retainAt: Long = nowMs,
    ) =
        upsertChange(
            ownerKind = "feed",
            ownerId = id,
            retentionBucket = "feed",
            retainAt = retainAt,
            payload = buildJsonObject { put("item", feedItemPayload(id, channelId, replyTo, publishedAt)) },
        )

    private fun feedRankSnapshotChange(
        rows: List<FeedRankEntity>,
        snapshotAt: Long,
    ) =
        upsertChange(
            ownerKind = "feed_rank_snapshot",
            ownerId = "main",
            payload =
                buildJsonObject {
                    put("snapshot_at", snapshotAt)
                    put("digest", feedRankSnapshotDigest(rows))
                    put(
                        "rows",
                        buildJsonArray {
                            rows.forEach { row ->
                                add(
                                    buildJsonObject {
                                        put("tweet_id", row.tweetId)
                                        put("rank_position", row.rankPosition)
                                    }
                                )
                            }
                        },
                    )
                },
        )

    private fun feedItemPayload(
        id: String,
        channelId: String,
        replyTo: String,
        publishedAt: Long,
    ) = buildJsonObject {
        put("tweet_id", id)
        put("source_channel_id", channelId)
        put("body_text", "body")
        put("lang", "en")
        put("is_retweet", false)
        put("reposter_channel_id", "")
        put("quote_tweet_id", "")
        put("quote_channel_id", "")
        put("quote_body_text", "")
        put("quote_lang", "")
        put("quote_media_json", "")
        put("quote_published_at", 0)
        put("quote_canonical_url", "")
        put("media_json", "")
        put("views", 0)
        put("likes", 0)
        put("retweets", 0)
        put("canonical_url", "")
        put("canonical_tweet_id", "")
        put("reply_channel_id", "")
        put("reply_to_status", replyTo)
        put("is_reply", replyTo.isNotEmpty())
        put("is_ghost", false)
        put("content_hash", "hash-$id")
        put("body_translation", "")
        put("body_source_lang", "")
        put("quote_translation", "")
        put("quote_source_lang", "")
        put("published_at", publishedAt)
        put("channel_id", channelId)
    }

    private fun videoChange(
        id: String,
        publishedAt: Long,
        retainAt: Long,
        reposterChannelId: String? = "sample_reposter",
        commentAuthorIds: List<String> = emptyList(),
    ) =
        upsertChange(
            ownerKind = "video",
            ownerId = id,
            retentionBucket = "youtube",
            retainAt = retainAt,
            payload = buildJsonObject {
                put(
                    "item",
                    buildJsonObject {
                        put("video_id", id)
                        put("channel_id", "youtube_UCsample_channel")
                        put("owner_kind", "youtube_video")
                        put("title", "title")
                        put("description", "")
                        put("duration", 60)
                        put("published_at", publishedAt)
                        put("media_kind", "video")
                        put("slide_count", 0)
                        put("source_kind", "")
                        put("metadata_json", "")
                        put("canonical_url", "")
                        put("dearrow_title", JsonNull)
                        put("dearrow_title_casual", JsonNull)
                    },
                )
                put(
                    "comments",
                    kotlinx.serialization.json.JsonArray(
                        commentAuthorIds.mapIndexed { index, authorId ->
                            buildJsonObject {
                                put("id", "sample_comment_$index")
                                put("parent", "")
                                put("author", "Sample Author")
                                put("author_id", authorId)
                                put("text", "Sample comment")
                                put("like_count", 0)
                                put("published_at", publishedAt)
                            }
                        }
                    ),
                )
                put("sponsorblock_segments", kotlinx.serialization.json.JsonArray(emptyList()))
                put("sponsorblock_checked", JsonNull)
                put(
                    "repost_sources",
                    kotlinx.serialization.json.JsonArray(
                        reposterChannelId?.let { channelId ->
                            listOf(
                                buildJsonObject {
                                    put("reposter_channel_id", channelId)
                                    put("reposted_at_ms", retainAt)
                                    put("first_seen_at_ms", retainAt)
                                    put("updated_at_ms", retainAt)
                                }
                            )
                        } ?: emptyList()
                    ),
                )
            },
        )

    private fun channelChange(id: String) =
        upsertChange(
            ownerKind = "channel",
            ownerId = id,
            payload = buildJsonObject {
                put(
                    "channel",
                    buildJsonObject {
                        put("channel_id", id)
                        put("source_id", id)
                        put("name", id)
                        put("url", "")
                        put("platform", "twitter")
                    },
                )
                put("profile", JsonNull)
            },
        )

    private fun profileOnlyChannelChange(id: String) =
        upsertChange(
            ownerKind = "channel",
            ownerId = id,
            payload = buildJsonObject {
                put("channel", JsonNull)
                put(
                    "profile",
                    buildJsonObject {
                        put("channel_id", id)
                        put("platform", "twitter")
                        put("handle", id)
                        put("display_name", "Sample Profile")
                        put("bio", "")
                        put("website", "")
                        put("followers", 0)
                        put("following", 0)
                        put("verified", false)
                        put("verified_type", "")
                        put("protected", false)
                    },
                )
            },
        )

    private fun retweetSourcesChange(contentHash: String, channelId: String) =
        upsertChange(
            ownerKind = "retweet_sources",
            ownerId = contentHash,
            payload = buildJsonObject {
                put(
                    "rows",
                    kotlinx.serialization.json.JsonArray(
                        listOf(
                            buildJsonObject {
                                put("content_hash", contentHash)
                                put("retweeter_channel_id", channelId)
                                put("tweet_id", "sample_retweet")
                                put("published_at", nowMs)
                            }
                        )
                    ),
                )
            },
        )

    private fun assetChange(asset: AndroidSyncAssetDto) =
        upsertChange(
            ownerKind = "asset",
            ownerId = asset.asset_id,
            retentionBucket = "feed",
            retainAt = nowMs,
            payload = iglooJson.encodeToJsonElement(asset).jsonObject,
        )

    private fun missingAsset(
        id: String,
        ownerId: String,
        ownerKind: String = "tweet",
        assetKind: String = "post_media",
        bucket: String = "feed",
    ) =
        AndroidSyncAssetDto(
            asset_id = id,
            asset_kind = assetKind,
            media_index = 0,
            owner_id = ownerId,
            owner_kind = ownerKind,
            bucket = bucket,
            content_type = "",
            size_bytes = 0,
            revision = 1,
            state = "server_missing",
            is_auto = null,
        )

    private fun feedLikeChange(id: String) =
        upsertChange(
            ownerKind = "feed_like",
            ownerId = id,
            payload = buildJsonObject {
                put("tweet_id", id)
                put("liked_at", nowMs)
            },
        )

    private fun channelFollowChange(id: String) =
        upsertChange(
            ownerKind = "channel_follow",
            ownerId = id,
            payload = buildJsonObject {
                put("channel_id", id)
                put("followed_at", nowMs)
            },
        )

    private fun channelSettingChange(
        id: String,
        mediaOnly: Int,
        maxVideos: Int,
        includeReposts: Int? = null,
        includeMemberOnly: Int? = null,
    ) =
        upsertChange(
            ownerKind = "channel_setting",
            ownerId = id,
            payload = buildJsonObject {
                put("channel_id", id)
                put("media_only", mediaOnly)
                put("max_videos", maxVideos)
                includeReposts?.let { put("include_reposts", it) }
                includeMemberOnly?.let { put("include_member_only", it) }
                put("updated_at", nowMs)
            },
        )

    private fun upsertChange(
        ownerKind: String,
        ownerId: String,
        retentionBucket: String = "",
        retainAt: Long = 0,
        payload: JsonObject,
    ) = AndroidSyncChangeDto(ownerKind, ownerId, "upsert", retentionBucket, retainAt, payload)

    private fun deleteChange(ownerKind: String, ownerId: String) =
        AndroidSyncChangeDto(ownerKind, ownerId, "delete", "", 0, null)

    private fun assertRetentionQuery(parameters: Map<String, String>) {
        assertEquals(RETENTION.feedDays.toString(), parameters["feed_days"])
        assertEquals(RETENTION.youtubeDays.toString(), parameters["youtube_days"])
        assertEquals(RETENTION.momentsDays.toString(), parameters["moments_days"])
        assertEquals(RETENTION.storyHours.toString(), parameters["story_hours"])
    }

    private inline fun <reified T> MockRequestHandleScope.respondJson(body: T) =
        respond(iglooJson.encodeToString(body), HttpStatusCode.OK, jsonHeaders())

    private fun MockRequestHandleScope.respondOk() = respond("{}", HttpStatusCode.OK, jsonHeaders())

    private fun jsonHeaders() = headersOf("Content-Type", ContentType.Application.Json.toString())

    private companion object {
        const val BASE_URL = "http://example.local"
        const val DAY_MS = 86_400_000L
        val RETENTION = AndroidSyncRetentionRequest(7, 7, 7, 48)
    }
}
