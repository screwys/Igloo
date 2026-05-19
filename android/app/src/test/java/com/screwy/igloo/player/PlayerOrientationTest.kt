package com.screwy.igloo.player

import android.content.pm.ActivityInfo
import org.junit.Assert.assertEquals
import org.junit.Test

class PlayerOrientationTest {

    @Test
    fun compact_inline_player_requests_portrait() {
        assertEquals(
            ActivityInfo.SCREEN_ORIENTATION_PORTRAIT,
            playerRequestedOrientation(fullscreen = false, largeScreen = false),
        )
    }

    @Test
    fun compact_fullscreen_player_requests_landscape() {
        assertEquals(
            ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE,
            playerRequestedOrientation(fullscreen = true, largeScreen = false),
        )
    }

    @Test
    fun large_screen_player_respects_user_orientation() {
        assertEquals(
            ActivityInfo.SCREEN_ORIENTATION_FULL_USER,
            playerRequestedOrientation(fullscreen = false, largeScreen = true),
        )
        assertEquals(
            ActivityInfo.SCREEN_ORIENTATION_FULL_USER,
            playerRequestedOrientation(fullscreen = true, largeScreen = true),
        )
    }
}
