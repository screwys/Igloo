package com.screwy.igloo.data

import androidx.room.DeleteColumn
import androidx.room.migration.AutoMigrationSpec
import androidx.room.migration.Migration
import androidx.sqlite.SQLiteConnection
import androidx.sqlite.SQLiteStatement
import androidx.sqlite.SQLITE_DATA_FLOAT
import androidx.sqlite.SQLITE_DATA_INTEGER
import androidx.sqlite.SQLITE_DATA_NULL
import androidx.sqlite.execSQL
import com.screwy.igloo.data.entity.FeedItemEntity
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject

object IglooMigrations {
    val MIGRATION_51_52 = object : Migration(51, 52) {
        override fun migrate(connection: SQLiteConnection) {
            migrateFeedPayloads(connection)
            migrateVideoPayloads(connection)
            migrateChannelPayloads(connection)
        }
    }

    val MIGRATION_43_44 = object : Migration(43, 44) {
        override fun migrate(connection: SQLiteConnection) {
            connection.execSQL("ALTER TABLE `android_sync_assets` ADD COLUMN `transfer_required` INTEGER NOT NULL DEFAULT 1")
        }
    }

    val MIGRATION_44_45 = object : Migration(44, 45) {
        override fun migrate(connection: SQLiteConnection) {
            connection.execSQL("ALTER TABLE `android_sync_state` ADD COLUMN `cleanup_required` INTEGER NOT NULL DEFAULT 0")
        }
    }

    @DeleteColumn(tableName = "android_sync_assets", columnName = "sha256")
    class DropAssetChecksum : AutoMigrationSpec

    class FillCursorOrder : AutoMigrationSpec {
        override fun onPostMigrate(connection: SQLiteConnection) {
            connection.execSQL(
                """
                UPDATE `moments_cursors`
                SET `order_position` = COALESCE((
                    SELECT CASE `moments_cursors`.`scope`
                        WHEN 'following' THEN `videos`.`moments_following_position`
                        ELSE `videos`.`moments_all_position`
                    END
                    FROM `videos`
                    WHERE `videos`.`video_id` = `moments_cursors`.`video_id`
                ), 0)
                WHERE `scope` IN ('all', 'following')
                """.trimIndent(),
            )
        }
    }

    private fun migrateFeedPayloads(connection: SQLiteConnection) {
        connection.execSQL(
            """
            CREATE TABLE `feed_items_new` (
                `tweet_id` TEXT NOT NULL PRIMARY KEY,
                `source_channel_id` TEXT, `channel_id` TEXT,
                `is_retweet` INTEGER NOT NULL, `reposter_channel_id` TEXT,
                `quote_tweet_id` TEXT, `quote_channel_id` TEXT, `canonical_tweet_id` TEXT,
                `reply_channel_id` TEXT, `reply_to_status` TEXT,
                `is_reply` INTEGER NOT NULL, `is_ghost` INTEGER NOT NULL,
                `content_hash` TEXT, `published_at` INTEGER NOT NULL,
                `content_type` TEXT NOT NULL, `has_content` INTEGER NOT NULL,
                `has_media` INTEGER NOT NULL, `payload_json` TEXT NOT NULL
            )
            """.trimIndent(),
        )
        val retained = listOf(
            "tweet_id", "source_channel_id", "channel_id", "is_retweet", "reposter_channel_id",
            "quote_tweet_id", "quote_channel_id", "canonical_tweet_id", "reply_channel_id",
            "reply_to_status", "is_reply", "is_ghost", "content_hash", "published_at",
        )
        connection.prepare("SELECT * FROM `feed_items`").use { rows ->
            val columns = rows.columnIndices()
            connection.prepare("INSERT INTO `feed_items_new` VALUES (${List(18) { "?" }.joinToString()})").use { insert ->
                while (rows.step()) {
                    val itemJson = rows.legacyJson(columns, setOf("is_retweet", "is_reply", "is_ghost"))
                    val item = FeedItemEntity(
                        tweetId = rows.getText(columns.getValue("tweet_id")),
                        bodyText = rows.nullableText(columns.getValue("body_text")),
                        articleTitle = rows.nullableText(columns.getValue("article_title")),
                        pollJson = rows.nullableText(columns.getValue("poll_json")),
                        mediaJson = rows.nullableText(columns.getValue("media_json")),
                        quoteBodyText = rows.nullableText(columns.getValue("quote_body_text")),
                        quoteArticleTitle = rows.nullableText(columns.getValue("quote_article_title")),
                        quotePollJson = rows.nullableText(columns.getValue("quote_poll_json")),
                        quoteMediaJson = rows.nullableText(columns.getValue("quote_media_json")),
                    )
                    insert.copyColumns(rows, columns, retained)
                    insert.bindText(15, feedContentType(item.mediaJson))
                    insert.bindLong(16, if (feedHasContent(item)) 1 else 0)
                    insert.bindLong(17, if (feedHasMedia(item)) 1 else 0)
                    insert.bindText(18, buildJsonObject { put("item", itemJson) }.toString())
                    insert.step()
                    insert.reset()
                }
            }
        }
        connection.execSQL("DROP TABLE `feed_items`")
        connection.execSQL("ALTER TABLE `feed_items_new` RENAME TO `feed_items`")
        connection.execSQL("CREATE INDEX `idx_feed_items_published` ON `feed_items` (`published_at` DESC)")
        connection.execSQL("CREATE INDEX `idx_feed_items_reply_parent` ON `feed_items` (`reply_to_status`)")
        connection.execSQL("CREATE INDEX `idx_feed_items_channel` ON `feed_items` (`channel_id` ASC, `published_at` DESC)")
        connection.execSQL("CREATE INDEX `idx_feed_items_quote` ON `feed_items` (`quote_tweet_id`)")
        connection.execSQL("CREATE INDEX `idx_feed_items_content_hash` ON `feed_items` (`content_hash`)")
        connection.execSQL("CREATE INDEX `idx_feed_items_canonical_tweet` ON `feed_items` (`canonical_tweet_id`)")
    }

    private fun migrateVideoPayloads(connection: SQLiteConnection) {
        connection.execSQL(
            """
            CREATE TABLE `videos_new` (
                `video_id` TEXT NOT NULL PRIMARY KEY, `channel_id` TEXT NOT NULL,
                `owner_kind` TEXT NOT NULL, `duration` INTEGER, `published_at` INTEGER NOT NULL,
                `is_temp` INTEGER NOT NULL DEFAULT 0, `media_kind` TEXT, `source_kind` TEXT,
                `moments_all_position` INTEGER NOT NULL DEFAULT 0,
                `moments_following_position` INTEGER NOT NULL DEFAULT 0,
                `is_moment` INTEGER NOT NULL, `payload_json` TEXT NOT NULL
            )
            """.trimIndent(),
        )
        val retained = listOf(
            "video_id", "channel_id", "owner_kind", "duration", "published_at", "is_temp",
            "media_kind", "source_kind", "moments_all_position", "moments_following_position",
        )
        connection.prepare("SELECT * FROM `videos`").use { rows ->
            val columns = rows.columnIndices()
            connection.prepare("INSERT INTO `videos_new` VALUES (${List(12) { "?" }.joinToString()})").use { insert ->
                connection.prepare("SELECT * FROM `video_comments` WHERE `video_id` = ? ORDER BY `comment_id`").use { comments ->
                    connection.prepare("SELECT * FROM `sponsorblock_segments` WHERE `video_id` = ? ORDER BY `start_time`, `end_time`, `category`").use { segments ->
                        connection.prepare("SELECT * FROM `sponsorblock_checked` WHERE `video_id` = ?").use { checked ->
                            connection.prepare("SELECT * FROM `video_repost_sources` WHERE `video_id` = ? ORDER BY `reposter_channel_id`").use { reposts ->
                                while (rows.step()) {
                                    val id = rows.getText(columns.getValue("video_id"))
                                    val payload = buildJsonObject {
                                        put("item", rows.legacyJson(columns, setOf("is_temp")))
                                        put("comments", comments.childRows(id, mapOf("comment_id" to "id", "parent_id" to "parent", "author_name" to "author")))
                                        put("sponsorblock_segments", segments.childRows(id, mapOf("start_time" to "start", "end_time" to "end")))
                                        put("sponsorblock_checked", checked.childRows(id, mapOf("checked_at" to "checked_at_ms")).firstOrNull() ?: JsonNull)
                                        put("repost_sources", reposts.childRows(id))
                                    }
                                    val durationIndex = columns.getValue("duration")
                                    val duration = if (rows.isNull(durationIndex)) null else rows.getLong(durationIndex)
                                    insert.copyColumns(rows, columns, retained)
                                    insert.bindLong(11, if (isMoment(rows.getText(columns.getValue("owner_kind")), rows.nullableText(columns.getValue("metadata_json")), duration)) 1 else 0)
                                    insert.bindText(12, payload.toString())
                                    insert.step()
                                    insert.reset()
                                }
                            }
                        }
                    }
                }
            }
        }
        connection.execSQL("DROP TABLE `videos`")
        connection.execSQL("ALTER TABLE `videos_new` RENAME TO `videos`")
        connection.execSQL("CREATE INDEX `idx_videos_channel_published` ON `videos` (`channel_id` ASC, `published_at` DESC)")
        connection.execSQL("CREATE INDEX `idx_videos_source_kind` ON `videos` (`source_kind` ASC, `published_at` DESC)")
        connection.execSQL("CREATE INDEX `idx_videos_owner_published` ON `videos` (`owner_kind` ASC, `published_at` DESC, `video_id` DESC)")
    }

    private fun migrateChannelPayloads(connection: SQLiteConnection) {
        connection.execSQL("CREATE TABLE `channels_new` (`channel_id` TEXT NOT NULL PRIMARY KEY, `source_id` TEXT, `name` TEXT NOT NULL, `url` TEXT, `platform` TEXT NOT NULL, `payload_json` TEXT NOT NULL)")
        connection.execSQL("CREATE TABLE `channel_profiles_new` (`channel_id` TEXT NOT NULL PRIMARY KEY, `platform` TEXT NOT NULL, `handle` TEXT, `display_name` TEXT, `payload_json` TEXT NOT NULL)")
        val profileBooleans = setOf("verified", "protected")
        connection.prepare("SELECT * FROM `channels`").use { rows ->
            val columns = rows.columnIndices()
            connection.prepare("SELECT * FROM `channel_profiles` WHERE `channel_id` = ?").use { profile ->
                val profileColumns = profile.columnIndices()
                connection.prepare("INSERT INTO `channels_new` VALUES (?, ?, ?, ?, ?, ?)").use { insert ->
                    while (rows.step()) {
                        profile.bindText(1, rows.getText(columns.getValue("channel_id")))
                        val payload = buildJsonObject {
                            put("channel", rows.legacyJson(columns))
                            put("profile", if (profile.step()) profile.legacyJson(profileColumns, profileBooleans) else JsonNull)
                        }
                        profile.reset()
                        insert.copyColumns(rows, columns, listOf("channel_id", "source_id", "name", "url", "platform"))
                        insert.bindText(6, payload.toString())
                        insert.step()
                        insert.reset()
                    }
                }
            }
        }
        connection.prepare("SELECT * FROM `channel_profiles`").use { rows ->
            val columns = rows.columnIndices()
            connection.prepare("SELECT * FROM `channels` WHERE `channel_id` = ?").use { channel ->
                val channelColumns = channel.columnIndices()
                connection.prepare("INSERT INTO `channel_profiles_new` VALUES (?, ?, ?, ?, ?)").use { insert ->
                    while (rows.step()) {
                        channel.bindText(1, rows.getText(columns.getValue("channel_id")))
                        val payload = buildJsonObject {
                            put("channel", if (channel.step()) channel.legacyJson(channelColumns) else JsonNull)
                            put("profile", rows.legacyJson(columns, profileBooleans))
                        }
                        channel.reset()
                        insert.copyColumns(rows, columns, listOf("channel_id", "platform", "handle", "display_name"))
                        insert.bindText(5, payload.toString())
                        insert.step()
                        insert.reset()
                    }
                }
            }
        }
        connection.execSQL("DROP TABLE `channels`")
        connection.execSQL("DROP TABLE `channel_profiles`")
        connection.execSQL("ALTER TABLE `channels_new` RENAME TO `channels`")
        connection.execSQL("ALTER TABLE `channel_profiles_new` RENAME TO `channel_profiles`")
        connection.execSQL("CREATE INDEX `idx_channels_platform` ON `channels` (`platform`)")
    }

    private fun SQLiteStatement.columnIndices(): Map<String, Int> =
        (0 until getColumnCount()).associate { getColumnName(it) to it }

    private fun SQLiteStatement.nullableText(index: Int): String? = if (isNull(index)) null else getText(index)

    private fun SQLiteStatement.legacyJson(columns: Map<String, Int>, booleans: Set<String> = emptySet()): JsonObject =
        buildJsonObject {
            columns.forEach { (name, index) ->
                val value: JsonElement = when (getColumnType(index)) {
                    SQLITE_DATA_NULL -> JsonNull
                    SQLITE_DATA_INTEGER -> if (name in booleans) JsonPrimitive(getLong(index) != 0L) else JsonPrimitive(getLong(index))
                    SQLITE_DATA_FLOAT -> JsonPrimitive(getDouble(index))
                    else -> JsonPrimitive(getText(index))
                }
                put(name, value)
            }
        }

    private fun SQLiteStatement.copyColumns(source: SQLiteStatement, columns: Map<String, Int>, retained: List<String>) {
        retained.forEachIndexed { target, name ->
            val index = columns.getValue(name)
            when (source.getColumnType(index)) {
                SQLITE_DATA_NULL -> bindNull(target + 1)
                SQLITE_DATA_INTEGER -> bindLong(target + 1, source.getLong(index))
                SQLITE_DATA_FLOAT -> bindDouble(target + 1, source.getDouble(index))
                else -> bindText(target + 1, source.getText(index))
            }
        }
    }

    private fun SQLiteStatement.childRows(owner: String, aliases: Map<String, String> = emptyMap()): JsonArray {
        val columns = columnIndices()
        bindText(1, owner)
        val rows = buildList {
            while (step()) {
                val row = legacyJson(columns)
                add(JsonObject(row.mapKeys { (name, _) -> aliases[name] ?: name }))
            }
        }
        reset()
        return JsonArray(rows)
    }
}
