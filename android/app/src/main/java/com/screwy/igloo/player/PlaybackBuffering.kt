package com.screwy.igloo.player

import kotlinx.serialization.Serializable

@Serializable
enum class PlaybackBufferProfile {
    Quick,
    Balanced,
    Smooth,
    Custom,
}

@Serializable
data class PlaybackBufferDurations(
    val startupMs: Int = 1_000,
    val refillMs: Int = 2_000,
    val aheadMs: Int = 30_000,
) {
    val isValid: Boolean
        get() = startupMs >= 0 && refillMs >= 0 && aheadMs > 0 &&
            aheadMs >= startupMs && aheadMs >= refillMs
}

@Serializable
data class PlaybackBuffering(
    val profile: PlaybackBufferProfile = PlaybackBufferProfile.Balanced,
    val custom: PlaybackBufferDurations = PlaybackBufferDurations(),
) {
    val durations: PlaybackBufferDurations
        get() = when (profile) {
            PlaybackBufferProfile.Quick -> PlaybackBufferDurations(250, 1_000, 12_000)
            PlaybackBufferProfile.Balanced -> PlaybackBufferDurations()
            PlaybackBufferProfile.Smooth -> PlaybackBufferDurations(3_000, 5_000, 60_000)
            PlaybackBufferProfile.Custom -> custom
        }
}
