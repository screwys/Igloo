package com.screwy.igloo.moments

import android.content.res.Configuration
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import com.screwy.igloo.R
import com.screwy.igloo.net.MomentsApi
import com.screwy.igloo.net.TikTokChatEvent
import com.screwy.igloo.net.TikTokLiveComment
import com.screwy.igloo.ui.component.DropShadow
import kotlinx.coroutines.CancellationException
import org.koin.compose.koinInject

@Composable
internal fun TikTokLiveChat(channelId: String, fullscreen: Boolean, onClose: () -> Unit, modifier: Modifier) {
    val api: MomentsApi = koinInject()
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    var comments by remember(channelId) { mutableStateOf(emptyList<TikTokLiveComment>()) }
    var unavailable by remember(channelId) { mutableStateOf(false) }
    LaunchedEffect(api, channelId, lifecycle) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            unavailable = false
            try {
                api.chat(channelId).collect { event ->
                    when (event) {
                        is TikTokChatEvent.Comment -> {
                            comments = (comments.filterNot { it.id == event.value.id } + event.value).takeLast(100)
                        }
                        TikTokChatEvent.Ended -> unavailable = true
                    }
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (_: Exception) {
                unavailable = true
            }
        }
    }
    val rightPanel = fullscreen || LocalConfiguration.current.orientation == Configuration.ORIENTATION_LANDSCAPE
    Box(modifier) {
        if (rightPanel) {
            val listState = rememberLazyListState()
            LaunchedEffect(comments.lastOrNull()?.id) {
                if (comments.isNotEmpty()) listState.animateScrollToItem(comments.lastIndex)
            }
            Column(
                modifier = Modifier.align(Alignment.CenterEnd).width(280.dp).fillMaxHeight()
                    .background(Color.Black.copy(alpha = 0.65f)).padding(start = 12.dp, end = 8.dp),
            ) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(stringResource(R.string.player_comments_heading), color = Color.White,
                        style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
                    IconButton(onClick = onClose) {
                        Icon(Icons.Default.Close, stringResource(R.string.action_close), tint = Color.White)
                    }
                }
                LazyColumn(state = listState, modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    items(comments, key = { it.id }) { CommentText(it) }
                }
                if (unavailable) Text(stringResource(R.string.live_chat_unavailable), color = Color.White,
                    style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(vertical = 12.dp))
            }
        } else {
            Column(
                modifier = Modifier.align(Alignment.BottomStart).padding(start = 12.dp, end = 76.dp, bottom = 200.dp)
                    .heightIn(max = 168.dp),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                comments.takeLast(5).forEach { CommentText(it) }
                if (unavailable) Text(stringResource(R.string.live_chat_unavailable), color = Color.White,
                    style = MaterialTheme.typography.bodySmall.copy(shadow = DropShadow))
            }
        }
    }
}

@Composable
private fun CommentText(comment: TikTokLiveComment) {
    Text(
        text = buildAnnotatedString {
            withStyle(SpanStyle(fontWeight = FontWeight.SemiBold)) { append(comment.author); append("  ") }
            append(comment.text)
        },
        color = Color.White,
        style = MaterialTheme.typography.bodySmall.copy(shadow = DropShadow),
        maxLines = 3,
    )
}
