package com.screwy.igloo.data

import androidx.room.testing.MigrationTestHelper
import androidx.sqlite.driver.bundled.BundledSQLiteDriver
import androidx.sqlite.execSQL
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
            instrumentation = InstrumentationRegistry.getInstrumentation(),
            file = InstrumentationRegistry.getInstrumentation().targetContext.getDatabasePath(DATABASE_NAME),
            driver = BundledSQLiteDriver(),
            databaseClass = IglooDatabase::class,
        )

    @Test
    fun migration40To41DropsAssetChecksumWithoutLosingLocalState() {
        helper.createDatabase(40).use { db ->
            db.prepare(
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
            ).use { statement ->
                statement.bindText(1, "sample_asset")
                statement.bindText(2, "post_media")
                statement.bindLong(3, 2)
                statement.bindText(4, "sample_post")
                statement.bindText(5, "tweet")
                statement.bindText(6, "feed")
                statement.bindText(7, "image/jpeg")
                statement.bindLong(8, 123)
                statement.bindText(9, "0".repeat(64))
                statement.bindLong(10, 7)
                statement.bindLong(11, 1)
                statement.bindText(12, "ready")
                statement.bindText(13, "/sample/cache/file.jpg")
                statement.bindLong(14, 456)
                statement.bindLong(15, 789)
                statement.step()
            }
            db.prepare(
                "INSERT INTO preferences (`key`, `value`, `updated_at`) VALUES (?, ?, ?)",
            ).use { statement ->
                statement.bindText(1, "theme")
                statement.bindText(2, "sample_theme")
                statement.bindLong(3, 321)
                statement.step()
            }
        }

        helper.runMigrationsAndValidate(41).use { db ->
            db.prepare("PRAGMA table_info(android_sync_assets)").use { cursor ->
                val nameIndex = 1
                val columns = buildSet {
                    while (cursor.step()) add(cursor.getText(nameIndex))
                }
                assertFalse(columns.contains("sha256"))
            }
            db.prepare(
                """
                SELECT asset_id, size_bytes, revision, local_path, verified_at_ms, next_attempt_at_ms
                FROM android_sync_assets
                """.trimIndent(),
            ).use { cursor ->
                assertTrue(cursor.step())
                assertEquals("sample_asset", cursor.getText(0))
                assertEquals(123L, cursor.getLong(1))
                assertEquals(7L, cursor.getLong(2))
                assertEquals("/sample/cache/file.jpg", cursor.getText(3))
                assertEquals(456L, cursor.getLong(4))
                assertEquals(789L, cursor.getLong(5))
            }
            db.prepare("SELECT value FROM preferences WHERE `key` = 'theme'").use { cursor ->
                assertTrue(cursor.step())
                assertEquals("sample_theme", cursor.getText(0))
            }
        }

        helper.runMigrationsAndValidate(
            51,
            listOf(IglooMigrations.MIGRATION_43_44, IglooMigrations.MIGRATION_44_45),
        ).use { db ->
            db.prepare("SELECT asset_id, local_path, revision, transfer_required FROM android_sync_assets").use { cursor ->
                assertTrue(cursor.step())
                assertEquals("sample_asset", cursor.getText(0))
                assertEquals("/sample/cache/file.jpg", cursor.getText(1))
                assertEquals(7L, cursor.getLong(2))
                assertEquals(1L, cursor.getLong(3))
            }
            db.prepare("SELECT value FROM preferences WHERE `key` = 'theme'").use { cursor ->
                assertTrue(cursor.step())
                assertEquals("sample_theme", cursor.getText(0))
            }
            db.execSQL("""
                INSERT INTO feed_items (
                    tweet_id, source_channel_id, channel_id, body_text, article_title, poll_json,
                    community_note, lang, is_retweet, quote_tweet_id, quote_body_text, quote_article_title,
                    quote_poll_json, quote_community_note, quote_lang, quote_media_json, quote_published_at,
                    media_json, views, likes, retweets, canonical_url, canonical_tweet_id,
                    reply_channel_id, reply_to_status, is_reply, is_ghost, content_hash,
                    body_translation, body_source_lang, quote_translation, quote_source_lang, published_at
                ) VALUES (
                    'sample_post', 'sample_source', 'sample_channel', 'Saved body', 'Saved article', '{"choices":["a","b"]}',
                    'Saved note', 'en', 0, 'sample_quote', 'Quoted body', 'Quoted article',
                    '{"choices":["c","d"]}', 'Quoted note', 'ja', '[{"type":"photo"}]', 88,
                    '[{"type":"video","url":"https://example.test/video.mp4"}]', 123, 4, 5,
                    'https://example.test/post', 'sample_canonical', 'reply_channel', 'reply_parent', 1, 0,
                    'sample_hash', 'Translated body', 'en', 'Translated quote', 'ja', 99
                )
            """.trimIndent())
            db.execSQL("""
                INSERT INTO videos (
                    video_id, channel_id, owner_kind, title, description, duration, published_at,
                    is_temp, media_kind, slide_count, source_kind, metadata_json, canonical_url,
                    dearrow_title, dearrow_title_casual, moments_all_position, moments_following_position
                ) VALUES (
                    'sample_video', 'sample_channel', 'youtube_video', 'Saved video', 'Saved description',
                    93, 66, 1, 'video', 0, 'manual', '{"duration":"NaN","width":"0x1.0p4","height":"2.1e1","future":{"kept":true}}',
                    'https://example.test/video?v=sample', 'Edited title', 'Casual title', 42, 43
                )
            """.trimIndent())
            db.execSQL("INSERT INTO channels (channel_id, source_id, name, url, platform) VALUES ('sample_channel', 'sample_source', 'Saved channel', 'https://example.test/channel', 'youtube'), ('channel_without_profile', NULL, 'Empty profile', NULL, 'youtube')")
            db.execSQL("INSERT INTO channel_profiles (channel_id, platform, handle, display_name, bio, website, followers, following, verified, verified_type, account_region, account_details_json, protected) VALUES ('sample_channel', 'youtube', 'sample_handle', 'Saved profile', 'Saved bio', 'https://example.test/profile', 5, 2, 1, 'blue', 'Japan', '{\"source\":\"archive\"}', 0), ('profile_without_channel', 'twitter', 'empty_channel', NULL, NULL, NULL, 0, 0, 0, NULL, NULL, NULL, 1)")
            db.execSQL("INSERT INTO video_comments (video_id, comment_id, parent_id, author_name, author_id, text, like_count, published_at) VALUES ('sample_video', 'sample_comment', NULL, 'Comment author', 'comment_author', 'Saved comment', 7, 55)")
            db.execSQL("INSERT INTO sponsorblock_segments (video_id, start_time, end_time, category) VALUES ('sample_video', 1.25, 2.5, 'sponsor')")
            db.execSQL("INSERT INTO sponsorblock_checked (video_id, checked_at, video_age_at_check) VALUES ('sample_video', 1234, 'month')")
            db.execSQL("INSERT INTO video_repost_sources (video_id, reposter_channel_id, reposted_at_ms, first_seen_at_ms, updated_at_ms) VALUES ('sample_video', 'sample_reposter', 33, 44, 55)")
            db.execSQL("INSERT INTO bookmarks (video_id, category_id, custom_title, account_handles, media_indices, bookmarked_at) VALUES ('sample_post', 0, 'Saved bookmark', '[\"sample_handle\"]', '[0]', 123)")
            db.execSQL("INSERT INTO feed_likes (tweet_id, liked_at) VALUES ('sample_post', 124)")
            db.execSQL("INSERT INTO outbox (id, kind, item_id, field, payload_json, state, attempt_count, next_attempt_at_ms, last_error_code, last_error_body, created_at_ms) VALUES (41, 'like', 'sample_post', NULL, '{\"future\":\"queued\"}', 'pending', 2, 123456, 503, 'temporary', 99)")
            db.execSQL("INSERT INTO watch_history (video_id, playback_position, duration, updated_at_ms) VALUES ('sample_video', 12.5, 93, 77)")
            db.execSQL("INSERT INTO moments_cursors (scope, video_id, position_ms, sort_at_ms, updated_at_ms, order_position) VALUES ('all', 'sample_video', 123, 66, 78, 42)")
            db.execSQL("INSERT INTO offline_video_downloads (video_id, state, updated_at_ms) VALUES ('sample_video', 'downloaded', 64)")
            db.execSQL("INSERT INTO android_sync_state (id, mode, cursor, feed_days, youtube_days, moments_days, story_hours, bootstrap_required, cleanup_required) VALUES (1, 'changes', 'opaque_cursor', 7, 14, 7, 48, 0, 1)")
        }

        helper.runMigrationsAndValidate(
            52,
            listOf(IglooMigrations.MIGRATION_51_52),
        ).use { db ->
            db.prepare("SELECT json_extract(payload_json, '$.item.body_text'), json_extract(payload_json, '$.item.article_title'), json_extract(payload_json, '$.item.body_translation'), json_extract(payload_json, '$.item.is_reply'), content_type, has_content, has_media FROM feed_items WHERE tweet_id = 'sample_post'").use { row ->
                assertTrue(row.step())
                assertEquals("Saved body", row.getText(0))
                assertEquals("Saved article", row.getText(1))
                assertEquals("Translated body", row.getText(2))
                assertEquals(1L, row.getLong(3))
                assertEquals("video", row.getText(4))
                assertEquals(1L, row.getLong(5))
                assertEquals(1L, row.getLong(6))
            }
            db.prepare("SELECT json_extract(payload_json, '$.item.title'), json_extract(payload_json, '$.item.slide_count'), json_extract(payload_json, '$.comments[0].id'), json_extract(payload_json, '$.comments[0].author'), json_extract(payload_json, '$.sponsorblock_segments[0].start'), json_extract(payload_json, '$.sponsorblock_checked.checked_at_ms'), json_extract(payload_json, '$.repost_sources[0].reposter_channel_id'), is_moment FROM videos WHERE video_id = 'sample_video'").use { row ->
                assertTrue(row.step())
                assertEquals("Saved video", row.getText(0))
                assertEquals(0L, row.getLong(1))
                assertEquals("sample_comment", row.getText(2))
                assertEquals("Comment author", row.getText(3))
                assertEquals(1.25, row.getDouble(4), 0.0)
                assertEquals(1234L, row.getLong(5))
                assertEquals("sample_reposter", row.getText(6))
                assertEquals(1L, row.getLong(7))
            }
            db.prepare("SELECT json_extract(payload_json, '$.profile.bio'), json_extract(payload_json, '$.profile.verified'), json_extract(payload_json, '$.channel.name') FROM channel_profiles WHERE channel_id = 'sample_channel'").use { row ->
                assertTrue(row.step())
                assertEquals("Saved bio", row.getText(0))
                assertEquals(1L, row.getLong(1))
                assertEquals("Saved channel", row.getText(2))
            }
            db.prepare("SELECT json_extract(payload_json, '$.profile.display_name') FROM channels WHERE channel_id = 'sample_channel'").use { row ->
                assertTrue(row.step())
                assertEquals("Saved profile", row.getText(0))
            }
            db.prepare("SELECT COUNT(*) FROM channels WHERE channel_id = 'channel_without_profile'").use { row ->
                assertTrue(row.step())
                assertEquals(1L, row.getLong(0))
            }
            db.prepare("SELECT COUNT(*) FROM channel_profiles WHERE channel_id = 'profile_without_channel'").use { row ->
                assertTrue(row.step())
                assertEquals(1L, row.getLong(0))
            }
            db.prepare("SELECT local_path, revision, transfer_required FROM android_sync_assets WHERE asset_id = 'sample_asset'").use { row ->
                assertTrue(row.step())
                assertEquals("/sample/cache/file.jpg", row.getText(0))
                assertEquals(7L, row.getLong(1))
                assertEquals(1L, row.getLong(2))
            }
            db.prepare("SELECT state, attempt_count, payload_json FROM outbox WHERE id = 41").use { row ->
                assertTrue(row.step())
                assertEquals("pending", row.getText(0))
                assertEquals(2L, row.getLong(1))
                assertEquals("{\"future\":\"queued\"}", row.getText(2))
            }
            db.prepare("SELECT value FROM preferences WHERE `key` = 'theme'").use { row ->
                assertTrue(row.step())
                assertEquals("sample_theme", row.getText(0))
            }
            db.prepare("SELECT playback_position, updated_at_ms FROM watch_history WHERE video_id = 'sample_video'").use { row ->
                assertTrue(row.step())
                assertEquals(12.5, row.getDouble(0), 0.0)
                assertEquals(77L, row.getLong(1))
            }
            db.prepare("SELECT position_ms, order_position FROM moments_cursors WHERE scope = 'all'").use { row ->
                assertTrue(row.step())
                assertEquals(123L, row.getLong(0))
                assertEquals(42L, row.getLong(1))
            }
            db.prepare("SELECT state FROM offline_video_downloads WHERE video_id = 'sample_video'").use { row ->
                assertTrue(row.step())
                assertEquals("downloaded", row.getText(0))
            }
            db.prepare("SELECT mode, cursor, bootstrap_required, cleanup_required FROM android_sync_state WHERE id = 1").use { row ->
                assertTrue(row.step())
                assertEquals("changes", row.getText(0))
                assertEquals("opaque_cursor", row.getText(1))
                assertEquals(0L, row.getLong(2))
                assertEquals(1L, row.getLong(3))
            }
            for (table in listOf("video_comments", "sponsorblock_segments", "sponsorblock_checked", "video_repost_sources", "bookmarks", "feed_likes")) {
                db.prepare("SELECT COUNT(*) FROM `$table`").use { row ->
                    assertTrue(row.step())
                    assertEquals(1L, row.getLong(0))
                }
            }
        }
    }

    @Test
    fun migration41To42KeepsVideosAndAddsOfflineDownloadState() {
        helper.createDatabase(41).use { db ->
            db.prepare(
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
            ).use { statement ->
                statement.bindText(1, "sample_video")
                statement.bindText(2, "sample_channel")
                statement.bindText(3, "youtube_video")
                statement.bindText(4, "Sample video")
                statement.bindLong(5, 123)
                statement.bindLong(6, 0)
                statement.step()
            }
        }

        helper.runMigrationsAndValidate(42).use { db ->
            db.prepare("SELECT is_temp FROM videos WHERE video_id = 'sample_video'").use { cursor ->
                assertTrue(cursor.step())
                assertEquals(0, cursor.getLong(0).toInt())
            }
            db.prepare("PRAGMA index_list(videos)").use { cursor ->
                val nameIndex = 1
                val indexes = buildSet {
                    while (cursor.step()) add(cursor.getText(nameIndex))
                }
                assertTrue(indexes.contains("idx_videos_owner_published"))
            }
            db.prepare(
                """
                INSERT INTO offline_video_downloads (video_id, state, updated_at_ms)
                VALUES (?, ?, ?)
                """.trimIndent(),
            ).use { statement ->
                statement.bindText(1, "sample_video")
                statement.bindText(2, "downloaded")
                statement.bindLong(3, 456)
                statement.step()
            }
            db.prepare(
                "SELECT video_id, state, updated_at_ms FROM offline_video_downloads",
            ).use { cursor ->
                assertTrue(cursor.step())
                assertEquals("sample_video", cursor.getText(0))
                assertEquals("downloaded", cursor.getText(1))
                assertEquals(456L, cursor.getLong(2))
            }
        }
    }

    @Test
    fun migration42To43KeepsChannelSettingsAndAddsMemberOnlyOverride() {
        helper.createDatabase(42).use { db ->
            db.prepare(
                """
                INSERT INTO channel_settings (channel_id, max_videos, updated_at)
                VALUES (?, ?, ?)
                """.trimIndent(),
            ).use { statement ->
                statement.bindText(1, "youtube_sample_channel")
                statement.bindLong(2, 7)
                statement.bindLong(3, 123)
                statement.step()
            }
        }

        helper.runMigrationsAndValidate(43).use { db ->
            db.prepare(
                "SELECT max_videos, include_member_only, updated_at FROM channel_settings",
            ).use { cursor ->
                assertTrue(cursor.step())
                assertEquals(7, cursor.getLong(0).toInt())
                assertTrue(cursor.isNull(1))
                assertEquals(123L, cursor.getLong(2))
            }
        }
    }

    @Test
    fun migration43To44KeepsAssetsAndMarksExistingDescriptorsRequired() {
        helper.createDatabase(43).use { db ->
            db.prepare(
                """
                INSERT INTO android_sync_assets (
                    asset_id, asset_kind, media_index, owner_id, owner_kind, bucket,
                    content_type, size_bytes, revision, subtitle_is_auto, state,
                    local_path, verified_at_ms, next_attempt_at_ms
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """.trimIndent(),
            ).use { statement ->
                statement.bindText(1, "sample_asset")
                statement.bindText(2, "video_stream")
                statement.bindLong(3, 0)
                statement.bindText(4, "sample_video")
                statement.bindText(5, "youtube_video")
                statement.bindText(6, "youtube")
                statement.bindText(7, "video/mp4")
                statement.bindLong(8, 123)
                statement.bindLong(9, 7)
                statement.bindLong(10, 1)
                statement.bindText(11, "ready")
                statement.bindNull(12)
                statement.bindNull(13)
                statement.bindLong(14, 0)
                statement.step()
            }
        }

        helper.runMigrationsAndValidate(44, listOf(IglooMigrations.MIGRATION_43_44)).use { db ->
            db.prepare(
                "SELECT asset_id, revision, transfer_required FROM android_sync_assets",
            ).use { cursor ->
                assertTrue(cursor.step())
                assertEquals("sample_asset", cursor.getText(0))
                assertEquals(7L, cursor.getLong(1))
                assertEquals(1, cursor.getLong(2).toInt())
            }
        }
    }

    @Test
    fun migration44To45KeepsSyncStateAndDefersNoCleanup() {
        helper.createDatabase(44).use { db ->
            db.execSQL(
                """
                INSERT INTO android_sync_state (
                    id, mode, cursor, feed_days, youtube_days, moments_days,
                    story_hours, bootstrap_required
                ) VALUES (1, 'changes', 'sample_cursor', 2, 3, 7, 48, 0)
                """.trimIndent(),
            )
        }

        helper.runMigrationsAndValidate(45, listOf(IglooMigrations.MIGRATION_44_45)).use { db ->
            db.prepare(
                "SELECT mode, cursor, cleanup_required FROM android_sync_state WHERE id = 1",
            ).use { cursor ->
                assertTrue(cursor.step())
                assertEquals("changes", cursor.getText(0))
                assertEquals("sample_cursor", cursor.getText(1))
                assertEquals(0, cursor.getLong(2).toInt())
            }
        }
    }

    @Test
    fun migration45To46KeepsVideosAndAddsMomentsPositions() {
        helper.createDatabase(45).use { db ->
            db.execSQL(
                "INSERT INTO videos (video_id, channel_id, owner_kind, published_at, slide_count) VALUES ('sample_video', 'tiktok_sample', 'tiktok_video', 100, 0)",
            )
        }

        helper.runMigrationsAndValidate(46).use { db ->
            db.prepare(
                "SELECT video_id, moments_all_position, moments_following_position FROM videos",
            ).use { cursor ->
                assertTrue(cursor.step())
                assertEquals("sample_video", cursor.getText(0))
                assertEquals(0L, cursor.getLong(1))
                assertEquals(0L, cursor.getLong(2))
            }
        }
    }

    @Test
    fun migration46To47KeepsCursorAndAddsOrderPosition() {
        helper.createDatabase(46).use { db ->
            db.execSQL(
                "INSERT INTO videos (video_id, channel_id, owner_kind, published_at, slide_count, moments_all_position, moments_following_position) VALUES ('sample_video', 'tiktok_sample', 'tiktok_video', 100, 0, 42, 43)",
            )
            db.execSQL(
                "INSERT INTO moments_cursors (scope, video_id, position_ms, sort_at_ms, updated_at_ms) VALUES ('all', 'sample_video', 0, 100, 200)",
            )
        }

        helper.runMigrationsAndValidate(47).use { db ->
            db.prepare("SELECT video_id, order_position, updated_at_ms FROM moments_cursors WHERE scope = 'all'").use { cursor ->
                assertTrue(cursor.step())
                assertEquals("sample_video", cursor.getText(0))
                assertEquals(42L, cursor.getLong(1))
                assertEquals(200L, cursor.getLong(2))
            }
        }
    }

    @Test
    fun migration47To48KeepsSavedArticleBodiesAndProfiles() {
        helper.createDatabase(47).use { db ->
            db.execSQL("INSERT INTO feed_items (tweet_id, body_text, is_retweet, quote_published_at, is_reply, is_ghost, published_at) VALUES ('sample_post', 'Saved body', 0, 0, 0, 0, 100)")
            db.execSQL("INSERT INTO channel_profiles (channel_id, platform, followers, following, verified, protected) VALUES ('twitter_sample', 'twitter', 5, 2, 0, 0)")
        }
        helper.runMigrationsAndValidate(48).use { db ->
            db.prepare("SELECT body_text, article_title, quote_article_title FROM feed_items WHERE tweet_id = 'sample_post'").use { cursor ->
                assertTrue(cursor.step())
                assertEquals("Saved body", cursor.getText(0))
                assertTrue(cursor.isNull(1))
                assertTrue(cursor.isNull(2))
            }
            db.prepare("SELECT followers, account_region FROM channel_profiles WHERE channel_id = 'twitter_sample'").use { cursor ->
                assertTrue(cursor.step())
                assertEquals(5, cursor.getLong(0).toInt())
                assertTrue(cursor.isNull(1))
            }
        }
    }

    @Test
    fun migration48To49KeepsArticleAndSavedStateWhileAddingPollsAndNotes() {
        helper.createDatabase(48).use { db ->
            db.execSQL("INSERT INTO feed_items (tweet_id, body_text, article_title, is_retweet, quote_published_at, is_reply, is_ghost, published_at) VALUES ('sample_post', 'Saved body', 'Saved article', 0, 0, 0, 0, 100)")
            db.execSQL("INSERT INTO feed_likes (tweet_id, liked_at) VALUES ('sample_post', 200)")
        }
        helper.runMigrationsAndValidate(49).use { db ->
            db.prepare("SELECT article_title, poll_json, quote_poll_json, community_note, quote_community_note FROM feed_items WHERE tweet_id = 'sample_post'").use { cursor ->
                assertTrue(cursor.step())
                assertEquals("Saved article", cursor.getText(0))
                (1..4).forEach { assertTrue(cursor.isNull(it)) }
            }
            db.prepare("SELECT liked_at FROM feed_likes WHERE tweet_id = 'sample_post'").use { cursor ->
                assertTrue(cursor.step())
                assertEquals(200L, cursor.getLong(0))
            }
        }
    }

    @Test
    fun migration49To50KeepsProfileRegionAndIdentity() {
        helper.createDatabase(49).use { db ->
            db.execSQL("INSERT INTO channel_profiles (channel_id, platform, display_name, account_region, followers, following, verified, protected) VALUES ('twitter_sample', 'twitter', 'Sample Name', 'United States', 5, 2, 0, 0)")
        }
        helper.runMigrationsAndValidate(50).use { db ->
            db.prepare("SELECT display_name, account_region, account_details_json FROM channel_profiles WHERE channel_id = 'twitter_sample'").use { cursor ->
                assertTrue(cursor.step())
                assertEquals("Sample Name", cursor.getText(0))
                assertEquals("United States", cursor.getText(1))
                assertTrue(cursor.isNull(2))
            }
        }
    }

    private companion object {
        const val DATABASE_NAME = "igloo-migration-test"
    }
}
