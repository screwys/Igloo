package com.screwy.igloo.net

import com.screwy.igloo.home.HomeBroadcast
import com.screwy.igloo.home.HomeLayout
import io.ktor.client.HttpClient
import io.ktor.client.call.body
import io.ktor.client.plugins.timeout
import io.ktor.client.request.get
import io.ktor.client.request.post
import io.ktor.client.request.put
import io.ktor.client.request.setBody
import io.ktor.http.ContentType
import io.ktor.http.contentType
import kotlinx.serialization.Serializable
import java.net.URLEncoder
import java.nio.charset.StandardCharsets

class HomeApi(private val client: HttpClient, private val baseUrlProvider: () -> String) {
    suspend fun layout(baseUrl: String = baseUrlProvider()): HomeLayout = client.get(baseUrl + "/api/home/layout").body()

    suspend fun saveLayout(layout: HomeLayout, baseUrl: String = baseUrlProvider()) {
        client.put(baseUrl + "/api/home/layout") {
            contentType(ContentType.Application.Json)
            setBody(layout)
        }.body<HomeSaveResponse>().also { check(it.success) }
    }

    suspend fun broadcasts(baseUrl: String = baseUrlProvider()): HomeBroadcastResponse =
        client.get(baseUrl + "/api/home/broadcasts").body()

    suspend fun stream(videoId: String, baseUrl: String = baseUrlProvider()): HomeStreamResponse =
        client.post(baseUrl + "/api/youtube/${URLEncoder.encode(videoId, StandardCharsets.UTF_8.toString())}/stream") {
            contentType(ContentType.Application.Json)
            setBody(HomeStreamRequest())
            timeout { requestTimeoutMillis = 130_000 }
        }.body()

    fun absoluteUrl(path: String, baseUrl: String = baseUrlProvider()): String =
        if (path.startsWith("/")) baseUrl + path else path
}

@Serializable
private data class HomeSaveResponse(val success: Boolean)

@Serializable
data class HomeBroadcastResponse(
    val broadcasts: List<HomeBroadcast> = emptyList(),
    val include_reposts: Boolean = true,
    val include_tagged: Boolean = true,
    val broadcasts_enabled: Boolean = true,
)

@Serializable
private data class HomeStreamRequest(val prefer_indexed: Boolean = false)

@Serializable
data class HomeStreamResponse(
    val manifest_url: String? = null,
    val manifest_type: String? = null,
    val media_url: String? = null,
    val media_type: String? = null,
    val error_message: String? = null,
)
