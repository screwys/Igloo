package com.screwy.igloo.settings

import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.settings.components.syncIntervalOptions
import org.junit.Assert.assertEquals
import org.junit.Test
import com.screwy.igloo.R

class StorageRouteTest {

    @Test fun syncIntervals_offerLiveBeforePeriodicCatchUp() {
        assertEquals(
            listOf(PreferencesRepo.Defaults.SYNC_INTERVAL_LIVE, 30, 60, 120, 240),
            syncIntervalOptions,
        )
    }

    @Test fun cacheBucketLabelResource_usesProductNames() {
        assertEquals(R.string.cache_feed_items, cacheBucketLabelResource("feed_items"))
        assertEquals(null, cacheBucketLabelResource("youtube_videos"))
        assertEquals(R.string.platform_youtube, cacheBucketLabelResource("videos"))
        assertEquals(R.string.nav_moments, cacheBucketLabelResource("shorts_videos"))
        assertEquals(R.string.cache_x_media, cacheBucketLabelResource("twitter_media"))
    }

    @Test fun humanizeCacheBucket_humanizesUnknownBuckets() {
        assertEquals("Some Future Bucket", humanizeCacheBucket("some_future_bucket"))
    }
}
