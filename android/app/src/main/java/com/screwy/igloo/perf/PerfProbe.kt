package com.screwy.igloo.perf

import android.os.Build
import android.os.SystemClock
import android.os.Trace
import android.util.Log
import java.util.Locale
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

internal object PerfProbe {
    private const val TAG = "IglooPerf"
    private const val MAX_SECTION_NAME = 120
    private val collectorCounts = ConcurrentHashMap<String, AtomicInteger>()
    private val counters = ConcurrentHashMap<String, AtomicInteger>()
    private val asyncCookies = AtomicInteger(1)

    fun logsEnabled(): Boolean = Log.isLoggable(TAG, Log.DEBUG)

    fun begin(section: String) {
        Trace.beginSection(sectionName(section))
    }

    fun end() {
        Trace.endSection()
    }

    fun beginAsync(section: String): Int {
        val cookie = asyncCookies.getAndIncrement()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            Trace.beginAsyncSection(sectionName(section), cookie)
        }
        return cookie
    }

    fun endAsync(section: String, cookie: Int) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            Trace.endAsyncSection(sectionName(section), cookie)
        }
    }

    inline fun <T> trace(section: String, block: () -> T): T {
        begin(section)
        return try {
            block()
        } finally {
            end()
        }
    }

    inline fun <T> timed(
        event: String,
        fields: Map<String, Any?> = emptyMap(),
        block: () -> T,
    ): T {
        val started = SystemClock.elapsedRealtimeNanos()
        val cookie = beginAsync(event)
        return try {
            block()
        } finally {
            endAsync(event, cookie)
            log(event, fields + ("duration_ms" to elapsedMsSince(started)))
        }
    }

    suspend inline fun <T> timedSuspend(
        event: String,
        fields: Map<String, Any?> = emptyMap(),
        crossinline block: suspend () -> T,
    ): T {
        val started = SystemClock.elapsedRealtimeNanos()
        val cookie = beginAsync(event)
        return try {
            block()
        } finally {
            endAsync(event, cookie)
            log(event, fields + ("duration_ms" to elapsedMsSince(started)))
        }
    }

    fun log(event: String, fields: Map<String, Any?> = emptyMap()) {
        if (!logsEnabled()) return
        Log.d(TAG, formatLine(event, fields))
    }

    fun incrementCounter(name: String, delta: Int = 1): Int {
        val value = counters.getOrPut(name) { AtomicInteger(0) }.addAndGet(delta)
        setCounter(name, value)
        return value
    }

    fun setCounter(name: String, value: Int) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            Trace.setCounter(sectionName(name), value.toLong())
        }
    }

    fun collectorStart(kind: String, fields: Map<String, Any?> = emptyMap()): String {
        val key = fields.entries
            .joinToString(separator = "|", prefix = kind) { (field, value) -> "$field=$value" }
        val active = collectorCounts.getOrPut(key) { AtomicInteger(0) }.incrementAndGet()
        setCounter("${counterName(kind)}_active", active)
        log("${kind}_collector_start", fields + ("active" to active))
        return key
    }

    fun collectorEnd(kind: String, key: String, fields: Map<String, Any?> = emptyMap()) {
        val active = collectorCounts[key]?.decrementAndGet()?.coerceAtLeast(0) ?: 0
        setCounter("${counterName(kind)}_active", active)
        log("${kind}_collector_end", fields + ("active" to active))
    }

    fun roomQuery(sql: String, argCount: Int) {
        val count = incrementCounter("igloo_room_query_count")
        log(
            event = "room_query",
            fields = mapOf(
                "count" to count,
                "op" to sql.trimStart().substringBefore(' ', missingDelimiterValue = "").uppercase(Locale.US),
                "tables" to roomTables(sql),
                "args" to argCount,
            ),
        )
    }

    fun roomInvalidated(tables: Set<String>) {
        val count = incrementCounter("igloo_room_invalidation_count")
        log(
            event = "room_invalidated",
            fields = mapOf(
                "count" to count,
                "tables" to tables.sorted().joinToString(","),
            ),
        )
    }

    fun uriKind(value: Any?): String = when {
        value == null -> "null"
        value.javaClass.simpleName == "Local" -> "local"
        value.javaClass.simpleName == "Remote" -> "remote"
        else -> "missing"
    }

    fun elapsedMsSince(startedNanos: Long): Long =
        TimeUnit.NANOSECONDS.toMillis(SystemClock.elapsedRealtimeNanos() - startedNanos)

    private fun sectionName(raw: String): String {
        val clean = raw.replace('\n', ' ').replace('\r', ' ')
        return if (clean.length <= MAX_SECTION_NAME) clean else clean.take(MAX_SECTION_NAME)
    }

    private fun counterName(raw: String): String =
        raw.lowercase(Locale.US).replace(Regex("[^a-z0-9_]+"), "_").trim('_')

    private fun formatLine(event: String, fields: Map<String, Any?>): String {
        if (fields.isEmpty()) return event
        return buildString {
            append(event)
            fields.entries
                .sortedBy { it.key }
                .forEach { (key, value) ->
                    append(' ')
                    append(key)
                    append('=')
                    append(value)
                }
        }
    }

    private fun roomTables(sql: String): String {
        val lower = sql.lowercase(Locale.US)
        val tables = KnownTables.filter { table ->
            lower.contains(" $table") || lower.contains("`$table`")
        }
        return tables.joinToString(",").ifBlank { "unknown" }
    }

    private val KnownTables = listOf(
        "android_sync_assets",
        "android_sync_generations",
        "android_sync_items",
        "media_inventory",
        "videos",
        "feed_items",
        "feed_likes",
        "bookmarks",
        "bookmark_categories",
        "bookmark_labels",
        "moment_views",
        "watch_history",
        "channels",
        "channel_profiles",
        "channel_follows",
        "channel_stars",
        "channel_settings",
        "outbox",
        "preferences",
        "feed_seen",
        "feed_rank",
        "feed_thread_context",
        "retweet_sources",
        "sponsorblock_segments",
        "sponsorblock_checked",
        "video_comments",
        "video_repost_sources",
    )
}
