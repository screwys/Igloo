package com.screwy.igloo.ui.nav

import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import android.content.pm.ActivityInfo
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.ui.platform.LocalContext

@Composable
internal fun ApplyRouteOrientation(route: String?, layout: IglooAdaptiveLayout) {
    val activity = LocalContext.current.findActivity()
    val requestedOrientation = routeRequestedOrientation(route, layout.isWide)
    DisposableEffect(activity, requestedOrientation) {
        if (activity != null && requestedOrientation != null) {
            activity.requestedOrientation = requestedOrientation
        }
        onDispose { }
    }
}

internal fun routeRequestedOrientation(route: String?, wideLayout: Boolean): Int? =
    when {
        route == RouteRegistry.Player.route -> null
        wideLayout -> ActivityInfo.SCREEN_ORIENTATION_FULL_USER
        else -> ActivityInfo.SCREEN_ORIENTATION_PORTRAIT
    }

private tailrec fun Context.findActivity(): Activity? = when (this) {
    is Activity -> this
    is ContextWrapper -> baseContext.findActivity()
    else -> null
}
