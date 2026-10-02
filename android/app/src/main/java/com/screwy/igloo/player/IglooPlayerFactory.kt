package com.screwy.igloo.player

import android.content.Context
import android.os.Looper
import androidx.media3.common.AudioAttributes
import androidx.media3.common.C
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSource
import androidx.media3.datasource.DataSourceBitmapLoader
import androidx.media3.datasource.DefaultDataSource
import androidx.media3.datasource.DefaultHttpDataSource
import androidx.media3.datasource.ResolvingDataSource
import androidx.media3.exoplayer.DefaultLoadControl
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import androidx.media3.session.CommandButton
import androidx.media3.session.MediaSession
import com.screwy.igloo.R
import com.screwy.igloo.net.IglooHostProvider
import com.screwy.igloo.net.NetDefaults
import com.screwy.igloo.net.auth.AuthTokenProvider
import com.screwy.igloo.net.isIglooServerUrl

/**
 * Media3 player factory for local files, Igloo server media, and public CDN media.
 *
 * Public CDN requests use the same browser-shaped UA as the app HTTP stack and never
 * receive Igloo credentials. Bearer auth is added only when the full playback URL
 * resolves to the configured Igloo server host.
 */
fun buildIglooPlayer(
    context: Context,
    tokenProvider: AuthTokenProvider,
    hostProvider: IglooHostProvider,
    bufferDurations: PlaybackBufferDurations = PlaybackBufferDurations(),
    applicationLooper: Looper = Looper.getMainLooper(),
): ExoPlayer = buildIglooPlayer(
    context = context,
    tokenProvider = tokenProvider,
    iglooHostResolver = hostProvider::hostSync,
    bufferDurations = bufferDurations,
    applicationLooper = applicationLooper,
)

@androidx.annotation.OptIn(markerClass = [UnstableApi::class])
internal fun buildIglooPlayer(
    context: Context,
    tokenProvider: AuthTokenProvider,
    iglooHostResolver: () -> String,
    bufferDurations: PlaybackBufferDurations = PlaybackBufferDurations(),
    applicationLooper: Looper = Looper.getMainLooper(),
): ExoPlayer {
    val dataSourceFactory = buildIglooDataSourceFactory(context, tokenProvider, iglooHostResolver)
    val loadControl = DefaultLoadControl.Builder()
        .setBufferDurationsMsForLocalPlayback(
            /* minBufferMs = */ 1_500,
            /* maxBufferMs = */ 12_000,
            /* bufferForPlaybackMs = */ 100,
            /* bufferForPlaybackAfterRebufferMs = */ 250,
        )
        .setPrioritizeTimeOverSizeThresholdsForLocalPlayback(false)
        .setBufferDurationsMsForStreaming(
            /* minBufferMs = */ bufferDurations.aheadMs,
            /* maxBufferMs = */ bufferDurations.aheadMs,
            /* bufferForPlaybackMs = */ bufferDurations.startupMs,
            /* bufferForPlaybackAfterRebufferMs = */ bufferDurations.refillMs,
        )
        .setPrioritizeTimeOverSizeThresholdsForStreaming(true)
        .build()
    return ExoPlayer.Builder(context)
        .setLooper(applicationLooper)
        .setLoadControl(loadControl)
        .setMediaSourceFactory(DefaultMediaSourceFactory(dataSourceFactory))
        .setAudioAttributes(
            AudioAttributes.Builder()
                .setUsage(C.USAGE_MEDIA)
                .setContentType(C.AUDIO_CONTENT_TYPE_MOVIE)
                .build(),
            true,
        )
        .setHandleAudioBecomingNoisy(true)
        .setSeekBackIncrementMs(PLAYER_SEEK_INCREMENT_MS)
        .setSeekForwardIncrementMs(PLAYER_SEEK_INCREMENT_MS)
        .build()
}

@androidx.annotation.OptIn(markerClass = [UnstableApi::class])
internal fun copyIglooPlaybackState(previous: ExoPlayer, replacement: ExoPlayer) {
    val playWhenReady = previous.playWhenReady
    replacement.playbackParameters = previous.playbackParameters
    replacement.volume = previous.volume
    replacement.repeatMode = previous.repeatMode
    replacement.shuffleModeEnabled = previous.shuffleModeEnabled
    replacement.trackSelectionParameters = previous.trackSelectionParameters
    replacement.videoScalingMode = previous.videoScalingMode
    replacement.pauseAtEndOfMediaItems = previous.pauseAtEndOfMediaItems
    if (previous.mediaItemCount > 0) {
        replacement.setMediaItems(
            (0 until previous.mediaItemCount).map(previous::getMediaItemAt),
            previous.currentMediaItemIndex,
            previous.currentPosition,
        )
        if (previous.playbackState != Player.STATE_IDLE) replacement.prepare()
    }
    previous.pause()
    replacement.playWhenReady = playWhenReady
}

@androidx.annotation.OptIn(markerClass = [UnstableApi::class])
private fun buildIglooDataSourceFactory(
    context: Context,
    tokenProvider: AuthTokenProvider,
    iglooHostResolver: () -> String,
): DataSource.Factory {
    val httpFactory = DefaultHttpDataSource.Factory()
        .setUserAgent(NetDefaults.PUBLIC_BROWSER_USER_AGENT)
    val resolvingHttpFactory = ResolvingDataSource.Factory(httpFactory) { dataSpec ->
        val authHeaders = iglooMediaRequestHeaders(
            url = dataSpec.uri.toString(),
            iglooHost = iglooHostResolver(),
            bearerToken = tokenProvider.bearerTokenSync(),
            existingHeaders = dataSpec.httpRequestHeaders,
        )
        if (authHeaders.isEmpty()) dataSpec else dataSpec.withAdditionalHeaders(authHeaders)
    }
    return DefaultDataSource.Factory(context, resolvingHttpFactory)
}

@androidx.annotation.OptIn(markerClass = [UnstableApi::class])
internal fun buildIglooMediaSession(
    context: Context,
    player: Player,
    tokenProvider: AuthTokenProvider,
    hostProvider: IglooHostProvider,
): MediaSession =
    MediaSession.Builder(context, player)
        .setBitmapLoader(
            DataSourceBitmapLoader.Builder(context)
                .setDataSourceFactory(buildIglooDataSourceFactory(context, tokenProvider, hostProvider::hostSync))
                .build()
        )
        .setMediaButtonPreferences(
            playerMediaButtonPreferences(
                backLabel = context.getString(R.string.player_back_10_seconds),
                forwardLabel = context.getString(R.string.player_forward_10_seconds),
            )
        )
        .build()

@androidx.annotation.OptIn(markerClass = [UnstableApi::class])
internal fun playerMediaButtonPreferences(
    backLabel: String,
    forwardLabel: String,
): List<CommandButton> =
    listOf(
        CommandButton.Builder(CommandButton.ICON_SKIP_BACK_10)
            .setPlayerCommand(Player.COMMAND_SEEK_BACK)
            .setDisplayName(backLabel)
            .setSlots(CommandButton.SLOT_BACK)
            .build(),
        CommandButton.Builder(CommandButton.ICON_SKIP_FORWARD_10)
            .setPlayerCommand(Player.COMMAND_SEEK_FORWARD)
            .setDisplayName(forwardLabel)
            .setSlots(CommandButton.SLOT_FORWARD)
            .build(),
    )

internal fun iglooMediaRequestHeaders(
    url: String,
    iglooHost: String,
    bearerToken: String?,
    existingHeaders: Map<String, String> = emptyMap(),
): Map<String, String> {
    val token = bearerToken?.takeIf { it.isNotBlank() } ?: return emptyMap()
    if (!isIglooServerUrl(url, iglooHost)) return emptyMap()
    if (existingHeaders.keys.any { it.equals("Authorization", ignoreCase = true) }) return emptyMap()
    return mapOf("Authorization" to "Bearer $token")
}
