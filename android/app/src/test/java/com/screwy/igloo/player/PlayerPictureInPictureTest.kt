package com.screwy.igloo.player

import androidx.media3.common.Player
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PlayerPictureInPictureTest {
    @Test
    fun autoEntryRequiresOptInPlaybackIntentAndAPlayableStream() {
        assertTrue(
            shouldAutoEnterMiniPlayer(
                preferenceEnabled = true,
                playWhenReady = true,
                playbackState = Player.STATE_READY,
                streamAvailable = true,
            )
        )
        assertFalse(
            shouldAutoEnterMiniPlayer(
                preferenceEnabled = false,
                playWhenReady = true,
                playbackState = Player.STATE_READY,
                streamAvailable = true,
            )
        )
        assertFalse(
            shouldAutoEnterMiniPlayer(
                preferenceEnabled = true,
                playWhenReady = false,
                playbackState = Player.STATE_READY,
                streamAvailable = true,
            )
        )
        assertFalse(
            shouldAutoEnterMiniPlayer(
                preferenceEnabled = true,
                playWhenReady = true,
                playbackState = Player.STATE_READY,
                streamAvailable = false,
            )
        )
    }

    @Test
    fun bufferingCanEnterButIdleAndEndedCannot() {
        assertTrue(
            shouldAutoEnterMiniPlayer(
                preferenceEnabled = true,
                playWhenReady = true,
                playbackState = Player.STATE_BUFFERING,
                streamAvailable = true,
            )
        )
        assertFalse(
            shouldAutoEnterMiniPlayer(
                preferenceEnabled = true,
                playWhenReady = true,
                playbackState = Player.STATE_IDLE,
                streamAvailable = true,
            )
        )
        assertFalse(
            shouldAutoEnterMiniPlayer(
                preferenceEnabled = true,
                playWhenReady = true,
                playbackState = Player.STATE_ENDED,
                streamAvailable = true,
            )
        )
    }
}
