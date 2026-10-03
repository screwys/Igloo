package com.screwy.igloo.data.dao

import androidx.room.Dao
import androidx.room.Query
import androidx.room.RewriteQueriesToDropUnusedColumns
import com.screwy.igloo.data.entity.BookmarkItem
import kotlinx.coroutines.flow.Flow

/**
 * Bookmarks tab mixed-platform list. Orders by `bookmarked_at DESC` and LEFT JOINs both content sources — exactly one matches per
 * row outside a cross-namespace identifier collision.
 *
 * Feed and video payloads have separate aliases so either side can be absent.
 */
@Dao
interface BookmarkReadDao {

    @RewriteQueriesToDropUnusedColumns
    @Query(
        """
        WITH bookmark_candidates AS (
            SELECT
                b.video_id AS candidate_video_id,
                CASE
                    WHEN fi.tweet_id IS NOT NULL
                         AND NULLIF(TRIM(COALESCE(fi.quote_tweet_id, '')), '') IS NULL
                         AND NULLIF(TRIM(COALESCE(fi.canonical_tweet_id, '')), '') IS NOT NULL
                        THEN 'twitter:' || fi.canonical_tweet_id
                    WHEN fi.tweet_id IS NOT NULL
                         AND NULLIF(TRIM(COALESCE(fi.quote_tweet_id, '')), '') IS NULL
                         AND COALESCE(fi.is_retweet, 0) != 0
                         AND NULLIF(TRIM(COALESCE(fi.content_hash, '')), '') IS NOT NULL
                        THEN 'twitter-hash:' || fi.content_hash
                    ELSE 'item:' || b.video_id
                END AS cluster_key,
                CASE
                    WHEN fi.tweet_id IS NOT NULL
                         AND NULLIF(TRIM(COALESCE(fi.canonical_tweet_id, '')), '') IS NOT NULL
                         AND fi.tweet_id = fi.canonical_tweet_id
                        THEN 0
                    WHEN fi.tweet_id IS NOT NULL
                         AND COALESCE(fi.is_retweet, 0) = 0
                        THEN 1
                    ELSE 2
                END AS representative_rank,
                b.bookmarked_at AS candidate_bookmarked_at,
                COALESCE(fi.published_at, v.published_at, 0) AS candidate_published_at
            FROM bookmarks b
            LEFT JOIN feed_items fi ON b.video_id = fi.tweet_id
            LEFT JOIN videos     v  ON b.video_id = v.video_id
            WHERE fi.tweet_id IS NULL
               OR fi.has_media = 1
        ),
        ranked_bookmarks AS (
            SELECT
                candidate_video_id,
                MAX(candidate_bookmarked_at) OVER (PARTITION BY cluster_key) AS cluster_bookmarked_at,
                ROW_NUMBER() OVER (
                    PARTITION BY cluster_key
                    ORDER BY
                        representative_rank ASC,
                        candidate_bookmarked_at DESC,
                        candidate_published_at DESC,
                        candidate_video_id DESC
                ) AS cluster_rank
            FROM bookmark_candidates
        )
        SELECT
            b.*,

            fi.payload_json AS tw_payload_json,
            cp.handle AS feed_author_handle,
            COALESCE(NULLIF(cp.display_name, ''), fc.name) AS feed_author_display_name,
            sp.handle                      AS feed_source_handle,
            qp.handle                      AS feed_quote_author_handle,

            v.payload_json AS vd_payload_json,

            COALESCE((
                SELECT COUNT(DISTINCT asa.media_index)
                FROM android_sync_assets asa
                WHERE asa.owner_id = b.video_id
                  AND asa.owner_kind = COALESCE(
                      CASE WHEN fi.tweet_id IS NOT NULL THEN 'tweet' END,
                      v.owner_kind,
                      ''
                  )
                  AND asa.asset_kind = 'post_media'
                  AND asa.state != 'server_missing'
            ), 0) AS asset_media_count,
            COALESCE((
                SELECT COUNT(DISTINCT asa.media_index)
                FROM android_sync_assets asa
                WHERE asa.owner_id = b.video_id
                  AND asa.owner_kind = COALESCE(
                      CASE WHEN fi.tweet_id IS NOT NULL THEN 'tweet' END,
                      v.owner_kind,
                      ''
                  )
                  AND asa.asset_kind = 'post_media'
                  AND asa.state != 'server_missing'
                  AND LOWER(COALESCE(asa.content_type, '')) LIKE 'video/%'
            ), 0) AS asset_video_count,
            COALESCE((
                SELECT COUNT(DISTINCT asa.media_index)
                FROM android_sync_assets asa
                WHERE asa.owner_id = b.video_id
                  AND asa.owner_kind = COALESCE(
                      CASE WHEN fi.tweet_id IS NOT NULL THEN 'tweet' END,
                      v.owner_kind,
                      ''
                  )
                  AND asa.asset_kind = 'post_media'
                  AND asa.state != 'server_missing'
                  AND LOWER(COALESCE(asa.content_type, '')) LIKE 'image/%'
            ), 0) AS asset_image_count,

            COALESCE(fi.channel_id, v.channel_id) AS resolved_channel_id,
            COALESCE(fc.name, vc.name)            AS resolved_channel_name,
            COALESCE(fc.source_id, vc.source_id)  AS resolved_channel_source_id,
            CASE WHEN cf.channel_id IS NOT NULL THEN 1 ELSE 0 END AS resolved_channel_is_followed
        FROM ranked_bookmarks rb
        INNER JOIN bookmarks b ON b.video_id = rb.candidate_video_id
        LEFT JOIN feed_items fi ON b.video_id = fi.tweet_id
        LEFT JOIN videos     v  ON b.video_id = v.video_id
        LEFT JOIN channels   fc ON fc.channel_id = fi.channel_id
        LEFT JOIN channels   vc ON vc.channel_id = v.channel_id
		LEFT JOIN channel_profiles cp ON cp.channel_id = fi.channel_id
		LEFT JOIN channel_profiles sp ON sp.channel_id = fi.source_channel_id
		LEFT JOIN channel_profiles qp ON qp.channel_id = fi.quote_channel_id
		LEFT JOIN channel_follows cf ON cf.channel_id = COALESCE(
			NULLIF(fi.channel_id, ''),
			NULLIF(v.channel_id, '')
		)
        WHERE rb.cluster_rank = 1
        ORDER BY rb.cluster_bookmarked_at DESC, b.bookmarked_at DESC, b.video_id DESC
        """
    )
    fun bookmarksFlow(): Flow<List<BookmarkItem>>
}
