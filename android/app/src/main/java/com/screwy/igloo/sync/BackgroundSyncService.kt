package com.screwy.igloo.sync

import android.app.Application
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import com.screwy.igloo.AppRuntime
import com.screwy.igloo.R
import com.screwy.igloo.auth.AuthRepo
import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.log.Logger
import com.screwy.igloo.media.ForegroundPromoter
import java.util.concurrent.atomic.AtomicBoolean
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import org.koin.core.context.GlobalContext

class BackgroundSyncService : Service() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private var dutyCycle: Job? = null
    private val runAgain = AtomicBoolean(false)

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        promote()
        if (dutyCycle?.isActive != true) {
            runAgain.set(false)
            dutyCycle = scope.launch { runDutyCycle() }
        } else {
            runAgain.set(true)
        }
        return START_STICKY
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onDestroy() {
        scope.cancel()
        stopForeground(STOP_FOREGROUND_REMOVE)
        super.onDestroy()
    }

    private suspend fun runDutyCycle() {
        try {
            val application = applicationContext as Application
            if (!AppRuntime.prepareLocalSession(application)) return
            val koin = GlobalContext.get()
            val auth: AuthRepo = koin.get()
            auth.onAppStart()
            if (!auth.hasSessionSync()) return
            val prefs: PreferencesRepo = koin.get()
            val coordinator: SyncCoordinator = koin.get()
            val logger: Logger = koin.get()
            logger.info(
                event = "background_sync_started",
                fields = mapOf("interval_minutes" to prefs.syncIntervalMinutes().first()),
            )
            koin.get<ForegroundPromoter>().acquireExternalForegroundLease().use {
                while (prefs.syncEnabled().first()) {
                    try {
                        if (!coordinator.convergeScheduledSnapshot()) {
                            delay(RETRY_DELAY_MS)
                            continue
                        }
                    } catch (e: CancellationException) {
                        throw e
                    } catch (e: Exception) {
                        logger.error("background_sync_failed", throwable = e)
                        delay(RETRY_DELAY_MS)
                        continue
                    }
                    logger.info(event = "background_sync_snapshot_converged", fields = emptyMap())
                    if (prefs.syncIntervalMinutes().first() == PreferencesRepo.Defaults.SYNC_INTERVAL_LIVE) {
                        delay(LIVE_POLL_MS)
                        continue
                    }
                    if (!runAgain.getAndSet(false)) return
                }
            }
        } finally {
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelf()
        }
    }

    private fun promote() {
        val manager = getSystemService(NOTIFICATION_SERVICE) as NotificationManager
        if (manager.getNotificationChannel(CHANNEL_ID) == null) {
            manager.createNotificationChannel(
                NotificationChannel(
                        CHANNEL_ID,
                        getString(R.string.notification_background_sync_channel_name),
                        NotificationManager.IMPORTANCE_LOW,
                    )
                    .apply {
                        description =
                            getString(R.string.notification_background_sync_channel_description)
                        enableVibration(false)
                        setSound(null, null)
                    }
            )
        }
        val notification =
            NotificationCompat.Builder(this, CHANNEL_ID)
                .setContentTitle(getString(R.string.notification_background_sync_title))
                .setSmallIcon(android.R.drawable.stat_sys_download)
                .setProgress(0, 0, true)
                .setOngoing(true)
                .setSilent(true)
                .build()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(
                NOTIFICATION_ID,
                notification,
                ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE,
            )
        } else {
            startForeground(NOTIFICATION_ID, notification)
        }
    }

    companion object {
        private const val ACTION_RUN = "com.screwy.igloo.sync.RUN_BACKGROUND_SYNC"
        private const val CHANNEL_ID = "igloo_background_sync"
        private const val NOTIFICATION_ID = 1003
        private const val RETRY_DELAY_MS = 30_000L
        private const val LIVE_POLL_MS = 5_000L

        fun start(context: Context) {
            ContextCompat.startForegroundService(
                context,
                Intent(context, BackgroundSyncService::class.java).setAction(ACTION_RUN),
            )
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, BackgroundSyncService::class.java))
        }
    }
}
