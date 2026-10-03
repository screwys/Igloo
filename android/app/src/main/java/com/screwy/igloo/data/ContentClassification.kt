package com.screwy.igloo.data

import com.screwy.igloo.data.entity.FeedItemEntity
import com.screwy.igloo.feed.parseFeedMediaDescriptors
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.doubleOrNull

private val contentJson = Json { ignoreUnknownKeys = true; encodeDefaults = true }

fun feedContentType(mediaJson: String?): String {
    val media = parseFeedMediaDescriptors(mediaJson)
    return when {
        media.size > 1 -> "slideshow"
        media.singleOrNull()?.type?.lowercase() in listOf("video", "gif", "animated_gif") -> "video"
        media.size == 1 -> "image"
        else -> "post"
    }
}

fun isMoment(ownerKind: String, metadataJson: String?, duration: Long?): Boolean {
    if (ownerKind != "youtube_video") return true
    val metadata = metadataJson?.takeIf { it.isNotBlank() }?.let {
        runCatching { contentJson.parseToJsonElement(it) as? JsonObject }.getOrNull()
    }
    val metadataDuration = (metadata?.get("duration") as? JsonPrimitive)?.doubleOrNull
        ?: duration?.toDouble() ?: 0.0
    val width = (metadata?.get("width") as? JsonPrimitive)?.doubleOrNull ?: 0.0
    val height = (metadata?.get("height") as? JsonPrimitive)?.doubleOrNull ?: 0.0
    return (metadataDuration > 0 && metadataDuration <= 90) ||
        (width > 0 && height > 0 && height / width > 1.3)
}

fun feedHasContent(item: FeedItemEntity): Boolean =
    item.bodyText.orEmpty().trim(' ').isNotEmpty() ||
        item.articleTitle.orEmpty().trim(' ').isNotEmpty() ||
        item.pollJson.orEmpty() !in listOf("", "{}", "null") ||
        item.mediaJson.orEmpty() !in listOf("", "[]", "null") ||
        item.quoteBodyText.orEmpty().trim(' ').isNotEmpty() ||
        item.quoteArticleTitle.orEmpty().trim(' ').isNotEmpty() ||
        item.quotePollJson.orEmpty() !in listOf("", "{}", "null") ||
        item.quoteMediaJson.orEmpty() !in listOf("", "[]", "null")

fun feedHasMedia(item: FeedItemEntity): Boolean =
    item.mediaJson.orEmpty().trim(' ').isNotEmpty() ||
        item.quoteMediaJson.orEmpty().trim(' ').isNotEmpty()
