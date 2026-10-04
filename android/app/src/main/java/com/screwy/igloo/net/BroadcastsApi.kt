package com.screwy.igloo.net

import com.screwy.igloo.videos.Broadcast
import io.ktor.client.HttpClient
import io.ktor.client.call.body
import io.ktor.client.plugins.timeout
import io.ktor.client.request.get
import io.ktor.client.request.post
import io.ktor.client.request.prepareGet
import io.ktor.client.request.setBody
import io.ktor.http.ContentType
import io.ktor.http.contentType
import io.ktor.http.isSuccess
import io.ktor.client.statement.bodyAsChannel
import io.ktor.client.plugins.HttpTimeoutConfig
import io.ktor.utils.io.readLine
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flow
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.Serializable
import java.net.URLEncoder
import java.nio.charset.StandardCharsets

class BroadcastsApi(private val client: HttpClient, private val baseUrlProvider: () -> String) {
    fun chat(videoId: String): Flow<BroadcastChatEvent> = flow {
        val id = URLEncoder.encode(videoId, StandardCharsets.UTF_8.toString())
        client.prepareGet(baseUrlProvider() + "/api/youtube/$id/chat") {
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
                        when (event) {
                            "start" -> emit(BroadcastChatEvent.Start(iglooJson.decodeFromString(data.toString())))
                            "chat" -> emit(BroadcastChatEvent.Record(iglooJson.decodeFromString(data.toString())))
                            "end" -> { emit(BroadcastChatEvent.Ended); return@execute }
                            "failed", "unavailable" -> { emit(BroadcastChatEvent.Unavailable); return@execute }
                        }
                        event = ""
                        data.clear()
                    }
                }
            }
        }
    }
    suspend fun broadcasts(baseUrl: String = baseUrlProvider()): BroadcastResponse =
        client.get(baseUrl + "/api/videos/broadcasts").body()

    suspend fun stream(videoId: String, baseUrl: String = baseUrlProvider(), forceFresh: Boolean = false): BroadcastStreamResponse =
        client.post(baseUrl + "/api/youtube/${URLEncoder.encode(videoId, StandardCharsets.UTF_8.toString())}/stream") {
            contentType(ContentType.Application.Json)
            setBody(BroadcastStreamRequest(force_fresh = forceFresh))
            timeout { requestTimeoutMillis = 130_000 }
        }.body()

    fun absoluteUrl(path: String, baseUrl: String = baseUrlProvider()): String =
        if (path.startsWith("/")) baseUrl + path else path
}

sealed interface BroadcastChatEvent {
    data class Start(val data: JsonObject) : BroadcastChatEvent
    data class Record(val data: JsonObject) : BroadcastChatEvent
    data object Ended : BroadcastChatEvent
    data object Unavailable : BroadcastChatEvent
}

@Serializable
data class BroadcastResponse(
    val broadcasts: List<Broadcast> = emptyList(),
    val include_reposts: Boolean = true,
    val include_tagged: Boolean = true,
    val broadcasts_enabled: Boolean = true,
)

@Serializable
private data class BroadcastStreamRequest(val prefer_indexed: Boolean = false, val force_fresh: Boolean = false)

@Serializable
data class BroadcastStreamResponse(
    val manifest_url: String? = null,
    val manifest_type: String? = null,
    val media_url: String? = null,
    val media_type: String? = null,
    val error_message: String? = null,
)
