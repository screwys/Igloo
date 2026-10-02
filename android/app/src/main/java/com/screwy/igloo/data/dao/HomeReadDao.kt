package com.screwy.igloo.data.dao

import androidx.room.ColumnInfo
import androidx.room.Dao
import androidx.room.Embedded
import androidx.room.Query
import androidx.room.RewriteQueriesToDropUnusedColumns
import com.screwy.igloo.data.entity.FeedRow
import com.screwy.igloo.data.entity.VideoGridItem
import kotlinx.coroutines.flow.Flow

data class HomeVideoRow(
    @Embedded val item: VideoGridItem,
    @ColumnInfo(name = "home_sort_at_ms") val sortAtMs: Long,
    @ColumnInfo(name = "home_platform") val platform: String,
    @ColumnInfo(name = "home_reposter_name") val reposterName: String?,
    @ColumnInfo(name = "home_custom_title") val customTitle: String?,
    @ColumnInfo(name = "home_is_bookmarked") val isBookmarked: Int,
)

data class HomeFeedRow(
    @Embedded val item: FeedRow,
    @ColumnInfo(name = "home_sort_at_ms") val sortAtMs: Long,
    @ColumnInfo(name = "home_reposter_name") val reposterName: String?,
)

private const val HOME_FEED_HAS_CONTENT = """
    NULLIF(TRIM(COALESCE(fi.body_text, '')), '') IS NOT NULL
    OR NULLIF(TRIM(COALESCE(fi.article_title, '')), '') IS NOT NULL
    OR COALESCE(fi.poll_json, '') NOT IN ('', '{}', 'null')
    OR COALESCE(fi.media_json, '') NOT IN ('', '[]', 'null')
    OR NULLIF(TRIM(COALESCE(fi.quote_body_text, '')), '') IS NOT NULL
    OR NULLIF(TRIM(COALESCE(fi.quote_article_title, '')), '') IS NOT NULL
    OR COALESCE(fi.quote_poll_json, '') NOT IN ('', '{}', 'null')
    OR COALESCE(fi.quote_media_json, '') NOT IN ('', '[]', 'null')
"""

private const val HOME_VIDEO_QUERY = """
        WITH tweet_video_feeds AS (
            SELECT v.video_id, fi.tweet_id, fi.canonical_tweet_id, fi.content_hash,
                   fi.channel_id, fi.source_channel_id, fi.reposter_channel_id, fi.published_at
            FROM videos v
            JOIN feed_items anchor ON anchor.tweet_id = v.video_id OR anchor.canonical_tweet_id = v.video_id
            JOIN feed_items fi ON fi.tweet_id = anchor.tweet_id
              OR fi.canonical_tweet_id = anchor.tweet_id OR fi.tweet_id = anchor.canonical_tweet_id
              OR (COALESCE(anchor.canonical_tweet_id, '') != '' AND fi.canonical_tweet_id = anchor.canonical_tweet_id)
              OR (COALESCE(anchor.content_hash, '') != '' AND fi.content_hash = anchor.content_hash)
            WHERE v.owner_kind = 'tweet'
        ),
        video_sources AS (
            SELECT video_id, reposter_channel_id,
                   COALESCE(NULLIF(reposted_at_ms, 0), first_seen_at_ms) AS source_at_ms FROM video_repost_sources
            UNION
            SELECT f.video_id, f.source_channel_id, f.published_at FROM tweet_video_feeds f
            JOIN videos owner ON owner.video_id = f.video_id
            WHERE COALESCE(f.source_channel_id, '') != '' AND f.source_channel_id != owner.channel_id
            UNION
            SELECT f.video_id, f.reposter_channel_id, f.published_at FROM tweet_video_feeds f
            WHERE COALESCE(f.reposter_channel_id, '') != ''
            UNION
            SELECT f.video_id, f.channel_id, f.published_at FROM tweet_video_feeds f
            JOIN videos owner ON owner.video_id = f.video_id WHERE f.channel_id != owner.channel_id
            UNION
            SELECT v.video_id, rs.retweeter_channel_id, rs.published_at
            FROM videos v JOIN retweet_sources rs ON rs.tweet_id = v.video_id WHERE v.owner_kind = 'tweet'
            UNION
            SELECT f.video_id, rs.retweeter_channel_id, rs.published_at FROM tweet_video_feeds f
            JOIN retweet_sources rs ON rs.tweet_id = f.tweet_id
                OR (COALESCE(f.content_hash, '') != '' AND rs.content_hash = f.content_hash)
        ),
        eligible_reposts AS (
            SELECT rs.video_id, rs.reposter_channel_id,
                   COALESCE(NULLIF(rp.display_name, ''), rc.name, rp.handle, '') AS reposter_name,
                   ROW_NUMBER() OVER (PARTITION BY rs.video_id ORDER BY
                       CASE WHEN rs_cs.channel_id IS NOT NULL THEN 0 ELSE 1 END,
                       rs.source_at_ms DESC,
                       rs.reposter_channel_id) AS source_rank
            FROM video_sources rs
            JOIN videos source_video ON source_video.video_id = rs.video_id
            LEFT JOIN channel_profiles rp ON rp.channel_id = rs.reposter_channel_id
            LEFT JOIN channels rc ON rc.channel_id = rs.reposter_channel_id
            LEFT JOIN channel_stars rs_cs ON rs_cs.channel_id = rs.reposter_channel_id
            LEFT JOIN channel_settings rs_settings ON rs_settings.channel_id = rs.reposter_channel_id
            LEFT JOIN muted_channels rs_muted ON rs_muted.channel_id = rs.reposter_channel_id
            WHERE COALESCE(rs_settings.include_reposts, 1) != 0
              AND (:type = 'account' OR rs_muted.channel_id IS NULL)
              AND (source_video.owner_kind != 'tiktok_video' OR :includeReposts)
              AND (source_video.owner_kind != 'instagram_reel' OR :includeTagged)
              AND (:allChannels OR rs.reposter_channel_id IN (:channels))
              AND (NOT :allChannels OR EXISTS (
                  SELECT 1 FROM channel_follows rs_cf WHERE rs_cf.channel_id = rs.reposter_channel_id))
              AND ((:type != 'starred' AND NOT :starredOnly) OR rs_cs.channel_id IS NOT NULL)
        )
        SELECT v.*,
               CASE WHEN v.owner_kind = 'tweet' THEN NULL ELSE wh.playback_position END AS wh_playback_position,
               CASE WHEN v.owner_kind = 'tweet' THEN NULL ELSE wh.duration END AS wh_duration,
               COALESCE(NULLIF(cp.display_name, ''), c.name, cp.handle, '') AS channel_name,
               c.source_id AS channel_source_id,
               b.custom_title AS home_custom_title,
               CASE WHEN b.video_id IS NOT NULL THEN 1 ELSE 0 END AS home_is_bookmarked,
               er.reposter_name AS home_reposter_name,
               COALESCE(c.platform, cp.platform, CASE v.owner_kind
                   WHEN 'youtube_video' THEN 'youtube'
                   WHEN 'tweet' THEN 'twitter'
                   WHEN 'tiktok_video' THEN 'tiktok'
                   WHEN 'instagram_reel' THEN 'instagram' ELSE '' END) AS home_platform,
               CASE WHEN :type = 'continue' AND :ordering != 'newest' THEN COALESCE(wh.updated_at_ms, 0)
                    WHEN :type = 'saved' AND :ordering != 'newest' THEN COALESCE(b.bookmarked_at, 0)
                    ELSE v.published_at END AS home_sort_at_ms
        FROM videos v
        LEFT JOIN channels c ON c.channel_id = v.channel_id
        LEFT JOIN channel_profiles cp ON cp.channel_id = v.channel_id
        LEFT JOIN channel_stars cs ON cs.channel_id = v.channel_id
        LEFT JOIN watch_history wh ON wh.video_id = v.video_id
        LEFT JOIN bookmarks b ON b.video_id = v.video_id
        LEFT JOIN eligible_reposts er ON er.video_id = v.video_id AND er.source_rank = 1
        WHERE (:allPlatforms OR COALESCE(c.platform, cp.platform, CASE v.owner_kind
                   WHEN 'youtube_video' THEN 'youtube'
                   WHEN 'tweet' THEN 'twitter'
                   WHEN 'tiktok_video' THEN 'tiktok'
                   WHEN 'instagram_reel' THEN 'instagram' ELSE '' END) IN (:platforms))
          AND (:type != 'account' OR NOT :allChannels)
          AND (:type NOT IN ('continue', 'latest') OR (v.owner_kind != 'tweet' AND COALESCE(c.platform, cp.platform,
              CASE WHEN v.owner_kind = 'tweet' THEN 'twitter' ELSE '' END) != 'twitter'))
          AND (:type IN ('saved', 'continue') OR NOT :allChannels OR er.video_id IS NOT NULL
              OR EXISTS (SELECT 1 FROM channel_follows cf WHERE cf.channel_id = v.channel_id))
          AND (:type IN ('continue', 'latest') OR NOT EXISTS (SELECT 1 FROM feed_items fi
              WHERE (fi.tweet_id = v.video_id OR fi.canonical_tweet_id = v.video_id)
                AND COALESCE(fi.is_ghost, 0) = 0 AND (""" + HOME_FEED_HAS_CONTENT + """)))
          AND (:allChannels OR v.channel_id IN (:channels) OR er.video_id IS NOT NULL)
          AND (:allContent OR CASE WHEN v.source_kind = 'story' THEN 'story'
              WHEN v.media_kind = 'slideshow' THEN 'slideshow'
              WHEN v.media_kind = 'image' THEN 'image' ELSE 'video' END IN (:contentTypes))
          AND ((:type != 'starred' AND NOT :starredOnly) OR cs.channel_id IS NOT NULL OR er.video_id IS NOT NULL)
          AND (:type != 'saved' OR b.video_id IS NOT NULL)
          AND (:type != 'continue' OR (
              wh.playback_position > 0
              AND (COALESCE(wh.duration, v.duration, 0) <= 0
                  OR wh.playback_position < COALESCE(wh.duration, v.duration) * 0.95)))
          AND (:type != 'moments' OR v.owner_kind IN ('tiktok_video', 'instagram_reel', 'youtube_video'))
          AND (:type != 'latest' OR COALESCE(v.media_kind, 'video') NOT IN ('image', 'slideshow'))
          AND (:type IN ('saved', 'continue') OR COALESCE(v.source_kind, '') != 'story'
              OR v.published_at >= :storyCutoffMs)
          AND (:type IN ('saved', 'account', 'continue') OR NOT EXISTS (
              SELECT 1 FROM muted_channels mc WHERE mc.channel_id = v.channel_id))
        ORDER BY CASE WHEN :ordering = 'account' THEN channel_name END COLLATE NOCASE ASC,
                 home_sort_at_ms DESC, v.video_id DESC
        LIMIT :limit OFFSET :offset
        """

private const val HOME_FEED_QUERY = """
    WITH bookmark_state AS (
        SELECT fi2.tweet_id, b.video_id, b.category_id, b.custom_title, b.bookmarked_at,
               b.account_handles, b.media_indices,
               ROW_NUMBER() OVER (PARTITION BY fi2.tweet_id
                 ORDER BY CASE WHEN b.video_id = fi2.tweet_id THEN 0 ELSE 1 END,
                          b.bookmarked_at DESC, b.video_id DESC) AS bookmark_rank
        FROM feed_items fi2
        JOIN feed_items sibling ON sibling.tweet_id = fi2.tweet_id OR (
            NULLIF(TRIM(COALESCE(fi2.content_hash, '')), '') IS NOT NULL
            AND sibling.content_hash = fi2.content_hash)
        JOIN bookmarks b ON b.video_id = sibling.tweet_id
    ),
    source_links AS (
        SELECT tweet_id, content_hash, source_channel_id AS source_id, published_at
        FROM feed_items WHERE source_channel_id != COALESCE(channel_id, '')
        UNION
        SELECT tweet_id, content_hash, reposter_channel_id AS source_id, published_at
        FROM feed_items WHERE reposter_channel_id != COALESCE(channel_id, '')
        UNION
        SELECT tweet_id, content_hash, retweeter_channel_id AS source_id, published_at FROM retweet_sources
    ),
    eligible_sources AS (
        SELECT l.*,
               COALESCE(NULLIF(sp.display_name, ''), sc.name, sp.handle, '') AS source_name,
               ROW_NUMBER() OVER (PARTITION BY CASE WHEN COALESCE(l.content_hash, '') != ''
                    THEN 'hash:' || l.content_hash ELSE 'tweet:' || l.tweet_id END
                 ORDER BY l.published_at DESC, l.source_id) AS source_rank
        FROM source_links l
        LEFT JOIN channel_profiles sp ON sp.channel_id = l.source_id
        LEFT JOIN channels sc ON sc.channel_id = l.source_id
        LEFT JOIN channel_settings ss ON ss.channel_id = l.source_id
        LEFT JOIN channel_follows sf ON sf.channel_id = l.source_id
        LEFT JOIN channel_stars star ON star.channel_id = l.source_id
        LEFT JOIN muted_channels sm ON sm.channel_id = l.source_id
        WHERE COALESCE(ss.include_reposts, 1) != 0
          AND (:type = 'account' OR sm.channel_id IS NULL)
          AND ((:allChannels AND sf.channel_id IS NOT NULL) OR (NOT :allChannels AND l.source_id IN (:channels)))
          AND ((:type != 'starred' AND NOT :starredOnly) OR star.channel_id IS NOT NULL)
    ),
    candidates AS (
        SELECT fi.*,
               COALESCE(NULLIF(cp.display_name, ''), c.name, cp.handle, '') AS channel_name,
               COALESCE(c.platform, cp.platform, 'twitter') AS channel_platform,
               cp.handle AS author_handle, cp.display_name AS author_display_name,
               cp.account_region AS author_account_region,
               cp.account_details_json AS author_account_details_json,
               sp.handle AS source_handle, sp.display_name AS source_display_name,
               qp.handle AS quote_author_handle, qp.display_name AS quote_author_display_name,
               qp.account_region AS quote_author_account_region,
               qp.account_details_json AS quote_author_account_details_json,
               replyp.handle AS reply_handle, rp.handle AS reposter_handle,
               rp.display_name AS reposter_display_name,
               CASE WHEN fl.tweet_id IS NOT NULL THEN 1 ELSE 0 END AS is_liked,
               fl.liked_at AS liked_at,
               CASE WHEN bm.video_id IS NOT NULL THEN 1 ELSE 0 END AS is_bookmarked,
               bm.category_id AS bookmark_category_id, bm.custom_title AS bookmark_custom_title,
               bm.bookmarked_at AS bookmarked_at,
               bm.account_handles AS bookmark_account_handles, bm.media_indices AS bookmark_media_indices,
               CASE WHEN cf.channel_id IS NOT NULL THEN 1 ELSE 0 END AS channel_is_followed,
               CASE WHEN cs.channel_id IS NOT NULL THEN 1 ELSE 0 END AS channel_is_starred,
               CASE WHEN qcf.channel_id IS NOT NULL THEN 1 ELSE 0 END AS quote_channel_is_followed,
               CASE WHEN :type = 'saved' AND :ordering != 'newest' THEN COALESCE(bm.bookmarked_at, 0)
                    ELSE fi.published_at END AS home_sort_at_ms,
               CASE WHEN fi.is_retweet OR (cs.channel_id IS NULL AND (:type = 'starred' OR :starredOnly))
                    THEN er.source_name ELSE NULL END AS home_reposter_name,
               ROW_NUMBER() OVER (
                 PARTITION BY CASE
                   WHEN COALESCE(fi.canonical_tweet_id, '') != '' OR EXISTS (
                     SELECT 1 FROM feed_items wrapper WHERE wrapper.canonical_tweet_id = fi.tweet_id)
                     THEN 'canonical:' || COALESCE(NULLIF(fi.canonical_tweet_id, ''), fi.tweet_id)
                   WHEN COALESCE(fi.content_hash, '') != '' THEN 'hash:' || fi.content_hash
                   ELSE 'tweet:' || fi.tweet_id END
                 ORDER BY CASE WHEN """ + HOME_FEED_HAS_CONTENT + """ THEN 0 ELSE 1 END,
                   CASE WHEN COALESCE(fi.canonical_tweet_id, '') IN ('', fi.tweet_id) THEN 0 ELSE 1 END,
                   fi.published_at DESC, fi.tweet_id DESC
               ) AS home_item_rank
        FROM feed_items fi
        LEFT JOIN channels c ON c.channel_id = fi.channel_id
        LEFT JOIN channel_profiles cp ON cp.channel_id = fi.channel_id
        LEFT JOIN channel_profiles sp ON sp.channel_id = fi.source_channel_id
        LEFT JOIN channel_profiles qp ON qp.channel_id = fi.quote_channel_id
        LEFT JOIN channel_profiles replyp ON replyp.channel_id = fi.reply_channel_id
        LEFT JOIN channel_profiles rp ON rp.channel_id = fi.reposter_channel_id
        LEFT JOIN feed_likes fl ON fl.tweet_id = fi.tweet_id
        LEFT JOIN bookmark_state bm ON bm.tweet_id = fi.tweet_id AND bm.bookmark_rank = 1
        LEFT JOIN channel_follows cf ON cf.channel_id = fi.channel_id
        LEFT JOIN channel_stars cs ON cs.channel_id = fi.channel_id
        LEFT JOIN channel_follows qcf ON qcf.channel_id = fi.quote_channel_id
        LEFT JOIN eligible_sources er ON er.source_rank = 1 AND (
            er.tweet_id = fi.tweet_id OR (COALESCE(fi.content_hash, '') != '' AND er.content_hash = fi.content_hash))
        WHERE COALESCE(fi.is_ghost, 0) = 0
          AND :type NOT IN ('continue', 'live', 'moments', 'latest')
          AND (:type != 'account' OR NOT :allChannels)
          AND (:type = 'saved' OR NOT :allChannels OR cf.channel_id IS NOT NULL OR er.tweet_id IS NOT NULL)
          AND (:allPlatforms OR COALESCE(c.platform, cp.platform, 'twitter') IN (:platforms))
          AND (:allChannels OR fi.channel_id IN (:channels) OR er.tweet_id IS NOT NULL)
          AND ((:type != 'starred' AND NOT :starredOnly) OR cs.channel_id IS NOT NULL OR er.tweet_id IS NOT NULL)
          AND (:type != 'saved' OR bm.video_id IS NOT NULL)
          AND (:type IN ('saved', 'account') OR NOT EXISTS (
              SELECT 1 FROM muted_channels mc WHERE mc.channel_id = fi.channel_id))
    )
    SELECT * FROM candidates WHERE home_item_rank = 1
    ORDER BY CASE WHEN :ordering = 'account' THEN channel_name END COLLATE NOCASE ASC,
             home_sort_at_ms DESC, tweet_id DESC
    LIMIT :limit OFFSET :offset
"""

@Dao
interface HomeReadDao {
    @RewriteQueriesToDropUnusedColumns
    @Query(HOME_VIDEO_QUERY)
    fun videosFlow(
        type: String, allPlatforms: Boolean, platforms: List<String>, allChannels: Boolean,
        channels: List<String>, allContent: Boolean, contentTypes: List<String>,
        starredOnly: Boolean, includeReposts: Boolean, includeTagged: Boolean, storyCutoffMs: Long,
        ordering: String, limit: Int, offset: Int = 0,
    ): Flow<List<HomeVideoRow>>

    @RewriteQueriesToDropUnusedColumns
    @Query(HOME_VIDEO_QUERY)
    suspend fun videosPage(
        type: String, allPlatforms: Boolean, platforms: List<String>, allChannels: Boolean,
        channels: List<String>, allContent: Boolean, contentTypes: List<String>,
        starredOnly: Boolean, includeReposts: Boolean, includeTagged: Boolean, storyCutoffMs: Long,
        ordering: String, limit: Int, offset: Int,
    ): List<HomeVideoRow>

    @RewriteQueriesToDropUnusedColumns
    @Query(HOME_FEED_QUERY)
    fun feedFlow(
        type: String, allPlatforms: Boolean, platforms: List<String>, allChannels: Boolean,
        channels: List<String>, starredOnly: Boolean, ordering: String, limit: Int, offset: Int = 0,
    ): Flow<List<HomeFeedRow>>

    @RewriteQueriesToDropUnusedColumns
    @Query(HOME_FEED_QUERY)
    suspend fun feedPage(
        type: String, allPlatforms: Boolean, platforms: List<String>, allChannels: Boolean,
        channels: List<String>, starredOnly: Boolean, ordering: String, limit: Int, offset: Int,
    ): List<HomeFeedRow>
}
