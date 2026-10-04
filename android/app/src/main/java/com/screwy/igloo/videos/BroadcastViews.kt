package com.screwy.igloo.videos

import android.content.pm.ActivityInfo
import android.content.res.Configuration
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import com.screwy.igloo.R
import com.screwy.igloo.media.MediaResolvers
import com.screwy.igloo.media.MediaUri
import com.screwy.igloo.media.OwnerKind
import com.screwy.igloo.net.BroadcastsApi
import com.screwy.igloo.player.PlayerChatLayout
import com.screwy.igloo.player.PlayerSurface
import com.screwy.igloo.player.PlayerSurfaceMode
import com.screwy.igloo.player.YouTubeChat
import com.screwy.igloo.player.findPlayerActivity
import com.screwy.igloo.player.hidePlayerSystemBars
import com.screwy.igloo.player.playerRequestedOrientation
import com.screwy.igloo.player.rememberIglooPlayer
import com.screwy.igloo.player.showPlayerSystemBars
import com.screwy.igloo.ui.component.MediaCellArtwork
import com.screwy.igloo.ui.theme.iglooColors
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import org.koin.compose.koinInject

@Composable
internal fun BroadcastCardView(card: BroadcastCard, modifier: Modifier = Modifier, onOpen: () -> Unit, onChannel: () -> Unit) {
    val resolvers: MediaResolvers = koinInject()
    val thumbnail by remember(card.id, card.broadcast.thumbnailUrl) {
        resolvers.thumbnailForPostFlow(card.id, OwnerKind.YouTubeVideo).map {
            if (it is MediaUri.Local) it else MediaUri.Remote(card.broadcast.thumbnailUrl)
        }
    }.collectAsStateWithLifecycle(initialValue = MediaUri.Missing)
    Column(modifier.clickable(onClick = onOpen), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Surface(shape = RoundedCornerShape(10.dp), color = MaterialTheme.iglooColors.surfaceVariant,
            modifier = Modifier.fillMaxWidth().aspectRatio(16f / 9f)) {
            Box(Modifier.fillMaxSize()) {
                MediaCellArtwork(thumbnail, contentDescription = null)
                Text("LIVE", color = Color.White, style = MaterialTheme.typography.labelSmall,
                    modifier = Modifier.align(Alignment.TopEnd).padding(6.dp)
                        .background(MaterialTheme.iglooColors.primary, RoundedCornerShape(4.dp))
                        .padding(horizontal = 4.dp, vertical = 2.dp))
                Icon(Icons.Default.PlayArrow, contentDescription = null, tint = Color.White,
                    modifier = Modifier.align(Alignment.BottomStart).padding(6.dp).size(22.dp)
                        .background(Color.Black.copy(alpha = 0.45f), RoundedCornerShape(4.dp)))
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(card.channelName, Modifier.weight(1f).clickable(onClick = onChannel),
                style = MaterialTheme.typography.labelMedium, color = MaterialTheme.iglooColors.onSurfaceMuted,
                maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        if (card.title.isNotBlank()) Text(card.title, style = MaterialTheme.typography.titleSmall,
            maxLines = 2, overflow = TextOverflow.Ellipsis)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(R.string.broadcast_live_now), style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.iglooColors.primary)
            card.broadcast.concurrentViewCount?.let {
                Text(java.text.NumberFormat.getInstance().format(it), style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.iglooColors.onSurfaceMuted)
            }
        }
    }
}

@androidx.annotation.OptIn(markerClass = [UnstableApi::class])
@Composable
internal fun BroadcastPlayer(playback: BroadcastPlayback, onClose: () -> Unit) {
    val activity = LocalContext.current.findPlayerActivity()
    val player = rememberIglooPlayer() ?: return
    var error by remember(playback) { mutableStateOf(false) }
    var controlsVisible by remember { mutableStateOf(true) }
    var fullscreen by remember { mutableStateOf(false) }
    var chatVisible by remember(playback.videoId) { mutableStateOf(true) }
    var source by remember(playback) { mutableStateOf(playback) }
    val configuration = LocalConfiguration.current
    LaunchedEffect(activity, fullscreen) {
        activity?.requestedOrientation = playerRequestedOrientation(
            fullscreen, configuration.smallestScreenWidthDp >= 600)
        if (fullscreen) hidePlayerSystemBars(activity)
        else showPlayerSystemBars(activity)
    }
    DisposableEffect(activity) {
        onDispose {
            activity?.requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
            showPlayerSystemBars(activity)
        }
    }
    val api: BroadcastsApi = koinInject()
    val scope = rememberCoroutineScope()
    fun refresh() {
        if (!error) {
            player.seekToDefaultPosition()
            player.play()
            return
        }
        scope.launch {
            try {
                val response = api.stream(playback.videoId)
                val path = response.manifest_url ?: response.media_url ?: error("Missing stream URL")
                source = playback.copy(url = api.absoluteUrl(path), mimeType = when (response.manifest_type) {
                    "hls" -> "application/x-mpegURL"
                    "dash" -> "application/dash+xml"
                    else -> response.media_type
                })
                error = false
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (_: Exception) { error = true }
        }
    }
    BackHandler { if (fullscreen) fullscreen = false else onClose() }
    LifecycleEventEffect(Lifecycle.Event.ON_STOP) { player.pause() }
    LaunchedEffect(player, source) {
        if (player.currentMediaItem?.localConfiguration?.uri?.toString() == source.url) return@LaunchedEffect
        val mediaItem = MediaItem.Builder().setUri(source.url).apply {
            source.mimeType?.let(::setMimeType)
        }.build()
        player.setMediaItem(mediaItem)
        player.prepare()
        player.playWhenReady = true
    }
    DisposableEffect(player) {
        val listener = object : Player.Listener {
            override fun onPlayerError(exception: PlaybackException) { error = true }
        }
        player.addListener(listener)
        onDispose {
            player.removeListener(listener)
            player.pause()
        }
    }
    Dialog(onDismissRequest = onClose, properties = DialogProperties(usePlatformDefaultWidth = false)) {
        PlayerChatLayout(
            showChat = chatVisible,
            sideBySide = configuration.orientation == Configuration.ORIENTATION_LANDSCAPE,
            modifier = Modifier.fillMaxSize().background(Color.Black),
            player = { surfaceModifier ->
                Box(surfaceModifier) {
                    PlayerSurface(
                        mode = if (fullscreen) PlayerSurfaceMode.Fullscreen else PlayerSurfaceMode.Inline,
                        player = player, posterUri = MediaUri.Missing, streamUri = MediaUri.Remote(source.url),
                        title = playback.title, onBack = { if (fullscreen) fullscreen = false else onClose() }, onPreviousVideo = null, onNextVideo = null,
                        segments = emptyList(), showSubtitles = false, onToggleSubtitles = null,
                        onToggleFullscreen = { fullscreen = !fullscreen }, onEnterPictureInPicture = null,
                        controlsVisible = controlsVisible, onControlsVisibleChange = { controlsVisible = it },
                        previewSpritePath = null, previewTrackJsonPath = null, subtitlePath = null,
                        currentPositionMs = { player.currentPosition }, sponsorBlockSkipSegment = null,
                        sponsorBlockAutoSkipMessage = null, onSkipSponsorBlock = {},
                        levelFeedback = null, onBrightnessChange = {}, onVolumeChange = {},
                        onToggleChat = { chatVisible = !chatVisible }, chatVisible = chatVisible,
                        onRefresh = ::refresh, modifier = Modifier.fillMaxSize(),
                    )
                    if (error) Text(stringResource(R.string.broadcast_playback_failed), color = Color.White,
                        modifier = Modifier.align(Alignment.Center).padding(16.dp))
                }
            },
            chat = { chatModifier ->
                YouTubeChat(playback.videoId, player, live = true,
                    onClose = { chatVisible = false }, modifier = chatModifier)
            },
        )
    }
}
