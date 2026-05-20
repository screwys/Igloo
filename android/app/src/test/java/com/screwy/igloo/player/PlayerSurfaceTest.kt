package com.screwy.igloo.player

import android.content.Context
import androidx.media3.ui.PlayerView
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class PlayerSurfaceTest {
    @Test
    fun player_view_tap_handler_invokes_latest_callback() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val view = PlayerView(context)
        var tapScore = 0

        setPlayerViewTapHandler(view) { tapScore += 1 }
        assertTrue(view.performClick())
        assertEquals(1, tapScore)

        setPlayerViewTapHandler(view) { tapScore += 10 }
        assertTrue(view.performClick())
        assertEquals(11, tapScore)
    }
}
