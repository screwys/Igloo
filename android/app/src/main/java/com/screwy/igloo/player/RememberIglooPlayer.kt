package com.screwy.igloo.player

import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.common.util.UnstableApi
import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.net.IglooHostProvider
import com.screwy.igloo.net.auth.AuthTokenProvider
import org.koin.compose.koinInject

@Composable
@androidx.annotation.OptIn(markerClass = [UnstableApi::class])
internal fun rememberIglooPlayer(): ExoPlayer? {
    val context = LocalContext.current
    val prefs: PreferencesRepo = koinInject()
    val authTokens: AuthTokenProvider = koinInject()
    val hostProvider: IglooHostProvider = koinInject()
    val buffering by remember(prefs) { prefs.playbackBuffering() }
        .collectAsStateWithLifecycle(initialValue = null)
    val durations = buffering?.durations ?: return null
    var player by remember(context, authTokens, hostProvider) {
        mutableStateOf(buildIglooPlayer(context, authTokens, hostProvider, durations))
    }
    var appliedDurations by remember(context, authTokens, hostProvider) {
        mutableStateOf(durations)
    }
    LaunchedEffect(durations) {
        if (appliedDurations != durations) {
            val previous = player
            val replacement = buildIglooPlayer(
                context, authTokens, hostProvider, durations, previous.applicationLooper,
            )
            copyIglooPlaybackState(previous, replacement)
            player = replacement
            appliedDurations = durations
            previous.release()
        }
    }
    DisposableEffect(context, authTokens, hostProvider) {
        onDispose { player.release() }
    }
    return player
}
