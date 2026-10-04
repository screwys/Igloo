package com.screwy.igloo.videos

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.Box
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.window.Dialog
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavController
import com.screwy.igloo.R
import com.screwy.igloo.videos.BroadcastsViewModel
import com.screwy.igloo.videos.BroadcastPlayer
import com.screwy.igloo.ui.theme.iglooColors
import com.screwy.igloo.sync.OfflineVideoActions
import com.screwy.igloo.ui.UiEffect
import com.screwy.igloo.ui.UiEffects
import com.screwy.igloo.ui.UiStateSwitch
import com.screwy.igloo.ui.component.DeleteDownloadedVideoDialog
import com.screwy.igloo.ui.component.VideoBinaryAction
import com.screwy.igloo.ui.component.VideoGrid
import com.screwy.igloo.ui.nav.IglooNavigationSource
import com.screwy.igloo.ui.nav.rememberIglooNavigator
import kotlinx.coroutines.launch
import org.koin.androidx.compose.koinViewModel
import org.koin.compose.koinInject

/**
 * YouTube long-form videos tab.
 * `videos` — vertically paged Room grid; pull-to-refresh triggers the YouTube sync stream.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun VideosRoute(
    navController: NavController,
    broadcastsVm: BroadcastsViewModel,
    modifier: Modifier = Modifier,
) {
    val vm: VideosViewModel = koinViewModel()
    val items by vm.items.collectAsStateWithLifecycle()
    val uiState by vm.uiState.collectAsStateWithLifecycle()
    val isRefreshing by vm.isRefreshing.collectAsStateWithLifecycle()
    val canLoadMore by vm.canLoadMore.collectAsStateWithLifecycle()
    val isLoadingMore by vm.isLoadingMore.collectAsStateWithLifecycle()
    val broadcasts by broadcastsVm.broadcasts.collectAsStateWithLifecycle()
    val live = broadcasts.filter { it.broadcast.liveStatus == "is_live" }
    val playback by broadcastsVm.activePlayback.collectAsStateWithLifecycle()
    val preparing by broadcastsVm.isPreparingPlayback.collectAsStateWithLifecycle()
    val offlineVideoActions: OfflineVideoActions = koinInject()
    val uiEffects: UiEffects = koinInject()
    val navigator = rememberIglooNavigator(navController)
    val scope = rememberCoroutineScope()
    var deleteVideoId by rememberSaveable { mutableStateOf<String?>(null) }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { broadcastsVm.refresh() }
    DisposableEffect(broadcastsVm) { onDispose { broadcastsVm.closePlayback() } }

    Box(modifier.fillMaxSize()) {
        UiStateSwitch(state = if (live.isNotEmpty()) com.screwy.igloo.ui.UiState.Data(Unit) else uiState) {
            PullToRefreshBox(
                isRefreshing = isRefreshing,
                onRefresh = { vm.refresh(); broadcastsVm.refresh() },
                modifier = Modifier.fillMaxSize(),
            ) {
                VideoGrid(
                    liveBroadcasts = live,
                    onBroadcastClick = broadcastsVm::playBroadcast,
                    items = items,
                    columns = 2,
                    onVideoClick = { videoId ->
                        navigator.openVideo(videoId, IglooNavigationSource.Videos)
                    },
                    onChannelClick = { channelId ->
                        navigator.openChannel(channelId, IglooNavigationSource.Videos)
                    },
                    onVideoLongClick = { videoId, action ->
                        when (action) {
                            VideoBinaryAction.Download -> {
                                scope.launch {
                                    offlineVideoActions.requestDownload(videoId)
                                    uiEffects.emit(UiEffect.ToastRes(R.string.status_video_download_queued))
                                }
                            }
                            VideoBinaryAction.Delete -> deleteVideoId = videoId
                        }
                    },
                    canLoadMore = canLoadMore && !isLoadingMore,
                    onLoadMore = vm::loadMore,
                    showScrollFabs = true,
                )
            }
        }
    }

    if (preparing) Dialog(onDismissRequest = broadcastsVm::closePlayback) {
        CircularProgressIndicator(color = MaterialTheme.iglooColors.primary)
    }
    playback?.let { BroadcastPlayer(it, broadcastsVm::closePlayback) }

    DeleteDownloadedVideoDialog(
        videoId = deleteVideoId,
        onDismiss = { deleteVideoId = null },
        onConfirm = { videoId ->
            deleteVideoId = null
            scope.launch { offlineVideoActions.removeDownload(videoId) }
        },
    )
}
