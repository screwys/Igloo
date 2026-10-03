package com.screwy.igloo.data.entity

import androidx.room.ColumnInfo
import androidx.room.Entity
import androidx.room.Index
import androidx.room.PrimaryKey
import com.screwy.igloo.data.feedContentType
import com.screwy.igloo.data.feedHasContent
import com.screwy.igloo.data.feedHasMedia
import com.screwy.igloo.data.isMoment

@Entity(
    tableName = "feed_items",
    indices = [
        Index(value = ["published_at"], orders = [Index.Order.DESC], name = "idx_feed_items_published"),
        Index(value = ["reply_to_status"], name = "idx_feed_items_reply_parent"),
        Index(value = ["channel_id", "published_at"], orders = [Index.Order.ASC, Index.Order.DESC], name = "idx_feed_items_channel"),
        Index(value = ["quote_tweet_id"], name = "idx_feed_items_quote"),
        Index(value = ["content_hash"], name = "idx_feed_items_content_hash"),
        Index(value = ["canonical_tweet_id"], name = "idx_feed_items_canonical_tweet"),
    ],
)
data class StoredFeedItem(
    @PrimaryKey @ColumnInfo(name = "tweet_id") val tweetId: String,
    @ColumnInfo(name = "source_channel_id") val sourceChannelId: String?,
    @ColumnInfo(name = "channel_id") val channelId: String?,
    @ColumnInfo(name = "is_retweet") val isRetweet: Boolean,
    @ColumnInfo(name = "reposter_channel_id") val reposterChannelId: String?,
    @ColumnInfo(name = "quote_tweet_id") val quoteTweetId: String?,
    @ColumnInfo(name = "quote_channel_id") val quoteChannelId: String?,
    @ColumnInfo(name = "canonical_tweet_id") val canonicalTweetId: String?,
    @ColumnInfo(name = "reply_channel_id") val replyChannelId: String?,
    @ColumnInfo(name = "reply_to_status") val replyToStatus: String?,
    @ColumnInfo(name = "is_reply") val isReply: Boolean,
    @ColumnInfo(name = "is_ghost") val isGhost: Boolean,
    @ColumnInfo(name = "content_hash") val contentHash: String?,
    @ColumnInfo(name = "published_at") val publishedAt: Long,
    @ColumnInfo(name = "content_type") val contentType: String,
    @ColumnInfo(name = "has_content") val hasContent: Boolean,
    @ColumnInfo(name = "has_media") val hasMedia: Boolean,
    @ColumnInfo(name = "payload_json") val payloadJson: String,
) {
    companion object {
        fun from(item: FeedItemEntity, payloadJson: String) = StoredFeedItem(
            item.tweetId, item.sourceChannelId, item.channelId, item.isRetweet,
            item.reposterChannelId, item.quoteTweetId, item.quoteChannelId, item.canonicalTweetId,
            item.replyChannelId, item.replyToStatus, item.isReply, item.isGhost, item.contentHash,
            item.publishedAt, feedContentType(item.mediaJson), feedHasContent(item), feedHasMedia(item), payloadJson,
        )
    }
}

@Entity(
    tableName = "videos",
    indices = [
        Index(value = ["channel_id", "published_at"], orders = [Index.Order.ASC, Index.Order.DESC], name = "idx_videos_channel_published"),
        Index(value = ["source_kind", "published_at"], orders = [Index.Order.ASC, Index.Order.DESC], name = "idx_videos_source_kind"),
        Index(value = ["owner_kind", "published_at", "video_id"], orders = [Index.Order.ASC, Index.Order.DESC, Index.Order.DESC], name = "idx_videos_owner_published"),
    ],
)
data class StoredVideo(
    @PrimaryKey @ColumnInfo(name = "video_id") val videoId: String,
    @ColumnInfo(name = "channel_id") val channelId: String,
    @ColumnInfo(name = "owner_kind") val ownerKind: String,
    @ColumnInfo(name = "duration") val duration: Long?,
    @ColumnInfo(name = "published_at") val publishedAt: Long,
    @ColumnInfo(name = "is_temp", defaultValue = "0") val isTemp: Boolean,
    @ColumnInfo(name = "media_kind") val mediaKind: String?,
    @ColumnInfo(name = "source_kind") val sourceKind: String?,
    @ColumnInfo(name = "moments_all_position", defaultValue = "0") val momentsAllPosition: Long,
    @ColumnInfo(name = "moments_following_position", defaultValue = "0") val momentsFollowingPosition: Long,
    @ColumnInfo(name = "is_moment") val isMoment: Boolean,
    @ColumnInfo(name = "payload_json") val payloadJson: String,
) {
    companion object {
        fun from(item: VideoEntity, payloadJson: String) = StoredVideo(
            item.videoId, item.channelId, item.ownerKind, item.duration, item.publishedAt,
            item.isTemp, item.mediaKind, item.sourceKind, item.momentsAllPosition,
            item.momentsFollowingPosition, isMoment(item.ownerKind, item.metadataJson, item.duration), payloadJson,
        )
    }
}

@Entity(tableName = "channel_profiles")
data class StoredChannelProfile(
    @PrimaryKey @ColumnInfo(name = "channel_id") val channelId: String,
    @ColumnInfo(name = "platform") val platform: String,
    @ColumnInfo(name = "handle") val handle: String?,
    @ColumnInfo(name = "display_name") val displayName: String?,
    @ColumnInfo(name = "payload_json") val payloadJson: String,
) {
    companion object {
        fun from(item: ChannelProfileEntity, payloadJson: String) = StoredChannelProfile(
            item.channelId, item.platform, item.handle, item.displayName, payloadJson,
        )
    }
}

@Entity(tableName = "channels", indices = [Index(value = ["platform"], name = "idx_channels_platform")])
data class StoredChannel(
    @PrimaryKey @ColumnInfo(name = "channel_id") val channelId: String,
    @ColumnInfo(name = "source_id") val sourceId: String?,
    @ColumnInfo(name = "name") val name: String,
    @ColumnInfo(name = "url") val url: String?,
    @ColumnInfo(name = "platform") val platform: String,
    @ColumnInfo(name = "payload_json") val payloadJson: String,
) {
    companion object {
        fun from(item: ChannelEntity, payloadJson: String) = StoredChannel(
            item.channelId, item.sourceId, item.name, item.url, item.platform, payloadJson,
        )
    }
}
