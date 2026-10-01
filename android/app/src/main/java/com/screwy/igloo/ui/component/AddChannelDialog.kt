package com.screwy.igloo.ui.component

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import com.screwy.igloo.R
import com.screwy.igloo.channel.followQueuedMessageRes
import com.screwy.igloo.net.Reachability
import com.screwy.igloo.outbox.OutboxKind
import com.screwy.igloo.outbox.OutboxWriter
import com.screwy.igloo.ui.UiEffect
import com.screwy.igloo.ui.UiEffects
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import org.koin.compose.koinInject

@Composable
fun AddChannelDialog(onDismiss: () -> Unit) {
    val outboxWriter: OutboxWriter = koinInject()
    val reachability: Reachability = koinInject()
    val uiEffects: UiEffects = koinInject()
    val scope = rememberCoroutineScope()
    var input by rememberSaveable { mutableStateOf("") }
    var saving by remember { mutableStateOf(false) }
    var failed by remember { mutableStateOf(false) }

    fun submit() {
        val url = input.trim()
        if (saving || url.isEmpty()) return
        saving = true
        failed = false
        scope.launch {
            try {
                outboxWriter.enqueue(OutboxKind.Subscribe(url))
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
            Column {
                Text(stringResource(R.string.channel_add_description))
                OutlinedTextField(
                    value = input,
                    onValueChange = { input = it; failed = false },
                    label = { Text(stringResource(R.string.channel_add_input)) },
                    enabled = !saving,
                    singleLine = true,
                    isError = failed,
                    supportingText = { if (failed) Text(stringResource(R.string.channel_add_failed)) },
                    keyboardOptions = KeyboardOptions(
                        keyboardType = KeyboardType.Uri,
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
