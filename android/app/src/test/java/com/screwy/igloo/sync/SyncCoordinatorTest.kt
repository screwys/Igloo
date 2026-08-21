package com.screwy.igloo.sync

import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.data.RoomTestSupport
import com.screwy.igloo.log.InMemoryLogSink
import com.screwy.igloo.log.Logger
import com.screwy.igloo.net.AndroidSyncHttpException
import com.screwy.igloo.net.Reachability
import com.screwy.igloo.outbox.OutboxDrainer
import com.screwy.igloo.outbox.OutboxPassResult
import com.screwy.igloo.outbox.OutboxRejectedMutation
import java.util.concurrent.atomic.AtomicInteger
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], manifest = Config.NONE)
@OptIn(ExperimentalCoroutinesApi::class)
class SyncCoordinatorTest {
    @Test
    fun actionLaneKeepsDrainingWhileAssetsAreBusy() = runTest {
        val logs = LoggerFixture()
        val outbox = FakeOutbox()
        val mirror = FakeMirror(blockAssets = true)
        val foreground = MutableStateFlow(false)
        val reachability = reachability(backgroundScope)
        val coordinator =
            coordinator(backgroundScope, outbox, mirror, reachability, foreground, logs.logger)
        try {
            coordinator.start()
            runCurrent()
            assertTrue(mirror.assetStarted.isCompleted)
            val before = outbox.calls.get()

            coordinator.triggerActions()
            runCurrent()

            assertTrue(outbox.calls.get() > before)
            assertTrue(!mirror.releaseAssets.isCompleted)
        } finally {
            coordinator.stopAll()
            logs.close()
        }
    }

    @Test
    fun liveMetadataKeepsConvergingWhileAssetsAreBusy() = runTest {
        val logs = LoggerFixture()
        val outbox = FakeOutbox()
        val mirror = FakeMirror(blockAssets = true)
        val foreground = MutableStateFlow(false)
        val interval = MutableStateFlow(PreferencesRepo.Defaults.SYNC_INTERVAL_LIVE)
        val reachability = reachability(backgroundScope)
        val coordinator =
            coordinator(
                backgroundScope,
                outbox,
                mirror,
                reachability,
                foreground,
                logs.logger,
                interval,
            )
        try {
            coordinator.start()
            runCurrent()
            val before = mirror.metadataCalls.get()
            assertTrue(mirror.assetStarted.isCompleted)

            foreground.value = true
            runCurrent()

            assertTrue(mirror.metadataCalls.get() > before)
            assertTrue(!mirror.releaseAssets.isCompleted)
        } finally {
            coordinator.stopAll()
            logs.close()
        }
    }

    @Test
    fun scheduledPriorityStateProgressesWhileALaterMetadataPageIsBusy() = runTest {
        val logs = LoggerFixture()
        val outbox = FakeOutbox()
        val mirror = FakeMirror(blockMetadata = true)
        val foreground = MutableStateFlow(false)
        val reachability = reachability(backgroundScope)
        val coordinator =
            coordinator(backgroundScope, outbox, mirror, reachability, foreground, logs.logger)
        try {
            coordinator.start()
            runCurrent()
            assertTrue(mirror.metadataStarted.isCompleted)

            foreground.value = true
            runCurrent()
            assertTrue(mirror.priorityCalls.get() > 0)
            assertTrue(!mirror.releaseMetadata.isCompleted)

            mirror.releaseMetadata.complete(Unit)
            runCurrent()
        } finally {
            coordinator.stopAll()
            logs.close()
        }
    }

    @Test
    fun timedPriorityProtectionWakesMetadataAndItsAssetHandoff() = runTest {
        val logs = LoggerFixture()
        val mirror =
            FakeMirror(priorityResult = PriorityStateSyncResult(metadataRequired = true))
        val foreground = MutableStateFlow(false)
        val coordinator =
            coordinator(
                backgroundScope,
                FakeOutbox(),
                mirror,
                reachability(backgroundScope),
                foreground,
                logs.logger,
            )
        try {
            coordinator.start()
            runCurrent()
            val metadataBefore = mirror.metadataCalls.get()
            val assetsBefore = mirror.assetCalls.get()

            foreground.value = true
            runCurrent()

            assertTrue(mirror.priorityCalls.get() > 0)
            assertTrue(mirror.metadataCalls.get() > metadataBefore)
            assertTrue(mirror.assetCalls.get() > assetsBefore)
        } finally {
            coordinator.stopAll()
            logs.close()
        }
    }

    @Test
    fun acknowledgedProtectionChangeWakesMetadataAndItsAssetHandoff() = runTest {
        val logs = LoggerFixture()
        var changed = false
        val outbox = FakeOutbox { OutboxPassResult(protectionChanged = changed) }
        val mirror = FakeMirror()
        val coordinator =
            coordinator(
                backgroundScope,
                outbox,
                mirror,
                reachability(backgroundScope),
                MutableStateFlow(false),
                logs.logger,
            )
        try {
            coordinator.start()
            runCurrent()
            val metadataBefore = mirror.metadataCalls.get()
            val assetsBefore = mirror.assetCalls.get()

            changed = true
            coordinator.triggerActions()
            runCurrent()

            assertTrue(mirror.metadataCalls.get() > metadataBefore)
            assertTrue(mirror.assetCalls.get() > assetsBefore)
        } finally {
            coordinator.stopAll()
            logs.close()
        }
    }

    @Test
    fun liveWakeSkipsMetadataAndAssetsAfterAFailedOfflineProbe() = runTest {
        val logs = LoggerFixture()
        val probeCalls = AtomicInteger()
        val reachability =
            reachability(backgroundScope) {
                probeCalls.incrementAndGet()
                false
            }.apply { downgrade() }
        val mirror = FakeMirror()
        val coordinator =
            coordinator(
                backgroundScope,
                FakeOutbox(),
                mirror,
                reachability,
                MutableStateFlow(true),
                logs.logger,
                interval = MutableStateFlow(PreferencesRepo.Defaults.SYNC_INTERVAL_LIVE),
            )
        try {
            coordinator.start()
            runCurrent()
            advanceTimeBy(5_000L)
            runCurrent()

            assertTrue(probeCalls.get() > 0)
            assertEquals(0, mirror.metadataCalls.get())
            assertEquals(0, mirror.assetCalls.get())
        } finally {
            coordinator.stopAll()
            logs.close()
        }
    }

    @Test
    fun explicitPassReportsFailedOfflineProbeWithoutRunningMirrorLanes() = runTest {
        val logs = LoggerFixture()
        val reachability = reachability(backgroundScope) { false }.apply { downgrade() }
        val mirror = FakeMirror()
        val coordinator =
            coordinator(
                backgroundScope,
                FakeOutbox(),
                mirror,
                reachability,
                MutableStateFlow(false),
                logs.logger,
            )
        try {
            assertFalse(coordinator.pass())
            assertEquals(0, mirror.metadataCalls.get())
            assertEquals(0, mirror.assetCalls.get())
        } finally {
            coordinator.stopAll()
            logs.close()
        }
    }

    @Test
    fun retryableMetadataFailureOwnsOneBoundedRetry() = runTest {
        val logs = LoggerFixture()
        val mirror =
            FakeMirror(
                metadataResult = { call ->
                    if (call == 1) {
                        throw AndroidSyncHttpException("changes", 503, "{}")
                    }
                }
            )
        val coordinator =
            coordinator(
                backgroundScope,
                FakeOutbox(),
                mirror,
                reachability(backgroundScope),
                MutableStateFlow(false),
                logs.logger,
                nowMsProvider = { testScheduler.currentTime },
            )
        try {
            coordinator.start()
            runCurrent()
            assertEquals(1, mirror.metadataCalls.get())
            assertEquals(0, mirror.assetCalls.get())

            advanceTimeBy(29_999L)
            runCurrent()
            assertEquals(1, mirror.metadataCalls.get())

            advanceTimeBy(1L)
            runCurrent()
            assertEquals(2, mirror.metadataCalls.get())
            assertEquals(1, mirror.assetCalls.get())
        } finally {
            coordinator.stopAll()
            logs.close()
        }
    }

    @Test
    fun terminalMetadataFailureDoesNotCreateARetryLoop() = runTest {
        val logs = LoggerFixture()
        val mirror =
            FakeMirror(
                metadataResult = {
                    throw AndroidSyncHttpException("changes", 400, "{}")
                }
            )
        val coordinator =
            coordinator(
                backgroundScope,
                FakeOutbox(),
                mirror,
                reachability(backgroundScope),
                MutableStateFlow(false),
                logs.logger,
                nowMsProvider = { testScheduler.currentTime },
            )
        try {
            coordinator.start()
            runCurrent()
            assertEquals(1, mirror.metadataCalls.get())

            advanceTimeBy(60_000L)
            runCurrent()
            assertEquals(1, mirror.metadataCalls.get())
            assertEquals(0, mirror.assetCalls.get())
        } finally {
            coordinator.stopAll()
            logs.close()
        }
    }

    @Test
    fun assetRetryDeadlineSurvivesAnEarlyRecoveryPass() = runTest {
        val logs = LoggerFixture()
        val mirror =
            FakeMirror(
                assetResult = { call ->
                    AssetSyncResult(nextAttemptAtMs = if (call <= 2) 30_000L else null)
                }
            )
        val coordinator =
            coordinator(
                backgroundScope,
                FakeOutbox(),
                mirror,
                reachability(backgroundScope),
                MutableStateFlow(false),
                logs.logger,
                nowMsProvider = { testScheduler.currentTime },
            )
        try {
            coordinator.start()
            runCurrent()
            assertEquals(1, mirror.assetCalls.get())

            advanceTimeBy(10_000L)
            coordinator.trigger()
            runCurrent()
            assertEquals(2, mirror.assetCalls.get())

            advanceTimeBy(19_999L)
            runCurrent()
            assertEquals(2, mirror.assetCalls.get())

            advanceTimeBy(1L)
            runCurrent()
            assertEquals(3, mirror.assetCalls.get())
        } finally {
            coordinator.stopAll()
            logs.close()
        }
    }

    @Test
    fun stopCancelsTheOwnedAssetRetryDeadline() = runTest {
        val logs = LoggerFixture()
        val mirror = FakeMirror(assetResult = { AssetSyncResult(nextAttemptAtMs = 30_000L) })
        val coordinator =
            coordinator(
                backgroundScope,
                FakeOutbox(),
                mirror,
                reachability(backgroundScope),
                MutableStateFlow(false),
                logs.logger,
                nowMsProvider = { testScheduler.currentTime },
            )
        try {
            coordinator.start()
            runCurrent()
            assertEquals(1, mirror.assetCalls.get())

            coordinator.stopAll()
            advanceTimeBy(30_000L)
            runCurrent()

            assertEquals(1, mirror.assetCalls.get())
        } finally {
            coordinator.stopAll()
            logs.close()
        }
    }

    @Test
    fun changedAssetHandsRecoveryBackToMetadataOwnership() = runTest {
        val logs = LoggerFixture()
        val mirror = FakeMirror(assetChangesOnce = true)
        val coordinator =
            coordinator(
                backgroundScope,
                FakeOutbox(),
                mirror,
                reachability(backgroundScope),
                MutableStateFlow(false),
                logs.logger,
            )
        try {
            coordinator.start()
            runCurrent()

            assertTrue(mirror.metadataCalls.get() >= 2)
            assertTrue(mirror.assetCalls.get() >= 2)
        } finally {
            coordinator.stopAll()
            logs.close()
        }
    }

    @Test
    fun retryTimerOwnsTheNextAttemptWithoutAnotherExternalWake() = runTest {
        val logs = LoggerFixture()
        val outbox =
            FakeOutbox {
                if (testScheduler.currentTime < 1_000L) {
                    OutboxPassResult(nextAttemptAtMs = 1_000L)
                } else {
                    OutboxPassResult()
                }
            }
        val mirror = FakeMirror()
        val reachability = reachability(backgroundScope)
        val coordinator =
            coordinator(
                backgroundScope,
                outbox,
                mirror,
                reachability,
                MutableStateFlow(false),
                logs.logger,
                nowMsProvider = { testScheduler.currentTime },
            )
        try {
            coordinator.start()
            runCurrent()
            val before = outbox.calls.get()

            advanceTimeBy(999L)
            runCurrent()
            assertEquals(before, outbox.calls.get())

            advanceTimeBy(1L)
            runCurrent()
            assertTrue(outbox.calls.get() > before)
        } finally {
            coordinator.stopAll()
            logs.close()
        }
    }

    @Test
    fun failedRetryProbeRearmsUntilRecoverySendsTheAction() = runTest {
        val logs = LoggerFixture()
        val probeCalls = AtomicInteger()
        val reachability =
            reachability(backgroundScope) {
                probeCalls.incrementAndGet() >= 2
            }
        var delivered = false
        val outbox =
            FakeOutbox {
                when {
                    testScheduler.currentTime < 1_000L ->
                        OutboxPassResult(nextAttemptAtMs = 1_000L)
                    reachability.state.value is Reachability.State.Offline ->
                        OutboxPassResult(nextAttemptAtMs = testScheduler.currentTime + 1_000L)
                    else -> {
                        delivered = true
                        OutboxPassResult()
                    }
                }
            }
        val coordinator =
            coordinator(
                backgroundScope,
                outbox,
                FakeMirror(),
                reachability,
                MutableStateFlow(false),
                logs.logger,
                nowMsProvider = { testScheduler.currentTime },
            )
        try {
            coordinator.start()
            runCurrent()
            reachability.downgrade()

            advanceTimeBy(1_000L)
            runCurrent()
            assertEquals(1, probeCalls.get())
            assertFalse(delivered)

            advanceTimeBy(1_000L)
            runCurrent()
            assertEquals(2, probeCalls.get())
            assertTrue(delivered)
        } finally {
            coordinator.stopAll()
            logs.close()
        }
    }

    @Test
    fun rejectedMutationKeepsItsOwnedRetryWhenReconciliationFailsTransiently() = runTest {
        val logs = LoggerFixture()
        val rejected = OutboxRejectedMutation(1L, "feed_like", "sample_post")
        var resultCalls = 0
        val outbox =
            FakeOutbox {
                resultCalls++
                if (resultCalls == 1) {
                    OutboxPassResult(
                        rejectedMutations = listOf(rejected),
                        nextAttemptAtMs = 30_000L,
                    )
                } else if (testScheduler.currentTime < 30_000L) {
                    OutboxPassResult(nextAttemptAtMs = 30_000L)
                } else {
                    OutboxPassResult()
                }
            }
        val mirror =
            FakeMirror(
                reconcileResult = { call ->
                    if (call == 1) {
                        throw AndroidSyncHttpException("reconcile", 500, "{}")
                    }
                }
            )
        val coordinator =
            coordinator(
                backgroundScope,
                outbox,
                mirror,
                reachability(backgroundScope),
                MutableStateFlow(false),
                logs.logger,
                nowMsProvider = { testScheduler.currentTime },
            )
        try {
            coordinator.start()
            runCurrent()
            val callsBeforeRetry = outbox.calls.get()
            assertEquals(1, mirror.reconcileCalls.get())

            advanceTimeBy(29_999L)
            runCurrent()
            assertEquals(callsBeforeRetry, outbox.calls.get())

            advanceTimeBy(1L)
            runCurrent()
            assertTrue(outbox.calls.get() > callsBeforeRetry)
        } finally {
            coordinator.stopAll()
            logs.close()
        }
    }

    @Test
    fun interactiveWakeProbesAStaleOfflineState() = runTest {
        val logs = LoggerFixture()
        val probeCalls = AtomicInteger()
        val reachability =
            reachability(backgroundScope) {
                probeCalls.incrementAndGet()
                true
            }
        val outbox = FakeOutbox()
        val coordinator =
            coordinator(
                backgroundScope,
                outbox,
                FakeMirror(),
                reachability,
                MutableStateFlow(false),
                logs.logger,
            )
        try {
            coordinator.start()
            runCurrent()
            reachability.downgrade()
            val before = outbox.calls.get()

            coordinator.triggerActions(probeIfOffline = true)
            runCurrent()

            assertEquals(1, probeCalls.get())
            assertTrue(outbox.calls.get() > before)
            assertEquals(Reachability.State.Online, reachability.state.value)
        } finally {
            coordinator.stopAll()
            logs.close()
        }
    }

    @Test
    fun explicitPassAwaitsActionsMetadataAndAssetsInOrder() = runTest {
        val logs = LoggerFixture()
        val events = mutableListOf<String>()
        val outbox = FakeOutbox { events += "actions"; OutboxPassResult() }
        val mirror = FakeMirror(events = events)
        val reachability = reachability(backgroundScope)
        val coordinator =
            coordinator(
                backgroundScope,
                outbox,
                mirror,
                reachability,
                MutableStateFlow(false),
                logs.logger,
            )
        try {
            coordinator.pass()

            assertEquals(listOf("actions", "metadata", "assets"), events)
        } finally {
            coordinator.stopAll()
            logs.close()
        }
    }

    @Test
    fun scheduledSnapshotRetriesAssetsWithoutRecapturingMetadata() = runTest {
        val logs = LoggerFixture()
        val events = mutableListOf<String>()
        val mirror =
            FakeMirror(
                events = events,
                assetResult = { call ->
                    if (call == 1) AssetSyncResult(nextAttemptAtMs = 100L) else AssetSyncResult()
                },
            )
        val coordinator =
            coordinator(
                backgroundScope,
                FakeOutbox { events += "actions"; OutboxPassResult() },
                mirror,
                reachability(backgroundScope),
                MutableStateFlow(false),
                logs.logger,
                nowMsProvider = { testScheduler.currentTime },
            )
        try {
            assertTrue(coordinator.convergeScheduledSnapshot())

            assertEquals(listOf("actions", "metadata", "assets", "assets"), events)
            assertEquals(1, mirror.metadataCalls.get())
            assertEquals(100L, testScheduler.currentTime)
        } finally {
            coordinator.stopAll()
            logs.close()
        }
    }

    private fun coordinator(
        scope: CoroutineScope,
        outbox: OutboxDrainer,
        mirror: SyncMirror,
        reachability: Reachability,
        foreground: MutableStateFlow<Boolean>,
        logger: Logger,
        interval: MutableStateFlow<Int> = MutableStateFlow(30),
        nowMsProvider: () -> Long = { 0L },
    ) =
        SyncCoordinator(
            scope = scope,
            outbox = outbox,
            mirror = mirror,
            syncIntervalMinutes = interval,
            retentionSettings = flowOf(listOf(2, 7, 3, 48)),
            reachability = reachability,
            foregroundFlow = foreground,
            logger = logger,
            nowMsProvider = nowMsProvider,
        )

    private fun reachability(
        scope: CoroutineScope,
        probe: suspend () -> Boolean = { true },
    ) =
        Reachability(scope = scope, probe = probe, foregroundFlow = emptyFlow()).apply {
            markOnline()
        }

    private class FakeOutbox(
        private val result: suspend () -> OutboxPassResult = { OutboxPassResult() },
    ) : OutboxDrainer {
        val calls = AtomicInteger()

        override suspend fun runOnce(): OutboxPassResult {
            calls.incrementAndGet()
            return result()
        }
    }

    private class FakeMirror(
        private val blockMetadata: Boolean = false,
        private val blockAssets: Boolean = false,
        private val assetChangesOnce: Boolean = false,
        private val events: MutableList<String>? = null,
        private val priorityResult: PriorityStateSyncResult = PriorityStateSyncResult(),
        private val metadataResult: suspend (Int) -> Unit = {},
        private val assetResult: suspend (Int) -> AssetSyncResult = { AssetSyncResult() },
        private val reconcileResult: suspend (Int) -> Unit = {},
    ) : SyncMirror {
        val metadataCalls = AtomicInteger()
        val assetCalls = AtomicInteger()
        val priorityCalls = AtomicInteger()
        val reconcileCalls = AtomicInteger()
        val metadataStarted = CompletableDeferred<Unit>()
        val assetStarted = CompletableDeferred<Unit>()
        val releaseMetadata = CompletableDeferred<Unit>()
        val releaseAssets = CompletableDeferred<Unit>()

        override suspend fun syncMetadataOnce(protectionChanged: Boolean) {
            val call = metadataCalls.incrementAndGet()
            events?.add("metadata")
            metadataStarted.complete(Unit)
            if (blockMetadata) releaseMetadata.await()
            metadataResult(call)
        }

        override suspend fun syncAssetsOnce(): AssetSyncResult {
            val call = assetCalls.incrementAndGet()
            events?.add("assets")
            assetStarted.complete(Unit)
            if (assetChangesOnce && call == 1) throw AndroidSyncAssetChangedException()
            if (blockAssets) releaseAssets.await()
            return assetResult(call)
        }

        override suspend fun syncPriorityStateOnce(): PriorityStateSyncResult {
            priorityCalls.incrementAndGet()
            return priorityResult
        }

        override suspend fun reconcileRejected(rejected: List<OutboxRejectedMutation>) {
            reconcileResult(reconcileCalls.incrementAndGet())
        }
    }

    private class LoggerFixture {
        private val db = RoomTestSupport.freshDb()
        private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
        private val prefs = PreferencesRepo(db.preferenceDao(), scope) { 0L }
        val logger = Logger(prefs, InMemoryLogSink(), scope) { 0L }

        fun close() {
            RoomTestSupport.closeAfterScope(scope, db)
        }
    }
}
