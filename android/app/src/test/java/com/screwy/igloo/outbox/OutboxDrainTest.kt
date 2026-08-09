package com.screwy.igloo.outbox

import com.screwy.igloo.data.IglooDatabase
import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.data.RoomTestSupport
import com.screwy.igloo.data.entity.AndroidSyncStateEntity
import com.screwy.igloo.data.entity.FeedLikeEntity
import com.screwy.igloo.data.entity.OutboxEntity
import com.screwy.igloo.log.InMemoryLogSink
import com.screwy.igloo.log.Logger
import com.screwy.igloo.net.IglooHostProvider
import com.screwy.igloo.net.OutboxApi
import com.screwy.igloo.net.Reachability
import com.screwy.igloo.net.auth.NoAuthTokenProvider
import com.screwy.igloo.net.buildIglooClient
import com.screwy.igloo.ui.UiEffects
import io.ktor.client.HttpClient
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.MockRequestHandleScope
import io.ktor.client.engine.mock.respond
import io.ktor.http.HttpStatusCode
import io.ktor.http.headersOf
import io.ktor.utils.io.ByteReadChannel
import java.util.concurrent.atomic.AtomicInteger
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], manifest = Config.NONE)
class OutboxDrainTest {
    private lateinit var db: IglooDatabase
    private lateinit var scope: CoroutineScope
    private lateinit var prefs: PreferencesRepo
    private lateinit var logger: Logger
    private val clients = mutableListOf<HttpClient>()
    private var nowMs = 10_000L

    @Before
    fun setUp() {
        db = RoomTestSupport.freshDb()
        scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        prefs = PreferencesRepo(db.preferenceDao(), scope, nowMsProvider = { nowMs })
        logger =
            Logger(
                prefs = prefs,
                sink = InMemoryLogSink(),
                scope = scope,
                nowMsProvider = { nowMs },
            )
    }

    @After
    fun tearDown() {
        clients.forEach(HttpClient::close)
        RoomTestSupport.closeAfterScope(scope, db)
    }

    @Test
    fun acknowledgedSetDeletesOnlyOutboxRow() = runBlocking {
        db.feedLikeDao().upsert(FeedLikeEntity("item-1", nowMs))
        db.outboxDao().insert(pendingLike("item-1", "set"))

        val result = buildDrain(MockEngine { respondOk() }).runOnce()

        assertTrue(result.rejectedMutations.isEmpty())
        assertEquals(0, db.outboxDao().countByState("pending"))
        assertTrue(db.feedLikeDao().exists("item-1"))
    }

    @Test
    fun acknowledgedClearFinalizesCanonicalDelete() = runBlocking {
        db.feedLikeDao().upsert(FeedLikeEntity("item-1", nowMs))
        db.outboxDao().insert(pendingLike("item-1", "clear"))

        val result = buildDrain(MockEngine { respondOk() }).runOnce()

        assertTrue(result.protectionChanged)
        assertFalse(db.feedLikeDao().exists("item-1"))
        assertEquals(0, db.outboxDao().countByState("pending"))
    }

    @Test
    fun acknowledgedSelectionWideningPromotesItsDurableReplayMarker() = runBlocking {
        db.androidSyncDao().upsertSyncState(changesState())
        db.outboxDao().insert(pendingSelectionWidening())

        val result = buildDrain(MockEngine { respondOk() }).runOnce()

        assertTrue(result.selectionExpanded)
        assertTrue(requireNotNull(db.androidSyncDao().syncState()).bootstrapRequired)
        assertTrue(db.outboxDao().pendingRows().isEmpty())
    }

    @Test
    fun retryKeepsSelectionWideningPendingWithoutStartingReplay() = runBlocking {
        db.androidSyncDao().upsertSyncState(changesState())
        db.outboxDao().insert(pendingSelectionWidening())

        val result =
            buildDrain(
                    MockEngine {
                        respond(
                            ByteReadChannel("""{"error_code":"busy"}"""),
                            HttpStatusCode.ServiceUnavailable,
                            jsonHeaders(),
                        )
                    }
                )
                .runOnce()

        assertFalse(result.selectionExpanded)
        assertFalse(requireNotNull(db.androidSyncDao().syncState()).bootstrapRequired)
        assertTrue(db.outboxDao().pendingRows().single().selectionWidening())
    }

    @Test
    fun rejectedSelectionWideningDoesNotStartReplay() = runBlocking {
        db.androidSyncDao().upsertSyncState(changesState())
        db.outboxDao().insert(pendingSelectionWidening())

        val result =
            buildDrain(
                    MockEngine {
                        respond(
                            ByteReadChannel("""{"error_code":"bad_target"}"""),
                            HttpStatusCode.BadRequest,
                            jsonHeaders(),
                        )
                    }
                )
                .runOnce()

        assertFalse(result.selectionExpanded)
        assertFalse(requireNotNull(db.androidSyncDao().syncState()).bootstrapRequired)
        assertEquals("channel_follow", result.rejectedMutations.single().ownerKind)
    }

    @Test
    fun supersededSelectionAckUsesTheCurrentCoalescedIntent() = runBlocking {
        db.androidSyncDao().upsertSyncState(changesState())
        db.outboxDao().insert(pendingSelectionWidening())
        val drain =
            buildDrain(
                MockEngine {
                    db.outboxDao()
                        .coalesceAndInsert(
                            OutboxEntity(
                                kind = OutboxKind.CODE_FOLLOW,
                                itemId = "new_channel",
                                payloadJson =
                                    """{"channel_id":"new_channel","action":"clear","updated_at_ms":$nowMs,"selection_baseline":0}""",
                                createdAtMs = nowMs + 1L,
                            )
                        )
                    respondOk()
                }
            )

        val result = drain.runOnce()

        assertFalse(result.selectionExpanded)
        assertFalse(requireNotNull(db.androidSyncDao().syncState()).bootstrapRequired)
        val current = db.outboxDao().pendingRows().single()
        assertTrue(current.payloadJson.contains("\"action\":\"clear\""))
    }

    @Test
    fun rejectedClearPreservesCanonicalState() = runBlocking {
        db.feedLikeDao().upsert(FeedLikeEntity("item-1", nowMs))
        db.outboxDao().insert(pendingLike("item-1", "clear"))
        val drain =
            buildDrain(
                MockEngine {
                    respond(
                        ByteReadChannel("""{"error_code":"bad_target"}"""),
                        HttpStatusCode.BadRequest,
                        jsonHeaders(),
                    )
                }
            )

        val result = drain.runOnce()

        assertEquals(1, result.rejectedMutations.size)
        assertEquals("feed_like", result.rejectedMutations.single().ownerKind)
        assertTrue(db.feedLikeDao().exists("item-1"))
        assertEquals(1, db.outboxDao().countByState("pending"))
    }

    @Test
    fun rejectedSetRequestsOwnerReconciliation() = runBlocking {
        db.feedLikeDao().upsert(FeedLikeEntity("item-1", nowMs))
        db.outboxDao().insert(pendingLike("item-1", "set"))
        val drain =
            buildDrain(
                MockEngine {
                    respond(
                        ByteReadChannel("""{"error_code":"bad_target"}"""),
                        HttpStatusCode.BadRequest,
                        jsonHeaders(),
                    )
                }
            )

        val result = drain.runOnce()

        assertEquals(1, result.rejectedMutations.size)
        assertEquals("feed_like", result.rejectedMutations.single().ownerKind)
        assertTrue(db.feedLikeDao().exists("item-1"))
        assertEquals(1, db.outboxDao().countByState("pending"))
    }

    @Test
    fun staleMutationKeepsQueueRowUntilOwnerReconciliation() = runBlocking {
        db.feedLikeDao().upsert(FeedLikeEntity("item-1", nowMs))
        db.outboxDao().insert(pendingLike("item-1", "clear"))
        val drain =
            buildDrain(
                MockEngine {
                    respond(
                        ByteReadChannel(
                            """{"ok":false,"error_code":"stale_mutation","error_message":"newer state"}"""
                        ),
                        HttpStatusCode.Conflict,
                        jsonHeaders(),
                    )
                }
            )

        val result = drain.runOnce()

        assertEquals(1, result.rejectedMutations.size)
        assertEquals("feed_like", result.rejectedMutations.single().ownerKind)
        assertTrue(db.feedLikeDao().exists("item-1"))
        assertEquals(1, db.outboxDao().countByState("pending"))
    }

    @Test
    fun authRefreshStopsPassWithoutReclaimingSameRows() = runBlocking {
        val calls = AtomicInteger()
        db.outboxDao().insert(pendingLike("item-1", "set"))
        db.outboxDao().insert(pendingLike("item-2", "set"))
        val drain =
            buildDrain(
                MockEngine {
                    calls.incrementAndGet()
                    respond(
                        ByteReadChannel("""{"error_code":"access_token_expired"}"""),
                        HttpStatusCode.Unauthorized,
                        jsonHeaders(),
                    )
                }
            )

        drain.runOnce()

        assertEquals(1, calls.get())
        assertEquals(2, db.outboxDao().countByState("pending"))
    }

    @Test
    fun seenRowsShareOneHttpCallAndOneBatchDelete() = runBlocking {
        val calls = AtomicInteger()
        repeat(3) { index -> db.outboxDao().insert(pendingSeen("item-$index", index.toLong())) }
        val drain =
            buildDrain(
                MockEngine {
                    calls.incrementAndGet()
                    respondOk()
                }
            )

        drain.runOnce()

        assertEquals(1, calls.get())
        assertEquals(0, db.outboxDao().countByState("pending"))
    }

    @Test
    fun logBatchIsAcknowledgedWithoutCreatingAnotherPass() = runBlocking {
        repeat(3) { index ->
            db.outboxDao()
                .insert(
                    OutboxEntity(
                        kind = OutboxKind.CODE_LOG,
                        payloadJson = """{"level":"info","event":"sample","timestamp_ms":$index}""",
                        state = "pending",
                        createdAtMs = index.toLong(),
                    )
                )
        }
        val drain = buildDrain(MockEngine { respondOk() })

        drain.runOnce()

        assertEquals(0, db.outboxDao().countByState("pending"))
        assertEquals(3, db.outboxDao().countByState("acked"))
    }

    @Test
    fun interactiveRowsDispatchBeforeOlderPassiveStateAndLogs() = runBlocking {
        val paths = mutableListOf<String>()
        db.outboxDao().insert(pendingSeen("item-passive", 1L))
        db.outboxDao()
            .insert(
                OutboxEntity(
                    kind = OutboxKind.CODE_LOG,
                    payloadJson =
                        """{"level":"info","event":"sample","timestamp_ms":2}""",
                    state = "pending",
                    createdAtMs = 2L,
                )
            )
        db.outboxDao().insert(pendingLike("item-action", "set").copy(createdAtMs = 3L))
        val drain =
            buildDrain(
                MockEngine { request ->
                    paths += request.url.encodedPath
                    respondOk()
                }
            )

        drain.runOnce()

        assertEquals(
            listOf(
                "/api/mutations/like",
                "/api/mutations/seen",
                "/api/logs/server",
            ),
            paths,
        )
    }

    @Test
    fun retryResultReportsTheNextOwnedAttempt() = runBlocking {
        db.outboxDao().insert(pendingLike("item-1", "set"))
        val drain =
            buildDrain(
                MockEngine {
                    respond(
                        ByteReadChannel("""{"error_code":"busy"}"""),
                        HttpStatusCode.ServiceUnavailable,
                        jsonHeaders(),
                    )
                }
            )

        val result = drain.runOnce()

        assertEquals(nowMs + 30_000L, result.nextAttemptAtMs)
        assertEquals(nowMs + 30_000L, db.outboxDao().pendingRows().single().nextAttemptAtMs)
    }

    @Test
    fun offlinePassLeavesQueueUntouched() = runBlocking {
        db.outboxDao().insert(pendingLike("item-1", "set"))
        val drain = buildDrain(MockEngine { error("Unexpected request") }, online = false)

        drain.runOnce()

        assertEquals(1, db.outboxDao().countByState("pending"))
    }

    private fun buildDrain(engine: MockEngine, online: Boolean = true): OutboxDrain {
        val client =
            buildIglooClient(
                    engine = engine,
                    prefsProvider = { prefs },
                    tokenProvider = NoAuthTokenProvider,
                    hostProvider = IglooHostProvider { "igloo.example" },
                    nowMsProvider = { nowMs },
                )
                .also(clients::add)
        val reachability =
            Reachability(scope = scope, probe = { true }, foregroundFlow = flowOf(false)).apply {
                if (online) markOnline() else downgrade()
            }
        return OutboxDrain(
            outboxDao = db.outboxDao(),
            dispatcher = OutboxDispatcher(OutboxApi(client) { BASE_URL }, db, UiEffects()),
            db = db,
            prefs = prefs,
            reachability = reachability,
            logger = logger,
            nowMsProvider = { nowMs },
        )
    }

    private fun pendingLike(itemId: String, action: String) =
        OutboxEntity(
            kind = OutboxKind.CODE_LIKE,
            itemId = itemId,
            payloadJson = """{"tweet_id":"$itemId","action":"$action","updated_at_ms":$nowMs}""",
            state = "pending",
            createdAtMs = nowMs,
        )

    private fun pendingSeen(itemId: String, createdAtMs: Long) =
        OutboxEntity(
            kind = OutboxKind.CODE_SEEN,
            itemId = itemId,
            payloadJson = """{"tweet_id":"$itemId","updated_at_ms":$createdAtMs}""",
            state = "pending",
            createdAtMs = createdAtMs,
        )

    private fun pendingSelectionWidening() =
        OutboxEntity(
            kind = OutboxKind.CODE_FOLLOW,
            itemId = "new_channel",
            payloadJson =
                """{"channel_id":"new_channel","action":"set","updated_at_ms":$nowMs,"selection_widening":true,"selection_baseline":0}""",
            state = "pending",
            createdAtMs = nowMs,
        )

    private fun changesState() =
        AndroidSyncStateEntity(
            mode = "changes",
            cursor = "content-cursor",
            feedDays = 2,
            youtubeDays = 7,
            momentsDays = 3,
            storyHours = 48,
        )

    private fun MockRequestHandleScope.respondOk() =
        respond(ByteReadChannel("{}"), HttpStatusCode.OK, jsonHeaders())

    private fun jsonHeaders() = headersOf("Content-Type", "application/json")

    private companion object {
        const val BASE_URL = "http://igloo.example"
    }
}
