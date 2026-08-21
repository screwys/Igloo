package com.screwy.igloo.net

import io.ktor.client.HttpClient
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.respond
import io.ktor.http.ContentType
import io.ktor.http.HttpStatusCode
import io.ktor.http.Url
import io.ktor.http.headersOf
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Test

class AndroidSyncApiTest {
    @Test
    fun syncReadsSendTheirSelection() = runBlocking {
        val requests = mutableListOf<Url>()
        val client =
            HttpClient(
                MockEngine { request ->
                    requests += request.url
                    respond(
                        "{\"changes\":[],\"next_cursor\":\"cursor\",\"end_of_stream\":true}",
                        HttpStatusCode.OK,
                        headersOf("Content-Type", ContentType.Application.Json.toString()),
                    )
                },
            )
        try {
            val api = AndroidSyncApi(client) { "https://igloo.example" }
            val retention = AndroidSyncRetentionRequest(7, 14, 7, 48)

            api.bootstrap(retention, after = null)
            api.changes(retention, after = "cursor")
            api.priorityState(retention, after = "cursor")

            assertEquals(
                listOf(
                    Triple("/api/android/sync/bootstrap", "1", "3"),
                    Triple("/api/android/sync/changes", "1", "3"),
                    Triple("/api/android/sync/state", null, "3"),
                ),
                requests.map {
                    Triple(
                        it.encodedPath,
                        it.parameters["full_youtube_metadata"],
                        it.parameters["model_version"],
                    )
                },
            )
            val priority = requests.last().parameters
            assertEquals("7", priority["feed_days"])
            assertEquals("14", priority["youtube_days"])
            assertEquals("7", priority["moments_days"])
            assertEquals("48", priority["story_hours"])
        } finally {
            client.close()
        }
    }
}
