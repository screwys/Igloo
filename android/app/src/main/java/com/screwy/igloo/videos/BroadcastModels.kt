package com.screwy.igloo.videos

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class Broadcast(
    @SerialName("video_id") val videoId: String,
    @SerialName("channel_id") val channelId: String,
    val title: String = "",
    @SerialName("thumbnail_url") val thumbnailUrl: String = "",
    @SerialName("live_status") val liveStatus: String,
    @SerialName("published_at_ms") val publishedAtMs: Long = 0,
    @SerialName("starts_at_ms") val startsAtMs: Long = 0,
    @SerialName("concurrent_view_count") val concurrentViewCount: Long? = null,
    @SerialName("observed_at_ms") val observedAtMs: Long = 0,
    @SerialName("source_rank") val sourceRank: Int = 0,
)

@Serializable
data class BroadcastCache(
    val broadcasts: List<Broadcast> = emptyList(),
    @SerialName("broadcasts_enabled") val broadcastsEnabled: Boolean = true,
)

data class BroadcastCard(
    val id: String,
    val channelId: String,
    val channelName: String,
    val title: String,
    val broadcast: Broadcast,
) {
    val key: String get() = "live:$id"
}

data class BroadcastPlayback(val videoId: String, val title: String, val url: String, val mimeType: String?)
