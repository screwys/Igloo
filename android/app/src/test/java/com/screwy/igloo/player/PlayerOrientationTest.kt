package com.screwy.igloo.player

import android.content.pm.ActivityInfo
import android.content.res.Configuration
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PlayerOrientationTest {

    @Test
    fun landscape_configuration_enters_fullscreen_when_auto_rotation_is_available() {
        assertTrue(
            shouldAutoEnterPlayerFullscreen(
                configurationOrientation = Configuration.ORIENTATION_LANDSCAPE,
                autoFullscreenSuppressed = false,
            ),
        )
    }

    @Test
    fun manual_fullscreen_exit_suppresses_landscape_reentry() {
        assertFalse(
            shouldAutoEnterPlayerFullscreen(
                configurationOrientation = Configuration.ORIENTATION_LANDSCAPE,
                autoFullscreenSuppressed = true,
            ),
        )
    }

    @Test
    fun manual_fullscreen_exit_requests_portrait_until_device_is_upright() {
        assertEquals(
            ActivityInfo.SCREEN_ORIENTATION_PORTRAIT,
            playerInlineRequestedOrientation(autoFullscreenSuppressed = true),
        )
        assertEquals(
            ActivityInfo.SCREEN_ORIENTATION_FULL_SENSOR,
            playerInlineRequestedOrientation(autoFullscreenSuppressed = false),
        )
    }

    @Test
    fun device_posture_maps_upright_and_reverse_portrait_as_portrait() {
        assertEquals(PlayerDevicePosture.Portrait, playerDevicePostureForDegrees(0))
        assertEquals(PlayerDevicePosture.Portrait, playerDevicePostureForDegrees(180))
    }

    @Test
    fun device_posture_maps_sideways_as_landscape() {
        assertEquals(PlayerDevicePosture.Landscape, playerDevicePostureForDegrees(90))
        assertEquals(PlayerDevicePosture.Landscape, playerDevicePostureForDegrees(270))
    }
}
