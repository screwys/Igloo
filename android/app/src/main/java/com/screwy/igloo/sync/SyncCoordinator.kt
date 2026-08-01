package com.screwy.igloo.sync

import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.log.Logger
import com.screwy.igloo.net.Reachability
import com.screwy.igloo.outbox.OutboxDrain
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.launch
import kotlinx.coroutines.isActive
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

class SyncCoordinator(
    private val scope: CoroutineScope,
    private val outbox: OutboxDrain,
    private val mirror: AndroidSyncMirror,
    private val prefs: PreferencesRepo,
    private val reachability: Reachability,
    private val foregroundFlow: Flow<Boolean>,
    private val logger: Logger,
) {
    private val triggers = Channel<Unit>(Channel.CONFLATED)
    private val passMutex = Mutex()
    private val jobs = mutableListOf<Job>()

    fun start() {
        if (jobs.any(Job::isActive)) return
        jobs.clear()
        jobs += scope.launch { run() }
        jobs += scope.launch {
            foregroundFlow.drop(1).collect { foreground ->
                if (foreground) trigger()
            }
        }
        jobs += scope.launch { pollPriorityStateWhileForeground() }
        jobs += scope.launch {
            var previous = reachability.state.value
            reachability.state.collect { current ->
                if (previous is Reachability.State.Offline && current is Reachability.State.Online) {
                    trigger()
                }
                previous = current
            }
        }
        jobs += scope.launch {
            combine(
                prefs.retentionDaysFeed(),
                prefs.retentionDaysMoments(),
                prefs.retentionDaysYoutube(),
                prefs.storiesWindowHours(),
            ) { feed, moments, youtube, stories -> listOf(feed, moments, youtube, stories) }
                .distinctUntilChanged()
                .drop(1)
                .collect { trigger() }
        }
        trigger()
    }

    fun stopAll() {
        jobs.forEach(Job::cancel)
        jobs.clear()
    }

    fun trigger() {
        triggers.trySend(Unit)
    }

    fun triggerAll() = trigger()

    suspend fun pass() {
        passMutex.withLock { executePass() }
    }

    private suspend fun run() {
        for (ignored in triggers) {
            try {
                pass()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                logger.error("sync_pass_failed", throwable = e)
            }
        }
    }

    private suspend fun executePass() {
        var protectionChanged = false
        while (true) {
            val outboxResult = outbox.runOnce()
            protectionChanged = protectionChanged || outboxResult.protectionChanged
            if (outboxResult.rejectedMutations.isEmpty()) break
            mirror.reconcileRejected(outboxResult.rejectedMutations)
        }
        mirror.syncOnce(protectionChanged = protectionChanged)
    }

    private suspend fun pollPriorityStateWhileForeground() = coroutineScope {
        var pollingJob: Job? = null
        try {
            foregroundFlow.collect { foreground ->
                pollingJob?.cancel()
                pollingJob =
                    if (foreground) {
                        launch {
                            while (isActive) {
                                try {
                                    mirror.syncPriorityStateOnce()
                                } catch (e: CancellationException) {
                                    throw e
                                } catch (e: Exception) {
                                    if (e.isLikelyTransportFailure()) reachability.downgrade()
                                    logger.info(
                                        "priority_state_sync_failed",
                                        mapOf("error" to (e.message ?: e::class.simpleName.orEmpty())),
                                    )
                                }
                                delay(PRIORITY_STATE_POLL_MS)
                            }
                        }
                    } else {
                        null
                    }
            }
        } finally {
            pollingJob?.cancel()
        }
    }

    private companion object {
        const val PRIORITY_STATE_POLL_MS = 5_000L
    }
}
