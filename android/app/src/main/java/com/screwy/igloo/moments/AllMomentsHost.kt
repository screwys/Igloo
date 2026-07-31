package com.screwy.igloo.moments

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavController
import com.screwy.igloo.ui.UiStateSwitch
import com.screwy.igloo.ui.nav.IglooNavigationSource
import com.screwy.igloo.ui.nav.rememberIglooNavigator
import org.koin.androidx.compose.koinViewModel

/**
 * Hosts [AllMomentsRoute] against the nav-graph-scoped [MomentsViewModel]. Kept
 * separate from `AllMomentsRoute` so the composable stays pure (testable with a
 * canned items list) while this host owns the wiring concerns.
 *
 * A grid tap opens the standalone player with the selected video in its route.
 * That route owns recording the explicit selection after its Room playlist loads.
 */
@Composable
fun AllMomentsHost(
    navController: NavController,
    modifier: Modifier = Modifier,
) {
    val backStackEntry = rememberMomentsGraphBackStackEntry(navController) ?: return
    val vm: MomentsViewModel = koinViewModel(viewModelStoreOwner = backStackEntry)

    val routeState by vm.gridRouteState.collectAsStateWithLifecycle()
    val navigator = rememberIglooNavigator(navController)

    UiStateSwitch(state = routeState.uiState, modifier = modifier) {
        AllMomentsRoute(
            items = routeState.items,
            initialIndex = routeState.startIndex,
            onMomentClick = { videoId ->
                navigator.openShorts(
                    playlistType =
                        if (routeState.scope == "following") {
                            ShortsPlaylistType.Moments.routeValue
                        } else {
                            ShortsPlaylistType.AllMoments.routeValue
                        },
                    playlistId = ShortsPlaylistSpec.RootPlaylistId,
                    videoId = videoId,
                    source = IglooNavigationSource.AllMoments,
                )
            },
            onChannelClick = { cid ->
                navigator.openChannel(cid, IglooNavigationSource.AllMoments)
            },
            storyChannels = routeState.storyChannels,
            onStoryClick = { _, firstVideoId ->
                navigator.openShorts(
                    playlistType = ShortsPlaylistType.StoryTray.routeValue,
                    playlistId = ShortsPlaylistSpec.RootPlaylistId,
                    videoId = firstVideoId,
                    source = IglooNavigationSource.AllMoments,
                )
            },
            activeTab = routeState.scope,
            onTabSelected = vm::setActiveTab,
            onBack = { navController.popBackStack() },
        )
    }
}
