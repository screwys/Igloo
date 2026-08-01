package com.screwy.igloo.outbox

import androidx.room.withTransaction
import com.screwy.igloo.data.IglooDatabase
import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.data.dao.OutboxDao
import com.screwy.igloo.data.entity.OutboxEntity
import com.screwy.igloo.log.Logger
import com.screwy.igloo.net.IglooError
import com.screwy.igloo.net.Reachability
import com.screwy.igloo.net.iglooJson
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject

data class OutboxPassResult(
    val rejectedMutations: List<OutboxRejectedMutation> = emptyList(),
    val protectionChanged: Boolean = false,
    val selectionExpanded: Boolean = false,
    val nextAttemptAtMs: Long? = null,
)

data class OutboxRejectedMutation(
    val rowId: Long,
    val ownerKind: String,
    val ownerId: String,
)

interface OutboxDrainer {
    suspend fun runOnce(): OutboxPassResult
}

class OutboxDrain(
    private val outboxDao: OutboxDao,
    private val dispatcher: OutboxDispatcher,
    private val db: IglooDatabase,
    private val prefs: PreferencesRepo,
    private val reachability: Reachability,
    private val logger: Logger,
    private val nowMsProvider: () -> Long = { System.currentTimeMillis() },
) : OutboxDrainer {
    override suspend fun runOnce(): OutboxPassResult {
        dropPendingDebugLogsWhenDisabled()
        if (reachability.state.value is Reachability.State.Offline) {
            return result(protectionChanged = false)
        }

        var protectionChanged = false
        var selectionExpanded = false
        var authRefreshRequired = false
        while (true) {
            val nowMs = nowMsProvider()
            val batch = outboxDao.claimPending(nowMs, CLAIM_LIMIT)
            if (batch.isEmpty()) break
            for (group in groupForDispatch(batch, nowMs)) {
                val results = dispatcher.dispatch(group)
                val acked = group.filter { results[it.id] == OutboxDispatcher.Result.Ack }
                if (applyAcks(acked)) selectionExpanded = true
                if (acked.any(OutboxEntity::isProtectionClear)) protectionChanged = true
                val rejected = mutableListOf<OutboxRejectedMutation>()
                for (row in group) {
                    when (val result = results[row.id] ?: continue) {
                        OutboxDispatcher.Result.Ack -> Unit
                        is OutboxDispatcher.Result.Retry -> scheduleRetry(row, result.error)
                        is OutboxDispatcher.Result.Rejected -> {
                            val owner = row.authoritativeOwner()
                            if (owner == null) {
                                discardRejected(row, result.error)
                            } else {
                                logger.info(
                                    event = "outbox_owner_reconcile_required",
                                    fields =
                                        mapOf(
                                            "id" to row.id.toString(),
                                            "kind" to row.kind,
                                            "status" to (result.error.status?.toString() ?: "?"),
                                            "code" to result.error.errorCode.orEmpty(),
                                        ),
                                )
                                rejected +=
                                    OutboxRejectedMutation(
                                        rowId = row.id,
                                        ownerKind = owner.first,
                                        ownerId = owner.second,
                                    )
                            }
                        }
                        OutboxDispatcher.Result.AuthRefresh -> authRefreshRequired = true
                    }
                }
                if (rejected.isNotEmpty()) {
                    return result(rejected, protectionChanged, selectionExpanded)
                }
                if (authRefreshRequired) break
            }
            if (authRefreshRequired) break
            if (batch.size < CLAIM_LIMIT) break
        }
        return result(
            protectionChanged = protectionChanged,
            selectionExpanded = selectionExpanded,
        )
    }

    private suspend fun result(
        rejectedMutations: List<OutboxRejectedMutation> = emptyList(),
        protectionChanged: Boolean,
        selectionExpanded: Boolean = false,
    ): OutboxPassResult {
        val nowMs = nowMsProvider()
        val earliest = outboxDao.earliestPendingAttemptAtMs()
        return OutboxPassResult(
            rejectedMutations = rejectedMutations,
            protectionChanged = protectionChanged,
            selectionExpanded = selectionExpanded,
            nextAttemptAtMs =
                earliest?.let { if (it <= nowMs) nowMs + RETRY_WAKE_FLOOR_MS else it },
        )
    }

    private suspend fun groupForDispatch(
        firstClaim: List<OutboxEntity>,
        nowMs: Long,
    ): List<List<OutboxEntity>> {
        val seenBatch =
            if (firstClaim.any { it.kind == OutboxKind.CODE_SEEN }) {
                outboxDao.claimKind(OutboxKind.CODE_SEEN, nowMs, SEEN_BATCH_LIMIT)
            } else emptyList()
        val logBatch =
            if (firstClaim.any { it.kind == OutboxKind.CODE_LOG }) {
                outboxDao.claimKind(OutboxKind.CODE_LOG, nowMs, LOG_BATCH_LIMIT)
            } else emptyList()
        val debugBatch =
            if (firstClaim.any { it.kind == OutboxKind.CODE_LOG_DEBUG }) {
                outboxDao.claimKind(OutboxKind.CODE_LOG_DEBUG, nowMs, LOG_BATCH_LIMIT)
            } else emptyList()
        val batched = (seenBatch + logBatch + debugBatch).mapTo(hashSetOf()) { it.id }
        return buildList {
            firstClaim.filter { it.id !in batched }.forEach { add(listOf(it)) }
            if (seenBatch.isNotEmpty()) add(seenBatch)
            if (logBatch.isNotEmpty()) add(logBatch)
            if (debugBatch.isNotEmpty()) add(debugBatch)
        }
    }

    private suspend fun applyAcks(rows: List<OutboxEntity>): Boolean {
        if (rows.isEmpty()) return false
        var selectionExpanded = false
        db.withTransaction {
            val currentIds = outboxDao.rowsByIds(rows.map(OutboxEntity::id)).mapTo(hashSetOf()) { it.id }
            val currentRows = rows.filter { it.id in currentIds }
            selectionExpanded = currentRows.any(OutboxEntity::selectionWidening)
            if (selectionExpanded) {
                db.androidSyncDao().syncState()?.let { state ->
                    if (!state.bootstrapRequired) {
                        db.androidSyncDao()
                            .upsertSyncState(state.copy(bootstrapRequired = true))
                    }
                }
            }
            currentRows.filter(OutboxEntity::isClear).forEach { finalizeClear(it) }
            currentRows
                .filterNot(OutboxEntity::isLogKind)
                .map(OutboxEntity::id)
                .takeIf(List<Long>::isNotEmpty)
                ?.let { outboxDao.completeAndDeleteAll(it) }
            currentRows
                .filter(OutboxEntity::isLogKind)
                .map(OutboxEntity::id)
                .takeIf(List<Long>::isNotEmpty)
                ?.let {
                    outboxDao.markAcked(it)
                    outboxDao.trimAckedLogs(LOGS_INSPECTOR_CAP)
                }
        }
        return selectionExpanded
    }

    private suspend fun finalizeClear(row: OutboxEntity) {
        val id = row.itemId ?: return
        when (row.kind) {
            OutboxKind.CODE_LIKE -> db.feedLikeDao().delete(id)
            OutboxKind.CODE_BOOKMARK -> db.bookmarkDao().delete(id)
            OutboxKind.CODE_FOLLOW -> db.channelFollowDao().delete(id)
            OutboxKind.CODE_STAR -> db.channelStarDao().delete(id)
            OutboxKind.CODE_MUTE -> db.mutedChannelDao().delete(id)
        }
    }

    private suspend fun scheduleRetry(row: OutboxEntity, error: IglooError) {
        val attempts = row.attemptCount + 1
        outboxDao.markPending(
            id = row.id,
            attemptCount = attempts,
            nextAttemptAtMs = nowMsProvider() + backoffMs(attempts),
            errorCode = error.status,
            errorBody = error.errorMessage?.take(200),
        )
    }

    private suspend fun discardRejected(row: OutboxEntity, error: IglooError) {
        db.withTransaction {
            outboxDao.completeAndDelete(row.id)
        }
        if (!row.isLogKind()) {
            logger.error(
                event = "outbox_row_dead",
                fields =
                    mapOf(
                        "id" to row.id.toString(),
                        "kind" to row.kind,
                        "status" to (error.status?.toString() ?: "?"),
                        "code" to error.errorCode.orEmpty(),
                    ),
            )
        }
    }

    private suspend fun dropPendingDebugLogsWhenDisabled() {
        if (!prefs.debugModeSync()) outboxDao.deleteAllPendingOfKind(OutboxKind.CODE_LOG_DEBUG)
    }

    private fun backoffMs(attempts: Int): Long =
        when (attempts) {
            1 -> 30_000L
            2 -> 2 * 60_000L
            3 -> 10 * 60_000L
            4 -> 30 * 60_000L
            else -> 60 * 60_000L
        }

    private companion object {
        const val CLAIM_LIMIT = 100
        const val SEEN_BATCH_LIMIT = 500
        const val LOG_BATCH_LIMIT = 100
        const val LOGS_INSPECTOR_CAP = 500
        const val RETRY_WAKE_FLOOR_MS = 30_000L
    }
}

private fun OutboxEntity.payload(): JsonObject =
    runCatching { iglooJson.parseToJsonElement(payloadJson).jsonObject }
        .getOrDefault(JsonObject(emptyMap()))

private fun OutboxEntity.isClear(): Boolean =
    (payload()["action"] as? JsonPrimitive)?.contentOrNull == "clear"

private fun OutboxEntity.isProtectionClear(): Boolean =
    isClear() &&
        kind in
            setOf(
                OutboxKind.CODE_LIKE,
                OutboxKind.CODE_BOOKMARK,
                OutboxKind.CODE_FOLLOW,
                OutboxKind.CODE_STAR,
                OutboxKind.CODE_MUTE,
            )

private fun OutboxEntity.isLogKind(): Boolean =
    kind == OutboxKind.CODE_LOG || kind == OutboxKind.CODE_LOG_DEBUG

private fun OutboxEntity.authoritativeOwner(): Pair<String, String>? {
    val id = itemId?.takeIf(String::isNotBlank) ?: return null
    val ownerKind =
        when (kind) {
            OutboxKind.CODE_LIKE -> "feed_like"
            OutboxKind.CODE_BOOKMARK -> "bookmark"
            OutboxKind.CODE_FOLLOW -> "channel_follow"
            OutboxKind.CODE_STAR -> "channel_star"
            OutboxKind.CODE_MUTE -> "muted_channel"
            OutboxKind.CODE_CHANNEL_SETTING -> "channel_setting"
            OutboxKind.CODE_SEEN -> "feed_seen"
            OutboxKind.CODE_MOMENT_VIEW -> "moment_view"
            OutboxKind.CODE_PROGRESS -> "watch_history"
            OutboxKind.CODE_MOMENTS_CURSOR -> "moments_cursor"
            OutboxKind.CODE_CREATE_CATEGORY -> "bookmark_category"
            else -> return null
        }
    return ownerKind to id
}
