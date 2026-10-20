package com.screwy.igloo.player

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Check
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.media3.common.C
import androidx.media3.common.PlaybackParameters
import androidx.media3.common.Player
import androidx.media3.common.TrackSelectionOverride
import androidx.media3.common.TrackSelectionParameters
import androidx.media3.common.Tracks
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.DefaultTrackNameProvider
import com.screwy.igloo.R

private enum class SettingsPanel { Main, Speed, Audio, Subtitles, Quality }

@androidx.annotation.OptIn(markerClass = [UnstableApi::class])
@Composable
internal fun PlayerSettingsMenu(
    player: ExoPlayer,
    expanded: Boolean,
    onDismiss: () -> Unit,
    showSubtitles: Boolean,
    onToggleSubtitles: (() -> Unit)?,
) {
    var panel by remember(expanded) { mutableStateOf(SettingsPanel.Main) }
    var tracks by remember(player) { mutableStateOf(player.currentTracks) }
    var parameters by remember(player) { mutableStateOf(player.trackSelectionParameters) }
    var speed by remember(player) { mutableStateOf(player.playbackParameters.speed) }
    val resources = LocalContext.current.resources
    val names = remember(resources) { DefaultTrackNameProvider(resources) }
    val audio = tracks.groups.filter { it.type == C.TRACK_TYPE_AUDIO }
    val captions = tracks.groups.filter { it.type == C.TRACK_TYPE_TEXT }
    val video = tracks.groups.filter { it.type == C.TRACK_TYPE_VIDEO }

    DisposableEffect(player) {
        val listener = object : Player.Listener {
            override fun onTracksChanged(value: Tracks) { tracks = value }
            override fun onTrackSelectionParametersChanged(value: TrackSelectionParameters) { parameters = value }
            override fun onPlaybackParametersChanged(value: PlaybackParameters) { speed = value.speed }
        }
        player.addListener(listener)
        onDispose { player.removeListener(listener) }
    }

    fun setCaptions(visible: Boolean) {
        if (showSubtitles != visible) onToggleSubtitles?.invoke()
    }

    DropdownMenu(expanded = expanded, onDismissRequest = onDismiss) {
        if (panel == SettingsPanel.Main) {
            DropdownMenuItem(
                text = { Text(stringResource(R.string.player_playback_speed)) },
                trailingIcon = { Text("${speed}×") },
                onClick = { panel = SettingsPanel.Speed },
            )
            DropdownMenuItem(
                text = { Text(stringResource(R.string.player_audio)) },
                enabled = audio.any { group -> (0 until group.length).any(group::isTrackSupported) },
                onClick = { panel = SettingsPanel.Audio },
            )
            DropdownMenuItem(
                text = { Text(stringResource(R.string.player_subtitles)) },
                enabled = onToggleSubtitles != null || captions.any { group -> (0 until group.length).any(group::isTrackSupported) },
                onClick = { panel = SettingsPanel.Subtitles },
            )
            DropdownMenuItem(
                text = { Text(stringResource(R.string.player_quality)) },
                enabled = video.any { group -> (0 until group.length).any(group::isTrackSupported) },
                onClick = { panel = SettingsPanel.Quality },
            )
        } else {
            DropdownMenuItem(
                text = { Text(stringResource(when (panel) {
                    SettingsPanel.Speed -> R.string.player_playback_speed
                    SettingsPanel.Audio -> R.string.player_audio
                    SettingsPanel.Subtitles -> R.string.player_subtitles
                    else -> R.string.player_quality
                })) },
                leadingIcon = { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.action_back)) },
                onClick = { panel = SettingsPanel.Main },
            )
            if (panel == SettingsPanel.Speed) {
                SPEED_CHOICES.forEach { choice ->
                    PlayerSettingChoice("${choice}×", speed == choice) {
                        player.setPlaybackSpeed(choice)
                        onDismiss()
                    }
                }
            } else {
                val type = when (panel) {
                    SettingsPanel.Audio -> C.TRACK_TYPE_AUDIO
                    SettingsPanel.Subtitles -> C.TRACK_TYPE_TEXT
                    else -> C.TRACK_TYPE_VIDEO
                }
                val groups = when (panel) {
                    SettingsPanel.Audio -> audio
                    SettingsPanel.Subtitles -> captions
                    else -> video
                }
                val disabled = type in parameters.disabledTrackTypes
                val overridden = parameters.overrides.values.any { it.type == type }
                if (type == C.TRACK_TYPE_TEXT) {
                    PlayerSettingChoice(
                        stringResource(R.string.option_off),
                        !showSubtitles && groups.none { it.isSelected },
                    ) {
                        setCaptions(false)
                        player.trackSelectionParameters = parameters.buildUpon()
                            .clearOverridesOfType(type).setTrackTypeDisabled(type, true).build()
                        onDismiss()
                    }
                    if (onToggleSubtitles != null) {
                        PlayerSettingChoice(stringResource(R.string.player_subtitles), showSubtitles) {
                            player.trackSelectionParameters = parameters.buildUpon()
                                .clearOverridesOfType(type).setTrackTypeDisabled(type, true).build()
                            setCaptions(true)
                            onDismiss()
                        }
                    }
                } else {
                    PlayerSettingChoice(stringResource(R.string.player_quality_auto), !disabled && !overridden) {
                        player.trackSelectionParameters = parameters.buildUpon()
                            .clearOverridesOfType(type).setTrackTypeDisabled(type, false).build()
                        onDismiss()
                    }
                }
                groups.forEach { group ->
                    (0 until group.length).filter(group::isTrackSupported).forEach { index ->
                        PlayerSettingChoice(
                            names.getTrackName(group.getTrackFormat(index)),
                            !disabled && group.isTrackSelected(index) && (type == C.TRACK_TYPE_TEXT || overridden),
                        ) {
                            if (type == C.TRACK_TYPE_TEXT) setCaptions(false)
                            player.trackSelectionParameters = parameters.buildUpon()
                                .setTrackTypeDisabled(type, false)
                                .setOverrideForType(TrackSelectionOverride(group.mediaTrackGroup, index))
                                .build()
                            onDismiss()
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun PlayerSettingChoice(label: String, selected: Boolean, onClick: () -> Unit) {
    DropdownMenuItem(
        text = { Text(label) },
        trailingIcon = { if (selected) Icon(Icons.Default.Check, contentDescription = null) },
        onClick = onClick,
    )
}
