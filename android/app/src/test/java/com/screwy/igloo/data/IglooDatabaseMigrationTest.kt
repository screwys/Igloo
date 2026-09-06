package com.screwy.igloo.data

import androidx.room.testing.MigrationTestHelper
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], manifest = Config.NONE)
class IglooDatabaseMigrationTest {
    @get:Rule
    val helper =
        MigrationTestHelper(
            InstrumentationRegistry.getInstrumentation(),
            IglooDatabase::class.java,
        )

    @Test
    fun migration40To41DropsAssetChecksumWithoutLosingLocalState() {
        helper.createDatabase(DATABASE_NAME, 40).use { db ->
            db.execSQL(
                """
                INSERT INTO android_sync_assets (
                    asset_id,
                    asset_kind,
                    media_index,
                    owner_id,
                    owner_kind,
                    bucket,
                    content_type,
                    size_bytes,
                    sha256,
                    revision,
                    subtitle_is_auto,
                    state,
                    local_path,
                    verified_at_ms,
                    next_attempt_at_ms
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """.trimIndent(),
                arrayOf<Any?>(
                    "sample_asset",
                    "post_media",
                    2,
                    "sample_post",
                    "tweet",
                    "feed",
                    "image/jpeg",
                    123L,
                    "0".repeat(64),
                    7L,
                    1,
                    "ready",
                    "/sample/cache/file.jpg",
                    456L,
                    789L,
                ),
            )
            db.execSQL(
                "INSERT INTO preferences (`key`, `value`, `updated_at`) VALUES (?, ?, ?)",
                arrayOf<Any?>("theme", "sample_theme", 321L),
            )
        }

        helper.runMigrationsAndValidate(
            DATABASE_NAME,
            41,
            true,
            IglooMigrations.MIGRATION_40_41,
        ).use { db ->
            db.query("PRAGMA table_info(android_sync_assets)").use { cursor ->
                val nameIndex = cursor.getColumnIndexOrThrow("name")
                val columns = buildSet {
                    while (cursor.moveToNext()) add(cursor.getString(nameIndex))
                }
                assertFalse(columns.contains("sha256"))
            }
            db.query(
                """
                SELECT asset_id, size_bytes, revision, local_path, verified_at_ms, next_attempt_at_ms
                FROM android_sync_assets
                """.trimIndent(),
            ).use { cursor ->
                cursor.moveToFirst()
                assertEquals("sample_asset", cursor.getString(0))
                assertEquals(123L, cursor.getLong(1))
                assertEquals(7L, cursor.getLong(2))
                assertEquals("/sample/cache/file.jpg", cursor.getString(3))
                assertEquals(456L, cursor.getLong(4))
                assertEquals(789L, cursor.getLong(5))
            }
            db.query("SELECT value FROM preferences WHERE `key` = 'theme'").use { cursor ->
                cursor.moveToFirst()
                assertEquals("sample_theme", cursor.getString(0))
            }
        }
    }

    @Test
    fun migration41To42KeepsVideosAndAddsOfflineDownloadState() {
        helper.createDatabase(DATABASE_NAME, 41).use { db ->
            db.execSQL(
                """
                INSERT INTO videos (
                    video_id,
                    channel_id,
                    owner_kind,
                    title,
                    published_at,
                    slide_count
                ) VALUES (?, ?, ?, ?, ?, ?)
                """.trimIndent(),
                arrayOf<Any?>(
                    "sample_video",
                    "sample_channel",
                    "youtube_video",
                    "Sample video",
                    123L,
                    0,
                ),
            )
        }

        helper.runMigrationsAndValidate(
            DATABASE_NAME,
            42,
            true,
            IglooMigrations.MIGRATION_41_42,
        ).use { db ->
            db.query("SELECT is_temp FROM videos WHERE video_id = 'sample_video'").use { cursor ->
                cursor.moveToFirst()
                assertEquals(0, cursor.getInt(0))
            }
            db.query("PRAGMA index_list(videos)").use { cursor ->
                val nameIndex = cursor.getColumnIndexOrThrow("name")
                val indexes = buildSet {
                    while (cursor.moveToNext()) add(cursor.getString(nameIndex))
                }
                assertTrue(indexes.contains("idx_videos_owner_published"))
            }
            db.execSQL(
                """
                INSERT INTO offline_video_downloads (video_id, state, updated_at_ms)
                VALUES (?, ?, ?)
                """.trimIndent(),
                arrayOf<Any?>("sample_video", "downloaded", 456L),
            )
            db.query(
                "SELECT video_id, state, updated_at_ms FROM offline_video_downloads",
            ).use { cursor ->
                cursor.moveToFirst()
                assertEquals("sample_video", cursor.getString(0))
                assertEquals("downloaded", cursor.getString(1))
                assertEquals(456L, cursor.getLong(2))
            }
        }
    }

    @Test
    fun migration42To43KeepsChannelSettingsAndAddsMemberOnlyOverride() {
        helper.createDatabase(DATABASE_NAME, 42).use { db ->
            db.execSQL(
                """
                INSERT INTO channel_settings (channel_id, max_videos, updated_at)
                VALUES (?, ?, ?)
                """.trimIndent(),
                arrayOf<Any?>("youtube_sample_channel", 7, 123L),
            )
        }

        helper.runMigrationsAndValidate(
            DATABASE_NAME,
            43,
            true,
            IglooMigrations.MIGRATION_42_43,
        ).use { db ->
            db.query(
                "SELECT max_videos, include_member_only, updated_at FROM channel_settings",
            ).use { cursor ->
                cursor.moveToFirst()
                assertEquals(7, cursor.getInt(0))
                assertTrue(cursor.isNull(1))
                assertEquals(123L, cursor.getLong(2))
            }
        }
    }

    @Test
    fun migration43To44KeepsAssetsAndMarksExistingDescriptorsRequired() {
        helper.createDatabase(DATABASE_NAME, 43).use { db ->
            db.execSQL(
                """
                INSERT INTO android_sync_assets (
                    asset_id, asset_kind, media_index, owner_id, owner_kind, bucket,
                    content_type, size_bytes, revision, subtitle_is_auto, state,
                    local_path, verified_at_ms, next_attempt_at_ms
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """.trimIndent(),
                arrayOf<Any?>(
                    "sample_asset",
                    "video_stream",
                    0,
                    "sample_video",
                    "youtube_video",
                    "youtube",
                    "video/mp4",
                    123L,
                    7L,
                    1,
                    "ready",
                    null,
                    null,
                    0L,
                ),
            )
        }

        helper.runMigrationsAndValidate(
            DATABASE_NAME,
            44,
            true,
            IglooMigrations.MIGRATION_43_44,
        ).use { db ->
            db.query(
                "SELECT asset_id, revision, transfer_required FROM android_sync_assets",
            ).use { cursor ->
                cursor.moveToFirst()
                assertEquals("sample_asset", cursor.getString(0))
                assertEquals(7L, cursor.getLong(1))
                assertEquals(1, cursor.getInt(2))
            }
        }
    }

    @Test
    fun migration44To45KeepsSyncStateAndDefersNoCleanup() {
        helper.createDatabase(DATABASE_NAME, 44).use { db ->
            db.execSQL(
                """
                INSERT INTO android_sync_state (
                    id, mode, cursor, feed_days, youtube_days, moments_days,
                    story_hours, bootstrap_required
                ) VALUES (1, 'changes', 'sample_cursor', 2, 3, 7, 48, 0)
                """.trimIndent(),
            )
        }

        helper.runMigrationsAndValidate(
            DATABASE_NAME,
            45,
            true,
            IglooMigrations.MIGRATION_44_45,
        ).use { db ->
            db.query(
                "SELECT mode, cursor, cleanup_required FROM android_sync_state WHERE id = 1",
            ).use { cursor ->
                cursor.moveToFirst()
                assertEquals("changes", cursor.getString(0))
                assertEquals("sample_cursor", cursor.getString(1))
                assertEquals(0, cursor.getInt(2))
            }
        }
    }

    @Test
    fun migration45To46KeepsVideosAndAddsMomentsPositions() {
        helper.createDatabase(DATABASE_NAME, 45).use { db ->
            db.execSQL(
                "INSERT INTO videos (video_id, channel_id, owner_kind, published_at, slide_count) VALUES ('sample_video', 'tiktok_sample', 'tiktok_video', 100, 0)",
            )
        }

        helper.runMigrationsAndValidate(
            DATABASE_NAME,
            46,
            true,
            IglooMigrations.MIGRATION_45_46,
        ).use { db ->
            db.query(
                "SELECT video_id, moments_all_position, moments_following_position FROM videos",
            ).use { cursor ->
                cursor.moveToFirst()
                assertEquals("sample_video", cursor.getString(0))
                assertEquals(0L, cursor.getLong(1))
                assertEquals(0L, cursor.getLong(2))
            }
        }
    }

    @Test
    fun migration46To47KeepsCursorAndAddsOrderPosition() {
        helper.createDatabase(DATABASE_NAME, 46).use { db ->
            db.execSQL(
                "INSERT INTO videos (video_id, channel_id, owner_kind, published_at, slide_count, moments_all_position, moments_following_position) VALUES ('sample_video', 'tiktok_sample', 'tiktok_video', 100, 0, 42, 43)",
            )
            db.execSQL(
                "INSERT INTO moments_cursors (scope, video_id, position_ms, sort_at_ms, updated_at_ms) VALUES ('all', 'sample_video', 0, 100, 200)",
            )
        }

        helper.runMigrationsAndValidate(
            DATABASE_NAME,
            47,
            true,
            IglooMigrations.MIGRATION_46_47,
        ).use { db ->
            db.query("SELECT video_id, order_position, updated_at_ms FROM moments_cursors WHERE scope = 'all'").use { cursor ->
                cursor.moveToFirst()
                assertEquals("sample_video", cursor.getString(0))
                assertEquals(42L, cursor.getLong(1))
                assertEquals(200L, cursor.getLong(2))
            }
        }
    }

    @Test
    fun migration47To48KeepsSavedArticleBodiesAndProfiles() {
        helper.createDatabase(DATABASE_NAME, 47).use { db ->
            db.execSQL("INSERT INTO feed_items (tweet_id, body_text, is_retweet, quote_published_at, is_reply, is_ghost, published_at) VALUES ('sample_post', 'Saved body', 0, 0, 0, 0, 100)")
            db.execSQL("INSERT INTO channel_profiles (channel_id, platform, followers, following, verified, protected) VALUES ('twitter_sample', 'twitter', 5, 2, 0, 0)")
        }
        helper.runMigrationsAndValidate(DATABASE_NAME, 48, true, IglooMigrations.MIGRATION_47_48).use { db ->
            db.query("SELECT body_text, article_title, quote_article_title FROM feed_items WHERE tweet_id = 'sample_post'").use { cursor ->
                assertTrue(cursor.moveToFirst())
                assertEquals("Saved body", cursor.getString(0))
                assertTrue(cursor.isNull(1))
                assertTrue(cursor.isNull(2))
            }
            db.query("SELECT followers, account_region FROM channel_profiles WHERE channel_id = 'twitter_sample'").use { cursor ->
                assertTrue(cursor.moveToFirst())
                assertEquals(5, cursor.getInt(0))
                assertTrue(cursor.isNull(1))
            }
        }
    }

    @Test
    fun migration48To49KeepsArticleAndSavedStateWhileAddingPollsAndNotes() {
        helper.createDatabase(DATABASE_NAME, 48).use { db ->
            db.execSQL("INSERT INTO feed_items (tweet_id, body_text, article_title, is_retweet, quote_published_at, is_reply, is_ghost, published_at) VALUES ('sample_post', 'Saved body', 'Saved article', 0, 0, 0, 0, 100)")
            db.execSQL("INSERT INTO feed_likes (tweet_id, liked_at) VALUES ('sample_post', 200)")
        }
        helper.runMigrationsAndValidate(DATABASE_NAME, 49, true, IglooMigrations.MIGRATION_48_49).use { db ->
            db.query("SELECT article_title, poll_json, quote_poll_json, community_note, quote_community_note FROM feed_items WHERE tweet_id = 'sample_post'").use { cursor ->
                assertTrue(cursor.moveToFirst())
                assertEquals("Saved article", cursor.getString(0))
                (1..4).forEach { assertTrue(cursor.isNull(it)) }
            }
            db.query("SELECT liked_at FROM feed_likes WHERE tweet_id = 'sample_post'").use { cursor ->
                assertTrue(cursor.moveToFirst())
                assertEquals(200L, cursor.getLong(0))
            }
        }
    }

    @Test
    fun migration49To50KeepsProfileRegionAndIdentity() {
        helper.createDatabase(DATABASE_NAME, 49).use { db ->
            db.execSQL("INSERT INTO channel_profiles (channel_id, platform, display_name, account_region, followers, following, verified, protected) VALUES ('twitter_sample', 'twitter', 'Sample Name', 'United States', 5, 2, 0, 0)")
        }
        helper.runMigrationsAndValidate(DATABASE_NAME, 50, true, IglooMigrations.MIGRATION_49_50).use { db ->
            db.query("SELECT display_name, account_region, account_details_json FROM channel_profiles WHERE channel_id = 'twitter_sample'").use { cursor ->
                assertTrue(cursor.moveToFirst())
                assertEquals("Sample Name", cursor.getString(0))
                assertEquals("United States", cursor.getString(1))
                assertTrue(cursor.isNull(2))
            }
        }
    }

    private companion object {
        const val DATABASE_NAME = "igloo-migration-test"
    }
}
