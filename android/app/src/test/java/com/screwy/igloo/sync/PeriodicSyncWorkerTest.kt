package com.screwy.igloo.sync

import com.screwy.igloo.data.PreferencesRepo
import org.junit.Assert.assertEquals
import org.junit.Test

class PeriodicSyncWorkerTest {
    @Test
    fun liveUsesWorkManagerMinimumAsItsBackgroundFallback() {
        assertEquals(
            PeriodicSyncWorker.MIN_INTERVAL_MINUTES,
            periodicSyncIntervalMinutes(PreferencesRepo.Defaults.SYNC_INTERVAL_LIVE),
        )
        assertEquals(30L, periodicSyncIntervalMinutes(30))
    }
}
