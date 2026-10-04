package com.screwy.igloo.net

import com.screwy.igloo.videos.Broadcast
import io.ktor.client.HttpClient
import io.ktor.client.call.body
import io.ktor.client.plugins.timeout
import io.ktor.client.request.get
import io.ktor.client.request.post
import io.ktor.client.request.setBody
import io.ktor.http.ContentType
import io.ktor.http.contentType
import kotlinx.serialization.Serializable
import java.net.URLEncoder
import java.nio.charset.StandardCharsets

class BroadcastsApi(private val client: HttpClient, private val baseUrlProvider: () -> String) {
    suspend fun broadcasts(baseUrl: String = baseUrlProvider()): BroadcastResponse =
        client.get(baseUrl + "/api/videos/broadcasts").body()

    suspend fun stream(videoId: String, baseUrl: String = baseUrlProvider()): BroadcastStreamResponse =
        client.post(baseUrl + "/api/youtube/${URLEncoder.encode(videoId, StandardCharsets.UTF_8.toString())}/stream") {
            contentType(ContentType.Application.Json)
            setBody(BroadcastStreamRequest())
            timeout { requestTimeoutMillis = 130_000 }
        }.body()

    fun absoluteUrl(path: String, baseUrl: String = baseUrlProvider()): String =
        if (path.startsWith("/")) baseUrl + path else path
}

@Serializable
data class BroadcastResponse(
    val broadcasts: List<Broadcast> = emptyList(),
    val include_reposts: Boolean = true,
    val include_tagged: Boolean = true,
    val broadcasts_enabled: Boolean = true,
)

@Serializable
private data class BroadcastStreamRequest(val prefer_indexed: Boolean = false)

@Serializable
data class BroadcastStreamResponse(
    val manifest_url: String? = null,
    val manifest_type: String? = null,
    val media_url: String? = null,
    val media_type: String? = null,
    val error_message: String? = null,
)
