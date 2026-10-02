package com.screwy.igloo.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Button
import androidx.compose.material3.FilterChip
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavController
import com.screwy.igloo.R
import com.screwy.igloo.player.PlaybackBufferDurations
import com.screwy.igloo.player.PlaybackBufferProfile
import com.screwy.igloo.player.PlaybackBuffering
import com.screwy.igloo.settings.components.SectionHeader
import com.screwy.igloo.settings.components.SectionDescription
import com.screwy.igloo.settings.components.SettingsSubScreen
import com.screwy.igloo.settings.components.SettingsSwitchRow
import com.screwy.igloo.ui.theme.iglooColors
import org.koin.androidx.compose.koinViewModel
import kotlin.math.roundToInt

private val speedOptions = listOf("0.5x", "0.75x", "1x", "1.25x", "1.5x", "2x")

@Composable
fun PlaybackRoute(
    navController: NavController,
    modifier: Modifier = Modifier,
) {
    val vm: PlaybackSettingsViewModel = koinViewModel()
    val autoplay by vm.autoplay.collectAsStateWithLifecycle()
    val muteDefault by vm.muteDefault.collectAsStateWithLifecycle()
    val speed by vm.playbackSpeedDefault.collectAsStateWithLifecycle()
    val miniPlayerAutoEnter by vm.miniPlayerAutoEnter.collectAsStateWithLifecycle()
    val buffering by vm.buffering.collectAsStateWithLifecycle()

    SettingsSubScreen(
        title = stringResource(R.string.settings_playback),
        onBack = { navController.popBackStack() },
        modifier = modifier,
    ) {
        SettingsSwitchRow(
            label = stringResource(R.string.settings_autoplay),
            checked = autoplay,
            onToggle = vm::setAutoplay,
        )
        SettingsSwitchRow(
            label = stringResource(R.string.settings_mute_by_default),
            checked = muteDefault,
            onToggle = vm::setMuteDefault,
        )
        SectionHeader(stringResource(R.string.mini_player_title))
        SettingsSwitchRow(
            label = stringResource(R.string.settings_mini_player_auto_enter),
            checked = miniPlayerAutoEnter,
            onToggle = vm::setMiniPlayerAutoEnter,
        )
        SectionDescription(stringResource(R.string.settings_mini_player_android_help))
        SectionHeader(stringResource(R.string.settings_default_speed))
        Row(
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            speedOptions.forEach { opt -> SpeedChip(opt, opt == speed, vm::setPlaybackSpeedDefault) }
        }
        buffering?.let { settings ->
            BufferingSettings(settings, vm::setBufferProfile, vm::setCustomBuffer)
        }
    }
}

@Composable
private fun BufferingSettings(
    buffering: PlaybackBuffering,
    onProfileChanged: (PlaybackBufferProfile) -> Unit,
    onCustomChanged: (PlaybackBufferDurations) -> Unit,
) {
    SectionHeader(stringResource(R.string.settings_buffering))
    Column(
        modifier = Modifier.padding(horizontal = 16.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            PlaybackBufferProfile.entries.forEach { profile ->
                val label = when (profile) {
                    PlaybackBufferProfile.Quick -> R.string.settings_buffering_quick
                    PlaybackBufferProfile.Balanced -> R.string.settings_buffering_balanced
                    PlaybackBufferProfile.Smooth -> R.string.settings_buffering_smooth
                    PlaybackBufferProfile.Custom -> R.string.settings_buffering_custom
                }
                FilterChip(
                    selected = buffering.profile == profile,
                    onClick = { onProfileChanged(profile) },
                    label = { Text(stringResource(label)) },
                )
            }
        }
        val durations = buffering.durations
        var startup by remember(buffering) { mutableStateOf(bufferSeconds(durations.startupMs)) }
        var refill by remember(buffering) { mutableStateOf(bufferSeconds(durations.refillMs)) }
        var ahead by remember(buffering) { mutableStateOf(bufferSeconds(durations.aheadMs)) }
        val custom = buffering.profile == PlaybackBufferProfile.Custom
        val startupMs = bufferMilliseconds(startup)
        val refillMs = bufferMilliseconds(refill)
        val aheadMs = bufferMilliseconds(ahead)
        val editedDurations = if (startupMs != null && refillMs != null && aheadMs != null) {
            PlaybackBufferDurations(startupMs, refillMs, aheadMs).takeIf { it.isValid }
        } else null
        OutlinedTextField(
            value = startup,
            onValueChange = { startup = it },
            label = { Text(stringResource(R.string.settings_buffering_startup_seconds)) },
            enabled = custom,
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
            isError = custom && startupMs == null,
            modifier = Modifier.fillMaxWidth(),
        )
        OutlinedTextField(
            value = refill,
            onValueChange = { refill = it },
            label = { Text(stringResource(R.string.settings_buffering_refill_seconds)) },
            enabled = custom,
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
            isError = custom && refillMs == null,
            modifier = Modifier.fillMaxWidth(),
        )
        OutlinedTextField(
            value = ahead,
            onValueChange = { ahead = it },
            label = { Text(stringResource(R.string.settings_buffering_ahead_seconds)) },
            enabled = custom,
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
            isError = custom && editedDurations == null,
            modifier = Modifier.fillMaxWidth(),
        )
        if (custom) {
            if (editedDurations == null) {
                Text(
                    stringResource(R.string.settings_buffering_invalid),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                )
            }
            Button(
                onClick = { editedDurations?.let(onCustomChanged) },
                enabled = editedDurations != null && editedDurations != buffering.custom,
            ) {
                Text(stringResource(R.string.action_save))
            }
        }
    }
}

private fun bufferSeconds(milliseconds: Int): String =
    if (milliseconds % 1000 == 0) (milliseconds / 1000).toString()
    else (milliseconds / 1000.0).toString()

private fun bufferMilliseconds(seconds: String): Int? =
    seconds.replace(',', '.').toDoubleOrNull()
        ?.takeIf { it.isFinite() && it >= 0 && it <= Int.MAX_VALUE / 1000.0 }
        ?.let { (it * 1000).roundToInt() }

@Composable
private fun SpeedChip(label: String, selected: Boolean, onSelect: (String) -> Unit) {
    val colors = MaterialTheme.iglooColors
    Box(
        modifier = Modifier
            .clip(RoundedCornerShape(8.dp))
            .background(if (selected) colors.surfaceElevated else Color.Transparent)
            .border(
                width = 1.dp,
                color = if (selected) colors.surfaceElevated else colors.borderSubtle,
                shape = RoundedCornerShape(8.dp),
            )
            .clickable { onSelect(label) }
            .padding(horizontal = 14.dp, vertical = 8.dp),
    ) {
        Text(
            text = label,
            style = MaterialTheme.typography.bodyMedium,
            color = if (selected) colors.onSurface else colors.onSurfaceMuted,
        )
    }
}
