package com.screwy.igloo.settings

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavController
import com.screwy.igloo.R
import com.screwy.igloo.settings.components.SettingsSubScreen
import com.screwy.igloo.settings.components.SettingsSwitchRow
import org.koin.androidx.compose.koinViewModel

@Composable
fun FeedRoute(
    navController: NavController,
    modifier: Modifier = Modifier,
) {
    val settingsVm: FeedSettingsViewModel = koinViewModel()

    val includeReposts by settingsVm.includeReposts.collectAsStateWithLifecycle()
    val mediaOnly by settingsVm.mediaOnly.collectAsStateWithLifecycle()
    val showAccountRegion by settingsVm.showAccountRegion.collectAsStateWithLifecycle()
    val showCommunityNotes by settingsVm.showCommunityNotes.collectAsStateWithLifecycle()

    SettingsSubScreen(
        title = stringResource(R.string.nav_feed),
        onBack = { navController.popBackStack() },
        modifier = modifier,
    ) {
        SettingsSwitchRow(
            label = stringResource(R.string.settings_feed_include_reposts),
            checked = includeReposts,
            onToggle = settingsVm::setIncludeReposts,
        )
        SettingsSwitchRow(
            label = stringResource(R.string.settings_media_only_x),
            checked = mediaOnly,
            onToggle = settingsVm::setMediaOnly,
        )

        SettingsSwitchRow(
            label = stringResource(R.string.settings_x_account_region),
            checked = showAccountRegion,
            onToggle = settingsVm::setShowAccountRegion,
        )

        SettingsSwitchRow(
            label = stringResource(R.string.settings_x_community_notes),
            checked = showCommunityNotes,
            onToggle = settingsVm::setShowCommunityNotes,
        )

    }
}
