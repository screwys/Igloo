package com.screwy.igloo.data

import com.screwy.igloo.data.entity.ChannelEntity
import com.screwy.igloo.data.entity.ChannelFollowEntity
import com.screwy.igloo.data.entity.FeedItemEntity
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], manifest = Config.NONE)
class ChannelFeedReadDaoTest {

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
    fun channelFeedFlow_fallsBackToAuthorHandleWhenChannelIdMissing() = runBlocking {
        db.channelDao().upsert(
            ChannelEntity(
                channelId = "twitter_account",
                sourceId = "account",
                name = "Account",
                platform = "twitter",
            )
        )
        db.channelFollowDao().upsert(ChannelFollowEntity("twitter_account", followedAt = 1))
        db.feedItemDao().upsert(
            listOf(
                FeedItemEntity(
                    tweetId = "direct",
                    authorHandle = "Account",
                    channelId = "twitter_account",
                    syncSeq = 2,
                ),
                FeedItemEntity(
                    tweetId = "legacy",
                    authorHandle = "@account",
                    channelId = null,
                    syncSeq = 3,
                ),
                FeedItemEntity(
                    tweetId = "other",
                    authorHandle = "other",
                    channelId = null,
                    syncSeq = 4,
                ),
            )
        )

        val rows = db.feedReadDao()
            .channelFeedFlow(channelId = "twitter_account", channelHandle = "account")
            .first()

        assertEquals(listOf("legacy", "direct"), rows.map { it.item.tweetId })
        assertEquals(listOf("twitter_account", "twitter_account"), rows.map { it.item.channelId })
        assertEquals(listOf(1, 1), rows.map { it.channelIsFollowed })
    }

    @Test
    fun channelFeedFlow_fallsBackToSourceRepostAndQuoteHandlesWhenChannelIdMissing() = runBlocking {
        db.channelDao().upsert(
            ChannelEntity(
                channelId = "twitter_sample_source",
                sourceId = "sample_source",
                name = "Account",
                platform = "twitter",
            )
        )
        db.channelFollowDao().upsert(ChannelFollowEntity("twitter_sample_source", followedAt = 1))
        db.feedItemDao().upsert(
            listOf(
                FeedItemEntity(
                    tweetId = "sample_tweet_source",
                    sourceHandle = "@sample_source",
                    authorHandle = "sample_author",
                    channelId = null,
                    publishedAt = 30,
                ),
                FeedItemEntity(
                    tweetId = "sample_tweet_repost",
                    authorHandle = "sample_author",
                    retweetedByHandle = "sample_source",
                    channelId = null,
                    publishedAt = 20,
                ),
                FeedItemEntity(
                    tweetId = "sample_tweet_quote",
                    authorHandle = "sample_author",
                    quoteAuthorHandle = "sample_source",
                    channelId = null,
                    publishedAt = 10,
                ),
                FeedItemEntity(
                    tweetId = "sample_tweet_other",
                    sourceHandle = "sample_source_two",
                    authorHandle = "sample_author",
                    channelId = null,
                    publishedAt = 40,
                ),
            )
        )

        val rows = db.feedReadDao()
            .channelFeedFlow(channelId = "twitter_sample_source", channelHandle = "sample_source")
            .first()

        assertEquals(
            listOf("sample_tweet_source", "sample_tweet_repost", "sample_tweet_quote"),
            rows.map { it.item.tweetId },
        )
        assertEquals(
            listOf("twitter_sample_source", "twitter_sample_source", "twitter_sample_source"),
            rows.map { it.item.channelId },
        )
    }
}
