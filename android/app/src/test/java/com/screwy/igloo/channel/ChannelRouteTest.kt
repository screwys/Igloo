package com.screwy.igloo.channel

import com.screwy.igloo.data.entity.ChannelEntity
import com.screwy.igloo.data.entity.ChannelProfileEntity
import com.screwy.igloo.ui.component.Platform
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ChannelRouteTest {

    @Test
    fun resolveChannelRoutePlatform_prefersProfileThenChannelThenChannelId() {
        assertEquals(
            Platform.YouTube,
            resolveChannelRoutePlatform(
                profile = ChannelProfileEntity(
                    channelId = "twitter_sample_creator",
                    platform = "youtube",
                    handle = "sample_creator",
                ),
                channel = ChannelEntity(
                    channelId = "twitter_sample_creator",
                    name = "Sample Creator",
                    platform = "twitter",
                    sourceId = "sample_creator",
                ),
            ),
        )
        assertEquals(
            Platform.TikTok,
            resolveChannelRoutePlatform(
                profile = null,
                channel = ChannelEntity(
                    channelId = "youtube_sample_creator",
                    name = "Sample Creator",
                    platform = "tiktok",
                    sourceId = "sample_creator",
                ),
            ),
        )
        assertEquals(
            Platform.Instagram,
            resolveChannelRoutePlatform(
                profile = null,
                channel = ChannelEntity(
                    channelId = "instagram_sample_creator",
                    name = "Sample Creator",
                    platform = "",
                    sourceId = "sample_creator",
                ),
            ),
        )
    }

    @Test
    fun channelRouteDisplayNameOverride_usesAuthorFallbackOnlyForTwitter() {
        assertEquals(
            "Sample Creator",
            channelRouteDisplayNameOverride(
                profileDisplayName = null,
                snapshotDisplayName = null,
                routePlatform = Platform.Twitter,
                primaryName = "@sample_creator",
                sourceHandle = "sample_creator",
                twitterAuthorDisplayNames = listOf("Sample Creator"),
            ),
        )
        assertNull(
            channelRouteDisplayNameOverride(
                profileDisplayName = null,
                snapshotDisplayName = null,
                routePlatform = Platform.YouTube,
                primaryName = "@sample_creator",
                sourceHandle = "sample_creator",
                twitterAuthorDisplayNames = listOf("Sample Creator"),
            ),
        )
    }

    @Test
    fun resolveHeaderDisplayName_prefersAuthorDisplayNameWhenStoredNameMatchesHandle() {
        assertEquals(
            "Sample Creator",
            resolveHeaderDisplayName(
                primaryName = "@sample_creator",
                sourceHandle = "sample_creator",
                authorDisplayNames = listOf("Sample Creator"),
            ),
        )
    }

    @Test
    fun resolveHeaderDisplayName_returnsNullWhenPrimaryAlreadyLooksLikeDisplayName() {
        assertNull(
            resolveHeaderDisplayName(
                primaryName = "Sample Creator",
                sourceHandle = "sample_creator",
                authorDisplayNames = listOf("Sample Creator"),
            ),
        )
    }
}
