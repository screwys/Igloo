package com.screwy.igloo.moments

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavController
import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.ui.UiStateSwitch
import com.screwy.igloo.ui.component.BookmarkSheet
import com.screwy.igloo.ui.component.MomentActionSheet
import com.screwy.igloo.ui.component.MomentsPlayer
import com.screwy.igloo.ui.component.sharePlainText
import com.screwy.igloo.ui.nav.IglooNavigationSource
import com.screwy.igloo.ui.nav.rememberIglooNavigator
import kotlinx.coroutines.launch
import org.koin.androidx.compose.koinViewModel
import org.koin.compose.koinInject

/**
 * Moments tab: TikTok-style vertical video pager.
 *
 * The player and grid are two presentations of one graph-owned session. Switching between them
 * never navigates or reconstructs the player playlist.
 */
@Composable
fun MomentsRoute(
    navController: NavController,
    modifier: Modifier = Modifier,
) {
    val backStackEntry = rememberMomentsGraphBackStackEntry(navController) ?: return
    val vm: MomentsViewModel = koinViewModel(viewModelStoreOwner = backStackEntry)

    val playerRouteState by vm.playerRouteState.collectAsStateWithLifecycle()
    val gridRouteState by vm.gridRouteState.collectAsStateWithLifecycle()
    val sessionVideoId by vm.sessionVideoId.collectAsStateWithLifecycle()
    val autoplayEnabled by vm.autoplayEnabled.collectAsStateWithLifecycle()
    val muted by vm.muted.collectAsStateWithLifecycle()
    val pendingBookmark by vm.pendingBookmark.collectAsStateWithLifecycle()
    val pendingMomentActions by vm.pendingMomentActions.collectAsStateWithLifecycle()
    val categories by vm.bookmarkCategories.collectAsStateWithLifecycle()
    val storyChannels by vm.storyChannels.collectAsStateWithLifecycle()
    val prefs: PreferencesRepo = koinInject()
    val useEmbedFriendlyShareLinks by prefs.shareEmbedFriendlyLinks()
        .collectAsStateWithLifecycle(initialValue = PreferencesRepo.Defaults.SHARE_EMBED_FRIENDLY_LINKS)
    var showStoryTray by remember { mutableStateOf(false) }
    var showAllMomentsGrid by remember { mutableStateOf(false) }
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val navigator = rememberIglooNavigator(navController)
    BackHandler(enabled = showAllMomentsGrid) { showAllMomentsGrid = false }

    Box(
        modifier = modifier
            .fillMaxSize()
            .background(Color.Black),
    ) {
        if (showAllMomentsGrid) {
            UiStateSwitch(state = gridRouteState.uiState, modifier = Modifier.fillMaxSize()) {
                AllMomentsRoute(
                    items = gridRouteState.items,
                    initialIndex = gridRouteState.startIndex,
                    onMomentClick = { videoId ->
                        if (vm.selectGridMoment(videoId)) showAllMomentsGrid = false
                    },
                    onChannelClick = { cid ->
                        navigator.openChannel(cid, IglooNavigationSource.Moments)
                    },
                    storyChannels = gridRouteState.storyChannels,
                    onStoryClick = { _, firstVideoId ->
                        navigator.openShorts(
                            playlistType = ShortsPlaylistType.StoryTray.routeValue,
                            playlistId = ShortsPlaylistSpec.RootPlaylistId,
                            videoId = firstVideoId,
                            source = IglooNavigationSource.Moments,
                        )
                    },
                    activeTab = gridRouteState.scope,
                    onTabSelected = vm::setActiveTab,
                    onBack = { showAllMomentsGrid = false },
                    modifier = Modifier.fillMaxSize(),
                )
            }
        } else {
            UiStateSwitch(state = playerRouteState.uiState, modifier = Modifier.fillMaxSize()) {
                MomentsPlayer(
                    items = playerRouteState.items,
                    startIndex = playerRouteState.selection.index,
                    startVideoId = sessionVideoId ?: playerRouteState.selection.videoId,
                    autoSwipeDefault = autoplayEnabled,
                    muteDefault = muted,
                    onAutoSwipeChanged = vm::setAutoplayEnabled,
                    onMuteChanged = vm::setMuted,
                    onIndexChange = vm::onIndexChange,
                    onViewEvent = vm::onViewEvent,
                    onChannelClick = { cid ->
                        navigator.openChannel(cid, IglooNavigationSource.Moments)
                    },
                    onStoryClick = { cid, firstVideoId ->
                        navigator.openShorts(
                            playlistType = ShortsPlaylistType.Story.routeValue,
                            playlistId = cid,
                            videoId = firstVideoId,
                            source = IglooNavigationSource.Moments,
                        )
                    },
                    onBookmarkToggle = vm::toggleBookmark,
                    onRequestBookmarkSheet = vm::requestBookmarkSheet,
                    onFollowChannel = vm::followChannel,
                    onUnfollowChannel = vm::unfollowChannel,
                    onRequestMomentActions = vm::requestMomentActions,
                    onShare = { item ->
                        scope.launch {
                            sharePlainText(context, item.canonicalUrl, useEmbedFriendlyShareLinks)
                        }
                    },
                    onMentionClick = vm::resolveMentionAndNavigate,
                    onSwipeLeftToChannel = { cid ->
                        navigator.openChannel(cid, IglooNavigationSource.Moments)
                    },
                    onOpenAllMomentsGrid = { showAllMomentsGrid = true },
                    onEndReached = vm::notifyUpToDate,
                    activeTab = playerRouteState.scope,
                    onTabSelected = { tab ->
                        if (tab == "stories") {
                            showStoryTray = true
                        } else {
                            vm.setActiveTab(tab)
                        }
                    },
                    chromeVisible = pendingMomentActions == null,
                )
            }
            StoryTray(
                visible = showStoryTray,
                rows = storyChannels.map { it.toStoryTrayItem() },
                onDismiss = { showStoryTray = false },
                onStoryClick = { _, firstVideoId ->
                    showStoryTray = false
                    navigator.openShorts(
                        playlistType = ShortsPlaylistType.StoryTray.routeValue,
                        playlistId = ShortsPlaylistSpec.RootPlaylistId,
                        videoId = firstVideoId,
                        source = IglooNavigationSource.Moments,
                    )
                },
                modifier = Modifier.align(Alignment.CenterEnd),
            )
        }
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
    pendingMomentActions?.let { item ->
        MomentActionSheet(
            item = item,
            onDismissRequest = vm::dismissMomentActions,
            onRepostsEnabledChanged = vm::setRepostsEnabled,
            onChannelMutedChanged = vm::setChannelMuted,
            onUnfollowChannel = vm::unfollowChannel,
            onShare = { item ->
                scope.launch {
                    sharePlainText(context, item.canonicalUrl, useEmbedFriendlyShareLinks)
                }
            },
            onVisitChannel = { cid ->
                navigator.openChannel(cid, IglooNavigationSource.Moments)
            },
        )
    }
}
