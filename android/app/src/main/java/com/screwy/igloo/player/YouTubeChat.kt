package com.screwy.igloo.player

import androidx.compose.foundation.background
import androidx.compose.foundation.interaction.DragInteraction
import androidx.compose.foundation.text.InlineTextContent
import androidx.compose.foundation.text.appendInlineContent
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.Verified
import androidx.compose.material.icons.filled.Shield
import androidx.compose.material.icons.filled.Star
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.Placeholder
import androidx.compose.ui.text.PlaceholderVerticalAlign
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.em
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import androidx.media3.common.C
import androidx.media3.common.Timeline
import androidx.media3.exoplayer.ExoPlayer
import coil3.compose.AsyncImage
import com.screwy.igloo.R
import com.screwy.igloo.media.MediaResolvers
import com.screwy.igloo.media.MediaUri
import com.screwy.igloo.net.BroadcastChatEvent
import com.screwy.igloo.net.BroadcastsApi
import com.screwy.igloo.net.iglooJson
import com.screwy.igloo.ui.component.rememberRemoteImageModel
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.serialization.json.*
import org.koin.compose.koinInject

private data class ChatRun(val text: String, val image: String = "", val icon: String = "")

private data class ChatMessage(
    val id: String,
    val authorId: String,
    val author: String,
    val avatar: String,
    val body: List<ChatRun>,
    val badges: List<ChatRun>,
    val amount: String,
    val color: Long?,
    val pinned: Boolean,
)

private data class ChatChange(
    val time: Long,
    val kind: String,
    val id: String = "",
    val authorId: String = "",
    val message: ChatMessage? = null,
)

private fun JsonObject.obj(key: String) = this[key] as? JsonObject
private fun JsonObject.str(key: String) = (this[key] as? JsonPrimitive)?.contentOrNull.orEmpty()
private fun JsonObject.number(key: String) = (this[key] as? JsonPrimitive)?.longOrNull ?: 0L
private fun JsonObject.list(key: String) = this[key] as? JsonArray ?: JsonArray(emptyList())
private fun JsonObject.label(): String = str("simpleText").ifEmpty {
    list("runs").joinToString("") { run ->
        val value = run.jsonObject
        value.str("text").ifEmpty {
            val emoji = value.obj("emoji")
            emoji?.list("shortcuts")?.firstOrNull()?.jsonPrimitive?.contentOrNull
                ?: emoji?.str("emojiId").orEmpty()
        }
    }
}
private fun JsonObject.image(): String = list("thumbnails").lastOrNull()?.jsonObject?.str("url").orEmpty()
private fun JsonObject.runs(): List<ChatRun> = if (str("simpleText").isNotEmpty()) {
    listOf(ChatRun(str("simpleText")))
} else list("runs").map { run ->
    val value = run.jsonObject
    val emoji = value.obj("emoji")
    ChatRun(value.str("text").ifEmpty {
        emoji?.list("shortcuts")?.firstOrNull()?.jsonPrimitive?.contentOrNull ?: emoji?.str("emojiId").orEmpty()
    }, emoji?.obj("image")?.image().orEmpty())
}

private fun chatChanges(record: JsonObject, live: Boolean, captureStarted: Long): List<ChatChange> {
    val replay = record.obj("replayChatItemAction")
    val offset = (replay ?: record).number("videoOffsetTimeMsec")
    val actions = replay?.list("actions") ?: JsonArray(listOf(record))
    return actions.mapNotNull { raw ->
        val action = raw.jsonObject
        val replace = action.obj("replaceChatItemAction")
        val banner = action.obj("addBannerToLiveChatCommand")?.obj("bannerRenderer")?.obj("liveChatBannerRenderer")
        val item = action.obj("addChatItemAction")?.obj("item")
            ?: replace?.obj("replacementItem") ?: banner?.obj("contents")
        val renderer = item?.entries?.firstOrNull { it.key.startsWith("liveChat") && it.key.endsWith("Renderer") }?.value as? JsonObject
        val timestamp = renderer?.number("timestampUsec")?.div(1000L) ?: 0L
        val time = if (live) timestamp.takeIf { it > 0 } ?: (captureStarted + offset) else offset
        if (renderer != null) {
            val header = renderer.obj("header")?.obj("liveChatSponsorshipsHeaderRenderer") ?: renderer
            val id = replace?.str("targetItemId") ?: banner?.str("bannerId") ?: renderer.str("id")
            if (id.isEmpty()) return@mapNotNull null
            val body = renderer.obj("message") ?: renderer.obj("headerSubtext")
                ?: renderer.obj("primaryText") ?: renderer.obj("text")
            val badges = header.list("authorBadges").mapNotNull { it.jsonObject.obj("liveChatAuthorBadgeRenderer") }
                .map { ChatRun(it.str("tooltip"), it.obj("customThumbnail")?.image().orEmpty(), it.obj("icon")?.str("iconType").orEmpty()) }
            val headerText = renderer.obj("headerPrimaryText") ?: renderer.obj("header")?.obj("liveChatSponsorshipsHeaderRenderer")?.obj("primaryText")
            val sticker = renderer.obj("sticker")
            val content = headerText?.runs().orEmpty() + body?.runs().orEmpty() + if (sticker != null) {
                listOf(ChatRun(sticker.obj("accessibility")?.obj("accessibilityData")?.str("label").orEmpty(), sticker.image()))
            } else emptyList()
            ChatChange(time, if (replace != null) "replace" else "add", message = ChatMessage(
                id, header.str("authorExternalChannelId"), header.obj("authorName")?.label().orEmpty(),
                header.obj("authorPhoto")?.image().orEmpty(),
                content,
                badges, renderer.obj("purchaseAmountText")?.label().orEmpty(),
                renderer.number("bodyBackgroundColor").takeIf { it != 0L },
                banner != null,
            ))
        } else {
            val remove = action.obj("removeChatItemAction") ?: action.obj("markChatItemAsDeletedAction")
            val author = action.obj("removeChatItemByAuthorAction") ?: action.obj("markChatItemsByAuthorAsDeletedAction")
            val removeBanner = action.obj("removeBannerForLiveChatCommand")
            when {
                remove != null -> ChatChange(time, "remove", id = remove.str("targetItemId"))
                removeBanner != null -> ChatChange(time, "remove", id = removeBanner.str("bannerId"))
                author != null -> ChatChange(time, "removeAuthor", authorId = author.str("externalChannelId"))
                else -> null
            }
        }
    }
}

@Composable
internal fun YouTubeChat(videoId: String, player: ExoPlayer, live: Boolean, onClose: () -> Unit, modifier: Modifier = Modifier) {
    val api: BroadcastsApi = koinInject()
    val resolvers: MediaResolvers = koinInject()
    val localChat by remember(videoId) { resolvers.replayChatFlow(videoId) }
        .collectAsState(initial = MediaUri.Missing)
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val changes = remember(videoId) { mutableListOf<ChatChange>() }
    var messages by remember(videoId) { mutableStateOf(emptyList<ChatMessage>()) }
    var status by remember(videoId) { mutableIntStateOf(R.string.status_loading_ellipsis) }
    var captureStarted by remember(videoId) { mutableLongStateOf(0L) }
    var revision by remember(videoId) { mutableIntStateOf(0) }
    val listState = rememberLazyListState()
    var follow by remember(videoId) { mutableStateOf(true) }
    LaunchedEffect(videoId, localChat, lifecycle) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            changes.clear()
            messages = emptyList()
            revision++
            status = R.string.status_loading_ellipsis
            try {
                val source = (localChat as? MediaUri.Local)?.let { asset ->
                    flow {
                        asset.file.bufferedReader().use { reader ->
                            while (true) {
                                currentCoroutineContext().ensureActive()
                                val line = reader.readLine() ?: break
                                if (line.isNotBlank()) emit(BroadcastChatEvent.Record(iglooJson.parseToJsonElement(line).jsonObject))
                            }
                        }
                        emit(BroadcastChatEvent.Ended)
                    }.flowOn(Dispatchers.IO)
                } ?: api.chat(videoId)
                source.collect { event ->
                    when (event) {
                        is BroadcastChatEvent.Start -> captureStarted = event.data.number("started_at_ms")
                        is BroadcastChatEvent.Record -> {
                            val incoming = chatChanges(event.data, live, captureStarted)
                            if (incoming.isNotEmpty()) {
                                val outOfOrder = changes.isNotEmpty() && incoming.first().time < changes.last().time
                                changes.addAll(incoming)
                                if (outOfOrder) { changes.sortBy { it.time }; revision++ }
                                status = 0
                            }
                        }
                        BroadcastChatEvent.Ended -> status = if (live) R.string.player_chat_ended else 0
                        BroadcastChatEvent.Unavailable -> status = R.string.player_chat_unavailable
                    }
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (_: Exception) {
                status = R.string.player_chat_failed
            }
        }
    }
    LaunchedEffect(videoId, player, live, revision, lifecycle) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            val visible = linkedMapOf<String, ChatMessage>()
            val window = Timeline.Window()
            var cursor = 0
            var previous = Long.MIN_VALUE
            var fallbackEpoch: Long? = null
            while (isActive) {
                val position = if (!live) player.currentPosition else {
                    if (player.currentTimeline.isEmpty) Long.MIN_VALUE else {
                        player.currentTimeline.getWindow(player.currentMediaItemIndex, window)
                        if (window.windowStartTimeMs != C.TIME_UNSET) window.windowStartTimeMs + player.currentPosition
                        else {
                            if (fallbackEpoch == null && window.durationMs > 0) {
                                fallbackEpoch = System.currentTimeMillis() - window.durationMs - window.positionInFirstPeriodMs
                            }
                            fallbackEpoch?.plus(player.currentPosition + window.positionInFirstPeriodMs) ?: Long.MIN_VALUE
                        }
                    }
                }
                var changed = false
                if (position < previous) { visible.clear(); cursor = 0; changed = true }
                while (cursor < changes.size && changes[cursor].time <= position) {
                    val event = changes[cursor++]
                    when (event.kind) {
                        "add", "replace" -> event.message?.let { visible[it.id] = it }
                        "remove" -> visible.remove(event.id)
                        "removeAuthor" -> visible.entries.removeAll { it.value.authorId == event.authorId }
                    }
                    changed = true
                }
                if (changed) messages = visible.values.toList()
                previous = position
                delay(250)
            }
        }
    }
    LaunchedEffect(listState) {
        listState.interactionSource.interactions.collect { if (it is DragInteraction.Start) follow = false }
    }
    LaunchedEffect(listState) {
        snapshotFlow { listState.canScrollForward }.collect { if (!it) follow = true }
    }
    LaunchedEffect(messages.lastOrNull()?.id, follow) {
        if (follow && messages.isNotEmpty()) listState.scrollToItem(messages.lastIndex)
    }
    Column(modifier.background(MaterialTheme.colorScheme.surface)) {
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(start = 12.dp)) {
            Text(stringResource(R.string.player_live_chat), Modifier.weight(1f), style = MaterialTheme.typography.titleSmall)
            if (!follow) IconButton(onClick = { follow = true }) {
                Icon(Icons.Default.KeyboardArrowDown, stringResource(R.string.player_chat_latest))
            }
            IconButton(onClick = onClose) { Icon(Icons.Default.Close, stringResource(R.string.action_close)) }
        }
        LazyColumn(state = listState, modifier = Modifier.weight(1f).padding(horizontal = 12.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp)) {
            items(messages, key = { it.id }) { message ->
                val inline = linkedMapOf<String, InlineTextContent>()
                val text = buildAnnotatedString {
                    fun appendRun(run: ChatRun) {
                        if (run.image.isEmpty() && run.icon.isEmpty()) append(run.text)
                        else {
                            val key = inline.size.toString()
                            appendInlineContent(key, run.text.ifEmpty { " " })
                            inline[key] = InlineTextContent(Placeholder(1.2.em, 1.2.em, PlaceholderVerticalAlign.TextCenter)) {
                                if (run.image.isNotEmpty()) AsyncImage(rememberRemoteImageModel(run.image),
                                    contentDescription = run.text, modifier = Modifier.fillMaxSize())
                                else Icon(when (run.icon) {
                                    "MODERATOR" -> Icons.Default.Shield
                                    "OWNER" -> Icons.Default.Star
                                    else -> Icons.Default.Verified
                                }, contentDescription = run.text, modifier = Modifier.fillMaxSize())
                            }
                        }
                    }
                    message.badges.forEach { appendRun(it); append(" ") }
                    withStyle(SpanStyle(fontWeight = FontWeight.SemiBold)) { append(message.author); append(" ") }
                    if (message.amount.isNotEmpty()) { append(message.amount); append(" ") }
                    message.body.forEach { appendRun(it) }
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp),
                    modifier = Modifier.fillMaxWidth().background(message.color?.let { Color(it.toInt()) }
                        ?: if (message.pinned) MaterialTheme.colorScheme.secondaryContainer else Color.Transparent)) {
                    if (message.avatar.isNotBlank()) AsyncImage(
                        model = rememberRemoteImageModel(message.avatar), contentDescription = null,
                        modifier = Modifier.size(24.dp).clip(CircleShape),
                    )
                    Text(text, inlineContent = inline, style = MaterialTheme.typography.bodyMedium)
                }
            }
        }
        if (status != 0) Text(stringResource(status), Modifier.padding(12.dp), style = MaterialTheme.typography.bodySmall)
    }
}
