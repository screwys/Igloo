package com.screwy.igloo.data

import androidx.room.TypeConverter
import com.screwy.igloo.data.entity.ChannelEntity
import com.screwy.igloo.data.entity.ChannelProfileEntity
import com.screwy.igloo.data.entity.FeedItemEntity
import com.screwy.igloo.data.entity.VideoEntity
import com.screwy.igloo.net.iglooJson
import kotlinx.serialization.ExperimentalSerializationApi
import kotlinx.serialization.Serializable
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNamingStrategy
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.jsonObject

class ContentPayloadConverters {
    @TypeConverter fun feedFromPayload(value: String): FeedItemEntity = ContentPayloads.feed(value)
    @TypeConverter fun videoFromPayload(value: String): VideoEntity = ContentPayloads.video(value)
    @TypeConverter fun channelFromPayload(value: String): ChannelEntity = ContentPayloads.channel(value)
    @TypeConverter fun profileFromPayload(value: String): ChannelProfileEntity = ContentPayloads.profile(value)
}

object ContentPayloads {
    fun feed(value: String): FeedItemEntity =
        payloadJson.decodeFromString<FeedOwnerPayload>(value).item

    fun video(value: String): VideoEntity =
        payloadJson.decodeFromString<VideoOwnerPayload>(value).item

    fun channel(value: String): ChannelEntity =
        payloadJson.decodeFromString<ChannelOwnerPayload>(value).channel

    fun profile(value: String): ChannelProfileEntity =
        payloadJson.decodeFromString<ProfileOwnerPayload>(value).profile

    fun feed(item: FeedItemEntity, ownerPayload: JsonObject?): String = merge("item", payloadJson.encodeToJsonElement(item), ownerPayload).toString()
    fun video(item: VideoEntity, ownerPayload: JsonObject?): String = merge("item", payloadJson.encodeToJsonElement(item), ownerPayload).toString()
    fun channel(item: ChannelEntity, ownerPayload: JsonObject?): String = merge("channel", payloadJson.encodeToJsonElement(item), ownerPayload).toString()
    fun profile(item: ChannelProfileEntity, ownerPayload: JsonObject?): String = merge("profile", payloadJson.encodeToJsonElement(item), ownerPayload).toString()

    fun channel(channel: ChannelEntity?, profile: ChannelProfileEntity?, ownerPayload: JsonObject): String {
        var owner = ownerPayload
        if (channel != null) owner = merge("channel", payloadJson.encodeToJsonElement(channel), owner)
        if (profile != null) owner = merge("profile", payloadJson.encodeToJsonElement(profile), owner)
        return owner.toString()
    }

    private fun merge(member: String, value: JsonElement, ownerPayload: JsonObject?): JsonObject {
        val owner = ownerPayload ?: JsonObject(emptyMap())
        val existing = owner[member]?.takeUnless { it == JsonNull }?.jsonObject ?: JsonObject(emptyMap())
        return JsonObject(owner + (member to JsonObject(existing + value.jsonObject)))
    }
}

@Serializable private data class FeedOwnerPayload(val item: FeedItemEntity)
@Serializable private data class VideoOwnerPayload(val item: VideoEntity)
@Serializable private data class ChannelOwnerPayload(val channel: ChannelEntity)
@Serializable private data class ProfileOwnerPayload(val profile: ChannelProfileEntity)

internal fun FeedItemEntity.cleaned() = copy(
    sourceChannelId = sourceChannelId.clean(), bodyText = bodyText.clean(), lang = lang.clean(),
    articleTitle = articleTitle.clean(), quoteArticleTitle = quoteArticleTitle.clean(),
    pollJson = pollJson.clean(), quotePollJson = quotePollJson.clean(),
    communityNote = communityNote.clean(), quoteCommunityNote = quoteCommunityNote.clean(),
    reposterChannelId = reposterChannelId.clean(), quoteTweetId = quoteTweetId.clean(),
    quoteChannelId = quoteChannelId.clean(), quoteBodyText = quoteBodyText.clean(),
    quoteLang = quoteLang.clean(), quoteMediaJson = quoteMediaJson.clean(),
    quoteCanonicalUrl = quoteCanonicalUrl.clean(), mediaJson = mediaJson.clean(),
    canonicalUrl = canonicalUrl.clean(), canonicalTweetId = canonicalTweetId.clean(),
    replyChannelId = replyChannelId.clean(), replyToStatus = replyToStatus.clean(),
    contentHash = contentHash.clean(), bodyTranslation = bodyTranslation.clean(),
    bodySourceLang = bodySourceLang.clean(), quoteTranslation = quoteTranslation.clean(),
    quoteSourceLang = quoteSourceLang.clean(), channelId = channelId.clean(),
)

internal fun VideoEntity.cleaned() = copy(
    title = title.clean(), description = description.clean(), mediaKind = mediaKind.clean(),
    sourceKind = sourceKind.clean(), metadataJson = metadataJson.clean(), canonicalUrl = canonicalUrl.clean(),
    dearrowTitle = dearrowTitle.clean(), dearrowTitleCasual = dearrowTitleCasual.clean(),
)

internal fun ChannelEntity.cleaned() = copy(sourceId = sourceId.clean(), url = url.clean())

internal fun ChannelProfileEntity.cleaned() = copy(
    handle = handle.clean(), displayName = displayName.clean(), bio = bio.clean(),
    website = website.clean(), verifiedType = verifiedType.clean(), accountRegion = accountRegion.clean(),
    accountDetailsJson = accountDetailsJson.clean(),
)

private fun String?.clean(): String? = this?.trim()?.takeIf(String::isNotEmpty)

@OptIn(ExperimentalSerializationApi::class)
private val payloadJson = Json(iglooJson) { namingStrategy = JsonNamingStrategy.SnakeCase }
