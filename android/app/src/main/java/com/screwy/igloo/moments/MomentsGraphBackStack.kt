package com.screwy.igloo.moments

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.navigation.NavBackStackEntry
import androidx.navigation.NavController
import androidx.navigation.compose.currentBackStackEntryAsState
import com.screwy.igloo.ui.nav.RouteRegistry

@Composable
internal fun rememberMomentsGraphBackStackEntry(navController: NavController): NavBackStackEntry? {
    val currentEntry by navController.currentBackStackEntryAsState()
    val currentRoute = currentEntry?.destination?.route
    if (!isMomentsGraphContentRoute(currentRoute)) return null
    return remember(navController, currentEntry) {
        runCatching {
            navController.getBackStackEntry(RouteRegistry.MomentsGraphRoute)
        }.getOrNull()
    }
}

internal fun isMomentsGraphContentRoute(route: String?): Boolean =
    route == RouteRegistry.Moments.route ||
        route == RouteRegistry.AllMoments.route
