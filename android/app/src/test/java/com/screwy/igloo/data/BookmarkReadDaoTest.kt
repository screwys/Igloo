package com.screwy.igloo.data

import com.screwy.igloo.data.entity.BookmarkEntity
import com.screwy.igloo.data.entity.FeedItemEntity
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], manifest = Config.NONE)
class BookmarkReadDaoTest {
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
    fun tweetBookmarkDoesNotMaterializeMissingVideo() = runBlocking {
        db.feedItemDao()
            .upsert(
                FeedItemEntity(
                    tweetId = "sample_tweet",
                    mediaJson = """[{"type":"image"}]""",
                )
            )
        db.bookmarkDao().upsert(BookmarkEntity(videoId = "sample_tweet", bookmarkedAt = 1L))

        val item = db.bookmarkReadDao().bookmarksFlow().first().single()

        assertEquals("sample_tweet", item.feedItem?.tweetId)
        assertNull(item.video)
    }
}
