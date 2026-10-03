package com.screwy.igloo.home

import com.screwy.igloo.data.entity.FeedRow
import com.screwy.igloo.data.entity.VideoGridItem
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class HomeLayout(
    val columns: Int = 3,
    val spacing: String = "comfortable",
    val widgets: List<HomeWidget> = defaultHomeWidgets(),
)

@Serializable
data class HomeWidget(
    val id: String,
    val type: String,
    val title: String = "",
    val size: String = "medium",
    val layout: String = "cards",
    val style: String = "surface",
    val count: Int = 4,
    val platforms: List<String> = emptyList(),
    val channels: List<String> = emptyList(),
    @SerialName("content_types") val contentTypes: List<String> = emptyList(),
    @SerialName("live_states") val liveStates: List<String> = listOf("is_live", "is_upcoming"),
    @SerialName("show_header") val showHeader: Boolean = true,
    @SerialName("show_media") val showMedia: Boolean = true,
    @SerialName("show_text") val showText: Boolean = true,
    @SerialName("starred_only") val starredOnly: Boolean = false,
    val order: String = "recent",
)

fun defaultHomeWidgets(): List<HomeWidget> = listOf(
    HomeWidget("continue", "continue", size = "large", layout = "feature", count = 3),
    HomeWidget("live", "live", size = "small", layout = "feature", starredOnly = true, order = "live"),
    HomeWidget("starred", "starred", size = "small", layout = "editorial", starredOnly = true, order = "newest"),
    HomeWidget("moments", "moments", size = "small", layout = "portraits", order = "newest"),
    HomeWidget("saved", "saved", size = "small", order = "recent"),
)

@Serializable
data class HomeBroadcast(
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
data class HomeCache(
    val layout: HomeLayout = HomeLayout(),
    val dirty: Boolean = false,
    val broadcasts: List<HomeBroadcast> = emptyList(),
    @SerialName("include_reposts") val includeReposts: Boolean = true,
    @SerialName("include_tagged") val includeTagged: Boolean = true,
    @SerialName("broadcasts_enabled") val broadcastsEnabled: Boolean = true,
)

data class HomeCard(
    val id: String,
    val channelId: String,
    val channelName: String,
    val platform: String,
    val title: String,
    val text: String,
    val sortAtMs: Long,
    val reposterName: String = "",
    val isBookmarked: Boolean = false,
    val video: VideoGridItem? = null,
    val feed: FeedRow? = null,
    val broadcast: HomeBroadcast? = null,
) {
    val key: String get() = "${if (feed != null) "tweet" else if (broadcast != null) "live" else "video"}:$id"
}

data class HomeWidgetContent(val widget: HomeWidget, val cards: List<HomeCard>)

data class HomePlayback(val title: String, val url: String, val mimeType: String?)
