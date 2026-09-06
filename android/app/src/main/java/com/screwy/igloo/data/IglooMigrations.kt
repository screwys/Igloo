package com.screwy.igloo.data

import androidx.room.migration.Migration
import androidx.sqlite.db.SupportSQLiteDatabase

object IglooMigrations {
    val MIGRATION_40_41 =
        object : Migration(40, 41) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL(
                    """
                    CREATE TABLE IF NOT EXISTS `android_sync_assets_new` (
                        `asset_id` TEXT NOT NULL,
                        `asset_kind` TEXT NOT NULL,
                        `media_index` INTEGER NOT NULL,
                        `owner_id` TEXT NOT NULL,
                        `owner_kind` TEXT NOT NULL,
                        `bucket` TEXT NOT NULL,
                        `content_type` TEXT,
                        `size_bytes` INTEGER NOT NULL,
                        `revision` INTEGER NOT NULL,
                        `subtitle_is_auto` INTEGER NOT NULL,
                        `state` TEXT NOT NULL,
                        `local_path` TEXT,
                        `verified_at_ms` INTEGER,
                        `next_attempt_at_ms` INTEGER NOT NULL,
                        PRIMARY KEY(`asset_id`)
                    )
                    """.trimIndent(),
                )
                db.execSQL(
                    """
                    INSERT INTO `android_sync_assets_new` (
                        `asset_id`,
                        `asset_kind`,
                        `media_index`,
                        `owner_id`,
                        `owner_kind`,
                        `bucket`,
                        `content_type`,
                        `size_bytes`,
                        `revision`,
                        `subtitle_is_auto`,
                        `state`,
                        `local_path`,
                        `verified_at_ms`,
                        `next_attempt_at_ms`
                    )
                    SELECT
                        `asset_id`,
                        `asset_kind`,
                        `media_index`,
                        `owner_id`,
                        `owner_kind`,
                        `bucket`,
                        `content_type`,
                        `size_bytes`,
                        `revision`,
                        `subtitle_is_auto`,
                        `state`,
                        `local_path`,
                        `verified_at_ms`,
                        `next_attempt_at_ms`
                    FROM `android_sync_assets`
                    """.trimIndent(),
                )
                db.execSQL("DROP TABLE `android_sync_assets`")
                db.execSQL("ALTER TABLE `android_sync_assets_new` RENAME TO `android_sync_assets`")
                db.execSQL(
                    """
                    CREATE INDEX IF NOT EXISTS `idx_android_sync_assets_claim`
                    ON `android_sync_assets` (`state`, `next_attempt_at_ms`)
                    """.trimIndent(),
                )
                db.execSQL(
                    """
                    CREATE INDEX IF NOT EXISTS `idx_android_sync_assets_owner`
                    ON `android_sync_assets` (`owner_kind`, `owner_id`, `asset_kind`, `media_index`)
                    """.trimIndent(),
                )
            }
        }

    val MIGRATION_41_42 =
        object : Migration(41, 42) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL(
                    "ALTER TABLE `videos` ADD COLUMN `is_temp` INTEGER NOT NULL DEFAULT 0",
                )
                db.execSQL(
                    """
                    CREATE INDEX IF NOT EXISTS `idx_videos_owner_published`
                    ON `videos` (`owner_kind` ASC, `published_at` DESC, `video_id` DESC)
                    """.trimIndent(),
                )
                db.execSQL(
                    """
                    CREATE TABLE IF NOT EXISTS `offline_video_downloads` (
                        `video_id` TEXT NOT NULL,
                        `state` TEXT NOT NULL,
                        `updated_at_ms` INTEGER NOT NULL,
                        PRIMARY KEY(`video_id`)
                    )
                    """.trimIndent(),
                )
            }
        }

    val MIGRATION_42_43 =
        object : Migration(42, 43) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL(
                    "ALTER TABLE `channel_settings` ADD COLUMN `include_member_only` INTEGER",
                )
            }
        }

    val MIGRATION_43_44 =
        object : Migration(43, 44) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL(
                    "ALTER TABLE `android_sync_assets` ADD COLUMN `transfer_required` INTEGER NOT NULL DEFAULT 1",
                )
            }
        }

    val MIGRATION_44_45 =
        object : Migration(44, 45) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL(
                    "ALTER TABLE `android_sync_state` ADD COLUMN `cleanup_required` INTEGER NOT NULL DEFAULT 0",
                )
            }
        }

    val MIGRATION_45_46 =
        object : Migration(45, 46) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL("ALTER TABLE `videos` ADD COLUMN `moments_all_position` INTEGER NOT NULL DEFAULT 0")
                db.execSQL("ALTER TABLE `videos` ADD COLUMN `moments_following_position` INTEGER NOT NULL DEFAULT 0")
            }
        }

    val MIGRATION_47_48 =
        object : Migration(47, 48) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL("ALTER TABLE `feed_items` ADD COLUMN `article_title` TEXT")
                db.execSQL("ALTER TABLE `feed_items` ADD COLUMN `quote_article_title` TEXT")
                db.execSQL("ALTER TABLE `channel_profiles` ADD COLUMN `account_region` TEXT")
            }
        }

    val MIGRATION_48_49 =
        object : Migration(48, 49) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL("ALTER TABLE `feed_items` ADD COLUMN `poll_json` TEXT")
                db.execSQL("ALTER TABLE `feed_items` ADD COLUMN `quote_poll_json` TEXT")
                db.execSQL("ALTER TABLE `feed_items` ADD COLUMN `community_note` TEXT")
                db.execSQL("ALTER TABLE `feed_items` ADD COLUMN `quote_community_note` TEXT")
            }
        }

    val MIGRATION_49_50 =
        object : Migration(49, 50) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL("ALTER TABLE `channel_profiles` ADD COLUMN `account_details_json` TEXT")
            }
        }

    val MIGRATION_46_47 =
        object : Migration(46, 47) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL("ALTER TABLE `moments_cursors` ADD COLUMN `order_position` INTEGER NOT NULL DEFAULT 0")
                db.execSQL(
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
}
