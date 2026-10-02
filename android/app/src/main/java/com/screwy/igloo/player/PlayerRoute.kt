package com.screwy.igloo.player

import android.app.Activity
import android.content.Context
import android.content.ComponentName
import android.content.Intent
import android.content.ServiceConnection
import android.os.IBinder
import android.app.PendingIntent
import android.net.Uri
import android.content.pm.ActivityInfo
import android.content.pm.PackageManager
import android.graphics.Rect
import android.os.Build
import android.util.Rational
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.PredictiveBackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Rect as ComposeRect
import androidx.compose.ui.layout.boundsInWindow
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.core.app.PictureInPictureModeChangedInfo
import androidx.core.app.PictureInPictureParamsCompat
import androidx.core.app.PictureInPictureUiStateCompat
import androidx.core.util.Consumer
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.media3.common.Player
import androidx.media3.common.MediaMetadata
import androidx.navigation.NavController
import com.screwy.igloo.R
import com.screwy.igloo.data.Dearrow
import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.data.dao.BookmarkDao
import com.screwy.igloo.data.dao.ChannelFollowDao
import com.screwy.igloo.data.dao.ChannelStarDao
import com.screwy.igloo.data.dao.OfflineVideoDownloadDao
import com.screwy.igloo.data.dao.VideoDao
import com.screwy.igloo.media.MediaUri
import com.screwy.igloo.MainActivity
import com.screwy.igloo.outbox.OutboxKind
import com.screwy.igloo.outbox.OutboxWriter
import com.screwy.igloo.sync.OfflineVideoActions
import com.screwy.igloo.ui.UiEffect
import com.screwy.igloo.ui.UiEffects
import com.screwy.igloo.ui.component.VideoBinaryAction
import com.screwy.igloo.ui.component.sharePlainText
import com.screwy.igloo.ui.component.videoBinaryAction
import com.screwy.igloo.ui.nav.IglooNavigationSource
import com.screwy.igloo.ui.nav.rememberIglooNavigator
import com.screwy.igloo.ui.theme.iglooColors
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.launch
import org.koin.androidx.compose.koinViewModel
import org.koin.compose.koinInject
import org.koin.core.parameter.parametersOf
import kotlin.math.roundToInt

/**
 * YouTube long-form player route.
 *
 * Layout (single continuous scroll — no tabs):
 * 1. Video surface (16:9, black, with overlay + gestures + subtitles).
 * 2. Title + channel row + stats + description card (with Show more).
 * 3. Inline "Comments" header.
 * 4. Comment list (replies indented under parents when `parent_id` is set).
 *
 * The player route is hosted directly so orientation changes cannot swap it into the app shell or
 * permanent sidebar.
 *
 * PlaybackService owns ExoPlayer so playback and system controls can outlive this screen.
 * The VM exposes screen data as Flows.
 */
@Composable
fun PlayerRoute(videoId: String, navController: NavController, modifier: Modifier = Modifier) {
    val context = LocalContext.current.applicationContext
    var service by remember { mutableStateOf<PlaybackService?>(null) }
    DisposableEffect(context) {
        val connection = object : ServiceConnection {
            override fun onServiceConnected(name: ComponentName, binder: IBinder) {
                service = (binder as PlaybackService.LocalBinder).service
            }

            override fun onServiceDisconnected(name: ComponentName) {
                service = null
            }
        }
        context.bindService(
            Intent(context, PlaybackService::class.java).setAction(PlaybackService.LOCAL_BIND),
            connection,
            Context.BIND_AUTO_CREATE,
        )
        onDispose { context.unbindService(connection) }
    }
    service?.let { PlayerContent(videoId, navController, it, modifier) }
}

@Composable
private fun PlayerContent(
    videoId: String,
    navController: NavController,
    service: PlaybackService,
    modifier: Modifier,
) {
    val brightnessLabel = stringResource(R.string.player_brightness)
    val volumeLabel = stringResource(R.string.player_volume)

    val vm: PlayerViewModel = koinViewModel(parameters = { parametersOf(videoId) })
    val video by vm.video.collectAsStateWithLifecycle()
    val channel by vm.channel.collectAsStateWithLifecycle()
    val comments by vm.comments.collectAsStateWithLifecycle()
    val segments by vm.segments.collectAsStateWithLifecycle()
    val subtitlePath by vm.subtitlePath.collectAsStateWithLifecycle()
    val subtitleIsAuto by vm.subtitleIsAuto.collectAsStateWithLifecycle()
    val previewSpritePath by vm.previewSpritePath.collectAsStateWithLifecycle()
    val previewTrackJsonPath by vm.previewTrackJsonPath.collectAsStateWithLifecycle()
    val streamUri by vm.streamUri.collectAsStateWithLifecycle()
    val thumbnailUri by vm.thumbnailUri.collectAsStateWithLifecycle()
    val watchHistory by vm.watchHistory.collectAsStateWithLifecycle()
    val isRefreshingComments by vm.isRefreshingComments.collectAsStateWithLifecycle()
    val dearrowMode by vm.dearrowMode.collectAsStateWithLifecycle()

    val ctx = LocalContext.current
    val configuration = LocalConfiguration.current
    val largeScreen = configuration.smallestScreenWidthDp >= 600
    val lifecycleOwner = LocalLifecycleOwner.current
    val uriHandler = LocalUriHandler.current
    val scope = rememberCoroutineScope()
    val listState = rememberLazyListState()
    val prefs: PreferencesRepo = koinInject()
    val bookmarkDao: BookmarkDao = koinInject()
    val channelFollowDao: ChannelFollowDao = koinInject()
    val channelStarDao: ChannelStarDao = koinInject()
    val offlineVideoDownloadDao: OfflineVideoDownloadDao = koinInject()
    val offlineVideoActions: OfflineVideoActions = koinInject()
    val uiEffects: UiEffects = koinInject()
    val videoDao: VideoDao = koinInject()
    val outboxWriter: OutboxWriter = koinInject()
    val player = service.player
    val playbackCoordinator = remember { PlaybackCoordinator() }
    val activity = ctx.findActivity()
    val componentActivity = activity as? ComponentActivity
    val pictureInPictureSupported =
        remember(componentActivity) {
            componentActivity?.packageManager?.hasSystemFeature(
                PackageManager.FEATURE_PICTURE_IN_PICTURE
            ) == true
        }
    var isFullscreen by remember { mutableStateOf(false) }
    var playerControlsVisible by remember { mutableStateOf(true) }
    var playerPlayWhenReady by remember(player) { mutableStateOf(player.playWhenReady) }
    var playerPlaybackState by remember(player) { mutableIntStateOf(player.playbackState) }
    var pictureInPictureSourceRect by remember { mutableStateOf<Rect?>(null) }
    var isInPictureInPicture by
        remember(componentActivity) {
            mutableStateOf(componentActivity?.isInPictureInPictureMode == true)
        }
    var isTransitioningToPictureInPicture by remember { mutableStateOf(false) }
    var showUnfollowDialog by remember(videoId) { mutableStateOf(false) }
    var showDeleteLocalDialog by remember(videoId) { mutableStateOf(false) }
    var levelFeedback by remember(videoId) { mutableStateOf<PlayerLevelFeedback?>(null) }
    var levelFeedbackNonce by remember(videoId) { mutableStateOf(0L) }

    val sbSponsor by
        prefs
            .flowString(PreferencesRepo.Keys.SB_SPONSOR, PreferencesRepo.Defaults.SB_SPONSOR)
            .collectAsStateWithLifecycle(initialValue = PreferencesRepo.Defaults.SB_SPONSOR)
    val sbSelfPromo by
        prefs
            .flowString(PreferencesRepo.Keys.SB_SELF_PROMO, PreferencesRepo.Defaults.SB_SELF_PROMO)
            .collectAsStateWithLifecycle(initialValue = PreferencesRepo.Defaults.SB_SELF_PROMO)
    val sbInteraction by
        prefs
            .flowString(
                PreferencesRepo.Keys.SB_INTERACTION,
                PreferencesRepo.Defaults.SB_INTERACTION,
            )
            .collectAsStateWithLifecycle(initialValue = PreferencesRepo.Defaults.SB_INTERACTION)
    val sbIntro by
        prefs
            .flowString(PreferencesRepo.Keys.SB_INTRO, PreferencesRepo.Defaults.SB_INTRO)
            .collectAsStateWithLifecycle(initialValue = PreferencesRepo.Defaults.SB_INTRO)
    val sbOutro by
        prefs
            .flowString(PreferencesRepo.Keys.SB_OUTRO, PreferencesRepo.Defaults.SB_OUTRO)
            .collectAsStateWithLifecycle(initialValue = PreferencesRepo.Defaults.SB_OUTRO)
    val sbPreview by
        prefs
            .flowString(PreferencesRepo.Keys.SB_PREVIEW, PreferencesRepo.Defaults.SB_PREVIEW)
            .collectAsStateWithLifecycle(initialValue = PreferencesRepo.Defaults.SB_PREVIEW)
    val sbFiller by
        prefs
            .flowString(PreferencesRepo.Keys.SB_FILLER, PreferencesRepo.Defaults.SB_FILLER)
            .collectAsStateWithLifecycle(initialValue = PreferencesRepo.Defaults.SB_FILLER)
    val sbMusic by
        prefs
            .flowString(
                PreferencesRepo.Keys.SB_MUSIC_OFFTOPIC,
                PreferencesRepo.Defaults.SB_MUSIC_OFFTOPIC,
            )
            .collectAsStateWithLifecycle(initialValue = PreferencesRepo.Defaults.SB_MUSIC_OFFTOPIC)
    val useEmbedFriendlyShareLinks by
        prefs
            .shareEmbedFriendlyLinks()
            .collectAsStateWithLifecycle(
                initialValue = PreferencesRepo.Defaults.SHARE_EMBED_FRIENDLY_LINKS
            )
    val miniPlayerAutoEnter by
        prefs
            .miniPlayerAutoEnter()
            .collectAsStateWithLifecycle(
                initialValue = PreferencesRepo.Defaults.MINI_PLAYER_AUTO_ENTER
            )

    val bookmarkRow by
        bookmarkDao.getByIdFlow(videoId).collectAsStateWithLifecycle(initialValue = null)
    val followedChannels by
        channelFollowDao.allFlow().collectAsStateWithLifecycle(initialValue = emptyList())
    val starredChannels by
        channelStarDao.allFlow().collectAsStateWithLifecycle(initialValue = emptyList())
    val previousVideoId by
        produceState<String?>(initialValue = null, key1 = videoId) {
            value = videoDao.getPreviousVideoId(videoId)
        }
    val nextVideoId by
        produceState<String?>(initialValue = null, key1 = videoId) {
            value = videoDao.getNextVideoId(videoId)
        }
    DisposableEffect(player) {
        onDispose {
            if (service.player === player && !service.backgroundPlayback) player.pause()
        }
    }
    DisposableEffect(activity) {
        val initialBrightness = activity?.window?.attributes?.screenBrightness
        onDispose {
            if (activity != null && initialBrightness != null) {
                val attributes = activity.window.attributes
                attributes.screenBrightness = initialBrightness
                activity.window.attributes = attributes
            }
        }
    }

    DisposableEffect(lifecycleOwner, player, activity, isFullscreen) {
        val observer = LifecycleEventObserver { _, event ->
            when (event) {
                Lifecycle.Event.ON_RESUME -> {
                    service.backgroundPlayback = false
                    if (isFullscreen) hidePlayerSystemBars(activity)
                }
                Lifecycle.Event.ON_STOP -> {
                    if (!service.backgroundPlayback) {
                        player.pause()
                    }
                }
                else -> Unit
            }
        }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose { lifecycleOwner.lifecycle.removeObserver(observer) }
    }

    fun enterFullscreen() {
        isFullscreen = true
    }

    fun exitFullscreen() {
        isFullscreen = false
    }

    LaunchedEffect(
        activity,
        isFullscreen,
        largeScreen,
        configuration.orientation,
        isInPictureInPicture,
        isTransitioningToPictureInPicture,
    ) {
        if (activity != null &&
            !isInPictureInPicture &&
            !isTransitioningToPictureInPicture
        ) {
            activity.requestedOrientation = playerRequestedOrientation(isFullscreen, largeScreen)
            if (isFullscreen) {
                hidePlayerSystemBars(activity)
            } else {
                showPlayerSystemBars(activity)
            }
        }
    }
    DisposableEffect(activity) {
        onDispose {
            activity?.requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
            showPlayerSystemBars(activity)
        }
    }
    BackHandler(enabled = isFullscreen) { exitFullscreen() }
    PredictiveBackHandler(enabled = !isFullscreen) { progress ->
        try {
            progress.collect {}
            navController.popBackStack()
        } catch (_: CancellationException) {
            // A cancelled edge swipe keeps the player exactly where it was.
        }
    }
    // Bind media item when the stream URI resolves. Re-run if Sync verifies a local
    // file after playback started. Stop first so a mid-session swap doesn't leak
    // a black frame or audio tail from the old item.
    LaunchedEffect(streamUri, videoId) {
        val uri = when (val source = streamUri) {
            is MediaUri.Local -> source.file.toURI().toString()
            is MediaUri.Remote -> source.url
            is MediaUri.Missing -> return@LaunchedEffect
        }
        val playbackPlayer = service.playerForPlayback()
        if (service.videoId == videoId && service.sourceUri == uri) return@LaunchedEffect
        val resumeMs = if (service.videoId == videoId) playbackPlayer.currentPosition else
            ((watchHistory?.playbackPosition ?: 0.0) * 1000).toLong()
        playbackPlayer.stop()
        service.videoId = videoId
        service.sourceUri = uri
        playbackCoordinator.bind(
            player = ExoPlayerPlaybackPlayer(playbackPlayer),
            source =
                PlaybackSource(
                    mediaUri = streamUri,
                    resumeMs = resumeMs,
                ),
        )
    }

    DisposableEffect(player) {
        val listener =
            object : Player.Listener {
                override fun onPlayWhenReadyChanged(playWhenReady: Boolean, reason: Int) {
                    playerPlayWhenReady = playWhenReady
                }

                override fun onPlaybackStateChanged(playbackState: Int) {
                    playerPlaybackState = playbackState
                }

            }
        player.addListener(listener)
        playerPlayWhenReady = player.playWhenReady
        playerPlaybackState = player.playbackState
        onDispose { player.removeListener(listener) }
    }

    var showSubtitles by remember(videoId) { mutableStateOf(false) }
    var subtitleDefaultApplied by remember(videoId) { mutableStateOf(false) }
    LaunchedEffect(videoId, subtitlePath, subtitleIsAuto) {
        val trackIsAuto = subtitleIsAuto
        if (shouldApplySubtitleDefault(subtitleDefaultApplied, subtitlePath, trackIsAuto)) {
            showSubtitles = subtitleVisibleByDefault(trackIsAuto == true)
            subtitleDefaultApplied = true
        }
    }
    val metadataCounts =
        remember(video?.metadataJson) { parseVideoMetadataCounts(video?.metadataJson) }
    val sponsorBlockModes =
        remember(
            sbSponsor,
            sbSelfPromo,
            sbInteraction,
            sbIntro,
            sbOutro,
            sbPreview,
            sbFiller,
            sbMusic,
        ) {
            sponsorBlockModeMap(
                sponsor = sbSponsor,
                selfPromo = sbSelfPromo,
                interaction = sbInteraction,
                intro = sbIntro,
                outro = sbOutro,
                preview = sbPreview,
                filler = sbFiller,
                music = sbMusic,
            )
        }
    val activeSegments = remember(segments, sponsorBlockModes) {
        buildSponsorBlockUiSegments(segments, sponsorBlockModes)
    }
    LaunchedEffect(videoId, activeSegments) { service.segments = activeSegments }
    val sponsorBlockPlayback = SponsorBlockPlaybackState(
        visibleSegments = activeSegments.map { it.source },
        skipSegment = service.sponsorBlock.skipSegment,
        autoSkipMessage = service.sponsorBlock.autoSkipMessage,
        onSkip = service.sponsorBlock::skip,
    )
    LaunchedEffect(
        playerControlsVisible,
        showSubtitles,
        segments.size,
        previewSpritePath,
        previewTrackJsonPath,
    ) {}
    fun showLevelFeedback(label: String, level: Float) {
        levelFeedbackNonce += 1
        levelFeedback = PlayerLevelFeedback(label, level, levelFeedbackNonce)
    }

    LaunchedEffect(levelFeedback?.nonce) {
        if (levelFeedback != null) {
            delay(900L)
            levelFeedback = null
        }
    }
    val channelId = video?.channelId ?: channel?.channelId
    val navigator = rememberIglooNavigator(navController)
    val isBookmarked = bookmarkRow != null
    val isFollowed = channelId != null && followedChannels.any { it.channelId == channelId }
    val isStarred = channelId != null && starredChannels.any { it.channelId == channelId }
    val offlineVideoDownload by
        offlineVideoDownloadDao.flow(videoId).collectAsStateWithLifecycle(initialValue = null)
    val binaryAction = videoBinaryAction(
        streamUri = streamUri,
        isManualDownload = offlineVideoDownload?.state == "downloaded",
    )
    val displayPosterUri = thumbnailUri
    val playerTitle =
        Dearrow.resolveTitle(
            dearrowMode,
            video?.title,
            video?.dearrowTitle,
            video?.dearrowTitleCasual,
        )
    val canonicalShareUrl = video?.canonicalUrl?.takeIf { it.isNotBlank() }
    LaunchedEffect(player, playerTitle, channel?.name, thumbnailUri, streamUri, videoId) {
        if (service.videoId != videoId) return@LaunchedEffect
        val item = player.currentMediaItem ?: return@LaunchedEffect
        val artworkUri = when (val thumbnail = thumbnailUri) {
            is MediaUri.Local -> Uri.fromFile(thumbnail.file)
            is MediaUri.Remote -> Uri.parse(thumbnail.url)
            is MediaUri.Missing -> null
        }
        player.replaceMediaItem(0, item.buildUpon().setMediaId(videoId)
            .setMediaMetadata(MediaMetadata.Builder().setTitle(playerTitle)
                .setArtist(channel?.name).setArtworkUri(artworkUri).build()).build())
        service.setSessionActivity(PendingIntent.getActivity(
            ctx, 0,
            Intent(ctx, MainActivity::class.java).apply {
                action = Intent.ACTION_VIEW
                data = Uri.parse("igloo://youtube/$videoId")
                flags = Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP
            },
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        ))
    }
    val onPreviousVideo =
        previousVideoId?.let { prevId ->
            { navigator.openVideo(prevId, IglooNavigationSource.Player) }
        }
    val onNextVideo =
        nextVideoId?.let { nextId -> { navigator.openVideo(nextId, IglooNavigationSource.Player) } }
    val autoMiniPlayerEligible =
        shouldAutoEnterMiniPlayer(
            preferenceEnabled = miniPlayerAutoEnter && !service.backgroundPlayback,
            playWhenReady = playerPlayWhenReady,
            playbackState = playerPlaybackState,
            streamAvailable = streamUri !is MediaUri.Missing,
        )
    val pictureInPictureParams =
        remember(autoMiniPlayerEligible, pictureInPictureSourceRect, playerTitle) {
            buildPictureInPictureParams(
                enabled = autoMiniPlayerEligible,
                title = playerTitle,
                sourceRect = pictureInPictureSourceRect,
            )
        }
    val currentAutoMiniPlayerEligible by rememberUpdatedState(autoMiniPlayerEligible)
    val currentPictureInPictureParams by rememberUpdatedState(pictureInPictureParams)

    LaunchedEffect(componentActivity, pictureInPictureSupported, pictureInPictureParams) {
        if (pictureInPictureSupported) {
            componentActivity?.setPictureInPictureParams(pictureInPictureParams)
        }
    }
    DisposableEffect(componentActivity, pictureInPictureSupported) {
        if (componentActivity == null || !pictureInPictureSupported) {
            return@DisposableEffect onDispose {}
        }

        val leaveListener =
            Runnable {
                if (currentAutoMiniPlayerEligible && !service.backgroundPlayback) {
                    playerControlsVisible = false
                    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.S) {
                        componentActivity.enterPictureInPictureMode(
                            currentPictureInPictureParams
                        )
                    }
                }
            }
        val modeListener =
            Consumer<PictureInPictureModeChangedInfo> { info ->
                isInPictureInPicture = info.isInPictureInPictureMode
                isTransitioningToPictureInPicture = false
                playerControlsVisible = !info.isInPictureInPictureMode
            }
        val uiStateListener =
            Consumer<PictureInPictureUiStateCompat> { state ->
                isTransitioningToPictureInPicture = state.isTransitioningToPip
                if (state.isTransitioningToPip) playerControlsVisible = false
            }
        componentActivity.addOnUserLeaveHintListener(leaveListener)
        componentActivity.addOnPictureInPictureModeChangedListener(modeListener)
        componentActivity.addOnPictureInPictureUiStateChangedListener(uiStateListener)
        onDispose {
            componentActivity.removeOnUserLeaveHintListener(leaveListener)
            componentActivity.removeOnPictureInPictureModeChangedListener(modeListener)
            componentActivity.removeOnPictureInPictureUiStateChangedListener(uiStateListener)
            componentActivity.setPictureInPictureParams(
                PictureInPictureParamsCompat.Builder().setEnabled(false).build()
            )
        }
    }

    fun enterMiniPlayer() {
        if (!pictureInPictureSupported || componentActivity == null) return
        service.backgroundPlayback = false
        playerControlsVisible = false
        componentActivity.enterPictureInPictureMode(
            buildPictureInPictureParams(
                enabled = true,
                title = playerTitle,
                sourceRect = pictureInPictureSourceRect,
            )
        )
    }

    fun updatePictureInPictureSourceRect(bounds: ComposeRect) {
        val next =
            Rect(
                bounds.left.roundToInt(),
                bounds.top.roundToInt(),
                bounds.right.roundToInt(),
                bounds.bottom.roundToInt(),
            )
        if (next.width() > 0 && next.height() > 0 && next != pictureInPictureSourceRect) {
            pictureInPictureSourceRect = next
        }
    }

    fun onVideoBinaryAction() {
        when (binaryAction) {
            VideoBinaryAction.Download -> {
                scope.launch {
                    offlineVideoActions.requestDownload(videoId)
                    uiEffects.emit(UiEffect.ToastRes(R.string.status_video_download_queued))
                }
            }
            VideoBinaryAction.Delete -> showDeleteLocalDialog = true
        }
    }
    val pictureInPicturePresentation =
        isInPictureInPicture || isTransitioningToPictureInPicture
    val miniPlayerAction: (() -> Unit)? =
        if (pictureInPictureSupported &&
            streamUri !is MediaUri.Missing &&
            !pictureInPicturePresentation
        ) {
            { enterMiniPlayer() }
        } else {
            null
        }
    val backgroundAction: (() -> Unit)? = if (streamUri !is MediaUri.Missing) {
        {
            service.backgroundPlayback = true
            if (pictureInPictureSupported) {
                componentActivity?.setPictureInPictureParams(
                    PictureInPictureParamsCompat.Builder().setEnabled(false).build()
                )
            }
            player.play()
            activity?.moveTaskToBack(true)
        }
    } else null
    if (isFullscreen || pictureInPicturePresentation) {
        PlayerSurface(
            mode = PlayerSurfaceMode.Fullscreen,
            player = player,
            posterUri = displayPosterUri,
            streamUri = streamUri,
            title = playerTitle,
            onBack = { exitFullscreen() },
            onPreviousVideo = onPreviousVideo,
            onNextVideo = onNextVideo,
            segments = sponsorBlockPlayback.visibleSegments,
            showSubtitles = showSubtitles,
            onToggleSubtitles = { showSubtitles = !showSubtitles },
            onToggleFullscreen = { exitFullscreen() },
            onEnterPictureInPicture = miniPlayerAction,
            onPlayInBackground = backgroundAction,
            controlsVisible = playerControlsVisible && !pictureInPicturePresentation,
            onControlsVisibleChange = {
                if (!pictureInPicturePresentation) playerControlsVisible = it
            },
            previewSpritePath = previewSpritePath,
            previewTrackJsonPath = previewTrackJsonPath,
            subtitlePath = subtitlePath,
            currentPositionMs = { player.currentPosition },
            sponsorBlockSkipSegment = sponsorBlockPlayback.skipSegment,
            sponsorBlockAutoSkipMessage = sponsorBlockPlayback.autoSkipMessage,
            onSkipSponsorBlock = sponsorBlockPlayback.onSkip,
            levelFeedback = levelFeedback,
            onBrightnessChange = { level -> showLevelFeedback(brightnessLabel, level) },
            onVolumeChange = { level -> showLevelFeedback(volumeLabel, level) },
            modifier =
                modifier.fillMaxSize().onGloballyPositioned {
                    updatePictureInPictureSourceRect(it.boundsInWindow())
                },
        )
    } else {
        LazyColumn(
            state = listState,
            modifier = modifier.fillMaxSize().background(MaterialTheme.iglooColors.background),
        ) {
            item(key = "player_status_spacer") {
                Spacer(modifier = Modifier.fillMaxWidth().statusBarsPadding().height(8.dp))
            }

            item {
                PlayerSurface(
                    mode = PlayerSurfaceMode.Inline,
                    player = player,
                    posterUri = displayPosterUri,
                    streamUri = streamUri,
                    title = playerTitle,
                    onBack = { navController.popBackStack() },
                    onPreviousVideo = onPreviousVideo,
                    onNextVideo = onNextVideo,
                    segments = sponsorBlockPlayback.visibleSegments,
                    showSubtitles = showSubtitles,
                    onToggleSubtitles = { showSubtitles = !showSubtitles },
                    onToggleFullscreen = { enterFullscreen() },
                    onEnterPictureInPicture = miniPlayerAction,
                    onPlayInBackground = backgroundAction,
                    controlsVisible = playerControlsVisible,
                    onControlsVisibleChange = { playerControlsVisible = it },
                    previewSpritePath = previewSpritePath,
                    previewTrackJsonPath = previewTrackJsonPath,
                    subtitlePath = subtitlePath,
                    currentPositionMs = { player.currentPosition },
                    sponsorBlockSkipSegment = sponsorBlockPlayback.skipSegment,
                    sponsorBlockAutoSkipMessage = sponsorBlockPlayback.autoSkipMessage,
                    onSkipSponsorBlock = sponsorBlockPlayback.onSkip,
                    levelFeedback = levelFeedback,
                    onBrightnessChange = { level -> showLevelFeedback(brightnessLabel, level) },
                    onVolumeChange = { level -> showLevelFeedback(volumeLabel, level) },
                    modifier =
                        Modifier.fillMaxWidth().aspectRatio(16f / 9f).onGloballyPositioned {
                            updatePictureInPictureSourceRect(it.boundsInWindow())
                        },
                )
            }

            item {
                VideoMetaBlock(
                    video = video,
                    dearrowMode = dearrowMode,
                    channel = channel,
                    metadataCounts = metadataCounts,
                    isBookmarked = isBookmarked,
                    isFollowed = isFollowed,
                    isStarred = isStarred,
                    videoBinaryAction = binaryAction,
                    onChannelClick = { cid ->
                        navigator.openChannel(cid, IglooNavigationSource.Player)
                    },
                    shareEnabled = canonicalShareUrl != null,
                    onShare = {
                        canonicalShareUrl?.let {
                            sharePlainText(ctx, it, useEmbedFriendlyShareLinks)
                        }
                    },
                    onBookmark = {
                        scope.launch {
                            outboxWriter.enqueue(
                                OutboxKind.Bookmark(
                                    videoId = videoId,
                                    action =
                                        if (isBookmarked) OutboxKind.Action.Clear
                                        else OutboxKind.Action.Set,
                                    categoryId = if (isBookmarked) null else 0L,
                                )
                            )
                        }
                    },
                    onToggleStar = {
                        val cid = channelId
                        if (cid != null) {
                            scope.launch {
                                outboxWriter.enqueue(
                                    OutboxKind.Star(
                                        channelId = cid,
                                        action =
                                            if (isStarred) OutboxKind.Action.Clear
                                            else OutboxKind.Action.Set,
                                    )
                                )
                            }
                        }
                    },
                    onUnfollow = { showUnfollowDialog = true },
                    onVideoBinaryAction = ::onVideoBinaryAction,
                    onMentionClick = vm::resolveMentionAndNavigate,
                    onUrlClick = uriHandler::openUri,
                    onTimestampClick = { targetMs ->
                        player.seekTo(targetMs)
                        scope.launch { listState.animateScrollToItem(0) }
                    },
                )
            }

            item {
                CommentsHeader(isRefreshing = isRefreshingComments, onRefresh = vm::refreshComments)
            }

            if (comments.isEmpty()) {
                item { CommentsEmptyState(isRefreshing = isRefreshingComments) }
            } else {
                val presentedComments = presentVideoComments(comments, channelId)
                itemsIndexed(
                    items = presentedComments,
                    key = { _, row -> row.comment.commentId },
                ) { _, row ->
                    CommentRow(
                        comment = row.comment,
                        threadDepth = row.depth,
                        replyToAuthor = row.replyToAuthor,
                        isCreator = row.isCreator,
                        onMentionClick = vm::resolveMentionAndNavigate,
                        onUrlClick = uriHandler::openUri,
                        onTimestampClick = { targetMs ->
                            player.seekTo(targetMs)
                            scope.launch { listState.animateScrollToItem(0) }
                        },
                    )
                }
            }
        }
    }

    if (showDeleteLocalDialog) {
        AlertDialog(
            onDismissRequest = { showDeleteLocalDialog = false },
            title = { Text(stringResource(R.string.action_delete_downloaded_video)) },
            text = { Text(stringResource(R.string.confirm_delete_downloaded_video_body)) },
            confirmButton = {
                TextButton(
                    onClick = {
                        showDeleteLocalDialog = false
                        scope.launch { offlineVideoActions.removeDownload(videoId) }
                    }
                ) {
                    Text(
                        stringResource(R.string.action_delete),
                        color = MaterialTheme.colorScheme.error,
                    )
                }
            },
            dismissButton = {
                TextButton(onClick = { showDeleteLocalDialog = false }) {
                    Text(stringResource(R.string.action_cancel))
                }
            },
        )
    }

    if (showUnfollowDialog && channelId != null) {
        val channelName =
            channel?.name ?: stringResource(R.string.confirm_unfollow_channel_default_name)
        AlertDialog(
            onDismissRequest = { showUnfollowDialog = false },
            title = { Text(stringResource(R.string.confirm_unfollow_channel_title)) },
            text = { Text(stringResource(R.string.confirm_unfollow_channel_body, channelName)) },
            confirmButton = {
                TextButton(
                    onClick = {
                        showUnfollowDialog = false
                        scope.launch {
                            outboxWriter.enqueue(
                                OutboxKind.Follow(
                                    channelId = channelId,
                                    action = OutboxKind.Action.Clear,
                                )
                            )
                        }
                    }
                ) {
                    Text(stringResource(R.string.action_unfollow))
                }
            },
            dismissButton = {
                TextButton(onClick = { showUnfollowDialog = false }) {
                    Text(stringResource(R.string.action_cancel))
                }
            },
        )
    }
}

internal fun shouldApplySubtitleDefault(
    defaultApplied: Boolean,
    subtitlePath: String?,
    subtitleIsAuto: Boolean?,
): Boolean = !defaultApplied && subtitlePath != null && subtitleIsAuto != null

internal fun subtitleVisibleByDefault(subtitleIsAuto: Boolean): Boolean = !subtitleIsAuto

internal fun shouldAutoEnterMiniPlayer(
    preferenceEnabled: Boolean,
    playWhenReady: Boolean,
    playbackState: Int,
    streamAvailable: Boolean,
): Boolean =
    preferenceEnabled &&
        playWhenReady &&
        streamAvailable &&
        playbackState != Player.STATE_IDLE &&
        playbackState != Player.STATE_ENDED

private fun buildPictureInPictureParams(
    enabled: Boolean,
    title: String,
    sourceRect: Rect?,
): PictureInPictureParamsCompat =
    PictureInPictureParamsCompat.Builder()
        .setEnabled(enabled)
        .setAspectRatio(Rational(16, 9))
        .apply {
            if (title.isNotBlank()) setTitle(title)
            if (sourceRect != null) setSourceRectHint(sourceRect)
        }
        .build()

private fun hidePlayerSystemBars(activity: Activity?) {
    val window = activity?.window ?: return
    WindowCompat.getInsetsController(window, window.decorView).apply {
        systemBarsBehavior =
            WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        hide(WindowInsetsCompat.Type.systemBars())
    }
}

private fun showPlayerSystemBars(activity: Activity?) {
    val window = activity?.window ?: return
    WindowCompat.getInsetsController(window, window.decorView)
        .show(WindowInsetsCompat.Type.systemBars())
}

private tailrec fun Context.findActivity(): Activity? =
    when (this) {
        is Activity -> this
        is android.content.ContextWrapper -> baseContext.findActivity()
        else -> null
    }

private fun Int.perfPlaybackStateName(): String =
    when (this) {
        Player.STATE_IDLE -> "idle"
        Player.STATE_BUFFERING -> "buffering"
        Player.STATE_READY -> "ready"
        Player.STATE_ENDED -> "ended"
        else -> "unknown"
    }
