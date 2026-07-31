package com.screwy.igloo.moments

import com.screwy.igloo.data.IglooDatabase
import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.data.RoomTestSupport
import com.screwy.igloo.data.entity.MomentsCursorEntity
import com.screwy.igloo.media.OwnerKind
import com.screwy.igloo.outbox.OutboxWriter
import com.screwy.igloo.ui.UiState
import com.screwy.igloo.ui.component.MomentItem as PlayerMomentItem
import com.screwy.igloo.ui.component.MomentThumbnailItem
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], manifest = Config.NONE)
class MomentsCursorWriteTest {
    private lateinit var db: IglooDatabase

    @Before
    fun setUp() {
        db = RoomTestSupport.freshDb()
    }

    @After
    fun tearDown() {
        db.close()
    }

    @Test
    fun queuedCursorWriteKeepsTheTabActiveWhenThePageSettled() = runTest {
        val prefs = PreferencesRepo(db.preferenceDao(), backgroundScope, nowMsProvider = { 1L })
        val writer =
            OutboxWriter(
                db = db,
                prefs = prefs,
                scope = backgroundScope,
                nowMsProvider = { 1L },
                writeDebounceMs = 1L,
            )
        var activeTab = "all"

        val write =
            backgroundScope.launchMomentsCursorWrite(
                outboxWriter = writer,
                videoId = "all_only_video",
                sortAtMs = 100L,
                activeTab = activeTab,
            )
        activeTab = "following"
        runCurrent()
        write.join()

        assertEquals("following", activeTab)
        assertEquals("all_only_video", db.momentsCursorDao().get("all")?.videoId)
        assertNull(db.momentsCursorDao().get("following"))
    }

    @Test
    fun delayedTabFlowsEmitOneCoherentPlayerRouteState() = runTest {
        val activeTab = MutableStateFlow("all")
        val rows =
            mapOf(
                "all" to Channel<List<ShortsStartItem>>(Channel.UNLIMITED),
                "following" to Channel<List<ShortsStartItem>>(Channel.UNLIMITED),
            )
        val cursors =
            mapOf(
                "all" to Channel<MomentsCursorEntity?>(Channel.UNLIMITED),
                "following" to Channel<MomentsCursorEntity?>(Channel.UNLIMITED),
            )
        val routeStates = mutableListOf<MomentsPlayerRouteState>()
        val collection =
            backgroundScope.launch {
                scopedShortsSnapshotFlow(
                        activeTab = activeTab,
                        rowsForScope = { scope -> rows.getValue(scope).receiveAsFlow() },
                        cursorForScope = { scope -> cursors.getValue(scope).receiveAsFlow() },
                        startItem = { item -> item },
                    )
                    .map { snapshot ->
                        momentsPlayerRouteState(snapshot) { scopedRows ->
                            scopedRows.map(::playerItem)
                        }
                    }
                    .collect { routeState -> routeStates += routeState }
            }
        runCurrent()

        rows.getValue("all")
            .send(
                listOf(
                    ShortsStartItem("shared", 100L),
                    ShortsStartItem("all_only", 200L),
                )
            )
        cursors.getValue("all")
            .send(MomentsCursorEntity("all", "all_only", sortAtMs = 200L, updatedAtMs = 1L))
        runCurrent()
        assertEquals("all", routeStates.last().scope)
        assertEquals("all_only", routeStates.last().selection.videoId)

        activeTab.value = "following"
        runCurrent()
        assertEquals("following", routeStates.last().scope)
        assertTrue(routeStates.last().uiState is UiState.Loading)
        assertTrue(routeStates.last().items.isEmpty())

        rows.getValue("following")
            .send(listOf(ShortsStartItem("following_only", 300L)))
        cursors.getValue("all")
            .send(MomentsCursorEntity("all", "all_return", sortAtMs = 400L, updatedAtMs = 2L))
        runCurrent()
        assertEquals("following", routeStates.last().scope)
        assertTrue(routeStates.last().uiState is UiState.Loading)
        assertTrue(routeStates.last().items.isEmpty())

        cursors.getValue("following")
            .send(
                MomentsCursorEntity(
                    "following",
                    "following_only",
                    sortAtMs = 300L,
                    updatedAtMs = 3L,
                )
            )
        runCurrent()
        assertEquals("following_only", routeStates.last().selection.videoId)

        activeTab.value = "all"
        runCurrent()
        assertEquals("all", routeStates.last().scope)
        assertTrue(routeStates.last().uiState is UiState.Loading)
        assertTrue(routeStates.last().items.isEmpty())

        cursors.getValue("following")
            .send(
                MomentsCursorEntity(
                    "following",
                    "following_stale",
                    sortAtMs = 500L,
                    updatedAtMs = 4L,
                )
            )
        runCurrent()
        assertEquals("all", routeStates.last().scope)
        assertTrue(routeStates.last().uiState is UiState.Loading)
        assertTrue(routeStates.last().items.isEmpty())

        rows.getValue("all").send(listOf(ShortsStartItem("all_return", 400L)))
        runCurrent()
        collection.cancel()

        val ready = routeStates.filter { routeState -> routeState.uiState is UiState.Data }
        assertEquals(listOf("all", "following", "all"), ready.map { routeState -> routeState.scope })
        assertEquals(listOf("all_only", "following_only", "all_return"), ready.map { it.selection.videoId })
        assertTrue(
            ready.all { routeState ->
                routeState.items.any { item -> item.videoId == routeState.selection.videoId }
            }
        )
    }

    @Test
    fun delayedTabFlowsEmitOneCoherentGridRouteState() = runTest {
        val activeTab = MutableStateFlow("all")
        val rows =
            mapOf(
                "all" to Channel<List<ShortsStartItem>>(Channel.UNLIMITED),
                "following" to Channel<List<ShortsStartItem>>(Channel.UNLIMITED),
            )
        val cursors =
            mapOf(
                "all" to Channel<MomentsCursorEntity?>(Channel.UNLIMITED),
                "following" to Channel<MomentsCursorEntity?>(Channel.UNLIMITED),
            )
        val routeStates = mutableListOf<MomentsGridRouteState>()
        val collection =
            backgroundScope.launch {
                scopedShortsSnapshotFlow(
                        activeTab = activeTab,
                        rowsForScope = { scope -> rows.getValue(scope).receiveAsFlow() },
                        cursorForScope = { scope -> cursors.getValue(scope).receiveAsFlow() },
                        startItem = { item -> item },
                        scopeForTab = PreferencesRepo.Defaults::normalizeMomentsTab,
                    )
                    .map { snapshot ->
                        momentsGridRouteState(snapshot, emptyList()) { scopedRows ->
                            scopedRows.map(::thumbnailItem)
                        }
                    }
                    .collect { routeState -> routeStates += routeState }
            }
        runCurrent()

        rows.getValue("all")
            .send(
                listOf(
                    ShortsStartItem("shared", 100L),
                    ShortsStartItem("all_only", 200L),
                )
            )
        cursors.getValue("all")
            .send(MomentsCursorEntity("all", "all_only", sortAtMs = 200L, updatedAtMs = 1L))
        runCurrent()
        assertEquals("all", routeStates.last().scope)
        assertEquals("all_only", routeStates.last().items[routeStates.last().startIndex].videoId)

        activeTab.value = "following"
        runCurrent()
        assertEquals("following", routeStates.last().scope)
        assertTrue(routeStates.last().uiState is UiState.Loading)
        assertTrue(routeStates.last().items.isEmpty())

        rows.getValue("following")
            .send(listOf(ShortsStartItem("following_only", 300L)))
        cursors.getValue("all")
            .send(MomentsCursorEntity("all", "all_return", sortAtMs = 400L, updatedAtMs = 2L))
        runCurrent()
        assertEquals("following", routeStates.last().scope)
        assertTrue(routeStates.last().uiState is UiState.Loading)
        assertTrue(routeStates.last().items.isEmpty())

        cursors.getValue("following")
            .send(
                MomentsCursorEntity(
                    "following",
                    "following_only",
                    sortAtMs = 300L,
                    updatedAtMs = 3L,
                )
            )
        runCurrent()
        assertEquals(
            "following_only",
            routeStates.last().items[routeStates.last().startIndex].videoId,
        )

        activeTab.value = "all"
        runCurrent()
        assertEquals("all", routeStates.last().scope)
        assertTrue(routeStates.last().uiState is UiState.Loading)
        assertTrue(routeStates.last().items.isEmpty())

        cursors.getValue("following")
            .send(
                MomentsCursorEntity(
                    "following",
                    "following_stale",
                    sortAtMs = 500L,
                    updatedAtMs = 4L,
                )
            )
        runCurrent()
        assertEquals("all", routeStates.last().scope)
        assertTrue(routeStates.last().uiState is UiState.Loading)
        assertTrue(routeStates.last().items.isEmpty())

        rows.getValue("all").send(listOf(ShortsStartItem("all_return", 400L)))
        runCurrent()
        collection.cancel()

        val ready = routeStates.filter { routeState -> routeState.uiState is UiState.Data }
        assertEquals(listOf("all", "following", "all"), ready.map { routeState -> routeState.scope })
        assertEquals(
            listOf("all_only", "following_only", "all_return"),
            ready.map { routeState -> routeState.items[routeState.startIndex].videoId },
        )
    }

    private fun playerItem(item: ShortsStartItem): PlayerMomentItem =
        PlayerMomentItem(
            videoId = item.videoId,
            channelId = "tiktok_sample",
            authorHandle = "@sample",
            description = item.videoId,
            likeCount = null,
            isLiked = false,
            isBookmarked = false,
            ownerKind = OwnerKind.TikTokVideo,
            sortAtMs = item.sortAtMs,
            publishedAt = item.sortAtMs,
        )

    private fun thumbnailItem(item: ShortsStartItem): MomentThumbnailItem =
        MomentThumbnailItem(
            videoId = item.videoId,
            channelId = "tiktok_sample",
            ownerKind = OwnerKind.TikTokVideo,
            durationMs = 0L,
            publishedAt = item.sortAtMs,
            isViewed = false,
        )
}
