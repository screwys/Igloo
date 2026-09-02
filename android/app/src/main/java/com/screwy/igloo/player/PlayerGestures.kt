package com.screwy.igloo.player

import android.app.Activity
import android.content.Context
import android.media.AudioManager
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.detectDragGestures
import androidx.compose.foundation.gestures.waitForUpOrCancellation
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.unit.dp
import androidx.media3.exoplayer.ExoPlayer
import kotlin.math.abs
import kotlin.math.roundToInt
import kotlinx.coroutines.withTimeoutOrNull

/**
 * Gesture layer for the player surface.
 *
 * Owns the pointer-input pipeline that translates touches into ExoPlayer calls:
 *  - double-tap L/R halves → 10s skip back/forward.
 *  - vertical drag L/R halves → brightness / volume.
 *  - horizontal drag → scrubber seek with preview tooltip, committed on release.
 *  - long-press → temporary 2× speed, restored on release.
 *
 * Pure math lives in [skipBackwardMs], [skipForwardMs], [seekFromHorizontalDrag]
 * so the tests don't need to instantiate ExoPlayer or Compose.
 */
/** Skip-back clamp at zero. */
internal fun skipBackwardMs(currentMs: Long): Long =
    (currentMs - PLAYER_SEEK_INCREMENT_MS).coerceAtLeast(0L)

/**
 * Skip-forward clamp at duration. If [currentMs] already exceeds [durationMs]
 * (defensive: racy updates on a just-swapped video), the clamp still returns
 * `currentMs` rather than reeling the position backwards.
 */
internal fun skipForwardMs(currentMs: Long, durationMs: Long): Long =
    (currentMs + PLAYER_SEEK_INCREMENT_MS).coerceAtMost(durationMs.coerceAtLeast(currentMs))

/**
 * Maps horizontal movement to a seek offset at a fixed physical sensitivity.
 * This keeps the same gesture useful for both short and long videos instead of
 * making one full-width swipe jump across the entire duration.
 */
internal fun seekFromHorizontalDrag(
    currentMs: Long,
    dragPx: Float,
    pixelsPerSecond: Float,
    durationMs: Long,
): Long {
    if (pixelsPerSecond <= 0f || durationMs <= 0L) return currentMs
    val delta = (dragPx / pixelsPerSecond) * 1_000.0
    val target = currentMs + delta.toLong()
    return target.coerceIn(0L, durationMs)
}

internal fun adjustedLevelFromVerticalDrag(
    startLevel: Float,
    dragPx: Float,
    heightPx: Float,
): Float {
    if (heightPx <= 0f) return startLevel.coerceIn(0f, 1f)
    val effectiveHeight = (heightPx * VERTICAL_LEVEL_DRAG_FRACTION).coerceAtLeast(1f)
    return (startLevel - (dragPx / effectiveHeight)).coerceIn(0f, 1f)
}

internal fun volumeIndexForFraction(fraction: Float, maxVolume: Int): Int {
    val max = maxVolume.coerceAtLeast(1)
    return (fraction.coerceIn(0f, 1f) * max).roundToInt().coerceIn(0, max)
}

internal enum class PlayerSeekDirection {
    Back,
    Forward,
}

internal enum class PlayerDragMode {
    Scrub,
    Brightness,
    Volume,
}

internal fun playerDragMode(
    start: Offset,
    totalDrag: Offset,
    surfaceWidthPx: Float,
    surfaceHeightPx: Float,
    horizontalExclusionPx: Float,
    verticalExclusionPx: Float,
): PlayerDragMode? {
    if (surfaceWidthPx <= 0f || surfaceHeightPx <= 0f) return null
    if (start.y < verticalExclusionPx || start.y > surfaceHeightPx - verticalExclusionPx) {
        return null
    }

    val horizontal = abs(totalDrag.x)
    val vertical = abs(totalDrag.y)
    return when {
        horizontal > vertical * DIRECTION_DOMINANCE -> {
            if (start.x < horizontalExclusionPx ||
                start.x > surfaceWidthPx - horizontalExclusionPx
            ) {
                null
            } else {
                PlayerDragMode.Scrub
            }
        }
        vertical > horizontal * DIRECTION_DOMINANCE -> {
            if (start.x < surfaceWidthPx / 2f) PlayerDragMode.Brightness
            else PlayerDragMode.Volume
        }
        else -> null
    }
}

internal const val PLAYER_SEEK_INCREMENT_MS: Long = 10_000L
private const val BOOST_SPEED: Float = 2.0f
private const val MIN_BRIGHTNESS: Float = 0.05f
private const val VERTICAL_LEVEL_DRAG_FRACTION: Float = 0.62f
private const val DIRECTION_DOMINANCE: Float = 2f
private val VERTICAL_SWIPE_EXCLUSION = 64.dp
private val HORIZONTAL_SWIPE_EXCLUSION = 48.dp
private val HORIZONTAL_SCRUB_DISTANCE_PER_SECOND = 8.dp

/**
 * Gesture overlay — sits above the video surface and below the transport
 * controls. Double-tap skips, long-press speed-boosts, horizontal drag scrubs
 * (commit-on-release), vertical drag adjusts brightness/volume.
 */
@Composable
internal fun PlayerGestures(
    player: ExoPlayer,
    modifier: Modifier = Modifier,
    onTap: () -> Unit = {},
    onScrubStart: () -> Unit = {},
    onScrubUpdate: (targetMs: Long) -> Unit = { _ -> },
    onScrubEnd: (targetMs: Long) -> Unit = { _ -> },
    onScrubCancel: () -> Unit = {},
    onSeek: (PlayerSeekDirection) -> Unit = {},
    onBrightnessChange: (level: Float) -> Unit = { _ -> },
    onVolumeChange: (level: Float) -> Unit = { _ -> },
    swipeGesturesEnabled: Boolean = true,
) {
    val currentOnTap by rememberUpdatedState(onTap)
    val currentOnScrubStart by rememberUpdatedState(onScrubStart)
    val currentOnScrubUpdate by rememberUpdatedState(onScrubUpdate)
    val currentOnScrubEnd by rememberUpdatedState(onScrubEnd)
    val currentOnScrubCancel by rememberUpdatedState(onScrubCancel)
    val currentOnSeek by rememberUpdatedState(onSeek)
    val currentOnBrightnessChange by rememberUpdatedState(onBrightnessChange)
    val currentOnVolumeChange by rememberUpdatedState(onVolumeChange)
    val context = LocalContext.current
    val density = LocalDensity.current
    val activity = context.findActivity()
    val audioManager = remember(context) {
        context.getSystemService(Context.AUDIO_SERVICE) as? AudioManager
    }
    val surfaceWidthPx = remember { mutableStateOf(0f) }
    val surfaceHeightPx = remember { mutableStateOf(0f) }
    val scrubTarget = remember { mutableStateOf(0L) }
    val scrubbing = remember { mutableStateOf(false) }
    val dragMode = remember { mutableStateOf<PlayerDragMode?>(null) }
    val dragStart = remember { mutableStateOf(Offset.Zero) }
    val totalDrag = remember { mutableStateOf(Offset.Zero) }
    val verticalDragPx = remember { mutableFloatStateOf(0f) }
    val verticalStartLevel = remember { mutableFloatStateOf(0f) }
    val horizontalExclusionPx = with(density) { HORIZONTAL_SWIPE_EXCLUSION.toPx() }
    val verticalExclusionPx = with(density) { VERTICAL_SWIPE_EXCLUSION.toPx() }
    val horizontalScrubPixelsPerSecond =
        with(density) { HORIZONTAL_SCRUB_DISTANCE_PER_SECOND.toPx() }

    val swipeModifier =
        if (swipeGesturesEnabled) {
            Modifier.pointerInput(player, horizontalExclusionPx, verticalExclusionPx) {
                detectDragGestures(
                    onDragStart = { start ->
                        scrubTarget.value = player.currentPosition
                        scrubbing.value = false
                        dragMode.value = null
                        dragStart.value = start
                        totalDrag.value = Offset.Zero
                        verticalDragPx.floatValue = 0f
                        verticalStartLevel.floatValue = 0f
                    },
                    onDrag = { change, dragAmount ->
                        totalDrag.value += dragAmount
                        val mode =
                            dragMode.value
                                ?: playerDragMode(
                                        start = dragStart.value,
                                        totalDrag = totalDrag.value,
                                        surfaceWidthPx = surfaceWidthPx.value,
                                        surfaceHeightPx = surfaceHeightPx.value,
                                        horizontalExclusionPx = horizontalExclusionPx,
                                        verticalExclusionPx = verticalExclusionPx,
                                    )
                                    ?.also { dragMode.value = it }
                        if (mode == null) return@detectDragGestures

                        change.consume()
                        when (mode) {
                            PlayerDragMode.Scrub -> {
                                if (!scrubbing.value) {
                                    scrubbing.value = true
                                    currentOnScrubStart()
                                }
                                val duration = player.duration.coerceAtLeast(0L)
                                scrubTarget.value =
                                    seekFromHorizontalDrag(
                                        currentMs = scrubTarget.value,
                                        dragPx = dragAmount.x,
                                        pixelsPerSecond = horizontalScrubPixelsPerSecond,
                                        durationMs = duration,
                                    )
                                currentOnScrubUpdate(scrubTarget.value)
                            }
                            PlayerDragMode.Brightness -> {
                                if (verticalDragPx.floatValue == 0f) {
                                    verticalStartLevel.floatValue = readBrightness(activity)
                                }
                                verticalDragPx.floatValue += dragAmount.y
                                val level =
                                    adjustedLevelFromVerticalDrag(
                                            startLevel = verticalStartLevel.floatValue,
                                            dragPx = verticalDragPx.floatValue,
                                            heightPx = surfaceHeightPx.value,
                                        )
                                        .coerceAtLeast(MIN_BRIGHTNESS)
                                setBrightness(activity, level)?.let(currentOnBrightnessChange)
                            }
                            PlayerDragMode.Volume -> {
                                if (verticalDragPx.floatValue == 0f) {
                                    verticalStartLevel.floatValue = readVolumeFraction(audioManager)
                                }
                                verticalDragPx.floatValue += dragAmount.y
                                val level =
                                    adjustedLevelFromVerticalDrag(
                                        startLevel = verticalStartLevel.floatValue,
                                        dragPx = verticalDragPx.floatValue,
                                        heightPx = surfaceHeightPx.value,
                                    )
                                setVolumeFraction(audioManager, level)?.let(currentOnVolumeChange)
                            }
                        }
                    },
                    onDragEnd = {
                        if (scrubbing.value) {
                            player.seekTo(scrubTarget.value)
                            currentOnScrubEnd(scrubTarget.value)
                            scrubbing.value = false
                        }
                        dragMode.value = null
                        totalDrag.value = Offset.Zero
                        verticalDragPx.floatValue = 0f
                    },
                    onDragCancel = {
                        if (scrubbing.value) currentOnScrubCancel()
                        scrubbing.value = false
                        dragMode.value = null
                        totalDrag.value = Offset.Zero
                        verticalDragPx.floatValue = 0f
                    },
                )
            }
        } else {
            Modifier
        }

    Box(
        modifier = modifier
            .fillMaxSize()
            .onSizeChanged {
                surfaceWidthPx.value = it.width.toFloat()
                surfaceHeightPx.value = it.height.toFloat()
            }
            .pointerInput(player) {
                awaitEachGesture {
                    awaitFirstDown(requireUnconsumed = false)
                    var completedBeforeLongPress = false
                    val firstUp = withTimeoutOrNull(viewConfiguration.longPressTimeoutMillis) {
                        waitForUpOrCancellation().also {
                            completedBeforeLongPress = true
                        }
                    }
                    if (!completedBeforeLongPress) {
                        val speedBeforeBoost = player.playbackParameters.speed
                        player.setPlaybackSpeed(BOOST_SPEED)
                        waitForUpOrCancellation()
                        if (player.playbackParameters.speed != speedBeforeBoost) {
                            player.setPlaybackSpeed(speedBeforeBoost)
                        }
                        return@awaitEachGesture
                    }
                    if (firstUp == null) return@awaitEachGesture

                    val secondDown = withTimeoutOrNull(viewConfiguration.doubleTapTimeoutMillis) {
                        awaitFirstDown(requireUnconsumed = false)
                    }
                    if (secondDown == null) {
                        currentOnTap()
                        return@awaitEachGesture
                    }

                    val secondUp = waitForUpOrCancellation()
                    if (secondUp != null) {
                        val widthPx = surfaceWidthPx.value
                        val isLeft = widthPx > 0f && secondDown.position.x < widthPx / 2f
                        val current = player.currentPosition
                        val duration = player.duration.coerceAtLeast(0L)
                        val target = if (isLeft) skipBackwardMs(current)
                                     else skipForwardMs(current, duration)
                        player.seekTo(target)
                        currentOnSeek(
                            if (isLeft) PlayerSeekDirection.Back else PlayerSeekDirection.Forward
                        )
                    }
                }
            }
            .then(swipeModifier)
    )
}

private fun readBrightness(activity: Activity?): Float {
    val attrs = activity?.window?.attributes
    return if (attrs != null && attrs.screenBrightness > 0f) attrs.screenBrightness else 0.5f
}

private fun setBrightness(activity: Activity?, level: Float): Float? {
    val window = activity?.window ?: return null
    val attrs = window.attributes
    attrs.screenBrightness = level.coerceIn(MIN_BRIGHTNESS, 1f)
    window.attributes = attrs
    return attrs.screenBrightness
}

private fun readVolumeFraction(audioManager: AudioManager?): Float {
    val audio = audioManager ?: return 0.5f
    val maxVolume = audio.getStreamMaxVolume(AudioManager.STREAM_MUSIC).coerceAtLeast(1)
    return audio.getStreamVolume(AudioManager.STREAM_MUSIC).toFloat() / maxVolume.toFloat()
}

private fun setVolumeFraction(audioManager: AudioManager?, level: Float): Float? {
    val audio = audioManager ?: return null
    val maxVolume = audio.getStreamMaxVolume(AudioManager.STREAM_MUSIC).coerceAtLeast(1)
    val current = audio.getStreamVolume(AudioManager.STREAM_MUSIC)
    val target = volumeIndexForFraction(level, maxVolume)
    if (target != current) {
        audio.setStreamVolume(AudioManager.STREAM_MUSIC, target, 0)
    }
    return target.toFloat() / maxVolume.toFloat()
}

private tailrec fun Context.findActivity(): Activity? = when (this) {
    is Activity -> this
    is android.content.ContextWrapper -> baseContext.findActivity()
    else -> null
}
