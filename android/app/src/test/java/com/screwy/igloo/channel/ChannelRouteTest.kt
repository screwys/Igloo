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
                    channelId = "twitter_alice",
                    platform = "youtube",
                    handle = "alice",
                ),
                channel = ChannelEntity(
                    channelId = "twitter_alice",
                    name = "Alice",
                    platform = "twitter",
                    sourceId = "alice",
                ),
            ),
        )
        assertEquals(
            Platform.TikTok,
            resolveChannelRoutePlatform(
                profile = null,
                channel = ChannelEntity(
                    channelId = "youtube_alice",
                    name = "Alice",
                    platform = "tiktok",
                    sourceId = "alice",
                ),
            ),
        )
        assertEquals(
            Platform.Instagram,
            resolveChannelRoutePlatform(
                profile = null,
                channel = ChannelEntity(
                    channelId = "instagram_alice",
                    name = "Alice",
                    platform = "",
                    sourceId = "alice",
                ),
            ),
        )
    }

    @Test
    fun channelRouteDisplayNameOverride_usesAuthorFallbackOnlyForTwitter() {
        assertEquals(
            "Alice Doe",
            channelRouteDisplayNameOverride(
                profileDisplayName = null,
                snapshotDisplayName = null,
                routePlatform = Platform.Twitter,
                primaryName = "@alice",
                sourceHandle = "alice",
                twitterAuthorDisplayNames = listOf("Alice Doe"),
            ),
        )
        assertNull(
            channelRouteDisplayNameOverride(
                profileDisplayName = null,
                snapshotDisplayName = null,
                routePlatform = Platform.YouTube,
                primaryName = "@alice",
                sourceHandle = "alice",
                twitterAuthorDisplayNames = listOf("Alice Doe"),
            ),
        )
    }

    @Test
    fun resolveHeaderDisplayName_prefersAuthorDisplayNameWhenStoredNameMatchesHandle() {
        assertEquals(
            "Alice Doe",
            resolveHeaderDisplayName(
                primaryName = "@alice",
                sourceHandle = "alice",
                authorDisplayNames = listOf("Alice Doe"),
            ),
        )
    }

    @Test
    fun resolveHeaderDisplayName_returnsNullWhenPrimaryAlreadyLooksLikeDisplayName() {
        assertNull(
            resolveHeaderDisplayName(
                primaryName = "Alice Doe",
                sourceHandle = "alice",
                authorDisplayNames = listOf("Alice Doe"),
            ),
        )
    }
}
