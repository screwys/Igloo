package com.screwy.igloo.data

import android.content.Context
import android.database.sqlite.SQLiteDatabase
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.json.JSONObject
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.io.File

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], manifest = Config.NONE)
class IglooMigrationTest {
    @Test fun committedRoomSchemasStartAtSupportedBaseline() {
        val schemaDir = schemaDir()
        assertTrue("missing Room schema directory: ${schemaDir.absolutePath}", schemaDir.isDirectory)

        val versions = schemaDir.listFiles()
            .orEmpty()
            .mapNotNull { file -> file.name.removeSuffix(".json").toIntOrNull() }
            .sorted()

        assertFalse("Room schema directory has no JSON snapshots", versions.isEmpty())
        assertEquals(
            (IglooMigrations.SUPPORTED_SCHEMA_BASELINE_VERSION..IglooMigrations.CURRENT_SCHEMA_VERSION).toList(),
            versions,
        )
    }

    @Test fun migration29To30AddsLanguageSourceColumnsWithoutDroppingFeedRows() {
        val dbName = "igloo-migration-29-30"
        val context: Context = ApplicationProvider.getApplicationContext()
        val sqlite = createDatabaseFromSchemaSnapshot(
            context,
            dbName,
            IglooMigrations.SUPPORTED_SCHEMA_BASELINE_VERSION,
        )
        try {
            sqlite.execSQL(
                """
                INSERT INTO feed_items (
                    tweet_id, author_handle, body_text, quote_body_text,
                    is_retweet, quote_published_at, is_reply, is_ghost,
                    published_at, sync_seq
                ) VALUES (
                    'tweet-29', 'author', 'body', 'quote',
                    0, 0, 0, 0,
                    123, 7
                )
                """.trimIndent(),
            )
        } finally {
            sqlite.close()
        }

        val roomDb = Room.databaseBuilder(context, IglooDatabase::class.java, dbName)
            .addMigrations(IglooMigrations.MIGRATION_29_30)
            .allowMainThreadQueries()
            .build()

        try {
            val readable = roomDb.openHelper.readableDatabase
            assertEquals(IglooMigrations.CURRENT_SCHEMA_VERSION, readable.version)
            val cursor = readable.query(
                """
                SELECT body_text, body_source_lang, quote_body_text, quote_source_lang
                FROM feed_items
                WHERE tweet_id = 'tweet-29'
                """.trimIndent(),
            )
            cursor.use {
                assertTrue(it.moveToFirst())
                assertEquals("body", it.getString(0))
                assertNull(it.getString(1))
                assertEquals("quote", it.getString(2))
                assertNull(it.getString(3))
            }
        } finally {
            roomDb.close()
            context.deleteDatabase(dbName)
        }
    }

    private fun createDatabaseFromSchemaSnapshot(
        context: Context,
        dbName: String,
        version: Int,
    ): SQLiteDatabase {
        context.deleteDatabase(dbName)
        val dbFile = context.getDatabasePath(dbName)
        dbFile.parentFile?.mkdirs()
        val db = SQLiteDatabase.openOrCreateDatabase(dbFile, null)
        val database = JSONObject(schemaFile(version).readText()).getJSONObject("database")

        val entities = database.getJSONArray("entities")
        for (i in 0 until entities.length()) {
            val entity = entities.getJSONObject(i)
            val tableName = entity.getString("tableName")
            db.execSQL(entity.getString("createSql").replace(TABLE_NAME_PLACEHOLDER, tableName))

            val indices = entity.optJSONArray("indices") ?: continue
            for (j in 0 until indices.length()) {
                db.execSQL(
                    indices.getJSONObject(j)
                        .getString("createSql")
                        .replace(TABLE_NAME_PLACEHOLDER, tableName),
                )
            }
        }

        val setupQueries = database.getJSONArray("setupQueries")
        for (i in 0 until setupQueries.length()) {
            db.execSQL(setupQueries.getString(i))
        }
        db.version = version
        return db
    }

    private fun schemaFile(version: Int): File = File(schemaDir(), "$version.json")

    private fun schemaDir(): File = File("schemas/${IglooDatabase::class.java.canonicalName}")

    private companion object {
        const val TABLE_NAME_PLACEHOLDER = "\${TABLE_NAME}"
    }
}
