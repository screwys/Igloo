package com.screwy.igloo.player

import android.content.pm.ActivityInfo

internal fun playerRequestedOrientation(fullscreen: Boolean, largeScreen: Boolean): Int =
    if (largeScreen) {
        ActivityInfo.SCREEN_ORIENTATION_FULL_USER
    } else if (fullscreen) {
        ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
    } else {
        ActivityInfo.SCREEN_ORIENTATION_PORTRAIT
    }
