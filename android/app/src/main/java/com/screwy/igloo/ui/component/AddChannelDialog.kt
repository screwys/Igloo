package com.screwy.igloo.ui.component

import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.Alignment
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.foundation.background
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.screwy.igloo.R
import com.screwy.igloo.channel.followQueuedMessageRes
import com.screwy.igloo.net.Reachability
import com.screwy.igloo.outbox.OutboxKind
import com.screwy.igloo.outbox.OutboxWriter
import com.screwy.igloo.ui.UiEffect
import com.screwy.igloo.ui.UiEffects
import com.screwy.igloo.ui.theme.iglooColors
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import org.koin.compose.koinInject

@Composable
fun AddChannelDialog(onDismiss: () -> Unit) {
    val outboxWriter: OutboxWriter = koinInject()
    val reachability: Reachability = koinInject()
    val uiEffects: UiEffects = koinInject()
    val scope = rememberCoroutineScope()
    val colors = MaterialTheme.iglooColors
    var input by rememberSaveable { mutableStateOf("") }
    var saving by remember { mutableStateOf(false) }
    var failed by remember { mutableStateOf(false) }
    var mode by rememberSaveable { mutableStateOf("url") }
    var platform by rememberSaveable { mutableStateOf("twitter") }
    val modes = listOf("url" to R.string.channel_add_url, "handle" to R.string.channel_add_handle)
    val platforms = listOf(
        "twitter" to R.string.platform_x,
        "youtube" to R.string.platform_youtube,
        "tiktok" to R.string.platform_tiktok,
        "instagram" to R.string.platform_instagram,
    )

    fun submit() {
        val url = input.trim()
        if (saving || url.isEmpty()) return
        saving = true
        failed = false
        scope.launch {
            try {
                outboxWriter.enqueue(OutboxKind.Subscribe(url, if (mode == "handle") platform else ""))
                val state = reachability.state.value
                uiEffects.emit(UiEffect.ToastRes(
                    resId = followQueuedMessageRes(true, state),
                    longDuration = state !is Reachability.State.Online,
                ))
                onDismiss()
            } catch (e: Exception) {
                if (e is CancellationException) throw e
                failed = true
            } finally {
                saving = false
            }
        }
    }

    AlertDialog(
        onDismissRequest = { if (!saving) onDismiss() },
        title = { Text(stringResource(R.string.action_add_channel)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(2.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    modes.forEach { (value, label) ->
                        OutlinedButton(
                            onClick = { mode = value; failed = false },
                            enabled = !saving,
                            colors = ButtonDefaults.outlinedButtonColors(
                                containerColor = if (mode == value) colors.primary else colors.surfaceElevated,
                                contentColor = if (mode == value) colors.onPrimary else colors.onSurface,
                            ),
                            contentPadding = PaddingValues(horizontal = 6.dp, vertical = 0.dp),
                            border = BorderStroke(1.dp, colors.border),
                            shape = RoundedCornerShape(6.dp),
                            modifier = Modifier.height(32.dp).defaultMinSize(minWidth = 1.dp),
                        ) {
                            Text(stringResource(label), style = MaterialTheme.typography.labelMedium)
                        }
                    }
                    Spacer(Modifier.weight(1f))
                    Row(horizontalArrangement = Arrangement.spacedBy(2.dp)) {
                        platforms.forEach { (value, label) ->
                            val name = stringResource(label)
                            val icon = when (value) {
                                "twitter" -> R.drawable.platform_x
                                "youtube" -> R.drawable.platform_youtube
                                "tiktok" -> R.drawable.platform_tiktok
                                else -> R.drawable.platform_instagram
                            }
                            Box(
                                modifier = Modifier.size(28.dp)
                                    .alpha(if (mode == "handle") 1f else 0f)
                                    .then(if (mode == "handle") Modifier.selectable(
                                        selected = platform == value, enabled = !saving, role = Role.RadioButton,
                                        onClick = { platform = value; failed = false },
                                    ).semantics { contentDescription = name } else Modifier),
                                contentAlignment = Alignment.Center,
                            ) {
                                Icon(painterResource(icon), contentDescription = null,
                                    modifier = Modifier.size(28.dp).clip(RoundedCornerShape(6.dp))
                                        .background(if (platform == value) colors.primary else androidx.compose.ui.graphics.Color.Transparent)
                                        .padding(3.5.dp),
                                    tint = if (platform == value) colors.onPrimary else colors.onSurface)
                            }
                        }
                    }
                }
                OutlinedTextField(
                    value = input,
                    onValueChange = { input = it; failed = false },
                    label = { Text(stringResource(
                        if (mode == "handle") R.string.channel_add_handle_input else R.string.channel_add_input
                    )) },
                    enabled = !saving,
                    singleLine = true,
                    isError = failed,
                    supportingText = if (failed) {
                        { Text(stringResource(R.string.channel_add_failed)) }
                    } else null,
                    keyboardOptions = KeyboardOptions(
                        keyboardType = if (mode == "handle") KeyboardType.Text else KeyboardType.Uri,
                        autoCorrectEnabled = false,
                        imeAction = ImeAction.Done,
                    ),
                    keyboardActions = KeyboardActions(onDone = { submit() }),
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        },
        confirmButton = {
            TextButton(onClick = { submit() }, enabled = input.isNotBlank() && !saving) {
                Text(stringResource(if (saving) R.string.status_adding_ellipsis else R.string.action_add))
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss, enabled = !saving) {
                Text(stringResource(R.string.action_cancel))
            }
        },
    )
}
