package com.screwy.igloo.feed

import com.screwy.igloo.data.entity.FeedItemEntity
import com.screwy.igloo.data.entity.FeedRow
import com.screwy.igloo.data.entity.ThreadedFeedRow
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class FeedSnapshotRefreshTest {
    @Test
    fun mergePinnedFeedRows_refreshesQuoteContentWithoutChangingThePinnedDeck() {
        val first = feedRow("sample_first")
        val pinnedTarget =
            feedRow("sample_target").copy(
                isLiked = 1,
                isBookmarked = 1,
                bookmarkCustomTitle = "Saved title",
            )
        val freshTarget =
            feedRow(
                tweetId = "sample_target",
                quoteTweetId = "sample_quote",
                quoteBodyText = "Quoted body",
            )
        val pinned =
            listOf(
                ThreadedFeedRow(row = first, chain = emptyList()),
                ThreadedFeedRow(row = pinnedTarget, chain = emptyList()),
            )

        val merged = mergePinnedFeedRows(
            pinnedRows = pinned,
            freshRows = listOf(freshTarget, feedRow("sample_new")),
        )

        assertEquals(listOf("sample_first", "sample_target"), merged.map { it.row.item.tweetId })
        assertNull(merged.first().row.item.quoteTweetId)
        assertEquals("sample_quote", merged[1].row.item.quoteTweetId)
        assertEquals("Quoted body", merged[1].row.item.quoteBodyText)
        assertEquals(1, merged[1].row.isLiked)
        assertEquals(1, merged[1].row.isBookmarked)
        assertEquals("Saved title", merged[1].row.bookmarkCustomTitle)
    }

    @Test
    fun mergePinnedFeedRows_refreshesThreadAncestorsInPlace() {
        val pinned =
            listOf(
                ThreadedFeedRow(
                    row = feedRow("sample_leaf"),
                    chain = listOf(feedRow("sample_parent")),
                )
            )
        val freshParent =
            feedRow(
                tweetId = "sample_parent",
                quoteTweetId = "sample_quote",
                quoteBodyText = "Quoted body",
            )

        val merged = mergePinnedFeedRows(pinned, listOf(freshParent))

        assertEquals("sample_leaf", merged.single().row.item.tweetId)
        assertEquals("sample_parent", merged.single().chain.single().item.tweetId)
        assertEquals("sample_quote", merged.single().chain.single().item.quoteTweetId)
    }

    private fun feedRow(
        tweetId: String,
        quoteTweetId: String? = null,
        quoteBodyText: String? = null,
    ): FeedRow =
        FeedRow(
            item =
                FeedItemEntity(
                    tweetId = tweetId,
                    quoteTweetId = quoteTweetId,
                    quoteBodyText = quoteBodyText,
                ),
            channelName = null,
            channelPlatform = "twitter",
            isLiked = 0,
            likedAt = null,
            isBookmarked = 0,
            bookmarkCategoryId = null,
            bookmarkCustomTitle = null,
            bookmarkedAt = null,
            channelIsFollowed = 0,
            channelIsStarred = 0,
        )
}
