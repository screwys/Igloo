package com.screwy.igloo.net

import io.ktor.client.HttpClient
import io.ktor.client.call.body
import io.ktor.client.request.get
import io.ktor.client.request.parameter
import io.ktor.client.request.post
import io.ktor.client.request.delete
import io.ktor.client.request.prepareGet
import io.ktor.client.request.setBody
import io.ktor.client.plugins.HttpTimeoutConfig
import io.ktor.client.plugins.timeout
import io.ktor.client.statement.bodyAsChannel
import io.ktor.client.statement.bodyAsText
import io.ktor.http.ContentType
import io.ktor.http.contentType
import io.ktor.http.isSuccess
import io.ktor.utils.io.readLine
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flow
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.Serializable
import java.net.URLEncoder
import java.nio.charset.StandardCharsets

class MomentsApi(private val client: HttpClient, private val baseUrlProvider: () -> String) {
    suspend fun lives(): List<TikTokLive> =
        client.get(baseUrlProvider() + "/api/tiktok/lives").body<TikTokLives>().lives

    suspend fun stream(channelId: String): TikTokLiveStream =
        client.post(baseUrlProvider() + "/api/tiktok/lives/${pathSegment(channelId)}/stream") {
            contentType(ContentType.Application.Json)
            setBody(JsonObject(emptyMap()))
            timeout { requestTimeoutMillis = 130_000 }
        }.body()

    fun absoluteUrl(path: String): String = if (path.startsWith("/")) baseUrlProvider() + path else path

    suspend fun releaseStream(sessionId: String) {
        client.delete(baseUrlProvider() + "/api/streams/${pathSegment(sessionId)}")
    }

    fun chat(channelId: String): Flow<TikTokChatEvent> = flow {
        client.prepareGet(baseUrlProvider() + "/api/tiktok/lives/${pathSegment(channelId)}/chat") {
            timeout { requestTimeoutMillis = HttpTimeoutConfig.INFINITE_TIMEOUT_MS; socketTimeoutMillis = 60_000 }
        }.execute { response ->
            check(response.status.isSuccess()) { "Live chat request failed" }
            val channel = response.bodyAsChannel()
            var event = ""
            val data = StringBuilder()
            while (true) {
                val line = channel.readLine() ?: break
                when {
                    line.startsWith("event:") -> event = line.substringAfter(":").trim()
                    line.startsWith("data:") -> {
                        if (data.isNotEmpty()) data.append('\n')
                        data.append(line.substringAfter(":").trimStart())
                    }
                    line.isEmpty() -> {
                        if (event == "chat") emit(TikTokChatEvent.Comment(iglooJson.decodeFromString(data.toString())))
                        if (event == "end" || event == "failed" || event == "unavailable") {
                            emit(TikTokChatEvent.Ended)
                            return@execute
                        }
                        event = ""
                        data.clear()
                    }
                }
            }
        }
    }

    suspend fun subtitles(videoId: String): String? {
        val id = URLEncoder.encode(videoId, StandardCharsets.UTF_8.toString())
        val tracks = client.get(baseUrlProvider() + "/api/videos/$id/subtitles").body<MomentSubtitleTracks>()
        val track = tracks.tracks.firstOrNull() ?: return null
        return client.get(baseUrlProvider() + "/api/media/subtitle/$id") {
            parameter("track", track.track_id)
        }.bodyAsText()
    }
}

private fun pathSegment(value: String): String = URLEncoder.encode(value, StandardCharsets.UTF_8.toString())

@Serializable
private data class TikTokLives(val lives: List<TikTokLive> = emptyList())

@Serializable
data class TikTokLive(
    val channel_id: String,
    val room_id: String,
    val handle: String,
    val display_name: String = "",
    val title: String = "",
    val avatar_url: String = "",
    val viewer_count: Long = 0,
    val observed_at_ms: Long = 0,
)

@Serializable
data class TikTokLiveStream(
    val success: Boolean = false,
    val video_id: String,
    val bookmarked: Boolean = false,
    val bookmark_category_id: Long? = null,
    val manifest_url: String = "",
    val manifest_type: String = "",
    val session_id: String = "",
    val live: TikTokLive,
)

@Serializable
data class TikTokLiveComment(
    val id: String,
    val author_id: String = "",
    val author: String = "",
    val text: String = "",
    val timestamp_ms: Long = 0,
)

sealed interface TikTokChatEvent {
    data class Comment(val value: TikTokLiveComment) : TikTokChatEvent
    data object Ended : TikTokChatEvent
}

@Serializable
private data class MomentSubtitleTracks(val tracks: List<MomentSubtitleTrack> = emptyList())

@Serializable
private data class MomentSubtitleTrack(val track_id: String)
