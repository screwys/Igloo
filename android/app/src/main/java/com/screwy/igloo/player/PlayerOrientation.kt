package com.screwy.igloo.player

import android.content.pm.ActivityInfo
import android.content.res.Configuration
import android.view.OrientationEventListener

internal enum class PlayerDevicePosture {
    Portrait,
    Landscape,
    Unknown,
}

internal fun playerDevicePostureForDegrees(degrees: Int): PlayerDevicePosture {
    if (degrees == OrientationEventListener.ORIENTATION_UNKNOWN) {
        return PlayerDevicePosture.Unknown
    }
    val normalized = ((degrees % 360) + 360) % 360
    return when (normalized) {
        in 315..359, in 0..44, in 135..224 -> PlayerDevicePosture.Portrait
        in 45..134, in 225..314 -> PlayerDevicePosture.Landscape
        else -> PlayerDevicePosture.Unknown
    }
}

internal fun shouldAutoEnterPlayerFullscreen(
    configurationOrientation: Int,
    autoFullscreenSuppressed: Boolean,
): Boolean =
    configurationOrientation == Configuration.ORIENTATION_LANDSCAPE && !autoFullscreenSuppressed

internal fun playerInlineRequestedOrientation(autoFullscreenSuppressed: Boolean): Int =
    if (autoFullscreenSuppressed) {
        ActivityInfo.SCREEN_ORIENTATION_PORTRAIT
    } else {
        ActivityInfo.SCREEN_ORIENTATION_FULL_SENSOR
    }
