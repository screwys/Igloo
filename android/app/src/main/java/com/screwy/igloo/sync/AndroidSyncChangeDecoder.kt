package com.screwy.igloo.sync

import com.screwy.igloo.data.cleaned
import com.screwy.igloo.data.ContentPayloads
import com.screwy.igloo.data.entity.BookmarkCategoryEntity
import com.screwy.igloo.data.entity.BookmarkEntity
import com.screwy.igloo.data.entity.ChannelEntity
import com.screwy.igloo.data.entity.ChannelFollowEntity
import com.screwy.igloo.data.entity.ChannelProfileEntity
import com.screwy.igloo.data.entity.ChannelSettingEntity
import com.screwy.igloo.data.entity.ChannelStarEntity
import com.screwy.igloo.data.entity.FeedItemEntity
import com.screwy.igloo.data.entity.FeedLikeEntity
import com.screwy.igloo.data.entity.FeedRankEntity
import com.screwy.igloo.data.entity.FeedSeenEntity
import com.screwy.igloo.data.entity.MomentViewEntity
import com.screwy.igloo.data.entity.MomentsCursorEntity
import com.screwy.igloo.data.entity.MutedChannelEntity
import com.screwy.igloo.data.entity.RetweetSourceEntity
import com.screwy.igloo.data.entity.SponsorBlockCheckedEntity
import com.screwy.igloo.data.entity.SponsorBlockSegmentEntity
import com.screwy.igloo.data.entity.VideoCommentEntity
import com.screwy.igloo.data.entity.VideoEntity
import com.screwy.igloo.data.entity.VideoRepostSourceEntity
import com.screwy.igloo.data.entity.WatchHistoryEntity
import com.screwy.igloo.data.entity.StoredFeedItem
import com.screwy.igloo.net.AndroidSyncAssetDto
import com.screwy.igloo.net.AndroidSyncChangeDto
import com.screwy.igloo.net.iglooJson
import java.security.MessageDigest
import kotlinx.serialization.ExperimentalSerializationApi
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonNamingStrategy
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.decodeFromJsonElement

internal data class AndroidVideoUpsert(
    val item: VideoEntity,
    val comments: List<VideoCommentEntity>,
    val sponsorBlockSegments: List<SponsorBlockSegmentEntity>,
    val sponsorBlockChecked: SponsorBlockCheckedEntity?,
    val repostSources: List<VideoRepostSourceEntity>,
    val ownerPayloadJson: String,
)

internal data class AndroidChannelUpsert(
    val channel: ChannelEntity?,
    val profile: ChannelProfileEntity?,
    val ownerPayloadJson: String,
)

internal data class AndroidFeedRankSnapshot(
    val snapshotAt: Long,
    val rows: List<FeedRankEntity>,
)

internal object AndroidSyncChangeDecoder {
    fun feed(change: AndroidSyncChangeDto): FeedItemEntity =
        change.decode<FeedPayload>().item.cleaned().also { require(it.tweetId == change.owner_id) }

    fun captureFeed(change: AndroidSyncChangeDto): StoredFeedItem {
        val item = feed(change)
        return StoredFeedItem.from(item, ContentPayloads.feed(item, change.ownerPayload()))
    }

    fun video(change: AndroidSyncChangeDto): AndroidVideoUpsert {
        val payload = change.decode<VideoPayload>()
        val item = payload.item.cleaned()
        require(item.videoId == change.owner_id) { "video owner id mismatch" }
        return AndroidVideoUpsert(
            item,
            payload.comments.map { it.toEntity(item.videoId) },
            payload.sponsorBlockSegments.map { it.toEntity(item.videoId) },
            payload.sponsorBlockChecked?.toEntity(item.videoId),
            payload.repostSources.map { it.toEntity(item.videoId) },
            ContentPayloads.video(item, change.ownerPayload()),
        )
    }

    fun channel(change: AndroidSyncChangeDto): AndroidChannelUpsert {
        val payload = change.decode<ChannelPayload>()
        require(payload.channel != null || payload.profile != null) { "empty channel owner" }
        require(payload.channel?.channelId in setOf(null, change.owner_id)) { "channel owner id mismatch" }
        require(payload.profile?.channelId in setOf(null, change.owner_id)) { "profile owner id mismatch" }
        val channel = payload.channel?.cleaned()
        val profile = payload.profile?.cleaned()
        return AndroidChannelUpsert(channel, profile, ContentPayloads.channel(channel, profile, change.ownerPayload()))
    }

    fun retweetSources(change: AndroidSyncChangeDto): List<RetweetSourceEntity> =
        change.decode<RetweetSourcesPayload>().rows.also { rows ->
            require(rows.all { it.contentHash == change.owner_id }) { "retweet source owner id mismatch" }
        }

    fun feedRank(change: AndroidSyncChangeDto): FeedRankEntity =
        change.decode<FeedRankEntity>().also { require(it.tweetId == change.owner_id) }

    fun feedRankSnapshot(change: AndroidSyncChangeDto): AndroidFeedRankSnapshot {
        require(change.owner_id == "main") { "feed rank snapshot owner id mismatch" }
        val payload = change.decode<FeedRankSnapshotPayload>()
        require(payload.snapshotAt > 0) { "feed rank snapshot time is missing" }
        require(payload.rows.size <= 5_000) { "feed rank snapshot exceeds the row limit" }
        val rows =
            payload.rows.map { row ->
                require(row.tweetId.isNotBlank()) { "feed rank snapshot has an empty owner" }
                require(row.rankPosition >= 0) { "feed rank snapshot has a negative position" }
                FeedRankEntity(row.tweetId, row.rankPosition, payload.snapshotAt)
            }
        require(rows.map(FeedRankEntity::tweetId).distinct().size == rows.size) {
            "feed rank snapshot has duplicate owners"
        }
        require(
            rows.zipWithNext().all { (before, after) ->
                before.rankPosition < after.rankPosition ||
                    (before.rankPosition == after.rankPosition && before.tweetId < after.tweetId)
            }
        ) { "feed rank snapshot is not ordered" }
        require(payload.digest == feedRankSnapshotDigest(rows)) {
            "feed rank snapshot digest mismatch"
        }
        return AndroidFeedRankSnapshot(payload.snapshotAt, rows)
    }

    fun asset(change: AndroidSyncChangeDto): AndroidSyncAssetDto =
        change.decode<AndroidSyncAssetDto>().also { require(it.asset_id == change.owner_id) }

    fun feedLike(change: AndroidSyncChangeDto): FeedLikeEntity = change.row { it.tweetId }
    fun bookmark(change: AndroidSyncChangeDto): BookmarkEntity = change.row { it.videoId }
    fun bookmarkCategory(change: AndroidSyncChangeDto): BookmarkCategoryEntity =
        change.row { it.categoryId.toString() }
    fun feedSeen(change: AndroidSyncChangeDto): FeedSeenEntity = change.row { it.tweetId }
    fun momentView(change: AndroidSyncChangeDto): MomentViewEntity = change.row { it.videoId }
    fun watchHistory(change: AndroidSyncChangeDto): WatchHistoryEntity = change.row { it.videoId }
    fun mutedChannel(change: AndroidSyncChangeDto): MutedChannelEntity = change.row { it.channelId }
    fun channelFollow(change: AndroidSyncChangeDto): ChannelFollowEntity = change.row { it.channelId }
    fun channelStar(change: AndroidSyncChangeDto): ChannelStarEntity = change.row { it.channelId }
    fun channelSetting(change: AndroidSyncChangeDto): ChannelSettingEntity =
        change.row<ChannelSettingEntity> { it.channelId }.also { row ->
            require(
                row.mediaOnly != null || row.includeReposts != null ||
                    row.mediaDownloadLimit != null || row.maxVideos != null ||
                    row.downloadSubtitles != null || row.includeMemberOnly != null
            ) { "empty channel setting upsert" }
        }
    fun momentsCursor(change: AndroidSyncChangeDto): MomentsCursorEntity = change.row { it.scope }
    fun setting(change: AndroidSyncChangeDto): String? =
        change.decode<SettingPayload>().also {
            require(it.key == change.owner_id) { "setting owner id mismatch" }
        }.value

    private inline fun <reified T> AndroidSyncChangeDto.row(id: (T) -> String): T =
        decode<T>().also { require(id(it) == owner_id) { "$owner_kind owner id mismatch" } }

    private inline fun <reified T> AndroidSyncChangeDto.decode(): T {
        require(operation == "upsert") { "cannot decode delete payload" }
        return syncPayloadJson.decodeFromJsonElement(requireNotNull(payload) { "missing $owner_kind payload" })
    }

    private fun AndroidSyncChangeDto.ownerPayload(): JsonObject =
        requireNotNull(payload) { "missing $owner_kind payload" }
}

@Serializable private data class FeedPayload(val item: FeedItemEntity)

@Serializable
private data class VideoPayload(
    val item: VideoEntity,
    val comments: List<CommentPayload>,
    @SerialName("sponsorblock_segments")
    val sponsorBlockSegments: List<SponsorBlockSegmentPayload>,
    @SerialName("sponsorblock_checked")
    val sponsorBlockChecked: SponsorBlockCheckedPayload?,
    val repostSources: List<VideoRepostSourcePayload>,
)

@Serializable
private data class CommentPayload(
    @SerialName("id") val id: String,
    @SerialName("parent") val parent: String,
    @SerialName("author") val author: String,
    val authorId: String,
    val text: String,
    val likeCount: Long,
    val publishedAt: Long,
) {
    fun toEntity(videoId: String) =
        VideoCommentEntity(
            videoId,
            id,
            parent.clean(),
            author.clean(),
            authorId.canonicalYouTubeCommentAuthorId(),
            text.clean(),
            likeCount,
            publishedAt,
        )
}

@Serializable private data class SponsorBlockSegmentPayload(val start: Double, val end: Double, val category: String) {
    fun toEntity(videoId: String) = SponsorBlockSegmentEntity(videoId, start, end, category)
}

@Serializable
private data class SponsorBlockCheckedPayload(val checkedAtMs: Long, val videoAgeAtCheck: String) {
    fun toEntity(videoId: String) = SponsorBlockCheckedEntity(videoId, checkedAtMs, videoAgeAtCheck.clean())
}

@Serializable
private data class VideoRepostSourcePayload(
    val reposterChannelId: String,
    val repostedAtMs: Long,
    val firstSeenAtMs: Long,
    val updatedAtMs: Long,
) {
    fun toEntity(videoId: String) =
        VideoRepostSourceEntity(videoId, reposterChannelId, repostedAtMs, firstSeenAtMs, updatedAtMs)
}

@Serializable
private data class ChannelPayload(
    val channel: ChannelEntity?,
    val profile: ChannelProfileEntity?,
)

@Serializable private data class RetweetSourcesPayload(val rows: List<RetweetSourceEntity>)
@Serializable
private data class FeedRankSnapshotPayload(
    val snapshotAt: Long,
    val digest: String,
    val rows: List<FeedRankSnapshotRow>,
)
@Serializable private data class FeedRankSnapshotRow(val tweetId: String, val rankPosition: Int)
@Serializable private data class SettingPayload(val key: String, val value: String?)

internal fun feedRankSnapshotDigest(rows: List<FeedRankEntity>): String {
    val digest = MessageDigest.getInstance("SHA-256")
    rows.forEach { row ->
        digest.update(row.tweetId.toByteArray(Charsets.UTF_8))
        digest.update(0.toByte())
        digest.update(row.rankPosition.toString().toByteArray(Charsets.UTF_8))
        digest.update('\n'.code.toByte())
    }
    return digest.digest().joinToString("") { byte -> "%02x".format(byte.toInt() and 0xff) }
}

private fun String?.clean(): String? = this?.trim()?.takeIf(String::isNotEmpty)

private fun String?.canonicalYouTubeCommentAuthorId(): String? {
    val value = clean() ?: return null
    val channelId = value.removePrefix("youtube_").trim()
    return if (channelId.startsWith("UC")) "youtube_$channelId" else value
}

@OptIn(ExperimentalSerializationApi::class)
private val syncPayloadJson = Json(iglooJson) { namingStrategy = JsonNamingStrategy.SnakeCase }
