package com.screwy.igloo.player

import android.content.Intent
import android.app.PendingIntent
import android.os.Binder
import android.os.IBinder
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.media3.common.C
import androidx.media3.common.Player
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.session.MediaSession
import androidx.media3.session.MediaSessionService
import com.screwy.igloo.AppRuntime
import com.screwy.igloo.R
import com.screwy.igloo.net.IglooHostProvider
import com.screwy.igloo.net.auth.AuthTokenProvider
import com.screwy.igloo.outbox.OutboxKind
import com.screwy.igloo.outbox.OutboxWriter
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.flow.collectLatest
import org.koin.core.context.GlobalContext

@androidx.annotation.OptIn(markerClass = [androidx.media3.common.util.UnstableApi::class])
class PlaybackService : MediaSessionService() {
    lateinit var player: ExoPlayer
        private set
    private lateinit var session: MediaSession
    var backgroundPlayback by mutableStateOf(false)
    var videoId: String? = null
        set(value) {
            if (field != value) sponsorBlock.reset(clearSeekHistory = true)
            field = value
        }
    var sourceUri: String? = null
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    internal lateinit var sponsorBlock: SponsorBlockPlaybackController
        private set
    internal var segments: List<SponsorBlockUiSegment> = emptyList()
        set(value) {
            field = value
            sponsorBlock.reset()
        }

    inner class LocalBinder : Binder() {
        val service: PlaybackService get() = this@PlaybackService
    }

    override fun onCreate() {
        super.onCreate()
        AppRuntime.prepareLocalSession(application)
        val koin = GlobalContext.get()
        player = buildIglooPlayer(this, koin.get<AuthTokenProvider>(), koin.get<IglooHostProvider>()).apply {
            setWakeMode(C.WAKE_MODE_LOCAL)
        }
        session = buildIglooMediaSession(this, player, koin.get(), koin.get())
        addSession(session)
        sponsorBlock = SponsorBlockPlaybackController(
            seekTo = player::seekTo,
            skippedMessage = { category ->
                getString(R.string.sponsorblock_segment_skipped, getString(sponsorBlockLabelRes(category)))
            },
        )
        val outbox: OutboxWriter = koin.get()
        fun saveProgress() {
            val id = videoId ?: return
            val position = player.currentPosition.coerceAtLeast(0L) / 1000.0
            val duration = player.duration.coerceAtLeast(0L) / 1000.0
            scope.launch { outbox.enqueue(OutboxKind.Progress(id, position, duration)) }
        }
        player.addListener(object : Player.Listener {
            override fun onIsPlayingChanged(isPlaying: Boolean) {
                if (!isPlaying) saveProgress()
            }

            override fun onPositionDiscontinuity(
                oldPosition: Player.PositionInfo,
                newPosition: Player.PositionInfo,
                reason: Int,
            ) {
                if (reason == Player.DISCONTINUITY_REASON_SEEK) {
                    sponsorBlock.onSeek(newPosition.positionMs)
                    saveProgress()
                }
            }
        })
        scope.launch {
            while (isActive) {
                delay(500L)
                sponsorBlock.onTick(player.isPlaying, player.currentPosition, segments)
            }
        }
        scope.launch {
            while (isActive) {
                delay(5_000L)
                if (player.isPlaying) saveProgress()
            }
        }
        scope.launch {
            snapshotFlow { sponsorBlock.autoSkipMessage }.collectLatest { message ->
                if (message != null) {
                    delay(2_000L)
                    sponsorBlock.clearAutoSkipMessage()
                }
            }
        }
    }

    override fun onBind(intent: Intent?): IBinder? =
        if (intent?.action == LOCAL_BIND) LocalBinder() else super.onBind(intent)

    override fun onGetSession(controllerInfo: MediaSession.ControllerInfo): MediaSession = session

    fun setSessionActivity(activity: PendingIntent) {
        session.setSessionActivity(activity)
    }

    override fun onDestroy() {
        session.release()
        player.release()
        scope.cancel()
        super.onDestroy()
    }

    companion object {
        const val LOCAL_BIND = "com.screwy.igloo.player.BIND"
    }
}
