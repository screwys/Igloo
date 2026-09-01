package com.screwy.igloo.moments

import com.screwy.igloo.bookmarks.BookmarkFilter
import com.screwy.igloo.bookmarks.bookmarkPlaylistId
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ShortsPlaylistSpecTest {
    @Test
    fun decodeRootPlaylistsNormalizesBlankId() {
        assertEquals(
            ShortsPlaylistSpec(type = ShortsPlaylistType.Moments, playlistId = ShortsPlaylistSpec.RootPlaylistId),
            ShortsPlaylistSpec.decode("moments", ""),
        )
        assertEquals(
            ShortsPlaylistSpec(type = ShortsPlaylistType.AllMoments, playlistId = ShortsPlaylistSpec.RootPlaylistId),
            ShortsPlaylistSpec.decode("all_moments", "   "),
        )
        assertEquals(
            ShortsPlaylistSpec(type = ShortsPlaylistType.Bookmarks, playlistId = ShortsPlaylistSpec.RootPlaylistId),
            ShortsPlaylistSpec.decode("bookmarks", null),
        )
        assertEquals(
            ShortsPlaylistSpec(type = ShortsPlaylistType.Bookmarks, playlistId = bookmarkPlaylistId(BookmarkFilter.Category(34L))),
            ShortsPlaylistSpec.decode("bookmarks", bookmarkPlaylistId(BookmarkFilter.Category(34L))),
        )
        assertEquals(
            ShortsPlaylistSpec(type = ShortsPlaylistType.StoryTray, playlistId = ShortsPlaylistSpec.RootPlaylistId),
            ShortsPlaylistSpec.decode("stories", "tiktok_ignored"),
        )
    }

    @Test
    fun decodeChannelRequiresChannelId() {
        assertEquals(
            ShortsPlaylistSpec(type = ShortsPlaylistType.Channel, playlistId = "tiktok_creator"),
            ShortsPlaylistSpec.decode("channel", " tiktok_creator "),
        )
        assertNull(ShortsPlaylistSpec.decode("channel", ""))
    }

    @Test
    fun routePartsUseStableRootId() {
        assertEquals("moments", ShortsPlaylistSpec.moments().routePlaylistType)
        assertEquals(ShortsPlaylistSpec.RootPlaylistId, ShortsPlaylistSpec.moments().routePlaylistId)
        assertEquals("all_moments", ShortsPlaylistSpec.allMoments().routePlaylistType)
        assertEquals(ShortsPlaylistSpec.RootPlaylistId, ShortsPlaylistSpec.bookmarks().routePlaylistId)
        assertEquals(
            bookmarkPlaylistId(BookmarkFilter.Label("art")),
            ShortsPlaylistSpec.bookmarks(bookmarkPlaylistId(BookmarkFilter.Label("art"))).routePlaylistId,
        )
        assertEquals("channel", ShortsPlaylistSpec.channel("instagram_a")?.routePlaylistType)
        assertEquals("instagram_a", ShortsPlaylistSpec.channel("instagram_a")?.routePlaylistId)
        assertEquals("stories", ShortsPlaylistSpec.storyTray().routePlaylistType)
        assertEquals(ShortsPlaylistSpec.RootPlaylistId, ShortsPlaylistSpec.storyTray().routePlaylistId)
    }

    @Test
    fun startIndexFallsBackToZeroWhenRequestedVideoIsMissing() {
        assertEquals(1, shortsStartIndex(listOf("a", "b", "c"), "b"))
        assertEquals(0, shortsStartIndex(listOf("a", "b", "c"), "missing"))
        assertEquals(0, shortsStartIndex(emptyList<String>(), "b"))
    }

    @Test
    fun startIndexFallsForwardToNearestTimelineItemWhenRequestedVideoIsMissing() {
        val items = listOf(
            ShortsStartItem(videoId = "old", sortAtMs = 100),
            ShortsStartItem(videoId = "next", sortAtMs = 300),
            ShortsStartItem(videoId = "newest", sortAtMs = 500),
        )

        assertEquals(1, shortsStartIndex(items, "hidden", fallbackSortAtMs = 200))
    }

    @Test
    fun exactCursorVideoWinsWhenItsPresentationTimeChanges() {
        val items = listOf(
            ShortsStartItem(videoId = "moved", sortAtMs = 100),
            ShortsStartItem(videoId = "near", sortAtMs = 300),
        )

        assertEquals(0, shortsStartIndex(items, "moved", fallbackSortAtMs = 250))
    }

    @Test
    fun missingCursorUsesNearestRetainedOrderPosition() {
        val items =
            listOf(
                ShortsStartItem(videoId = "bookmark", sortAtMs = 10, orderPosition = 20),
                ShortsStartItem(videoId = "retained_old", sortAtMs = 900, orderPosition = 100),
                ShortsStartItem(videoId = "retained_new", sortAtMs = 1000, orderPosition = 110),
            )

        assertEquals(
            1,
            shortsStartIndex(
                items,
                requestedVideoId = "pruned_cursor",
                fallbackSortAtMs = 15,
                fallbackOrderPosition = 95,
            ),
        )
    }

    @Test
    fun startIndexUsesVideoIdAsTheTimelineTieBreaker() {
        val items = listOf(
            ShortsStartItem(videoId = "a", sortAtMs = 100),
            ShortsStartItem(videoId = "c", sortAtMs = 100),
            ShortsStartItem(videoId = "d", sortAtMs = 200),
        )

        assertEquals(1, shortsStartIndex(items, "b", fallbackSortAtMs = 100))
    }

    @Test
    fun visibleSelectionReturnsOneCoherentPlaylistPair() {
        val items = listOf(
            ShortsStartItem(videoId = "old", sortAtMs = 100),
            ShortsStartItem(videoId = "next", sortAtMs = 300),
        )

        assertEquals(
            VisibleShortsSelection(videoId = "next", index = 1),
            visibleShortsSelection(items, "hidden", fallbackSortAtMs = 200),
        )
        assertEquals(
            VisibleShortsSelection(videoId = "old", index = 0),
            visibleShortsSelection(items, requestedVideoId = null),
        )
        assertEquals(
            VisibleShortsSelection(videoId = null, index = 0),
            visibleShortsSelection(emptyList(), "hidden", fallbackSortAtMs = 200),
        )
    }
}
