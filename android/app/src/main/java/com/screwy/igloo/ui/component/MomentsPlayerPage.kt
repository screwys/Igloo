package com.screwy.igloo.ui.component

import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.systemGestureExclusion
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.VolumeOff
import androidx.compose.material.icons.automirrored.filled.VolumeUp
import androidx.compose.material.icons.filled.Bookmark
import androidx.compose.material.icons.filled.PlayCircle
import androidx.compose.material.icons.outlined.BookmarkBorder
import androidx.compose.material.icons.outlined.PlayCircleOutline
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.res.vectorResource
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.zIndex
import androidx.media3.common.MediaItem
import androidx.media3.common.MimeTypes
import androidx.media3.common.Player
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.PlayerView
import com.screwy.igloo.R
import com.screwy.igloo.data.dao.BookmarkDao
import com.screwy.igloo.data.dao.AndroidSyncDao
import com.screwy.igloo.data.IglooDatabase
import com.screwy.igloo.outbox.pendingFeedActionOverrides
import com.screwy.igloo.data.stripPlatformPrefix
import com.screwy.igloo.log.Logger
import com.screwy.igloo.media.MediaResolvers
import com.screwy.igloo.media.MediaUri
import com.screwy.igloo.media.assetOwnerKind
import com.screwy.igloo.net.MomentsApi
import com.screwy.igloo.net.Reachability
import com.screwy.igloo.player.SubtitleOverlay
import com.screwy.igloo.moments.TikTokLiveChat
import com.screwy.igloo.player.rememberIglooPlayer
import com.screwy.igloo.ui.theme.iglooColors
import kotlin.math.max
import kotlinx.coroutines.delay
import kotlinx.coroutines.CancellationException
import org.koin.compose.koinInject

private fun prepareMomentVideo(
    player: ExoPlayer,
    loadedKey: String?,
    item: MomentItem,
    pageIndex: Int,
    streamUri: MediaUri,
    logger: Logger,
): String? {
    val targetLoadKey = momentStreamLoadKey(item.videoId, streamUri)
    if (targetLoadKey == null) {
        if (loadedKey != null || player.mediaItemCount > 0) {

            logger.debugMoment("moments_player_clear_missing_stream") {
                momentVideoDebugFields(
                    item = item,
                    pageIndex = pageIndex,
                    streamUri = streamUri,
                    player = player,
                    loadedKey = loadedKey,
                    targetLoadKey = null,
                )
            }
            player.playWhenReady = false
            player.pause()
            player.clearMediaItems()
        }
        return null
    }

    if (
        loadedKey == targetLoadKey &&
            player.mediaItemCount > 0 &&
            player.currentMediaItem?.mediaId == item.videoId
    ) {

        return loadedKey
    }

    val sourceItem = momentPlayerMediaItem(item.videoId, streamUri) ?: return null
    val mediaItem = if (item.isLive) sourceItem.buildUpon().setMimeType(MimeTypes.APPLICATION_M3U8).build() else sourceItem

    logger.debugMoment("moments_player_prepare_page") {
        momentVideoDebugFields(
            item = item,
            pageIndex = pageIndex,
            streamUri = streamUri,
            player = player,
            loadedKey = loadedKey,
            targetLoadKey = targetLoadKey,
        )
    }
    replaceMomentPlayerMediaItem(player, mediaItem)

    return targetLoadKey
}

private fun replaceMomentPlayerMediaItem(
    player: ExoPlayer,
    mediaItem: MediaItem,
) {
    val positionMs = if (player.currentMediaItem?.mediaId == mediaItem.mediaId) player.currentPosition else 0L
    player.setMediaItem(mediaItem, positionMs)
    player.prepare()
}

internal fun shouldRewindInactiveMomentPlayback(
    currentMediaId: String?,
    expectedVideoId: String,
    settledVideoId: String?,
    loadedVideoId: String?,
    mediaItemCount: Int,
    currentPositionMs: Long,
): Boolean =
    mediaItemCount > 0 &&
        currentMediaId == expectedVideoId &&
        settledVideoId != expectedVideoId &&
        loadedVideoId == expectedVideoId &&
        currentPositionMs > 0L

@Composable
internal fun MomentPage(
    playbackSpeed: Float = 1f,
    pageIndex: Int,
    item: MomentItem,
    storyMode: Boolean,
    muted: Boolean,
    onMuteToggle: () -> Unit,
    autoSwipe: Boolean,
    onAutoSwipeToggle: () -> Unit,
    showAutoSwipeControl: Boolean,
    isActive: Boolean,
    settledVideoId: String?,
    pagerScrolling: Boolean,
    shouldPrepare: Boolean,
    onAutoAdvance: () -> Unit,
    onChannelClick: (channelId: String) -> Unit,
    onStoryClick: (channelId: String, firstVideoId: String) -> Unit,
    onLiveClick: (channelId: String) -> Unit,
    onMentionClick: (handle: String) -> Unit,
    onBookmarkToggle: (MomentItem) -> Unit,
    onRequestBookmarkSheet: (MomentItem) -> Unit,
    onShare: (MomentItem) -> Unit,
    onFollowChannel: (channelId: String) -> Unit,
    onRequestUnfollowChannel: (MomentItem) -> Unit,
    onRequestMomentActions: (MomentItem) -> Unit,
    onSwipeLeftToChannel: (channelId: String) -> Unit,
    onSwipeRightFromEdge: () -> Unit,
    chromeVisible: Boolean = true,
    subtitlesVisible: Boolean = false,
    commentsVisible: Boolean = true,
    onCommentsVisibleChanged: (Boolean) -> Unit = {},
    liveFullscreen: Boolean = false,
    logger: Logger,
    sharedVideoPlayer: ExoPlayer? = null,
    sharedPlayerView: PlayerView? = null,
) {
    val colors = MaterialTheme.iglooColors
    val density = LocalDensity.current
    val swipeThresholdPx = with(density) { 80.dp.toPx() }
    val resolvers: MediaResolvers = koinInject()
    val bookmarkDao: BookmarkDao = koinInject()

    var dragAccumulator by remember(item.videoId) { mutableStateOf(0f) }
    // Description expand state resets on page change — simpler than persisting
    // per-video. `key1 = item.videoId` means swiping to a new moment collapses
    // the caption, matching the screenshot's default state.
    var expanded by remember(item.videoId) { mutableStateOf(false) }

    val mediaMode =
        remember(item.mediaKind, item.slideCount) {
            momentMediaMode(item.mediaKind, item.slideCount)
        }
    var manualSlideAdvanceTick by remember(item.videoId) { mutableIntStateOf(0) }
    val ownerKind = item.ownerKind
    val thumbnailFlow =
        remember(resolvers, item.mediaOwnerId, ownerKind) {
            resolvers.thumbnailForPostFlow(item.mediaOwnerId, ownerKind)
        }
    val thumbnailUri by thumbnailFlow.collectAsState(initial = MediaUri.Missing)
    val bookmarkFlow = remember(bookmarkDao, item.videoId) { bookmarkDao.getByIdFlow(item.videoId) }
    val bookmarkRow by bookmarkFlow.collectAsState(initial = null)
    val pendingBookmarkOverride = if (item.isLive) {
        val db: IglooDatabase = koinInject()
        val actions by db.outboxDao().pendingFeedActionRowsFlow().collectAsState(initial = emptyList())
        remember(actions, item.videoId) { pendingFeedActionOverrides(actions).bookmarksByTweetId[item.videoId] }
    } else null
    val isBookmarked = pendingBookmarkOverride ?: (bookmarkRow != null)
    val bookmarkItem =
        if (isBookmarked == item.isBookmarked) item else item.copy(isBookmarked = isBookmarked)
    val pageModifier =
        if (storyMode || mediaMode == MomentMediaMode.Slideshow) {
            Modifier.fillMaxSize().background(Color.Black)
        } else {
            Modifier.fillMaxSize().background(Color.Black).pointerInput(item.videoId) {
                detectHorizontalDragGestures(
                    onDragEnd = {
                        if (isLeftSwipe(dragAccumulator, swipeThresholdPx)) {
                            onSwipeLeftToChannel(item.channelId)
                        }
                        dragAccumulator = 0f
                    },
                    onDragCancel = { dragAccumulator = 0f },
                    onHorizontalDrag = { _, delta -> dragAccumulator += delta },
                )
            }
        }
    Box(modifier = pageModifier) {
        when (mediaMode) {
            MomentMediaMode.Image ->
                MomentImageSurface(
                    videoId = item.mediaOwnerId,
                    ownerKind = ownerKind,
                    thumbnailUri = thumbnailUri,
                    isActive = isActive,
                    autoSwipe = autoSwipe,
                    onAutoAdvance = onAutoAdvance,
                    modifier = Modifier.fillMaxSize(),
                )
            MomentMediaMode.Slideshow ->
                MomentSlideshowSurface(
                    videoId = item.mediaOwnerId,
                    ownerKind = ownerKind,
                    thumbnailUri = thumbnailUri,
                    isActive = isActive,
                    autoSwipe = autoSwipe,
                    onAutoAdvance = onAutoAdvance,
                    manualAdvanceTick = manualSlideAdvanceTick,
                    onManualAdvanceAtEnd = onAutoAdvance,
                    onSwipeLeftAtEnd =
                        if (storyMode) null else ({ onSwipeLeftToChannel(item.channelId) }),
                    onTap = if (storyMode) ({ manualSlideAdvanceTick++ }) else null,
                    onLongPress =
                        if (!storyMode) {
                            { onRequestMomentActions(bookmarkItem) }
                        } else {
                            null
                        },
                    muted = muted,
                    modifier = Modifier.fillMaxSize(),
                )
            MomentMediaMode.Video -> {
                MomentVideoLayer(
                    playbackSpeed = playbackSpeed,
                    subtitlesVisible = subtitlesVisible,
                    fitVideo = item.isLive && (liveFullscreen || LocalConfiguration.current.orientation == android.content.res.Configuration.ORIENTATION_LANDSCAPE),
                    pageIndex = pageIndex,
                    item = item,
                    thumbnailUri = thumbnailUri,
                    muted = muted,
                    isActive = isActive,
                    settledVideoId = settledVideoId,
                    pagerScrolling = pagerScrolling,
                    shouldPrepare = shouldPrepare,
                    autoSwipe = autoSwipe,
                    onAutoAdvance = onAutoAdvance,
                    onLongPress =
                        { onRequestMomentActions(bookmarkItem) },
                    logger = logger,
                    storyMode = storyMode,
                    sharedVideoPlayer = sharedVideoPlayer,
                    sharedPlayerView = sharedPlayerView,
                    modifier = Modifier.fillMaxSize(),
                )
            }
        }

        if (
            !storyMode &&
                mediaMode == MomentMediaMode.Image
        ) {
            MomentRepostLongPressLayer(
                onLongPress = { onRequestMomentActions(bookmarkItem) },
                modifier = Modifier.fillMaxSize(),
            )
        }

        if (storyMode && mediaMode != MomentMediaMode.Slideshow) {
            StoryTapAdvanceLayer(
                onTap = onAutoAdvance,
                modifier = Modifier.fillMaxSize(),
            )
        }

        if (chromeVisible) {
            // Top dim gradient keeps the TikTok-style tab row legible against any thumbnail.
            Box(
                modifier =
                    Modifier.fillMaxWidth()
                        .height(96.dp)
                        .background(
                            Brush.verticalGradient(
                                colors = listOf(Color.Black.copy(alpha = 0.45f), Color.Transparent)
                            )
                        )
            )

            if (mediaMode != MomentMediaMode.Video) {
                MomentBottomScrim(modifier = Modifier.align(Alignment.BottomCenter))
            }

            // Right-edge action rail — avatar/follow state plus mute / autoplay / bookmark / share.
            // The rail sits lower than before so the avatar occupies the social-action position and
            // the first control starts where the older middle controls used to sit.
            //
            // Play/pause is not a side-action: tap the video surface to toggle. The
            // auto-swipe button controls whether the player advances to the next
            // short or loops the current one when it ends.
            Column(
                modifier = Modifier.align(Alignment.BottomEnd).padding(end = 12.dp, bottom = 164.dp),
                verticalArrangement = Arrangement.spacedBy(18.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                val muteLabel = stringResource(R.string.action_mute)
                val unmuteLabel = stringResource(R.string.action_unmute)
                val autoSwipeStateLabel =
                    stringResource(
                        R.string.moments_auto_swipe_state,
                        stringResource(if (autoSwipe) R.string.state_on else R.string.state_off),
                    )
                val manageBookmarkLabel = stringResource(R.string.action_manage_bookmark)
                val addBookmarkLabel = stringResource(R.string.action_bookmark)
                val shareLabel = stringResource(R.string.action_share)
                MomentRailAvatar(
                    item = item,
                    onChannelClick = onChannelClick,
                    onStoryClick = onStoryClick,
                    onLiveClick = onLiveClick,
                    onFollowChannel = onFollowChannel,
                    onRequestUnfollowChannel = onRequestUnfollowChannel,
                    colors = colors,
                )
                ShadowIcon(
                    if (muted) Icons.AutoMirrored.Filled.VolumeOff
                    else Icons.AutoMirrored.Filled.VolumeUp,
                    if (muted) unmuteLabel else muteLabel,
                    onMuteToggle,
                    muted,
                    colors.primary,
                )
                if (showAutoSwipeControl) {
                    ShadowIcon(
                        if (autoSwipe) Icons.Filled.PlayCircle else Icons.Outlined.PlayCircleOutline,
                        autoSwipeStateLabel,
                        onAutoSwipeToggle,
                        autoSwipe,
                        colors.primary,
                    )
                }
                ShadowIcon(
                    if (isBookmarked) Icons.Filled.Bookmark else Icons.Outlined.BookmarkBorder,
                    if (isBookmarked) manageBookmarkLabel else addBookmarkLabel,
                    { onRequestBookmarkSheet(bookmarkItem) },
                    isBookmarked,
                    colors.primary,
                )
                ShadowIcon(
                    ImageVector.vectorResource(R.drawable.ic_feed_share_24),
                    shareLabel,
                    { onShare(bookmarkItem) },
                    false,
                    colors.primary,
                )
            }

            // Keep the drawer's left-edge gesture below the caption. Otherwise its
            // full-height hit target wins over the repost author's profile link.
            if (!storyMode) {
                MomentDrawerGestureHandle(
                    onOpenDrawer = onSwipeRightFromEdge,
                    onLongPress = { onRequestMomentActions(bookmarkItem) },
                )
            }

            // Bottom overlay — timestamp + description. Tapping overflowing text
            // only changes the description line limit; the caption stays anchored.
            val captionBaseBottomPadding = momentCaptionBaseBottomPaddingDp(mediaMode).dp
            val captionBottomPadding =
                if (storyMode) {
                    storyCaptionBottomPadding(captionBaseBottomPadding)
                } else {
                    captionBaseBottomPadding
                }
            CollapsedDescription(
                item = item,
                expanded = expanded,
                onMentionClick = onMentionClick,
                onChannelClick = onChannelClick,
                onReposterChannelClick = onChannelClick,
                onExpandedChange = { expanded = it },
                modifier =
                    Modifier.align(Alignment.BottomStart).padding(bottom = captionBottomPadding),
            )
        }
        if (item.isLive && isActive && commentsVisible) {
            TikTokLiveChat(
                channelId = item.channelId,
                fullscreen = liveFullscreen,
                onClose = { onCommentsVisibleChanged(false) },
                modifier = Modifier.fillMaxSize(),
            )
        }
    }
}

@Composable
private fun StoryTapAdvanceLayer(onTap: () -> Unit, modifier: Modifier = Modifier) {
    Box(modifier = modifier.pointerInput(onTap) { detectTapGestures(onTap = { onTap() }) })
}

internal fun Modifier.momentStationaryGestures(
    onTap: (() -> Unit)?,
    onLongPress: (() -> Unit)?,
): Modifier =
    then(
        if (onTap == null && onLongPress == null) {
            Modifier
        } else {
            Modifier.combinedClickable(
                onLongClick = onLongPress,
                onClick = onTap ?: {},
            )
        }
    )

@Composable
private fun storyCaptionBottomPadding(base: Dp): Dp =
    with(LocalDensity.current) {
        max(base.value, WindowInsets.navigationBars.getBottom(this).toDp().value + 12f).dp
    }

@Composable
private fun MomentRepostLongPressLayer(onLongPress: () -> Unit, modifier: Modifier = Modifier) {
    Box(
        modifier =
            modifier.pointerInput(onLongPress) {
                detectTapGestures(onLongPress = { onLongPress() })
            }
    )
}

@Composable
private fun BoxScope.MomentVideoLayer(
    playbackSpeed: Float,
    subtitlesVisible: Boolean,
    fitVideo: Boolean,
    pageIndex: Int,
    item: MomentItem,
    thumbnailUri: MediaUri,
    muted: Boolean,
    isActive: Boolean,
    settledVideoId: String?,
    pagerScrolling: Boolean,
    shouldPrepare: Boolean,
    autoSwipe: Boolean,
    onAutoAdvance: () -> Unit,
    onLongPress: (() -> Unit)?,
    logger: Logger,
    storyMode: Boolean,
    sharedVideoPlayer: ExoPlayer? = null,
    sharedPlayerView: PlayerView? = null,
    modifier: Modifier = Modifier,
) {
    val resolvers: MediaResolvers = koinInject()
    val ownerKind = item.ownerKind
    val syncDao: AndroidSyncDao = koinInject()
    val api: MomentsApi = koinInject()
    val reachability: Reachability = koinInject()
    val serverState by reachability.state.collectAsState()
    val assetFlow = remember(syncDao, item.mediaOwnerId, ownerKind) {
        syncDao.assetsForOwnerFlow(ownerKind.assetOwnerKind(), item.mediaOwnerId)
    }
    val assets by assetFlow.collectAsState(initial = emptyList())
    val subtitlePath = assets.firstOrNull { it.assetKind == "subtitle" && !it.localPath.isNullOrBlank() }?.localPath
    var subtitleContent by remember(item.videoId) { mutableStateOf<String?>(null) }
    LaunchedEffect(item.videoId, isActive, subtitlesVisible, subtitlePath, serverState) {
        if (!isActive || !subtitlesVisible || subtitlePath != null || serverState is Reachability.State.Offline) return@LaunchedEffect
        try {
            subtitleContent = api.subtitles(item.mediaOwnerId)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (_: Exception) {
            subtitleContent = null
        }
    }
    val streamFlow =
        remember(resolvers, item.mediaOwnerId, ownerKind) {
            if (item.liveStreamUrl.isNotBlank()) kotlinx.coroutines.flow.flowOf(MediaUri.Remote(item.liveStreamUrl))
            else resolvers.videoStreamFlow(item.mediaOwnerId, ownerKind)
        }
    val candidateStreamUri by streamFlow.collectAsState(initial = MediaUri.Missing)
    var playbackStreamUri by remember(item.videoId) { mutableStateOf(candidateStreamUri) }
    LaunchedEffect(candidateStreamUri, isActive, pagerScrolling, item.videoId) {
        if (
            shouldAdoptMomentPlaybackStreamUri(
                currentStreamUri = playbackStreamUri,
                isActive = isActive,
                pagerScrolling = pagerScrolling,
            )
        ) {
            playbackStreamUri = candidateStreamUri
        }
    }

    val playerIsShared = sharedVideoPlayer != null
    val player = sharedVideoPlayer ?: rememberIglooPlayer() ?: return
    var loadedKey by remember(item.videoId) { mutableStateOf<String?>(null) }
    var surfaceState by remember(item.videoId) { mutableStateOf(MomentVideoSurfaceState()) }
    var hasBeenActive by remember(item.videoId) { mutableStateOf(false) }
    if (!playerIsShared || isActive) {
        MomentVideoDebugTelemetry(
            pageIndex = pageIndex,
            item = item,
            streamUri = playbackStreamUri,
            player = player,
            loadedKey = loadedKey,
            isActive = isActive,
            shouldPrepare = shouldPrepare,
            logger = logger,
        )
    }

    val shouldPreparePlayer =
        shouldPrepareMomentVideoPlayer(
            isActive = isActive,
            shouldPrepare = shouldPrepare,
            sharedPlayer = playerIsShared,
        )
    LaunchedEffect(player, playbackStreamUri, item.videoId, shouldPreparePlayer, playerIsShared) {
        if (!shouldPreparePlayer) {
            if (!playerIsShared && !shouldPrepare) {

                player.playWhenReady = false
                player.pause()
                player.clearMediaItems()
                loadedKey = null
            }
            surfaceState = MomentVideoSurfaceState()
            return@LaunchedEffect
        }
        val nextLoadedKey =
            prepareMomentVideo(
                player = player,
                loadedKey = loadedKey,
                item = item,
                pageIndex = pageIndex,
                streamUri = playbackStreamUri,
                logger = logger,
            )
        loadedKey = nextLoadedKey
    }

    LaunchedEffect(player, muted) { player.volume = if (muted) 0f else 1f }
    LaunchedEffect(player, playbackSpeed, isActive) {
        if (!playerIsShared || isActive) player.setPlaybackSpeed(playbackSpeed)
    }
    if (!playerIsShared || isActive) {
        LaunchedEffect(
            player,
            isActive,
            settledVideoId,
            shouldPreparePlayer,
            loadedKey,
            pagerScrolling,
        ) {
            if (isActive && shouldPreparePlayer && loadedKey != null) {
                hasBeenActive = true

                player.playWhenReady = true
            } else {
                if (pagerScrolling && shouldPreparePlayer) {
                    delay(MOMENTS_STOP_OLD_PAGE_DELAY_MS)
                }
                player.playWhenReady = false
                player.pause()
                if (
                    hasBeenActive &&
                        shouldRewindInactiveMomentPlayback(
                            currentMediaId = player.currentMediaItem?.mediaId,
                            expectedVideoId = item.videoId,
                            settledVideoId = settledVideoId,
                            loadedVideoId = momentStreamLoadKeyVideoId(loadedKey),
                            mediaItemCount = player.mediaItemCount,
                            currentPositionMs = player.currentPosition,
                        )
                ) {

                    player.seekTo(0L)
                }
                if (!shouldPreparePlayer && player.mediaItemCount > 0) player.seekTo(0L)
            }
        }
    }

    if (!playerIsShared || isActive) {
        DisposableEffect(player, item.videoId, autoSwipe, isActive) {
            val listener =
                object : Player.Listener {
                    override fun onPlaybackStateChanged(state: Int) {
                        if (state != Player.STATE_ENDED) return
                        if (item.isLive) return
                        if (player.currentMediaItem?.mediaId != item.videoId) return
                        if (!isActive) return
                        if (autoSwipe) {
                            onAutoAdvance()
                            return
                        }
                        logger.debugMoment("moments_player_loop_restart") {
                            momentVideoDebugFields(
                                item = item,
                                pageIndex = pageIndex,
                                streamUri = playbackStreamUri,
                                player = player,
                                loadedKey = loadedKey,
                                targetLoadKey = loadedKey,
                            )
                        }
                        player.seekTo(0L)
                        player.playWhenReady = isActive
                    }
                }
            player.addListener(listener)
            onDispose { player.removeListener(listener) }
        }
    }

    val remoteOffline = isIglooRemoteOffline(playbackStreamUri)
    val showFallback =
        shouldShowMomentThumbnailFallback(
            remoteOffline = remoteOffline,
            surfaceState = surfaceState,
        )
    val hasLoadedMedia = momentStreamLoadKeyVideoId(loadedKey) == item.videoId
    val showFallbackLayer =
        shouldShowMomentVideoFallbackLayer(
            fallback = showFallback,
            sharedPlayer = playerIsShared,
            isActive = isActive,
            pagerScrolling = pagerScrolling,
            hasLoadedMedia = hasLoadedMedia,
        )
    val shouldMountVideoSurface =
        shouldMountMomentVideoSurface(
            isActive = isActive,
            shouldPrepare = shouldPrepare,
            sharedPlayer = playerIsShared,
            streamUri = playbackStreamUri,
            remoteOffline = remoteOffline,
        )
    Box(modifier = modifier.fillMaxSize().background(Color.Black)) {
        if (shouldMountVideoSurface) {
            VideoSurface(
                player = player,
                mediaKey = item.videoId,
                pageIndex = pageIndex,
                onStateChange = { surfaceState = it },
                sharedPlayerView = sharedPlayerView,
                fitVideo = fitVideo,
                modifier = Modifier.fillMaxSize().zIndex(momentVideoSurfaceZIndex()),
            )
        }
        if (showFallbackLayer) {
            ThumbnailFallback(
                thumbnailUri = thumbnailUri,
                alphaOverride = if (remoteOffline) 0.55f else 1f,
                brokenIconTint = MaterialTheme.iglooColors.onSurfaceFaint,
                contentScale = momentVideoFallbackContentScale(),
                modifier = Modifier.zIndex(momentVideoFallbackZIndex()),
            )
        }
        val momentActionLongPress = onLongPress
        if (!storyMode && isActive && playbackStreamUri !is MediaUri.Missing && !remoteOffline) {
            MomentVideoGestureLayer(
                player = player,
                onLongPress = momentActionLongPress,
                modifier = Modifier.fillMaxSize(),
            )
        } else if (
            shouldUseMomentActionFallbackLongPress(storyMode, momentActionLongPress != null) &&
                momentActionLongPress != null
        ) {
            // Repost account actions do not depend on a stream being ready. Keep them
            // available while media is loading or unavailable offline.
            MomentRepostLongPressLayer(
                onLongPress = momentActionLongPress,
                modifier = Modifier.fillMaxSize(),
            )
        }
        MomentBottomScrim(modifier = Modifier.align(Alignment.BottomCenter))
        if (isActive) {
            SubtitleOverlay(
                subtitlePath = subtitlePath,
                subtitleContent = subtitleContent,
                currentPositionMs = { player.currentPosition },
                visible = subtitlesVisible,
                bottomPadding = 232.dp,
                modifier = Modifier.align(Alignment.BottomCenter),
            )
        }
        if (
            !item.isLive && shouldShowMomentsVideoProgressBar(
                isActive = isActive,
                shouldPrepare = shouldPrepare,
                streamUri = playbackStreamUri,
                remoteOffline = remoteOffline,
                surfaceState = surfaceState,
            )
        ) {
            MomentsVideoProgressBar(
                player = player,
                modifier = Modifier.align(Alignment.BottomCenter).padding(bottom = 8.dp),
            )
        }
        if (remoteOffline) DownloadPendingBadge()
    }
}

@Composable
private fun MomentBottomScrim(modifier: Modifier = Modifier) {
    Box(
        modifier =
            modifier
                .fillMaxWidth()
                .height(220.dp)
                .background(
                    Brush.verticalGradient(
                        colors =
                            listOf(
                                Color.Transparent,
                                Color.Black.copy(alpha = 0.28f),
                                Color.Black.copy(alpha = 0.74f),
                            )
                    )
                )
    )
}

@Composable
private fun BoxScope.MomentVideoGestureLayer(
    player: ExoPlayer,
    onLongPress: (() -> Unit)?,
    modifier: Modifier = Modifier,
) {
    var seekIndicator by remember(player) { mutableStateOf<SeekIndicator?>(null) }
    Box(
        modifier =
            modifier.pointerInput(player, onLongPress) {
                detectTapGestures(
                    onTap = {
                        seekIndicator = null
                        player.playWhenReady = !player.playWhenReady
                    },
                    onDoubleTap = { offset ->
                        val isLeft = offset.x < size.width / 2
                        val deltaMs = if (isLeft) -5_000L else 5_000L
                        val duration = player.duration.coerceAtLeast(0L)
                        val target = (player.currentPosition + deltaMs).coerceIn(0L, duration)
                        player.seekTo(target)
                        seekIndicator = if (isLeft) SeekIndicator.Back else SeekIndicator.Forward
                    },
                    onLongPress = {
                        seekIndicator = null
                        onLongPress?.invoke()
                    },
                )
            }
    )
    if (seekIndicator != null) {
        val indicator = seekIndicator
        LaunchedEffect(indicator) {
            delay(600L)
            if (seekIndicator == indicator) seekIndicator = null
        }
        val isBack = indicator == SeekIndicator.Back
        Box(
            modifier =
                Modifier.align(if (isBack) Alignment.CenterStart else Alignment.CenterEnd)
                    .padding(horizontal = 32.dp)
                    .background(
                        Color.Black.copy(alpha = 0.55f),
                        androidx.compose.foundation.shape.CircleShape,
                    )
                    .padding(horizontal = 16.dp, vertical = 10.dp)
        ) {
            Text(
                text = if (isBack) "-5s" else "+5s",
                color = Color.White,
                style = MaterialTheme.typography.labelLarge,
            )
        }
    }
}

internal fun shouldUseMomentActionFallbackLongPress(
    storyMode: Boolean,
    hasMomentActions: Boolean,
): Boolean =
    !storyMode && hasMomentActions

@Composable
internal fun BoxScope.MomentDrawerGestureHandle(
    onOpenDrawer: () -> Unit,
    onLongPress: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
) {
    val thresholdPx = with(LocalDensity.current) { 56.dp.toPx() }
    val longPressModifier =
        if (onLongPress == null) {
            Modifier
        } else {
            Modifier.pointerInput(onLongPress) {
                detectTapGestures(onLongPress = { onLongPress() })
            }
        }
    Box(
        modifier =
            Modifier.align(Alignment.CenterStart)
                .fillMaxHeight()
                .width(96.dp)
                .systemGestureExclusion()
                .then(modifier)
                // The edge owns both gestures. A horizontal drag consumes the long-press
                // detector, while a stationary press opens the Moment actions.
                .then(longPressModifier)
                .pointerInput(onOpenDrawer, thresholdPx) {
                    var totalDragX = 0f
                    var opened = false
                    detectHorizontalDragGestures(
                        onDragStart = {
                            totalDragX = 0f
                            opened = false
                        },
                        onDragCancel = {
                            totalDragX = 0f
                            opened = false
                        },
                        onDragEnd = {
                            totalDragX = 0f
                            opened = false
                        },
                        onHorizontalDrag = { change, delta ->
                            totalDragX = (totalDragX + delta).coerceAtLeast(0f)
                            if (totalDragX > 0f) change.consume()
                            if (!opened && totalDragX >= thresholdPx) {
                                opened = true
                                onOpenDrawer()
                            }
                        },
                    )
                }
    )
}

/** Which side of the screen the user double-tapped — drives the seek indicator. */
private enum class SeekIndicator {
    Back,
    Forward,
}

internal fun momentAuthorLabel(item: MomentItem): String {
    val normalizedHandle = normalizeHandle(item.authorHandle)
    return displayLabel(
        primary = item.authorDisplayName,
        handle = normalizedHandle,
        fallback = stripPlatformPrefix(item.channelId),
    )
}

@Composable
internal fun momentRepostLabel(item: MomentItem): String? {
    val author = item.repostAuthorLabel?.takeIf { it.isNotBlank() } ?: return null
    return when {
        item.repostOtherCount <= 0 -> stringResource(R.string.feed_reposted_single, author)
        item.repostOtherCount == 1 -> stringResource(R.string.feed_reposted_one_other, author)
        else -> stringResource(R.string.feed_reposted_many_others, author, item.repostOtherCount)
    }
}
