package com.screwy.igloo.moments

import androidx.lifecycle.SavedStateHandle
import com.screwy.igloo.bookmarks.BookmarkFilter
import com.screwy.igloo.bookmarks.bookmarkPlaylistId
import com.screwy.igloo.data.IglooDatabase
import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.data.RoomTestSupport
import com.screwy.igloo.data.entity.BookmarkEntity
import com.screwy.igloo.data.entity.ChannelEntity
import com.screwy.igloo.data.entity.ChannelFollowEntity
import com.screwy.igloo.data.entity.MomentViewEntity
import com.screwy.igloo.data.entity.MomentsCursorEntity
import com.screwy.igloo.data.entity.VideoEntity
import com.screwy.igloo.data.entity.VideoRepostSourceEntity
import com.screwy.igloo.net.ServerBaseUrlProvider
import com.screwy.igloo.outbox.OutboxWriter
import com.screwy.igloo.testutil.ViewModelTestTracker
import com.screwy.igloo.testutil.clearViewModel
import com.screwy.igloo.ui.UiEffects
import com.screwy.igloo.ui.UiState
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.coroutines.yield
import java.util.concurrent.atomic.AtomicInteger
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], manifest = Config.NONE)
class ShortsRouteViewModelTest {

    private lateinit var db: IglooDatabase
    private lateinit var scope: CoroutineScope
    private lateinit var prefs: PreferencesRepo
    private lateinit var writer: OutboxWriter
    private lateinit var uiEffects: UiEffects
    private val viewModels = ViewModelTestTracker()

    @Before fun setUp() {
        Dispatchers.setMain(UnconfinedTestDispatcher())
        db = RoomTestSupport.freshDb()
        scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        prefs = PreferencesRepo(db.preferenceDao(), scope, nowMsProvider = { 0L })
        writer = OutboxWriter(
            db = db,
            prefs = prefs,
            scope = scope,
            nowMsProvider = { 0L },
            writeDebounceMs = 50L,
        )
        uiEffects = UiEffects()
    }

    @After fun tearDown() {
        viewModels.clearAll()
        scope.cancel()
        db.close()
        Dispatchers.resetMain()
    }

    private fun subscribe(vm: ShortsRouteViewModel): Job = scope.launch {
        launch { vm.items.collect {} }
        launch { vm.startIndex.collect {} }
        launch { vm.currentVideoId.collect {} }
        launch { vm.uiState.collect {} }
    }

    private fun restoredHandle(source: SavedStateHandle): SavedStateHandle =
        SavedStateHandle(source.keys().associateWith { key -> source.get<Any?>(key) })

    @Test fun allMomentsItemsExposeSyncedCanonicalUrlWithoutSynthesis() = runBlocking {
        db.channelDao().upsert(ChannelEntity(
            channelId = "tiktok_alice", name = "Alice", platform = "tiktok",
            sourceId = "alice",
        ))
        db.channelFollowDao().upsert(ChannelFollowEntity(channelId = "tiktok_alice"))
        db.videoDao().upsert(VideoEntity(
			videoId = "tiktok_clip_1",
			channelId = "tiktok_alice",
			ownerKind = "tiktok_video",
            title = "Short",
            canonicalUrl = "https://www.tiktok.com/@canonical/video/clip_1",
            publishedAt = 1L,
        ))

        val vm = viewModels.track(ShortsRouteViewModel(
            playlistSpec = ShortsPlaylistSpec.allMoments(),
            startVideoId = "tiktok_clip_1",
            db = db,
            outboxWriter = writer,
            prefs = prefs,
            uiEffects = uiEffects,
            baseUrlProvider = ServerBaseUrlProvider { "https://example.test" },
            savedStateHandle = SavedStateHandle(),
        ))
        val sub = subscribe(vm)
        val ok = withTimeoutOrNull(2_000L) {
            while (vm.items.value.isEmpty()) delay(10)
            true
        }
        sub.cancel()

        assertEquals(true, ok)
        assertEquals("https://www.tiktok.com/@canonical/video/clip_1", vm.items.value.single().canonicalUrl)
    }

    @Test fun storyTrayPlaylistWrapsFromSelectedChannel() = runBlocking {
        db.channelDao().upsert(listOf(
            ChannelEntity("tiktok_newer", name = "Newer", platform = "tiktok", sourceId = "newer"),
            ChannelEntity("tiktok_older", name = "Older", platform = "tiktok", sourceId = "older"),
        ))
        db.channelFollowDao().upsert(ChannelFollowEntity(channelId = "tiktok_newer"))
        db.channelFollowDao().upsert(ChannelFollowEntity(channelId = "tiktok_older"))
        val now = System.currentTimeMillis()
        db.videoDao().upsert(listOf(
            VideoEntity("v_newer_first", "tiktok_newer", "tiktok_video", title = "Newer first", publishedAt = now - 2_000L, sourceKind = "story"),
            VideoEntity("v_newer_last", "tiktok_newer", "tiktok_video", title = "Newer last", publishedAt = now - 1_000L, sourceKind = "story"),
            VideoEntity("v_older", "tiktok_older", "tiktok_video", title = "Older story", publishedAt = now - 3_000L, sourceKind = "story"),
        ))

        val vm = viewModels.track(ShortsRouteViewModel(
            playlistSpec = ShortsPlaylistSpec.storyTray(),
            startVideoId = "v_older",
            db = db,
            outboxWriter = writer,
            prefs = prefs,
            uiEffects = uiEffects,
            baseUrlProvider = ServerBaseUrlProvider { "https://example.test" },
            savedStateHandle = SavedStateHandle(),
        ))
        val sub = subscribe(vm)
        val ok = withTimeoutOrNull(2_000L) {
            while (vm.items.value.size < 3) delay(10)
            true
        }
        sub.cancel()

        assertEquals(true, ok)
        assertEquals(listOf("v_older", "v_newer_first", "v_newer_last"), vm.items.value.map { it.videoId })
        assertEquals(0, vm.startIndex.value)
    }

    @Test fun storyTrayPlaylistOrderDoesNotChangeAfterViewingStories() = runBlocking {
        db.channelDao().upsert(listOf(
            ChannelEntity("tiktok_sample_one", name = "Sample One", platform = "tiktok", sourceId = "sample_one"),
            ChannelEntity("tiktok_sample_two", name = "Sample Two", platform = "tiktok", sourceId = "sample_two"),
            ChannelEntity("tiktok_sample_old", name = "Sample Old", platform = "tiktok", sourceId = "sample_old"),
        ))
        db.channelFollowDao().upsert(ChannelFollowEntity(channelId = "tiktok_sample_one"))
        db.channelFollowDao().upsert(ChannelFollowEntity(channelId = "tiktok_sample_two"))
        db.channelFollowDao().upsert(ChannelFollowEntity(channelId = "tiktok_sample_old"))
        val now = System.currentTimeMillis()
        db.videoDao().upsert(listOf(
            VideoEntity("v_sample_one", "tiktok_sample_one", "tiktok_video", title = "Sample One", publishedAt = now - 1_000L, sourceKind = "story"),
            VideoEntity("v_sample_two", "tiktok_sample_two", "tiktok_video", title = "Sample Two", publishedAt = now - 2_000L, sourceKind = "story"),
            VideoEntity("v_sample_old", "tiktok_sample_old", "tiktok_video", title = "Sample Old", publishedAt = now - 3_000L, sourceKind = "story"),
        ))

        val vm = viewModels.track(ShortsRouteViewModel(
            playlistSpec = ShortsPlaylistSpec.storyTray(),
            startVideoId = "v_sample_two",
            db = db,
            outboxWriter = writer,
            prefs = prefs,
            uiEffects = uiEffects,
            baseUrlProvider = ServerBaseUrlProvider { "https://example.test" },
            savedStateHandle = SavedStateHandle(),
        ))
        val sub = subscribe(vm)
        val loaded = withTimeoutOrNull(2_000L) {
            while (vm.items.value.size < 3) delay(10)
            true
        }
        assertEquals(true, loaded)
        assertEquals(listOf("v_sample_two", "v_sample_old", "v_sample_one"), vm.items.value.map { it.videoId })

        db.momentViewDao().upsert(MomentViewEntity("v_sample_two", viewedAt = now))
        delay(250L)
        sub.cancel()

        assertEquals(listOf("v_sample_two", "v_sample_old", "v_sample_one"), vm.items.value.map { it.videoId })
    }

    @Test fun bookmarksPlaylistUsesRouteFilter() = runBlocking {
        db.videoDao().upsert(listOf(
            VideoEntity("art_new", "tiktok_artist", "tiktok_video", title = "Art new", mediaKind = "video", publishedAt = 30L),
            VideoEntity("music_new", "tiktok_artist", "tiktok_video", title = "Music new", mediaKind = "video", publishedAt = 20L),
            VideoEntity("art_old", "tiktok_artist", "tiktok_video", title = "Art old", mediaKind = "video", publishedAt = 10L),
        ))
        db.bookmarkDao().upsert(BookmarkEntity("art_new", categoryId = 34L, customTitle = "art", bookmarkedAt = 300L))
        db.bookmarkDao().upsert(BookmarkEntity("music_new", categoryId = 5L, customTitle = "music", bookmarkedAt = 200L))
        db.bookmarkDao().upsert(BookmarkEntity("art_old", categoryId = 34L, customTitle = "art", bookmarkedAt = 100L))

        val vm = viewModels.track(ShortsRouteViewModel(
            playlistSpec = ShortsPlaylistSpec.bookmarks(bookmarkPlaylistId(BookmarkFilter.Category(34L))),
            startVideoId = "art_new",
            db = db,
            outboxWriter = writer,
            prefs = prefs,
            uiEffects = uiEffects,
            baseUrlProvider = ServerBaseUrlProvider { "https://example.test" },
            savedStateHandle = SavedStateHandle(),
        ))
        val sub = subscribe(vm)
        val loaded = withTimeoutOrNull(2_000L) {
            while (vm.items.value.size < 2) delay(10)
            true
        }
        sub.cancel()

        assertEquals(true, loaded)
        assertEquals(listOf("art_new", "art_old"), vm.items.value.map { it.videoId })
        assertEquals(0, vm.startIndex.value)
    }

    @Test fun passiveCarriedVideoUsesTheDestinationCursorAndIgnoresLateArrival() = runBlocking {
        db.channelDao().upsert(
            ChannelEntity(
                channelId = "tiktok_sample",
                name = "Sample",
                platform = "tiktok",
                sourceId = "sample",
            )
        )
        db.channelFollowDao().upsert(ChannelFollowEntity(channelId = "tiktok_sample"))
        db.videoDao().upsert(
            listOf(
                VideoEntity(
                    videoId = "older",
                    channelId = "tiktok_sample",
                    ownerKind = "tiktok_video",
                    title = "Older",
                    publishedAt = 100L,
                ),
                VideoEntity(
                    videoId = "resume",
                    channelId = "tiktok_sample",
                    ownerKind = "tiktok_video",
                    title = "Resume",
                    publishedAt = 200L,
                ),
            )
        )
        db.momentsCursorDao()
            .upsert(
                MomentsCursorEntity(
                    scope = "following",
                    videoId = "resume",
                    sortAtMs = 200L,
                    updatedAtMs = 300L,
                )
            )

        val vm =
            viewModels.track(
                ShortsRouteViewModel(
                    playlistSpec = ShortsPlaylistSpec.moments(),
                    startVideoId = "missing_from_following",
                    initialSelectionExplicit = false,
                    db = db,
                    outboxWriter = writer,
                    prefs = prefs,
                    uiEffects = uiEffects,
                    baseUrlProvider = ServerBaseUrlProvider { "https://example.test" },
                    savedStateHandle = SavedStateHandle(),
                )
            )
        val sub = subscribe(vm)
        val passiveSelection = scope.launch { vm.consumePendingInitialMomentsSelection() }
        val loaded =
            withTimeoutOrNull(2_000L) {
                while (vm.items.value.size < 2 || vm.currentVideoId.value != "resume") delay(10)
                true
            }
        val resolved =
            withTimeoutOrNull(2_000L) {
                passiveSelection.join()
                true
            }

        db.videoDao().upsert(
            VideoEntity(
                videoId = "missing_from_following",
                channelId = "tiktok_sample",
                ownerKind = "tiktok_video",
                title = "Late carried row",
                publishedAt = 250L,
            )
        )
        val lateRowLoaded =
            withTimeoutOrNull(2_000L) {
                while (vm.items.value.none { it.videoId == "missing_from_following" }) delay(10)
                true
            }
        yield()
        sub.cancel()

        assertEquals(true, loaded)
        assertEquals(true, resolved)
        assertEquals(true, lateRowLoaded)
        assertEquals("resume", vm.currentVideoId.value)
        assertEquals(1, vm.startIndex.value)
        assertEquals(0, db.outboxDao().pendingRows().size)
        assertEquals("resume", db.momentsCursorDao().get("following")?.videoId)
    }

    @Test fun legacyFollowingCursorUsesPublishedTimelineInsteadOfRepostTime() = runBlocking {
        db.channelDao().upsert(
            listOf(
                ChannelEntity(
                    channelId = "tiktok_visible",
                    sourceId = "visible",
                    name = "Visible",
                    platform = "tiktok",
                ),
                ChannelEntity(
                    channelId = "tiktok_hidden",
                    sourceId = "hidden",
                    name = "Hidden",
                    platform = "tiktok",
                ),
                ChannelEntity(
                    channelId = "tiktok_reposter",
                    sourceId = "reposter",
                    name = "Reposter",
                    platform = "tiktok",
                ),
            )
        )
        db.channelFollowDao().upsert(ChannelFollowEntity(channelId = "tiktok_visible"))
        db.channelFollowDao().upsert(ChannelFollowEntity(channelId = "tiktok_reposter"))
        db.videoDao().upsert(
            listOf(
                VideoEntity("older", "tiktok_visible", "tiktok_video", publishedAt = 100L),
                VideoEntity("hidden", "tiktok_hidden", "tiktok_video", publishedAt = 50L),
                VideoEntity("next", "tiktok_visible", "tiktok_video", publishedAt = 300L),
            )
        )
        db.videoRepostSourceDao()
            .upsert(
                listOf(
                    VideoRepostSourceEntity(
                        videoId = "hidden",
                        reposterChannelId = "tiktok_reposter",
                        repostedAtMs = 200L,
                        firstSeenAtMs = 200L,
                        updatedAtMs = 200L,
                    )
                )
            )
        db.momentsCursorDao()
            .upsert(
                MomentsCursorEntity(
                    scope = "following",
                    videoId = "hidden",
                    sortAtMs = 0L,
                    updatedAtMs = 400L,
                )
            )

        val vm =
            viewModels.track(
                ShortsRouteViewModel(
                    playlistSpec = ShortsPlaylistSpec.moments(),
                    startVideoId = "missing_route_item",
                    initialSelectionExplicit = false,
                    db = db,
                    outboxWriter = writer,
                    prefs = prefs,
                    uiEffects = uiEffects,
                    baseUrlProvider = ServerBaseUrlProvider { "https://example.test" },
                    savedStateHandle = SavedStateHandle(),
                )
            )
        val subscription = subscribe(vm)
        val passiveSelection = scope.launch { vm.consumePendingInitialMomentsSelection() }
        val loaded =
            withTimeoutOrNull(2_000L) {
                while (vm.items.value.size < 2 || vm.currentVideoId.value != "older") delay(10)
                true
            }
        val resolved =
            withTimeoutOrNull(2_000L) {
                passiveSelection.join()
                true
            }
        subscription.cancel()

        assertEquals(true, loaded)
        assertEquals(true, resolved)
        assertEquals(listOf("older", "next"), vm.items.value.map { it.videoId })
        assertEquals("older", vm.currentVideoId.value)
        assertEquals(0, vm.startIndex.value)
        assertEquals(0, db.outboxDao().pendingRows().size)
    }

    @Test fun legacyFollowingCursorUsesVideoIdToBreakEqualSortTies() = runBlocking {
        db.channelDao().upsert(
            listOf(
                ChannelEntity(
                    channelId = "tiktok_visible",
                    sourceId = "visible",
                    name = "Visible",
                    platform = "tiktok",
                ),
                ChannelEntity(
                    channelId = "tiktok_hidden",
                    sourceId = "hidden",
                    name = "Hidden",
                    platform = "tiktok",
                ),
            )
        )
        db.channelFollowDao().upsert(ChannelFollowEntity(channelId = "tiktok_visible"))
        db.videoDao().upsert(
            listOf(
                VideoEntity("a", "tiktok_visible", "tiktok_video", publishedAt = 100L),
                VideoEntity("b", "tiktok_hidden", "tiktok_video", publishedAt = 100L),
                VideoEntity("c", "tiktok_visible", "tiktok_video", publishedAt = 100L),
            )
        )
        db.momentsCursorDao()
            .upsert(
                MomentsCursorEntity(
                    scope = "following",
                    videoId = "b",
                    sortAtMs = 0L,
                    updatedAtMs = 200L,
                )
            )

        val vm =
            viewModels.track(
                ShortsRouteViewModel(
                    playlistSpec = ShortsPlaylistSpec.moments(),
                    startVideoId = "missing_route_item",
                    initialSelectionExplicit = false,
                    db = db,
                    outboxWriter = writer,
                    prefs = prefs,
                    uiEffects = uiEffects,
                    baseUrlProvider = ServerBaseUrlProvider { "https://example.test" },
                    savedStateHandle = SavedStateHandle(),
                )
            )
        val subscription = subscribe(vm)
        val passiveSelection = scope.launch { vm.consumePendingInitialMomentsSelection() }
        val loaded =
            withTimeoutOrNull(2_000L) {
                while (vm.items.value.size < 2 || vm.currentVideoId.value != "c") delay(10)
                true
            }
        val resolved =
            withTimeoutOrNull(2_000L) {
                passiveSelection.join()
                true
            }
        subscription.cancel()

        assertEquals(true, loaded)
        assertEquals(true, resolved)
        assertEquals(listOf("a", "c"), vm.items.value.map { it.videoId })
        assertEquals("c", vm.currentVideoId.value)
        assertEquals(1, vm.startIndex.value)
        assertEquals(0, db.outboxDao().pendingRows().size)
    }

    @Test fun restoredSavedStateUsesDurableCursorWithoutRepublishingRouteSelection() = runBlocking {
        db.channelDao().upsert(
            ChannelEntity(
                channelId = "tiktok_sample",
                name = "Sample",
                platform = "tiktok",
                sourceId = "sample",
            )
        )
        db.channelFollowDao().upsert(ChannelFollowEntity(channelId = "tiktok_sample"))
        db.videoDao().upsert(
            listOf(
                VideoEntity(
                    videoId = "older",
                    channelId = "tiktok_sample",
                    ownerKind = "tiktok_video",
                    title = "Older",
                    publishedAt = 100L,
                ),
                VideoEntity(
                    videoId = "selected",
                    channelId = "tiktok_sample",
                    ownerKind = "tiktok_video",
                    title = "Selected",
                    publishedAt = 200L,
                ),
                VideoEntity(
                    videoId = "newer",
                    channelId = "tiktok_sample",
                    ownerKind = "tiktok_video",
                    title = "Newer",
                    publishedAt = 300L,
                ),
            )
        )
        val writeClock = AtomicInteger()
        val routeWriter =
            OutboxWriter(
                db = db,
                prefs = prefs,
                scope = scope,
                nowMsProvider = { writeClock.incrementAndGet().toLong() },
                writeDebounceMs = 50L,
            )
        val initialHandle = SavedStateHandle()
        val initialVm =
            ShortsRouteViewModel(
                playlistSpec = ShortsPlaylistSpec.moments(),
                startVideoId = "selected",
                db = db,
                outboxWriter = routeWriter,
                prefs = prefs,
                uiEffects = uiEffects,
                baseUrlProvider = ServerBaseUrlProvider { "https://example.test" },
                savedStateHandle = initialHandle,
        )
        val initialSubscription = subscribe(initialVm)
        val initialSelection = scope.launch { initialVm.consumePendingInitialMomentsSelection() }
        val loaded =
            withTimeoutOrNull(2_000L) {
                while (
                    initialVm.items.value.size < 3 ||
                        db.momentsCursorDao().get("following")?.videoId != "selected"
                ) {
                    delay(10)
                }
                true
            }

        assertEquals(true, loaded)
        assertEquals("selected", initialVm.currentVideoId.value)
        assertEquals(1, initialVm.startIndex.value)
        assertEquals(1, writeClock.get())
        assertEquals(200L, db.momentsCursorDao().get("following")?.sortAtMs)
        initialSelection.cancelAndJoin()

        initialVm.onIndexChange(initialVm.items.value.single { it.videoId == "newer" })
        val advanced =
            withTimeoutOrNull(2_000L) {
                while (db.momentsCursorDao().get("following")?.videoId != "newer") delay(10)
                true
            }
        assertEquals(true, advanced)
        assertEquals(2, writeClock.get())
        val latestOutboxRow = db.outboxDao().pendingRows().single()

        val restoredHandle = restoredHandle(initialHandle)
        initialSubscription.cancel()
        clearViewModel(initialVm)
        val restoredVm =
            viewModels.track(
                ShortsRouteViewModel(
                    playlistSpec = ShortsPlaylistSpec.moments(),
                    startVideoId = "selected",
                    db = db,
                    outboxWriter = routeWriter,
                    prefs = prefs,
                    uiEffects = uiEffects,
                    baseUrlProvider = ServerBaseUrlProvider { "https://example.test" },
                    savedStateHandle = restoredHandle,
                )
            )
        val restoredSubscription = subscribe(restoredVm)
        val restoredLoaded =
            withTimeoutOrNull(2_000L) {
                while (
                    restoredVm.items.value.size < 3 || restoredVm.currentVideoId.value != "newer"
                ) {
                    delay(10)
                }
                true
            }
        yield()
        restoredSubscription.cancel()

        assertEquals(true, restoredLoaded)
        assertEquals("newer", restoredVm.currentVideoId.value)
        assertEquals(2, restoredVm.startIndex.value)
        assertEquals(2, writeClock.get())
        assertEquals(latestOutboxRow.id, db.outboxDao().pendingRows().single().id)
    }

    @Test fun freshInitialSelectionWaitsForItsRequestedRoomRow() = runBlocking {
        db.channelDao().upsert(
            ChannelEntity(
                channelId = "tiktok_sample",
                name = "Sample",
                platform = "tiktok",
                sourceId = "sample",
            )
        )
        db.channelFollowDao().upsert(ChannelFollowEntity(channelId = "tiktok_sample"))
        val writeClock = AtomicInteger()
        val routeWriter =
            OutboxWriter(
                db = db,
                prefs = prefs,
                scope = scope,
                nowMsProvider = { writeClock.incrementAndGet().toLong() },
                writeDebounceMs = 50L,
            )
        val vm =
            viewModels.track(
                ShortsRouteViewModel(
                    playlistSpec = ShortsPlaylistSpec.moments(),
                    startVideoId = "selected",
                    db = db,
                    outboxWriter = routeWriter,
                    prefs = prefs,
                    uiEffects = uiEffects,
                    baseUrlProvider = ServerBaseUrlProvider { "https://example.test" },
                    savedStateHandle = SavedStateHandle(),
                )
        )
        val subscription = subscribe(vm)
        val initialSelection = scope.launch { vm.consumePendingInitialMomentsSelection() }
        val emptyLoaded =
            withTimeoutOrNull(2_000L) {
                while (vm.uiState.value != UiState.Empty) delay(10)
                true
            }

        assertEquals(true, emptyLoaded)
        assertEquals(0, writeClock.get())
        db.videoDao().upsert(
            VideoEntity(
                videoId = "selected",
                channelId = "tiktok_sample",
                ownerKind = "tiktok_video",
                title = "Selected",
                publishedAt = 200L,
            )
        )
        val recorded =
            withTimeoutOrNull(2_000L) {
                while (db.momentsCursorDao().get("following")?.videoId != "selected") delay(10)
                true
            }
        initialSelection.cancelAndJoin()
        subscription.cancel()

        assertEquals(true, recorded)
        assertEquals(1, writeClock.get())
        assertEquals(1, db.outboxDao().pendingRows().size)
        assertEquals(200L, db.momentsCursorDao().get("following")?.sortAtMs)
    }

    @Test fun newerCursorSupersedesPendingRouteBeforeItsRoomRowArrives() = runBlocking {
        db.channelDao().upsert(
            ChannelEntity(
                channelId = "tiktok_sample",
                name = "Sample",
                platform = "tiktok",
                sourceId = "sample",
            )
        )
        db.channelFollowDao().upsert(ChannelFollowEntity(channelId = "tiktok_sample"))
        db.videoDao().upsert(
            VideoEntity(
                videoId = "newer",
                channelId = "tiktok_sample",
                ownerKind = "tiktok_video",
                title = "Newer",
                publishedAt = 300L,
            )
        )
        val writeClock = AtomicInteger()
        val routeWriter =
            OutboxWriter(
                db = db,
                prefs = prefs,
                scope = scope,
                nowMsProvider = { writeClock.incrementAndGet().toLong() },
                writeDebounceMs = 50L,
            )
        val vm =
            viewModels.track(
                ShortsRouteViewModel(
                    playlistSpec = ShortsPlaylistSpec.moments(),
                    startVideoId = "selected",
                    db = db,
                    outboxWriter = routeWriter,
                    prefs = prefs,
                    uiEffects = uiEffects,
                    baseUrlProvider = ServerBaseUrlProvider { "https://example.test" },
                    savedStateHandle = SavedStateHandle(),
                )
            )
        val subscription = subscribe(vm)
        val baselineCaptured = CompletableDeferred<Unit>()
        val pendingInitialSelection =
            scope.launch {
                vm.consumePendingInitialMomentsSelection {
                    baselineCaptured.complete(Unit)
                }
            }
        val ready = withTimeoutOrNull(2_000L) { baselineCaptured.await() }
        assertEquals(Unit, ready)

        db.momentsCursorDao()
            .upsert(
                MomentsCursorEntity(
                    scope = "following",
                    videoId = "newer",
                    sortAtMs = 300L,
                    updatedAtMs = 500L,
                )
            )
        val superseded =
            withTimeoutOrNull(2_000L) {
                pendingInitialSelection.join()
                true
            }
        db.videoDao().upsert(
            VideoEntity(
                videoId = "selected",
                channelId = "tiktok_sample",
                ownerKind = "tiktok_video",
                title = "Selected",
                publishedAt = 200L,
            )
        )
        val delayedRowLoaded =
            withTimeoutOrNull(2_000L) {
                while (vm.items.value.none { it.videoId == "selected" }) delay(10)
                true
            }
        yield()
        subscription.cancel()

        assertEquals(true, superseded)
        assertEquals(true, delayedRowLoaded)
        assertEquals("newer", db.momentsCursorDao().get("following")?.videoId)
        assertEquals("newer", vm.currentVideoId.value)
        assertEquals(0, writeClock.get())
        assertEquals(0, db.outboxDao().pendingRows().size)
    }

    @Test fun restoredPendingRouteIdentityStaysPassiveWhenTheRowArrives() = runBlocking {
        db.channelDao().upsert(
            ChannelEntity(
                channelId = "tiktok_sample",
                name = "Sample",
                platform = "tiktok",
                sourceId = "sample",
            )
        )
        db.channelFollowDao().upsert(ChannelFollowEntity(channelId = "tiktok_sample"))
        val writeClock = AtomicInteger()
        val routeWriter =
            OutboxWriter(
                db = db,
                prefs = prefs,
                scope = scope,
                nowMsProvider = { writeClock.incrementAndGet().toLong() },
                writeDebounceMs = 50L,
            )
        val initialHandle = SavedStateHandle()
        val initialVm =
            ShortsRouteViewModel(
                playlistSpec = ShortsPlaylistSpec.moments(),
                startVideoId = "selected",
                db = db,
                outboxWriter = routeWriter,
                prefs = prefs,
                uiEffects = uiEffects,
                baseUrlProvider = ServerBaseUrlProvider { "https://example.test" },
                savedStateHandle = initialHandle,
        )
        val initialSubscription = subscribe(initialVm)
        val pendingInitialSelection =
            scope.launch { initialVm.consumePendingInitialMomentsSelection() }
        val initialEmptyLoaded =
            withTimeoutOrNull(2_000L) {
                while (initialVm.uiState.value != UiState.Empty) delay(10)
                true
            }

        assertEquals(true, initialEmptyLoaded)
        assertEquals(0, writeClock.get())
        val restoredHandle = restoredHandle(initialHandle)
        pendingInitialSelection.cancelAndJoin()
        initialSubscription.cancel()
        clearViewModel(initialVm)

        val restoredVm =
            viewModels.track(
                ShortsRouteViewModel(
                    playlistSpec = ShortsPlaylistSpec.moments(),
                    startVideoId = "selected",
                    db = db,
                    outboxWriter = routeWriter,
                    prefs = prefs,
                    uiEffects = uiEffects,
                    baseUrlProvider = ServerBaseUrlProvider { "https://example.test" },
                    savedStateHandle = restoredHandle,
                )
            )
        val restoredSubscription = subscribe(restoredVm)
        val restoredEmptyLoaded =
            withTimeoutOrNull(2_000L) {
                while (restoredVm.uiState.value != UiState.Empty) delay(10)
                true
            }
        assertEquals(true, restoredEmptyLoaded)

        db.videoDao().upsert(
            VideoEntity(
                videoId = "selected",
                channelId = "tiktok_sample",
                ownerKind = "tiktok_video",
                title = "Selected",
                publishedAt = 200L,
            )
        )
        val restoredRowLoaded =
            withTimeoutOrNull(2_000L) {
                while (restoredVm.items.value.none { it.videoId == "selected" }) delay(10)
                true
            }
        yield()
        restoredSubscription.cancel()

        assertEquals(true, restoredRowLoaded)
        assertEquals(0, writeClock.get())
        assertEquals(0, db.outboxDao().pendingRows().size)
        assertEquals(null, db.momentsCursorDao().get("following"))
    }

}
