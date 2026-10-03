package com.screwy.igloo.home

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.ArrowDownward
import androidx.compose.material.icons.filled.ArrowUpward
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.ContentCopy
import androidx.compose.material.icons.filled.DeleteOutline
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Repeat
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Tune
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
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
import androidx.navigation.NavController
import com.screwy.igloo.R
import com.screwy.igloo.media.MediaResolvers
import com.screwy.igloo.media.MediaUri
import com.screwy.igloo.media.OwnerKind
import com.screwy.igloo.media.ownerKindFromAssetOwnerKind
import com.screwy.igloo.player.rememberIglooPlayer
import com.screwy.igloo.ui.component.Avatar
import com.screwy.igloo.ui.component.MediaCellArtwork
import com.screwy.igloo.ui.component.PlatformChip
import com.screwy.igloo.ui.component.ScreenHeader
import com.screwy.igloo.ui.component.createComposePlayerView
import com.screwy.igloo.ui.component.parseFeedPoll
import com.screwy.igloo.ui.component.videoDurationBadgeLabel
import com.screwy.igloo.ui.nav.IglooNavigationSource
import com.screwy.igloo.ui.nav.LocalDrawerController
import com.screwy.igloo.ui.nav.rememberIglooNavigator
import com.screwy.igloo.ui.theme.iglooColors
import org.koin.compose.koinInject
import java.text.DateFormat
import java.util.Date
import kotlinx.coroutines.flow.map

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HomeRoute(navController: NavController, vm: HomeViewModel) {
    val layout by vm.layout.collectAsStateWithLifecycle()
    val content by vm.content.collectAsStateWithLifecycle()
    val accounts by vm.accounts.collectAsStateWithLifecycle()
    val refreshing by vm.isRefreshing.collectAsStateWithLifecycle()
    val failed by vm.syncFailed.collectAsStateWithLifecycle()
    val playback by vm.activePlayback.collectAsStateWithLifecycle()
    val preparingPlayback by vm.isPreparingPlayback.collectAsStateWithLifecycle()
    val navigator = rememberIglooNavigator(navController)
    val drawer = LocalDrawerController.current
    var editing by rememberSaveable { mutableStateOf(false) }
    var adding by rememberSaveable { mutableStateOf(false) }
    var editingLayout by rememberSaveable { mutableStateOf(false) }
    var editingWidgetId by rememberSaveable { mutableStateOf<String?>(null) }
    var removingWidgetId by rememberSaveable { mutableStateOf<String?>(null) }
    DisposableEffect(vm) { onDispose { vm.closePlayback() } }

    fun openCard(card: HomeCard) {
        when {
            card.broadcast?.liveStatus == "is_upcoming" -> navigator.openChannel(card.channelId, IglooNavigationSource.Home)
            card.broadcast != null -> vm.playBroadcast(card)
            card.feed != null -> navigator.openThread(card.id, IglooNavigationSource.Home)
            card.video?.video?.ownerKind == "youtube_video" -> navigator.openVideo(card.id, IglooNavigationSource.Home)
            card.video?.video?.ownerKind == "tweet" && card.isBookmarked -> navigator.openShorts(
                "bookmarks", "_", card.id, IglooNavigationSource.Home,
            )
            card.video != null -> navigator.openShorts(
                if (card.video.video.sourceKind == "story") "story" else "channel",
                card.channelId, card.id, IglooNavigationSource.Home,
            )
        }
    }
    val onChannel: (String) -> Unit = { navigator.openChannel(it, IglooNavigationSource.Home) }

    BoxWithConstraints(Modifier.fillMaxSize()) {
        val columns = minOf(layout.columns, when {
            maxWidth >= 1400.dp -> 6
            maxWidth >= 1100.dp -> 4
            maxWidth >= 850.dp -> 3
            maxWidth >= 600.dp -> 2
            else -> 1
        })
        val gap = if (layout.spacing == "compact") 8.dp else 16.dp
        PullToRefreshBox(isRefreshing = refreshing, onRefresh = vm::refresh, modifier = Modifier.fillMaxSize()) {
            LazyVerticalGrid(
                columns = GridCells.Fixed(columns),
                contentPadding = PaddingValues(start = gap, end = gap, bottom = gap),
                horizontalArrangement = Arrangement.spacedBy(gap),
                verticalArrangement = Arrangement.spacedBy(gap),
                modifier = Modifier.fillMaxSize(),
            ) {
                item(span = { GridItemSpan(maxLineSpan) }) {
                    Column {
                        ScreenHeader(
                            title = stringResource(R.string.nav_home),
                            navigationIcon = {
                                IconButton(onClick = drawer::open) {
                                    Icon(Icons.Default.Menu, stringResource(R.string.action_open_drawer))
                                }
                            },
                            actions = {
                                if (failed) IconButton(onClick = vm::refresh) {
                                    Icon(Icons.Default.Refresh, stringResource(R.string.action_refresh))
                                }
                                IconButton(onClick = { adding = true }) {
                                    Icon(Icons.Default.Add, stringResource(R.string.home_add_widget))
                                }
                                IconButton(onClick = { editing = !editing }) {
                                    Icon(Icons.Default.Tune, stringResource(R.string.home_customize),
                                        tint = if (editing) MaterialTheme.iglooColors.primary else MaterialTheme.iglooColors.onSurface)
                                }
                            },
                        )
                        if (editing) Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                            IconButton(onClick = { editingLayout = true }) {
                                Icon(Icons.Default.Settings, stringResource(R.string.home_layout))
                            }
                        }
                    }
                }
                items(content, key = { it.widget.id }, span = { GridItemSpan(widgetSpan(it.widget.size, columns)) }) { item ->
                    HomeWidgetView(
                        content = item, editing = editing, onOpen = ::openCard, onChannel = onChannel,
                        onSettings = { editingWidgetId = item.widget.id },
                        onUp = { vm.moveWidget(item.widget.id, -1) },
                        onDown = { vm.moveWidget(item.widget.id, 1) },
                        onDuplicate = { vm.duplicateWidget(item.widget) },
                        onRemove = { removingWidgetId = item.widget.id },
                    )
                }
                if (layout.widgets.isEmpty()) item(span = { GridItemSpan(maxLineSpan) }) {
                    IconButton(onClick = { adding = true }, modifier = Modifier.fillMaxWidth()) {
                        Icon(Icons.Default.Add, stringResource(R.string.home_add_widget))
                    }
                }
            }
        }
    }

    if (adding) HomeWidgetCatalog(onDismiss = { adding = false }, onAdd = {
        vm.addWidget(it)
        adding = false
        editing = true
    })
    if (editingLayout) HomeLayoutSettings(layout, onDismiss = { editingLayout = false }, onSave = {
        vm.setLayout(it)
        editingLayout = false
    })
    layout.widgets.firstOrNull { it.id == editingWidgetId }?.let { widget ->
        HomeWidgetSettings(widget, accounts, onDismiss = { editingWidgetId = null }, onSave = {
            vm.saveWidget(it)
            editingWidgetId = null
        })
    }
    if (removingWidgetId != null) AlertDialog(
        onDismissRequest = { removingWidgetId = null },
        title = { Text(stringResource(R.string.home_remove_widget)) },
        confirmButton = { TextButton(onClick = {
            removingWidgetId?.let(vm::removeWidget)
            removingWidgetId = null
        }) { Text(stringResource(R.string.action_remove)) } },
        dismissButton = { TextButton(onClick = { removingWidgetId = null }) { Text(stringResource(R.string.action_cancel)) } },
    )
    if (preparingPlayback) Dialog(onDismissRequest = vm::closePlayback) {
        Surface(shape = RoundedCornerShape(16.dp)) {
            Row(Modifier.padding(24.dp), verticalAlignment = Alignment.CenterVertically) {
                CircularProgressIndicator(Modifier.size(32.dp))
                IconButton(onClick = vm::closePlayback) { Icon(Icons.Default.Close, stringResource(R.string.action_close)) }
            }
        }
    }
    playback?.let { HomeLivePlayer(it, onClose = vm::closePlayback) }
}

private fun widgetSpan(size: String, columns: Int): Int = when (size) {
    "full" -> columns
    "large" -> minOf(columns, maxOf(2, (columns * 2 + 2) / 3))
    "medium" -> (columns + 1) / 2
    else -> 1
}

@Composable
private fun HomeWidgetView(
    content: HomeWidgetContent, editing: Boolean, onOpen: (HomeCard) -> Unit,
    onChannel: (String) -> Unit, onSettings: () -> Unit, onUp: () -> Unit, onDown: () -> Unit,
    onDuplicate: () -> Unit, onRemove: () -> Unit,
) {
    val widget = content.widget
    val colors = MaterialTheme.iglooColors
    val background = when (widget.style) {
        "open" -> Color.Transparent
        "accent" -> colors.primary.copy(alpha = 0.08f)
        else -> colors.surface
    }
    Surface(shape = RoundedCornerShape(16.dp), color = background) {
        Column(Modifier.padding(if (widget.style == "open") 0.dp else 12.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            if (widget.showHeader || editing) Row(verticalAlignment = Alignment.CenterVertically) {
                Text(widget.title.ifBlank { stringResource(homeTypeLabel(widget.type)) },
                    style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold,
                    modifier = Modifier.weight(1f), color = colors.onSurface,
                    maxLines = 1, overflow = TextOverflow.Ellipsis)
                if (editing) IconButton(onClick = onSettings) {
                    Icon(Icons.Default.Settings, stringResource(R.string.home_widget_settings))
                }
            }
            if (editing) Row(horizontalArrangement = Arrangement.End, modifier = Modifier.fillMaxWidth()) {
                IconButton(onClick = onUp) { Icon(Icons.Default.ArrowUpward, stringResource(R.string.action_move_up)) }
                IconButton(onClick = onDown) { Icon(Icons.Default.ArrowDownward, stringResource(R.string.action_move_down)) }
                IconButton(onClick = onDuplicate) { Icon(Icons.Default.ContentCopy, stringResource(R.string.home_duplicate)) }
                IconButton(onClick = onRemove) { Icon(Icons.Default.DeleteOutline, stringResource(R.string.home_remove_widget)) }
            }
            if (content.cards.isEmpty()) Text(stringResource(R.string.home_empty),
                style = MaterialTheme.typography.bodySmall, color = colors.onSurfaceMuted,
                modifier = Modifier.padding(vertical = 12.dp))
            else when (widget.layout) {
                "portraits", "lanes" -> LazyRow(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    items(content.cards, key = { it.key }) { card ->
                        HomeCardView(card, widget, portrait = widget.layout == "portraits",
                            modifier = Modifier.width(if (widget.layout == "portraits") 150.dp else 260.dp),
                            onOpen = { onOpen(card) }, onChannel = { onChannel(card.channelId) })
                    }
                }
                "cards" -> BoxWithConstraints {
                    val perRow = if (maxWidth >= 380.dp) 2 else 1
                    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                        content.cards.chunked(perRow).forEach { row ->
                            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                                row.forEach { card ->
                                    HomeCardView(card, widget, modifier = Modifier.weight(1f), onOpen = { onOpen(card) },
                                        onChannel = { onChannel(card.channelId) })
                                }
                                if (row.size < perRow) Spacer(Modifier.weight(1f))
                            }
                        }
                    }
                }
                "feature" -> content.cards.forEachIndexed { index, card ->
                    HomeCardView(card, widget, compact = index > 0, onOpen = { onOpen(card) },
                        onChannel = { onChannel(card.channelId) })
                }
                else -> content.cards.forEachIndexed { index, card ->
                    if (index > 0) HorizontalDivider(color = colors.onSurfaceFaint.copy(alpha = 0.15f))
                    HomeCardView(card, widget, compact = widget.layout == "list", onOpen = { onOpen(card) },
                        onChannel = { onChannel(card.channelId) })
                }
            }
        }
    }
}

@Composable
internal fun HomeCardView(
    card: HomeCard, widget: HomeWidget, compact: Boolean = false, portrait: Boolean = false,
    modifier: Modifier = Modifier, onOpen: () -> Unit, onChannel: () -> Unit,
) {
    val colors = MaterialTheme.iglooColors
    val feedMedia = card.feed?.item?.mediaJson
    val hasMedia = card.video != null || card.broadcast != null ||
        (!feedMedia.isNullOrBlank() && feedMedia != "[]")
    Column(modifier.clickable(onClick = onOpen), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        if (compact) Row(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
            if (widget.showMedia && hasMedia) HomeArtwork(card,
                Modifier.width(104.dp).aspectRatio(16f / 9f))
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                if (widget.showText) HomeCardText(card, maxLines = 2)
                HomeCardIdentity(card, onChannel, avatar = false)
                HomeCardStatus(card)
            }
        } else {
            if (widget.showMedia && hasMedia) HomeArtwork(card,
                Modifier.fillMaxWidth().aspectRatio(if (portrait) 9f / 14f else 16f / 9f))
            HomeCardIdentity(card, onChannel, avatar = widget.layout == "editorial")
            if (widget.showText) {
                HomeCardText(card, maxLines = if (widget.layout == "editorial") 7 else 3)
                card.feed?.item?.quoteBodyText?.takeIf { it.isNotBlank() }?.let { quote ->
                    Surface(color = colors.surfaceVariant, shape = RoundedCornerShape(8.dp)) {
                        Column(Modifier.padding(10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            Text(card.feed.quoteAuthorDisplayName.orEmpty().ifBlank { card.feed.quoteAuthorHandle.orEmpty() },
                                style = MaterialTheme.typography.labelMedium, color = colors.onSurfaceMuted)
                            Text(quote, style = MaterialTheme.typography.bodySmall, maxLines = 3, overflow = TextOverflow.Ellipsis,
                                color = colors.onSurface)
                        }
                    }
                }
                val poll = remember(card.feed?.item?.pollJson) { parseFeedPoll(card.feed?.item?.pollJson) }
                poll?.choices?.forEach { choice ->
                    Surface(color = colors.surfaceVariant, shape = RoundedCornerShape(6.dp)) {
                        Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp, vertical = 5.dp)) {
                            Text(choice.label, Modifier.weight(1f), style = MaterialTheme.typography.bodySmall,
                                maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Text("${choice.percentage.toInt()}%", style = MaterialTheme.typography.labelSmall)
                        }
                    }
                }
            }
            HomeCardStatus(card)
        }
        val v = card.video
        if (card.platform != "twitter" && v?.playbackPosition != null && v.playbackPosition > 0) {
            val duration = v.watchDuration ?: v.video.duration?.toDouble() ?: 0.0
            if (duration > 0) LinearProgressIndicator(
                progress = { (v.playbackPosition / duration).toFloat().coerceIn(0f, 1f) },
                modifier = Modifier.fillMaxWidth(), color = colors.primary, trackColor = colors.surfaceVariant,
            )
        }
    }
}

@Composable
private fun HomeCardText(card: HomeCard, maxLines: Int) {
    val colors = MaterialTheme.iglooColors
    if (card.title.isNotBlank()) Text(card.title, style = MaterialTheme.typography.titleSmall,
        color = colors.onSurface, maxLines = 2, overflow = TextOverflow.Ellipsis)
    if (card.text.isNotBlank() && (card.feed != null || card.platform == "twitter") && card.text != card.title) Text(card.text, style = MaterialTheme.typography.bodyMedium,
        color = colors.onSurface, maxLines = maxLines, overflow = TextOverflow.Ellipsis)
}

@Composable
private fun HomeCardIdentity(card: HomeCard, onChannel: () -> Unit, avatar: Boolean) {
    if (card.reposterName.isNotBlank()) Row(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalAlignment = Alignment.CenterVertically) {
        Icon(Icons.Default.Repeat, contentDescription = null, modifier = Modifier.size(14.dp), tint = MaterialTheme.iglooColors.onSurfaceMuted)
        Text(card.reposterName, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.iglooColors.onSurfaceMuted,
            maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
    Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
        if (avatar) Avatar(card.channelId, size = 28.dp, onClick = onChannel)
        Text(card.channelName, Modifier.weight(1f).clickable(onClick = onChannel),
            style = MaterialTheme.typography.labelMedium, color = MaterialTheme.iglooColors.onSurfaceMuted,
            maxLines = 1, overflow = TextOverflow.Ellipsis)
        PlatformChip(card.platform)
    }
}

@Composable
private fun HomeCardStatus(card: HomeCard) {
    val broadcast = card.broadcast
    if (broadcast != null) Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(stringResource(when (broadcast.liveStatus) {
            "is_live" -> R.string.home_live_now
            "is_upcoming" -> R.string.home_upcoming
            "post_live" -> R.string.home_processing
            else -> R.string.home_replays
        }), style = MaterialTheme.typography.labelSmall,
            color = if (broadcast.liveStatus == "is_live") MaterialTheme.iglooColors.primary else MaterialTheme.iglooColors.onSurfaceMuted)
        val detail = when {
            broadcast.liveStatus == "is_upcoming" && broadcast.startsAtMs > 0 ->
                DateFormat.getDateTimeInstance(DateFormat.SHORT, DateFormat.SHORT).format(Date(broadcast.startsAtMs))
            broadcast.liveStatus == "is_live" && broadcast.concurrentViewCount != null ->
                java.text.NumberFormat.getInstance().format(broadcast.concurrentViewCount)
            else -> null
        }
        detail?.let { Text(it, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.iglooColors.onSurfaceMuted) }
    }
}

@Composable
private fun HomeArtwork(card: HomeCard, modifier: Modifier) {
    val resolvers: MediaResolvers = koinInject()
    val owner = card.video?.video?.ownerKind?.let(::ownerKindFromAssetOwnerKind) ?: OwnerKind.Tweet
    val thumbnail by remember(card.id, owner, card.broadcast) {
        if (card.broadcast != null) resolvers.thumbnailForPostFlow(card.id, OwnerKind.YouTubeVideo).map {
            if (it is MediaUri.Local) it else MediaUri.Remote(card.broadcast.thumbnailUrl)
        }
        else resolvers.thumbnailForPostFlow(card.id, owner)
    }.collectAsStateWithLifecycle(initialValue = MediaUri.Missing)
    Surface(shape = RoundedCornerShape(10.dp), color = MaterialTheme.iglooColors.surfaceVariant, modifier = modifier) {
        Box(Modifier.fillMaxSize()) {
            MediaCellArtwork(thumbnail, contentDescription = null)
            if (card.broadcast?.liveStatus == "is_live") Text("LIVE", color = Color.White,
                style = MaterialTheme.typography.labelSmall,
                modifier = Modifier.align(Alignment.TopEnd).padding(6.dp)
                    .background(MaterialTheme.iglooColors.primary, RoundedCornerShape(4.dp))
                    .padding(horizontal = 4.dp, vertical = 2.dp))
            card.video?.video?.let { video ->
                val durationLabel = videoDurationBadgeLabel(video)
                if (durationLabel.isNotEmpty()) Text(durationLabel, color = Color.White,
                    style = MaterialTheme.typography.labelSmall,
                    modifier = Modifier.align(Alignment.BottomEnd).padding(6.dp)
                        .background(Color.Black.copy(alpha = 0.65f), RoundedCornerShape(4.dp))
                        .padding(horizontal = 4.dp, vertical = 2.dp))
            }
            if (card.video != null || card.broadcast != null) Icon(Icons.Default.PlayArrow,
                contentDescription = null, tint = Color.White,
                modifier = Modifier.align(Alignment.BottomStart).padding(6.dp).size(22.dp)
                    .background(Color.Black.copy(alpha = 0.45f), RoundedCornerShape(4.dp)))
        }
    }
}

@androidx.annotation.OptIn(markerClass = [UnstableApi::class])
@Composable
internal fun HomeLivePlayer(playback: HomePlayback, onClose: () -> Unit) {
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
