package com.screwy.igloo.sync

import com.screwy.igloo.log.Logger
import com.screwy.igloo.net.Reachability
import com.screwy.igloo.outbox.OutboxDrainRunner
import com.screwy.igloo.outbox.OutboxWriter
import io.mockk.clearMocks
import io.mockk.coEvery
import io.mockk.coVerify
import io.mockk.every
import io.mockk.just
import io.mockk.mockk
import io.mockk.runs
import io.mockk.verify
import io.mockk.verifyOrder
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class SchedulerTest {
    @Test
    fun triggerAllStartsSyncWhenOutboxCompletesBeforeInboundFinishes() = runTest {
        val foreground = MutableSharedFlow<Boolean>(extraBufferCapacity = 1)
        val reachability = Reachability(
            scope = this,
            probe = { true },
            foregroundFlow = foreground,
        )
        reachability.markOnline()

        val inbound = mockk<InboundReconciler>(relaxed = true)
        coEvery { inbound.run() } coAnswers { awaitCancellation() }
        every { inbound.trigger() } just runs

        val outbox = FakeOutboxDrain()
        val retentionReplay = mockk<RetentionReplayCoordinator>(relaxed = true)
        val androidSync = mockk<AndroidSyncMirror>(relaxed = true)
        coEvery { androidSync.run() } coAnswers { awaitCancellation() }
        every { androidSync.trigger() } just runs
        val writer = mockk<OutboxWriter>(relaxed = true)
        val mutationDelta = mockk<MutationDeltaSync>(relaxed = true)
        val logger = mockk<Logger>(relaxed = true)

        val scheduler = Scheduler(
            scope = this,
            inbound = inbound,
            outbox = outbox,
            androidSync = androidSync,
            retentionReplay = retentionReplay,
            reachability = reachability,
            foregroundFlow = foreground,
            writer = writer,
            mutationDelta = mutationDelta,
            logger = logger,
        )

        scheduler.start()
        runCurrent()
        clearMocks(androidSync, answers = false, recordedCalls = true)
        scheduler.triggerAll()
        outbox.passCompleted.emit(Unit)
        runCurrent()

        verify(exactly = 2) { androidSync.trigger() }
        verify(exactly = 1) { inbound.trigger() }
        verifyOrder {
            androidSync.trigger()
            androidSync.trigger()
            inbound.trigger()
        }

        scheduler.stopAll()
    }

    @Test
    fun triggerStreamMergesScopedRequestsAfterOutboxCompletion() = runTest {
        val foreground = MutableSharedFlow<Boolean>(extraBufferCapacity = 1)
        val reachability = Reachability(
            scope = this,
            probe = { true },
            foregroundFlow = foreground,
        )
        reachability.markOnline()

        val inbound = mockk<InboundReconciler>(relaxed = true)
        coEvery { inbound.run() } coAnswers { awaitCancellation() }
        every { inbound.triggerStreams(any()) } just runs

        val outbox = FakeOutboxDrain()
        val retentionReplay = mockk<RetentionReplayCoordinator>(relaxed = true)
        val androidSync = mockk<AndroidSyncMirror>(relaxed = true)
        coEvery { androidSync.run() } coAnswers { awaitCancellation() }
        every { androidSync.trigger() } just runs
        val writer = mockk<OutboxWriter>(relaxed = true)
        val mutationDelta = mockk<MutationDeltaSync>(relaxed = true)
        coEvery { mutationDelta.sync() } returns MutationDeltaResult()
        val logger = mockk<Logger>(relaxed = true)

        val scheduler = Scheduler(
            scope = this,
            inbound = inbound,
            outbox = outbox,
            androidSync = androidSync,
            retentionReplay = retentionReplay,
            reachability = reachability,
            foregroundFlow = foreground,
            writer = writer,
            mutationDelta = mutationDelta,
            logger = logger,
        )

        scheduler.start()
        runCurrent()
        clearMocks(androidSync, inbound, mutationDelta, answers = false, recordedCalls = true)
        scheduler.triggerStream(SyncStream.Feed)
        scheduler.triggerStream(SyncStream.Channels)
        outbox.passCompleted.emit(Unit)
        runCurrent()

        verify(exactly = 1) { androidSync.trigger() }
        verify(exactly = 1) { inbound.triggerStreams(setOf(SyncStream.Feed, SyncStream.Channels)) }
        coVerify(exactly = 1) { mutationDelta.sync() }

        scheduler.stopAll()
    }

    @Test
    fun foregroundMutationDeltaRankChangeTriggersAndroidSyncRefresh() = runTest {
        val foreground = MutableSharedFlow<Boolean>(extraBufferCapacity = 1)
        val reachability = Reachability(
            scope = this,
            probe = { true },
            foregroundFlow = foreground,
        )
        reachability.markOnline()

        val inbound = mockk<InboundReconciler>(relaxed = true)
        coEvery { inbound.run() } coAnswers { awaitCancellation() }

        val outbox = FakeOutboxDrain()
        val retentionReplay = mockk<RetentionReplayCoordinator>(relaxed = true)
        val androidSync = mockk<AndroidSyncMirror>(relaxed = true)
        coEvery { androidSync.run() } coAnswers { awaitCancellation() }
        every { androidSync.trigger() } just runs
        val writer = mockk<OutboxWriter>(relaxed = true)
        val mutationDelta = mockk<MutationDeltaSync>(relaxed = true)
        coEvery { mutationDelta.sync() } returns MutationDeltaResult(rankAffecting = true)
        val logger = mockk<Logger>(relaxed = true)

        val scheduler = Scheduler(
            scope = this,
            inbound = inbound,
            outbox = outbox,
            androidSync = androidSync,
            retentionReplay = retentionReplay,
            reachability = reachability,
            foregroundFlow = foreground,
            writer = writer,
            mutationDelta = mutationDelta,
            logger = logger,
        )

        scheduler.start()
        runCurrent()
        clearMocks(androidSync, mutationDelta, answers = false, recordedCalls = true)

        foreground.emit(true)
        runCurrent()

        coVerify(exactly = 1) { mutationDelta.sync() }
        verify(exactly = 2) { androidSync.trigger() }

        scheduler.stopAll()
    }

    private class FakeOutboxDrain : OutboxDrainRunner {
        override val passCompleted = MutableSharedFlow<Unit>(extraBufferCapacity = 1)

        override fun wireWriter(writer: OutboxWriter) = Unit

        override fun trigger() = Unit

        override suspend fun run() {
            awaitCancellation()
        }
    }
}
