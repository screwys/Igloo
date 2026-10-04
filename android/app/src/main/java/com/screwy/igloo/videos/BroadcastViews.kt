package com.screwy.igloo.videos

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
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.Lifecycle
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.ui.AspectRatioFrameLayout
import com.screwy.igloo.R
import com.screwy.igloo.media.MediaResolvers
import com.screwy.igloo.media.MediaUri
import com.screwy.igloo.media.OwnerKind
import com.screwy.igloo.player.rememberIglooPlayer
import com.screwy.igloo.ui.component.MediaCellArtwork
import com.screwy.igloo.ui.component.PlatformChip
import com.screwy.igloo.ui.component.createComposePlayerView
import com.screwy.igloo.ui.theme.iglooColors
import org.koin.compose.koinInject
import kotlinx.coroutines.flow.map

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
            PlatformChip("youtube")
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
    val context = LocalContext.current
    val player = rememberIglooPlayer() ?: return
    var error by remember(playback) { mutableStateOf(false) }
    val playerView = remember { createComposePlayerView(context).apply {
        useController = true
        resizeMode = AspectRatioFrameLayout.RESIZE_MODE_FIT
    } }
    BackHandler(onBack = onClose)
    LifecycleEventEffect(Lifecycle.Event.ON_STOP) { player.pause() }
    LaunchedEffect(player, playback) {
        if (player.currentMediaItem?.localConfiguration?.uri?.toString() == playback.url) return@LaunchedEffect
        val mediaItem = MediaItem.Builder().setUri(playback.url).apply {
            playback.mimeType?.let(::setMimeType)
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
            playerView.player = null
            player.removeListener(listener)
            player.pause()
        }
    }
    Dialog(onDismissRequest = onClose, properties = DialogProperties(usePlatformDefaultWidth = false)) {
        Box(Modifier.fillMaxSize().background(Color.Black)) {
            AndroidView(factory = { playerView }, update = { it.player = player }, modifier = Modifier.fillMaxSize())
            IconButton(onClick = onClose, modifier = Modifier.align(Alignment.TopEnd).padding(12.dp)) {
                Icon(Icons.Default.Close, stringResource(R.string.action_close), tint = Color.White)
            }
            if (error) IconButton(onClick = { error = false; player.prepare(); player.play() },
                modifier = Modifier.align(Alignment.Center)) {
                Icon(Icons.Default.Refresh, stringResource(R.string.action_refresh), tint = Color.White)
            }
        }
    }
}
