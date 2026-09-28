package com.screwy.igloo.player

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.screwy.igloo.data.entity.SponsorBlockSegmentEntity

internal class SponsorBlockPlaybackController(
    private val seekTo: (Long) -> Unit,
    private val skippedMessage: (String) -> String,
    private val nowMs: () -> Long = System::currentTimeMillis,
) {
    var skipSegment by mutableStateOf<SponsorBlockUiSegment?>(null)
        private set
    var autoSkipMessage by mutableStateOf<String?>(null)
        private set

    private var activeKey: String? = null
    private var lastSeekAtMs by mutableLongStateOf(0L)
    private var lastSeekPositionMs by mutableStateOf<Long?>(null)
    private var manualSegmentKey: String? = null

    fun reset(clearSeekHistory: Boolean = false) {
        skipSegment = null
        autoSkipMessage = null
        activeKey = null
        manualSegmentKey = null
        if (clearSeekHistory) {
            lastSeekAtMs = 0L
            lastSeekPositionMs = null
        }
    }

    fun onSeek(positionMs: Long) {
        lastSeekAtMs = nowMs()
        lastSeekPositionMs = positionMs
        manualSegmentKey = null
    }

    fun onTick(
        isPlaying: Boolean,
        positionMs: Long,
        segments: List<SponsorBlockUiSegment>,
    ) {
        if (!isPlaying || segments.isEmpty()) return
        val segment = segments.firstOrNull { seg ->
            positionMs >= seg.startMs && positionMs < seg.endMs - 300L
        }
        if (segment == null) {
            if (activeKey != null) {
                activeKey = null
                skipSegment = null
            }
            manualSegmentKey = null
            return
        }
        if (lastSeekPositionMs?.let { it >= segment.startMs && it < segment.endMs } == true) {
            manualSegmentKey = segment.key
        }
        if (segment.key == activeKey) return
        activeKey = segment.key
        when (segment.mode) {
            SponsorBlockModeAsk -> skipSegment = segment
            SponsorBlockModeSilent -> {
                val wasRecentSeek = nowMs() - lastSeekAtMs < 1_000L
                if (wasRecentSeek || manualSegmentKey == segment.key) {
                    skipSegment = segment
                } else {
                    seekTo(segment.endMs)
                    skipSegment = null
                    activeKey = null
                    autoSkipMessage = skippedMessage(segment.category)
                }
            }
        }
    }

    fun skip(segment: SponsorBlockUiSegment) {
        seekTo(segment.endMs)
        skipSegment = null
        activeKey = null
        autoSkipMessage = skippedMessage(segment.category)
    }

    fun clearAutoSkipMessage() {
        autoSkipMessage = null
    }
}

internal data class SponsorBlockPlaybackState(
    val visibleSegments: List<SponsorBlockSegmentEntity>,
    val skipSegment: SponsorBlockUiSegment?,
    val autoSkipMessage: String?,
    val onSkip: (SponsorBlockUiSegment) -> Unit,
)
