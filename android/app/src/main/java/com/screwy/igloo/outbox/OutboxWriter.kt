package com.screwy.igloo.outbox

import androidx.room.withTransaction
import com.screwy.igloo.data.IglooDatabase
import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.data.entity.MomentsCursorEntity
import com.screwy.igloo.data.entity.OutboxEntity
import com.screwy.igloo.log.LogEntry
import com.screwy.igloo.log.LogLevel
import com.screwy.igloo.log.LogSink
import com.screwy.igloo.net.iglooJson
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonObjectBuilder
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.put

/** Persists user mutations and applies safe optimistic sets in one Room transaction. */
@OptIn(FlowPreview::class)
class OutboxWriter(
    private val db: IglooDatabase,
    private val prefs: PreferencesRepo,
    private val scope: CoroutineScope,
    private val onDrainRequested: (probeIfOffline: Boolean) -> Unit = {},
    private val nowMsProvider: () -> Long = { System.currentTimeMillis() },
    private val writeDebounceMs: Long = WRITE_DEBOUNCE_MS,
) {

    private val _debounceSignal =
        MutableSharedFlow<Unit>(
            replay = 0,
            extraBufferCapacity = 8,
            onBufferOverflow = BufferOverflow.DROP_OLDEST,
        )

    init {
        scope.launch {
            _debounceSignal.debounce(writeDebounceMs).collect {
                onDrainRequested(false)
            }
        }
    }

    // ─── Enqueue ───────────────────────────────────────────────────────────────

    suspend fun enqueue(kind: OutboxKind) {
        if (kind is OutboxKind.MomentsCursor) {
            recordMomentsCursor(
                videoId = kind.videoId,
                positionMs = kind.positionMs,
                scope = kind.scope,
                sortAtMs = kind.sortAtMs,
                orderPosition = null,
            )
            return
        }
        val nowMs = serverCorrectedNowMs()
        db.withTransaction {
            val outbox = db.outboxDao()
            val previousPending = outbox.pendingRows().firstOrNull { it.matches(kind) }
            val selectionBaseline =
                previousPending?.selectionBaseline() ?: selectionBaseline(kind)
            val selectionWidening = selectionBaseline?.expandsTo(kind) == true
            val row =
                buildOutboxRow(kind, nowMs, selectionWidening, selectionBaseline)
            when (kind.coalesceKey) {
                OutboxKind.CoalesceKey.ByKindItemField -> outbox.coalesceAndInsert(row)
                OutboxKind.CoalesceKey.Fifo -> outbox.insert(row)
            }
            applyOptimisticMutation(db, row)
        }
        if (kind.isInteractiveAction) {
            onDrainRequested(true)
        } else if (kind !is OutboxKind.Log && kind !is OutboxKind.LogDebug) {
            _debounceSignal.tryEmit(Unit)
        }
    }

    suspend fun recordMomentsCursor(
        videoId: String,
        positionMs: Long,
        scope: String,
        sortAtMs: Long? = null,
        orderPosition: Long? = null,
    ) {
        val normalized = PreferencesRepo.Defaults.normalizeMomentsTab(scope)
        db.withTransaction {
            val cursorDao = db.momentsCursorDao()
            val current = cursorDao.get(normalized)

            val updatedAtMs = nextMomentsCursorTimestamp(current?.updatedAtMs)
            val next =
                MomentsCursorEntity(
                    scope = normalized,
                    videoId = videoId,
                    positionMs = positionMs,
                    sortAtMs = sortAtMs ?: 0L,
                    orderPosition = orderPosition ?: 0L,
                    updatedAtMs = updatedAtMs,
                )
            if (normalized == "stories") {
                cursorDao.upsert(next)
            } else {
                val kind =
                    OutboxKind.MomentsCursor(
                        videoId = videoId,
                        positionMs = positionMs,
                        scope = normalized,
                        sortAtMs = sortAtMs,
                        orderPosition = orderPosition,
                    )
                db.outboxDao().coalesceAndInsert(buildOutboxRow(kind, updatedAtMs))
                cursorDao.upsert(next)
            }
        }
        if (normalized != "stories") _debounceSignal.tryEmit(Unit)
    }

    // ─── Row construction ──────────────────────────────────────────────────────

    private fun buildOutboxRow(
        kind: OutboxKind,
        nowMs: Long,
        selectionWidening: Boolean = false,
        selectionBaseline: SelectionBaseline? = null,
    ): OutboxEntity {
        val payload = buildPayload(kind, nowMs, selectionWidening, selectionBaseline)
        return OutboxEntity(
            kind = kind.code,
            itemId = kind.itemId,
            field = kind.field,
            payloadJson = payload,
            state = "pending",
            attemptCount = 0,
            nextAttemptAtMs = 0,
            lastErrorCode = null,
            lastErrorBody = null,
            createdAtMs = nowMs,
        )
    }

    private fun buildPayload(
        kind: OutboxKind,
        nowMs: Long,
        selectionWidening: Boolean,
        selectionBaseline: SelectionBaseline?,
    ): String {
        val obj = buildJsonObject {
            put("updated_at_ms", nowMs)
            if (selectionWidening) put(SELECTION_WIDENING_KEY, true)
            selectionBaseline?.let { put(SELECTION_BASELINE_KEY, it.value) }
            when (kind) {
                is OutboxKind.Like -> {
                    put("tweet_id", kind.tweetId)
                    put("action", kind.action.wire)
                }
                is OutboxKind.Bookmark -> {
                    put("video_id", kind.videoId)
                    put("action", kind.action.wire)
                    kind.categoryId?.let { put("category_id", it) }
                    kind.customTitle?.let { put("custom_title", it) }
                    kind.accountHandles?.let { put("account_handles", it) }
                    kind.mediaIndices?.let { put("media_indices", it) }
                }
                is OutboxKind.Follow -> {
                    put("channel_id", kind.channelId)
                    put("action", kind.action.wire)
                }
                is OutboxKind.Star -> {
                    put("channel_id", kind.channelId)
                    put("action", kind.action.wire)
                }
                is OutboxKind.Mute -> {
                    put("channel_id", kind.channelId)
                    put("action", kind.action.wire)
                }
                is OutboxKind.ChannelSetting -> {
                    put("channel_id", kind.channelId)
                    put("field", kind.settingField)
                    kind.value?.let { put("value", it) }
                }
                is OutboxKind.Seen -> put("tweet_id", kind.tweetId)
                is OutboxKind.MomentView -> put("video_id", kind.videoId)
                is OutboxKind.Progress -> {
                    put("video_id", kind.videoId)
                    put("position", kind.position)
                    put("duration", kind.duration)
                }
                is OutboxKind.MomentsCursor -> {
                    put("video_id", kind.videoId)
                    put("position_ms", kind.positionMs)
                    put("scope", kind.scope)
                    kind.sortAtMs?.takeIf { it > 0L }?.let { put("sort_at_ms", it) }
                    kind.orderPosition?.takeIf { it > 0L }?.let { put("order_position", it) }
                }
                is OutboxKind.CreateCategory -> {
                    put("name", kind.name)
                    put("provisional_id", kind.provisionalId)
                    put("request_id", kind.requestId)
                }
                is OutboxKind.Log -> {
                    put("level", kind.level)
                    put("event", kind.event)
                    put("timestamp_ms", kind.timestampMs)
                    putStringMap("fields", kind.fields)
                }
                is OutboxKind.LogDebug -> {
                    put("event", kind.event)
                    put("timestamp_ms", kind.timestampMs)
                    putStringMap("fields", kind.fields)
                }
            }
        }
        return obj.toString()
    }

    // ─── Log sink plumbing ────────────────────────────────────────────────────

    /**
     * `LogSink` implementation — Logger calls into this and we enqueue log rows. Kept inline (not a
     * separate class) so one object owns both the queue-side contract and the outbox transaction.
     */
    val logSink: LogSink = LogSink { entry -> enqueue(entry.toOutboxKind()) }

    private fun LogEntry.toOutboxKind(): OutboxKind {
        val stringFields = fields.mapValues { (_, v) -> v?.toString() ?: "" }
        return when (level) {
            LogLevel.Debug -> OutboxKind.LogDebug(event, stringFields, timestampMs)
            LogLevel.Info ->
                OutboxKind.Log(
                    level = "info",
                    event = event,
                    fields = stringFields,
                    timestampMs = timestampMs,
                )
            LogLevel.Error ->
                OutboxKind.Log(
                    level = "error",
                    event = event,
                    fields = stringFields,
                    timestampMs = timestampMs,
                )
        }
    }

    // ─── Internal helpers ─────────────────────────────────────────────────────

    private fun serverCorrectedNowMs(): Long = nowMsProvider() + prefs.serverTimeOffsetMsSync()

    private suspend fun selectionBaseline(kind: OutboxKind): SelectionBaseline? =
        when (kind) {
            is OutboxKind.Follow ->
                SelectionBaseline(if (db.channelFollowDao().exists(kind.channelId)) 1 else 0)
            is OutboxKind.ChannelSetting -> {
                if (kind.settingField != "include_reposts") {
                    null
                } else {
                    val previous = db.channelSettingDao().getById(kind.channelId)?.includeReposts
                    SelectionBaseline(previous ?: SELECTION_INHERIT_VALUE)
                }
            }
            else -> null
        }

    private fun nextMomentsCursorTimestamp(currentUpdatedAtMs: Long?): Long {
        val afterCurrent =
            when (currentUpdatedAtMs) {
                null -> Long.MIN_VALUE
                Long.MAX_VALUE -> Long.MAX_VALUE
                else -> currentUpdatedAtMs + 1L
            }
        return maxOf(serverCorrectedNowMs(), afterCurrent)
    }

    private fun JsonObjectBuilder.putJsonObject(key: String, block: JsonObjectBuilder.() -> Unit) {
        put(key, buildJsonObject(block))
    }

    private fun JsonObjectBuilder.putStringMap(key: String, map: Map<String, String>) {
        put(key, buildJsonObject { map.forEach { (k, v) -> put(k, v) } })
    }

    companion object {
        const val WRITE_DEBOUNCE_MS: Long = 3_000L
    }
}

internal const val SELECTION_WIDENING_KEY = "selection_widening"
private const val SELECTION_BASELINE_KEY = "selection_baseline"
private const val SELECTION_INHERIT_VALUE = -1

private data class SelectionBaseline(val value: Int) {
    fun expandsTo(kind: OutboxKind): Boolean =
        when (kind) {
            is OutboxKind.Follow -> value == 0 && kind.action == OutboxKind.Action.Set
            is OutboxKind.ChannelSetting -> {
                if (kind.settingField != "include_reposts") return false
                val previous = value.takeUnless { it == SELECTION_INHERIT_VALUE }
                val next = kind.value?.toInt()
                (previous == 0 && next != 0) || (previous == null && next == 1)
            }
            else -> false
        }
}

internal fun OutboxEntity.selectionWidening(): Boolean =
    runCatching {
            val payload = iglooJson.parseToJsonElement(payloadJson).jsonObject
            (payload[SELECTION_WIDENING_KEY] as? JsonPrimitive)?.booleanOrNull == true
        }
        .getOrDefault(false)

private fun OutboxEntity.matches(kind: OutboxKind): Boolean =
    this.kind == kind.code && itemId == kind.itemId && field == kind.field

private fun OutboxEntity.selectionBaseline(): SelectionBaseline? =
    runCatching {
            val payload = iglooJson.parseToJsonElement(payloadJson).jsonObject
            (payload[SELECTION_BASELINE_KEY] as? JsonPrimitive)
                ?.content
                ?.toIntOrNull()
                ?.let(::SelectionBaseline)
        }
        .getOrNull()
