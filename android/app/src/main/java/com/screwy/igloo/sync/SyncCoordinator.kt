package com.screwy.igloo.sync

import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.log.Logger
import com.screwy.igloo.net.AndroidSyncHttpException
import com.screwy.igloo.net.Reachability
import com.screwy.igloo.outbox.OutboxDrainer
import com.screwy.igloo.outbox.OutboxRejectedMutation
import java.util.concurrent.atomic.AtomicBoolean
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

interface SyncMirror {
    suspend fun syncMetadataOnce(protectionChanged: Boolean)

    suspend fun syncAssetsOnce(): AssetSyncResult

    suspend fun syncPriorityStateOnce(): PriorityStateSyncResult

    suspend fun reconcileRejected(rejected: List<OutboxRejectedMutation>)
}

data class PriorityStateSyncResult(
    val metadataRequired: Boolean = false,
)

data class AssetSyncResult(
    val nextAttemptAtMs: Long? = null,
)

internal class AndroidSyncMirrorRunner(
    private val mirror: AndroidSyncMirror,
) : SyncMirror {
    override suspend fun syncMetadataOnce(protectionChanged: Boolean) {
        mirror.syncMetadataOnce(protectionChanged)
    }

    override suspend fun syncAssetsOnce(): AssetSyncResult = mirror.syncAssetsOnce()

    override suspend fun syncPriorityStateOnce(): PriorityStateSyncResult =
        mirror.syncPriorityStateOnce()

    override suspend fun reconcileRejected(rejected: List<OutboxRejectedMutation>) {
        mirror.reconcileRejected(rejected)
    }
}

/**
 * Owns independent action, metadata, and asset lanes. User actions and Live metadata refreshes can
 * reach the server while assets are still downloading; full passes still await all three in order.
 */
class SyncCoordinator(
    private val scope: CoroutineScope,
    private val outbox: OutboxDrainer,
    private val mirror: SyncMirror,
    private val syncIntervalMinutes: Flow<Int>,
    private val retentionSettings: Flow<List<Int>>,
    private val reachability: Reachability,
    private val foregroundFlow: Flow<Boolean>,
    private val logger: Logger,
    private val nowMsProvider: () -> Long = System::currentTimeMillis,
) {
    private val actionTriggers = Channel<Unit>(Channel.CONFLATED)
    private val metadataTriggers = Channel<Unit>(Channel.CONFLATED)
    private val assetTriggers = Channel<Unit>(Channel.CONFLATED)
    private val actionMutex = Mutex()
    private val metadataPassMutex = Mutex()
    private val assetMutex = Mutex()
    private val probeBeforeActionDrain = AtomicBoolean(false)
    private val protectionChanged = AtomicBoolean(false)
    private val jobs = mutableListOf<Job>()
    private val actionRetryWake =
        DeadlineWake(scope, nowMsProvider) { triggerActions(probeIfOffline = true) }
    private val metadataRetryWake = DeadlineWake(scope, nowMsProvider, ::triggerMetadata)
    private val assetRetryWake = DeadlineWake(scope, nowMsProvider, ::triggerAssets)

    fun start() {
        if (jobs.any(Job::isActive)) return
        jobs.clear()
        jobs += scope.launch { runActions() }
        jobs += scope.launch { runMetadata() }
        jobs += scope.launch { runAssets() }
        jobs += scope.launch {
            foregroundFlow.drop(1).collect { foreground ->
                if (foreground) triggerAll()
            }
        }
        jobs += scope.launch { pollIncomingStateWhileForeground() }
        jobs += scope.launch {
            var previous = reachability.state.value
            reachability.state.collect { current ->
                if (previous is Reachability.State.Offline && current is Reachability.State.Online) {
                    triggerAll()
                }
                previous = current
            }
        }
        jobs += scope.launch {
            retentionSettings.distinctUntilChanged().drop(1).collect { triggerAll() }
        }
        triggerAll()
    }

    fun stopAll() {
        jobs.forEach(Job::cancel)
        jobs.clear()
        actionRetryWake.cancel()
        metadataRetryWake.cancel()
        assetRetryWake.cancel()
    }

    /** Full convergence for manual refreshes and data-owner changes. */
    fun trigger() = triggerAll()

    fun triggerAll() {
        triggerActions(probeIfOffline = true)
        triggerMetadata()
    }

    /** Wake only the outgoing action lane. */
    fun triggerActions(probeIfOffline: Boolean = false) {
        if (probeIfOffline) probeBeforeActionDrain.set(true)
        actionTriggers.trySend(Unit)
    }

    /** WorkManager and explicit passes preserve drain-before-mirror ordering. */
    suspend fun pass(): Boolean {
        val actionPass =
            actionMutex.withLock { executeActionPass(probeIfOffline = true) }
        if (!actionPass.canReachServer) return false
        metadataPassMutex.withLock { executeMetadataPass() }
        try {
            val assets = assetMutex.withLock { mirror.syncAssetsOnce() }
            assetRetryWake.schedule(assets.nextAttemptAtMs)
        } catch (e: AndroidSyncAssetChangedException) {
            assetRetryWake.schedule(e.nextAttemptAtMs)
            metadataPassMutex.withLock { executeMetadataPass() }
            val assets = assetMutex.withLock { mirror.syncAssetsOnce() }
            assetRetryWake.schedule(assets.nextAttemptAtMs)
        }
        return true
    }

    private fun triggerMetadata() {
        metadataTriggers.trySend(Unit)
    }

    private fun triggerAssets() {
        assetTriggers.trySend(Unit)
    }

    private suspend fun runActions() {
        for (ignored in actionTriggers) {
            try {
                val result =
                    actionMutex.withLock {
                        executeActionPass(probeBeforeActionDrain.getAndSet(false))
                    }
                if (result.metadataRequired) triggerMetadata()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                logger.error("action_sync_failed", throwable = e)
            }
        }
    }

    private suspend fun runMetadata() {
        for (ignored in metadataTriggers) {
            var metadataStarted = false
            try {
                val actionPass =
                    actionMutex.withLock { executeActionPass(probeIfOffline = true) }
                if (!actionPass.canReachServer) {
                    scheduleMetadataRetry()
                    continue
                }
                metadataStarted = true
                metadataPassMutex.withLock { executeMetadataPass() }
                triggerAssets()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                if (metadataStarted && e.isRetryableMetadataFailure()) {
                    scheduleMetadataRetry()
                } else if (metadataStarted) {
                    metadataRetryWake.cancel()
                }
                logger.error("metadata_sync_failed", throwable = e)
            }
        }
    }

    private suspend fun runAssets() {
        for (ignored in assetTriggers) {
            try {
                val result = assetMutex.withLock { mirror.syncAssetsOnce() }
                assetRetryWake.schedule(result.nextAttemptAtMs)
            } catch (e: CancellationException) {
                throw e
            } catch (e: AndroidSyncAssetChangedException) {
                assetRetryWake.schedule(e.nextAttemptAtMs)
                triggerMetadata()
            } catch (e: Exception) {
                logger.error("asset_sync_failed", throwable = e)
            }
        }
    }

    private suspend fun executeActionPass(probeIfOffline: Boolean): ActionPassResult {
        if (
            probeIfOffline &&
                reachability.state.value is Reachability.State.Offline &&
                !reachability.probeNow()
        ) {
            actionRetryWake.schedule(outbox.runOnce().nextAttemptAtMs)
            return ActionPassResult(canReachServer = false)
        }

        var metadataRequired = false
        while (true) {
            val result = outbox.runOnce()
            if (result.protectionChanged) {
                protectionChanged.set(true)
                metadataRequired = true
            }
            if (result.selectionExpanded) metadataRequired = true
            actionRetryWake.schedule(result.nextAttemptAtMs)
            if (result.rejectedMutations.isEmpty()) break
            mirror.reconcileRejected(result.rejectedMutations)
        }
        return ActionPassResult(
            canReachServer = true,
            metadataRequired = metadataRequired,
        )
    }

    private suspend fun executeMetadataPass() {
        val changed = protectionChanged.getAndSet(false)
        try {
            mirror.syncMetadataOnce(protectionChanged = changed)
        } catch (e: Exception) {
            if (changed) protectionChanged.set(true)
            throw e
        }
        metadataRetryWake.cancel()
    }

    private fun scheduleMetadataRetry() {
        metadataRetryWake.schedule(
            nowMsProvider() + METADATA_RETRY_WAKE_MS,
            preserveEarlier = true,
        )
    }

    private suspend fun pollIncomingStateWhileForeground() {
        combine(
                foregroundFlow.distinctUntilChanged(),
                syncIntervalMinutes.distinctUntilChanged(),
            ) { foreground, interval -> foreground to interval }
            .collectLatest { (foreground, interval) ->
                if (!foreground) return@collectLatest
                while (true) {
                    if (interval == PreferencesRepo.Defaults.SYNC_INTERVAL_LIVE) {
                        triggerMetadata()
                    } else {
                        try {
                            if (mirror.syncPriorityStateOnce().metadataRequired) {
                                triggerMetadata()
                            }
                        } catch (e: CancellationException) {
                            throw e
                        } catch (e: Exception) {
                            if (e.isLikelyTransportFailure()) reachability.downgrade()
                            logger.info(
                                "priority_state_sync_failed",
                                mapOf("error" to (e.message ?: e::class.simpleName.orEmpty())),
                            )
                        }
                    }
                    delay(FOREGROUND_POLL_MS)
                }
            }
    }

    private companion object {
        const val FOREGROUND_POLL_MS = 5_000L
        const val METADATA_RETRY_WAKE_MS = 30_000L
    }
}

private data class ActionPassResult(
    val canReachServer: Boolean,
    val metadataRequired: Boolean = false,
)

private class DeadlineWake(
    private val scope: CoroutineScope,
    private val nowMsProvider: () -> Long,
    private val wake: () -> Unit,
) {
    private val lock = Any()
    private var job: Job? = null
    private var atMs: Long? = null

    fun schedule(nextAtMs: Long?, preserveEarlier: Boolean = false) {
        synchronized(lock) {
            if (nextAtMs == null) {
                cancelLocked()
                return
            }
            val currentAt = atMs
            if (
                job?.isActive == true &&
                    (currentAt == nextAtMs ||
                        (preserveEarlier && currentAt != null && currentAt <= nextAtMs))
            ) {
                return
            }

            job?.cancel()
            atMs = nextAtMs
            job =
                scope.launch {
                    delay((nextAtMs - nowMsProvider()).coerceAtLeast(0L))
                    val stillOwned =
                        synchronized(lock) {
                            if (atMs != nextAtMs) return@synchronized false
                            atMs = null
                            job = null
                            true
                        }
                    if (stillOwned) wake()
                }
        }
    }

    fun cancel() {
        synchronized(lock) { cancelLocked() }
    }

    private fun cancelLocked() {
        job?.cancel()
        job = null
        atMs = null
    }
}

private fun Throwable.isRetryableMetadataFailure(): Boolean =
    isLikelyTransportFailure() ||
        generateSequence(this) { it.cause }
            .any { cause -> cause is AndroidSyncHttpException && cause.isTransient }
