package com.screwy.igloo.home

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Bookmark
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.DynamicFeed
import androidx.compose.material.icons.filled.History
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.PlayCircle
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.filled.VideoLibrary
import androidx.compose.material3.Checkbox
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import com.screwy.igloo.R
import com.screwy.igloo.data.entity.ChannelDisplay
import com.screwy.igloo.data.entity.displayOrName
import com.screwy.igloo.ui.component.PlatformChip

private val widgetTypes = listOf("continue", "live", "starred", "moments", "saved", "account", "latest")

internal fun homeTypeLabel(type: String): Int = when (type) {
    "continue" -> R.string.home_continue
    "live" -> R.string.home_live
    "starred" -> R.string.home_starred
    "moments" -> R.string.home_moments
    "saved" -> R.string.home_saved
    "account" -> R.string.home_account
    else -> R.string.home_latest
}

private fun homeTypeIcon(type: String): ImageVector = when (type) {
    "continue" -> Icons.Default.History
    "live", "moments" -> Icons.Default.PlayCircle
    "starred" -> Icons.Default.Star
    "saved" -> Icons.Default.Bookmark
    "account" -> Icons.Default.Person
    else -> Icons.Default.VideoLibrary
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun HomeWidgetCatalog(onDismiss: () -> Unit, onAdd: (String) -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
            SettingsHeader(stringResource(R.string.home_add_widget), onDismiss)
            widgetTypes.forEach { type ->
                Row(Modifier.fillMaxWidth().clickable { onAdd(type) }.padding(vertical = 16.dp),
                    verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                    Icon(homeTypeIcon(type), contentDescription = null)
                    Text(stringResource(homeTypeLabel(type)), Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
                    Icon(Icons.Default.Add, contentDescription = null)
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun HomeWidgetSettings(
    widget: HomeWidget, accounts: List<ChannelDisplay>, onDismiss: () -> Unit, onSave: (HomeWidget) -> Unit,
) {
    var draft by remember(widget.id) { mutableStateOf(widget) }
    var count by remember(widget.id) { mutableStateOf(widget.count.toString()) }
    var choosingAccounts by remember { mutableStateOf(false) }
    val validCount = count.toIntOrNull()?.takeIf { it > 0 }
    val layouts = when (widget.type) {
        "continue", "live" -> listOf("feature", "cards", "list")
        "starred" -> listOf("editorial", "cards", "list", "lanes")
        "moments" -> listOf("portraits", "cards", "list")
        "account" -> listOf("editorial", "cards", "list")
        else -> listOf("cards", "list")
    }
    ModalBottomSheet(onDismissRequest = onDismiss) {
        LazyColumn(Modifier.fillMaxHeight(0.9f).padding(horizontal = 16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp)) {
            item {
                SettingsHeader(stringResource(R.string.home_widget_settings), onDismiss,
                    onSave = { validCount?.let { onSave(draft.copy(count = it)) } }, canSave = validCount != null)
            }
            item {
                OutlinedTextField(draft.title, onValueChange = { draft = draft.copy(title = it) },
                    label = { Text(stringResource(R.string.home_title)) }, singleLine = true, modifier = Modifier.fillMaxWidth())
            }
            item {
                OutlinedTextField(count, onValueChange = { count = it },
                    label = { Text(stringResource(R.string.home_count)) }, singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number), modifier = Modifier.fillMaxWidth())
            }
            item { ChoiceRow(R.string.home_size, draft.size, listOf("small", "medium", "large", "full")) { draft = draft.copy(size = it) } }
            item { ChoiceRow(R.string.home_layout, draft.layout, layouts) { draft = draft.copy(layout = it) } }
            item { ChoiceRow(R.string.home_style, draft.style, listOf("surface", "open", "accent")) { draft = draft.copy(style = it) } }
            item { ChoiceRow(R.string.home_order, draft.order,
                when (draft.type) {
                    "live" -> listOf("live", "newest", "account")
                    "continue", "saved" -> listOf("recent", "newest", "account")
                    else -> listOf("newest", "account")
                }, labelFor = { if (draft.type == "saved" && it == "recent") R.string.home_recent_saved else optionLabel(it) }) {
                draft = draft.copy(order = it)
            } }
            item {
                FilterOptions(R.string.home_platforms,
                    if (draft.type in listOf("continue", "latest")) listOf("youtube", "tiktok", "instagram")
                    else listOf("youtube", "twitter", "tiktok", "instagram"), draft.platforms) {
                    draft = draft.copy(platforms = it)
                }
            }
            item {
                FilterOptions(R.string.home_content, listOf("post", "video", "image", "slideshow", "story"), draft.contentTypes) {
                    draft = draft.copy(contentTypes = it)
                }
            }
            item {
                Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                    Text(stringResource(R.string.home_accounts), Modifier.weight(1f))
                    TextButton(onClick = { choosingAccounts = true }) {
                        Text(if (draft.channels.isEmpty()) stringResource(R.string.label_all) else draft.channels.size.toString())
                    }
                }
            }
            if (draft.type == "live") item {
                Text(stringResource(R.string.home_live), style = MaterialTheme.typography.labelLarge)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    FilterChip(draft.liveStates.isEmpty(), onClick = { draft = draft.copy(liveStates = emptyList()) },
                        label = { Text(stringResource(R.string.label_all)) })
                    listOf("is_live", "is_upcoming", "was_live").forEach { state ->
                        val selected = state in draft.liveStates || (state == "was_live" && "post_live" in draft.liveStates)
                        FilterChip(selected, onClick = {
                            val states = if (state == "was_live") listOf("was_live", "post_live") else listOf(state)
                            draft = draft.copy(liveStates = if (selected) draft.liveStates - states.toSet() else draft.liveStates + states)
                        }, label = { Text(stringResource(optionLabel(state))) })
                    }
                }
            }
            if (draft.type != "starred") item {
                ToggleRow(R.string.home_starred_only, draft.starredOnly) { draft = draft.copy(starredOnly = it) }
            }
            item { ToggleRow(R.string.home_header, draft.showHeader) { draft = draft.copy(showHeader = it) } }
            item { ToggleRow(R.string.home_media, draft.showMedia) { draft = draft.copy(showMedia = it) } }
            item { ToggleRow(R.string.home_text, draft.showText) { draft = draft.copy(showText = it) } }
            item { Row(Modifier.padding(bottom = 16.dp)) {} }
        }
    }
    if (choosingAccounts) HomeAccountPicker(accounts, draft.channels,
        onDismiss = { choosingAccounts = false }, onChange = { draft = draft.copy(channels = it) })
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun HomeLayoutSettings(layout: HomeLayout, onDismiss: () -> Unit, onSave: (HomeLayout) -> Unit) {
    var columns by remember { mutableStateOf(layout.columns.toString()) }
    var spacing by remember { mutableStateOf(layout.spacing) }
    val columnCount = columns.toIntOrNull()?.takeIf { it in 1..6 }
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            SettingsHeader(stringResource(R.string.home_layout), onDismiss,
                onSave = { columnCount?.let { onSave(layout.copy(columns = it, spacing = spacing)) } }, canSave = columnCount != null)
            OutlinedTextField(columns, onValueChange = { columns = it },
                label = { Text(stringResource(R.string.home_columns)) }, singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number), modifier = Modifier.fillMaxWidth())
            ChoiceRow(R.string.home_spacing, spacing, listOf("comfortable", "compact")) { spacing = it }
        }
    }
}

@Composable
private fun SettingsHeader(title: String, onDismiss: () -> Unit, onSave: (() -> Unit)? = null, canSave: Boolean = true) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(title, Modifier.weight(1f), style = MaterialTheme.typography.titleLarge)
        if (onSave != null) IconButton(onClick = onSave, enabled = canSave) {
            Icon(Icons.Default.Check, stringResource(R.string.action_save))
        }
        IconButton(onClick = onDismiss) { Icon(Icons.Default.Close, stringResource(R.string.action_close)) }
    }
}

@Composable
private fun ChoiceRow(label: Int, value: String, choices: List<String>, labelFor: (String) -> Int = ::optionLabel, onChange: (String) -> Unit) {
    var expanded by remember { mutableStateOf(false) }
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text(stringResource(label), Modifier.weight(1f))
        Column {
            TextButton(onClick = { expanded = true }) { Text(stringResource(labelFor(value))) }
            DropdownMenu(expanded, onDismissRequest = { expanded = false }) {
                choices.forEach { choice ->
                    DropdownMenuItem(text = { Text(stringResource(labelFor(choice))) }, onClick = {
                        onChange(choice)
                        expanded = false
                    })
                }
            }
        }
    }
}

@Composable
private fun FilterOptions(label: Int, choices: List<String>, selected: List<String>, onChange: (List<String>) -> Unit) {
    Column {
        Text(stringResource(label), style = MaterialTheme.typography.labelLarge)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FilterChip(selected.isEmpty(), onClick = { onChange(emptyList()) }, label = { Text(stringResource(R.string.label_all)) })
            choices.forEach { choice ->
                FilterChip(choice in selected, onClick = {
                    onChange(if (choice in selected) selected - choice else selected + choice)
                }, label = { Text(stringResource(optionLabel(choice))) })
            }
        }
    }
}

@Composable
private fun ToggleRow(label: Int, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text(stringResource(label), Modifier.weight(1f))
        Switch(checked = checked, onCheckedChange = onChange)
    }
}

@Composable
private fun HomeAccountPicker(accounts: List<ChannelDisplay>, selected: List<String>, onDismiss: () -> Unit, onChange: (List<String>) -> Unit) {
    var query by remember { mutableStateOf("") }
    val filtered = remember(accounts, query) {
        accounts.filter { it.displayOrName.contains(query, true) || it.handle.orEmpty().contains(query, true) }
    }
    Dialog(onDismissRequest = onDismiss) {
        Surface(shape = MaterialTheme.shapes.large) {
            Column(Modifier.padding(16.dp).fillMaxWidth().fillMaxHeight(0.8f)) {
                SettingsHeader(stringResource(R.string.home_accounts), onDismiss)
                OutlinedTextField(query, onValueChange = { query = it }, modifier = Modifier.fillMaxWidth(), singleLine = true,
                    label = { Text(stringResource(R.string.drawer_search_accounts)) })
                TextButton(onClick = { onChange(emptyList()) }) { Text(stringResource(R.string.label_all)) }
                LazyColumn(Modifier.weight(1f)) {
                    items(filtered, key = { it.channel.channelId }) { account ->
                        val id = account.channel.channelId
                        Row(Modifier.fillMaxWidth().clickable { onChange(if (id in selected) selected - id else selected + id) },
                            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            Checkbox(id in selected, onCheckedChange = null)
                            Text(account.displayOrName, Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
                            PlatformChip(account.channel.platform)
                        }
                    }
                }
            }
        }
    }
}

private fun optionLabel(option: String): Int = when (option) {
    "small" -> R.string.home_small
    "medium" -> R.string.home_medium
    "large" -> R.string.home_large
    "full" -> R.string.home_full
    "feature" -> R.string.home_feature
    "cards" -> R.string.home_cards
    "list" -> R.string.home_list
    "editorial" -> R.string.home_editorial
    "lanes" -> R.string.home_lanes
    "portraits" -> R.string.home_portraits
    "surface" -> R.string.home_surface
    "open" -> R.string.home_open
    "accent" -> R.string.home_accent
    "comfortable" -> R.string.home_comfortable
    "compact" -> R.string.home_compact
    "recent" -> R.string.home_recent
    "newest" -> R.string.home_newest
    "account" -> R.string.home_by_account
    "live", "is_live" -> R.string.home_live_now
    "is_upcoming" -> R.string.home_upcoming
    "was_live" -> R.string.home_replays
    "youtube" -> R.string.platform_youtube
    "twitter" -> R.string.platform_x
    "tiktok" -> R.string.platform_tiktok
    "instagram" -> R.string.platform_instagram
    "post" -> R.string.home_post
    "video" -> R.string.home_video
    "image" -> R.string.home_image
    "slideshow" -> R.string.home_slideshow
    else -> R.string.home_story
}
