package com.screwy.igloo.player

import androidx.compose.foundation.layout.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier

@Composable
internal fun PlayerChatLayout(
    showChat: Boolean,
    sideBySide: Boolean,
    modifier: Modifier = Modifier,
    player: @Composable (Modifier) -> Unit,
    chat: @Composable (Modifier) -> Unit,
) {
    if (!showChat) {
        player(modifier)
    } else if (sideBySide) {
        Row(modifier) {
            player(Modifier.weight(3f).fillMaxHeight())
            chat(Modifier.weight(1f).fillMaxHeight())
        }
    } else {
        Column(modifier) {
            player(Modifier.fillMaxWidth().aspectRatio(16f / 9f))
            chat(Modifier.fillMaxWidth().weight(1f))
        }
    }
}
