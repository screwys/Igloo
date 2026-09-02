package com.screwy.igloo.player

import androidx.media3.common.Player
import androidx.media3.session.CommandButton
import com.screwy.igloo.net.NetDefaults
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class IglooPlayerFactoryTest {

    @Test
    fun mediaSessionPrefersTenSecondBackAndForwardControls() {
        val buttons =
            playerMediaButtonPreferences(
                backLabel = "Back 10 seconds",
                forwardLabel = "Forward 10 seconds",
            )

        assertEquals(
            listOf(Player.COMMAND_SEEK_BACK, Player.COMMAND_SEEK_FORWARD),
            buttons.map { it.playerCommand },
        )
        assertEquals(
            listOf(CommandButton.ICON_SKIP_BACK_10, CommandButton.ICON_SKIP_FORWARD_10),
            buttons.map { it.icon },
        )
        assertTrue(buttons.all { it.displayName.isNotBlank() })
    }

    @Test
    fun iglooMediaRequest_getsBearerHeader() {
        val headers = iglooMediaRequestHeaders(
            url = "https://igloo.example.com/api/media/videos/one.mp4",
            iglooHost = "igloo.example.com",
            bearerToken = "token-123",
        )

        assertEquals("Bearer token-123", headers["Authorization"])
    }

    @Test
    fun publicMediaRequest_doesNotGetBearerHeader() {
        val headers = iglooMediaRequestHeaders(
            url = "https://video.twimg.com/ext_tw_video/clip.mp4",
            iglooHost = "igloo.example.com",
            bearerToken = "token-123",
        )

        assertTrue(headers.isEmpty())
    }

    @Test
    fun blankToken_doesNotGetBearerHeader() {
        val headers = iglooMediaRequestHeaders(
            url = "https://igloo.example.com/api/media/videos/one.mp4",
            iglooHost = "igloo.example.com",
            bearerToken = "   ",
        )

        assertTrue(headers.isEmpty())
    }

    @Test
    fun existingAuthorizationHeader_isPreserved() {
        val headers = iglooMediaRequestHeaders(
            url = "https://igloo.example.com/api/media/videos/one.mp4",
            iglooHost = "igloo.example.com",
            bearerToken = "token-123",
            existingHeaders = mapOf("authorization" to "Bearer caller-owned"),
        )

        assertTrue(headers.isEmpty())
    }

    @Test
    fun publicUserAgent_isBrowserShapedAndNotAppSpecific() {
        val userAgent = NetDefaults.PUBLIC_BROWSER_USER_AGENT

        assertTrue(userAgent.startsWith("Mozilla/5.0"))
        assertTrue(userAgent.contains("Firefox/140.0"))
        assertFalse(userAgent.contains("Igloo"))
        assertFalse(userAgent.contains("Android"))
    }
}
