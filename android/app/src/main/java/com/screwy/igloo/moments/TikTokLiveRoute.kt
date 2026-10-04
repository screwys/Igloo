package com.screwy.igloo.moments

import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import android.content.pm.ActivityInfo
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
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
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.repeatOnLifecycle
import androidx.navigation.NavController
import com.screwy.igloo.R
import com.screwy.igloo.media.OwnerKind
import com.screwy.igloo.net.MomentsApi
import com.screwy.igloo.net.TikTokLiveStream
import com.screwy.igloo.player.playerRequestedOrientation
import com.screwy.igloo.ui.component.BookmarkSheet
import com.screwy.igloo.ui.component.MomentActionSheet
import com.screwy.igloo.ui.component.MomentItem
import com.screwy.igloo.ui.component.MomentsPlayer
import com.screwy.igloo.ui.component.sharePlainText
import com.screwy.igloo.ui.nav.ApplyOverlayChrome
import com.screwy.igloo.ui.nav.OverlayChromeState
import com.screwy.igloo.ui.nav.IglooNavigationSource
import com.screwy.igloo.ui.nav.rememberIglooNavigator
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.withContext
import org.koin.androidx.compose.koinViewModel
import org.koin.compose.koinInject

@Composable
fun TikTokLiveRoute(channelId: String, navController: NavController) {
    val api: MomentsApi = koinInject()
    val vm: MomentsViewModel = koinViewModel()
    val context = LocalContext.current
    val navigator = rememberIglooNavigator(navController)
    var stream by remember(channelId) { mutableStateOf<TikTokLiveStream?>(null) }
    var failed by remember(channelId) { mutableStateOf(false) }
    var commentsVisible by rememberSaveable(channelId) { mutableStateOf(true) }
    var fullscreen by rememberSaveable(channelId) { mutableStateOf(false) }
    val muted by vm.muted.collectAsStateWithLifecycle()
    val pendingActions by vm.pendingMomentActions.collectAsStateWithLifecycle()
    val pendingBookmark by vm.pendingBookmark.collectAsStateWithLifecycle()
    val categories by vm.bookmarkCategories.collectAsStateWithLifecycle()
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    LaunchedEffect(channelId, lifecycle) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            failed = false
            stream = null
            var sessionId = ""
            try {
                val response = api.stream(channelId)
                check(response.success && response.manifest_url.isNotBlank())
                sessionId = response.session_id
                vm.captureLiveBookmark(response.video_id, response.bookmarked, response.bookmark_category_id)
                stream = response
                awaitCancellation()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (_: Exception) {
                failed = true
            } finally {
                if (sessionId.isNotBlank()) withContext(NonCancellable) {
                    runCatching { api.releaseStream(sessionId) }
                }
                stream = null
            }
        }
    }
    val activity = context.liveActivity()
    val largeScreen = LocalConfiguration.current.smallestScreenWidthDp >= 600
    ApplyOverlayChrome(if (fullscreen) OverlayChromeState.FullscreenMedia else OverlayChromeState.None)
    DisposableEffect(activity, fullscreen, largeScreen) {
        activity?.requestedOrientation = playerRequestedOrientation(fullscreen, largeScreen)
        activity?.window?.let { window ->
            WindowCompat.getInsetsController(window, window.decorView).apply {
                if (fullscreen) {
                    systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
                    hide(WindowInsetsCompat.Type.systemBars())
                } else show(WindowInsetsCompat.Type.systemBars())
            }
        }
        onDispose { }
    }
    DisposableEffect(activity) {
        onDispose {
            activity?.requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
            activity?.window?.let { WindowCompat.getInsetsController(it, it.decorView).show(WindowInsetsCompat.Type.systemBars()) }
        }
    }
    BackHandler(enabled = fullscreen) { fullscreen = false }
    val item = stream?.let { response ->
        val live = response.live
        MomentItem(
            videoId = response.video_id,
            channelId = live.channel_id,
            canonicalUrl = "https://www.tiktok.com/@${live.handle.removePrefix("@")}/live",
            authorDisplayName = live.display_name,
            authorHandle = "@${live.handle.removePrefix("@")}",
            description = live.title,
            likeCount = null,
            isLiked = false,
            isBookmarked = response.bookmarked,
            ownerKind = OwnerKind.TikTokVideo,
            liveStreamUrl = api.absoluteUrl(response.manifest_url),
        )
    }
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        if (failed) Text(stringResource(R.string.home_playback_failed))
        else if (item == null) CircularProgressIndicator()
        else MomentsPlayer(
            items = listOf(item),
            startIndex = 0,
            muteDefault = muted,
            onMuteChanged = vm::setMuted,
            onIndexChange = {},
            onViewEvent = {},
            onChannelClick = { navigator.openChannel(it, IglooNavigationSource.Moments) },
            onBookmarkToggle = vm::toggleBookmark,
            onRequestBookmarkSheet = vm::requestBookmarkSheet,
            onShare = { sharePlainText(context, it.canonicalUrl) },
            onFollowChannel = vm::followChannel,
            onUnfollowChannel = vm::unfollowChannel,
            onRequestMomentActions = vm::requestMomentActions,
            onMentionClick = vm::resolveMentionAndNavigate,
            onSwipeLeftToChannel = { navigator.openChannel(it, IglooNavigationSource.Moments) },
            commentsVisible = commentsVisible,
            onCommentsVisibleChanged = { commentsVisible = it },
            liveFullscreen = fullscreen,
            chromeVisible = pendingActions == null,
        )
    }
    pendingActions?.let { target ->
        MomentActionSheet(
            item = target,
            playbackSpeed = 1f,
            onPlaybackSpeedChanged = {},
            onDismissRequest = vm::dismissMomentActions,
            onRepostsEnabledChanged = vm::setRepostsEnabled,
            onChannelMutedChanged = vm::setChannelMuted,
            onUnfollowChannel = vm::unfollowChannel,
            onShare = { sharePlainText(context, it.canonicalUrl) },
            onVisitChannel = { navigator.openChannel(it, IglooNavigationSource.Moments) },
            commentsVisible = commentsVisible,
            onToggleComments = { commentsVisible = !commentsVisible },
            onToggleFullscreen = { fullscreen = !fullscreen },
        )
    }
    pendingBookmark?.let { target ->
        BookmarkSheet(
            target = target,
            categories = categories,
            onConfirm = vm::confirmBookmark,
            onRemove = vm::removePendingBookmark,
            onDismiss = vm::dismissBookmarkSheet,
            onCreateCategory = vm::createCategory,
        )
    }
}

private tailrec fun Context.liveActivity(): Activity? = when (this) {
    is Activity -> this
    is ContextWrapper -> baseContext.liveActivity()
    else -> null
}
