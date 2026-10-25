package com.screwy.igloo.bookmarks

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items as gridItems
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.FilterAlt
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.SelectableDates
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Button
import androidx.compose.material3.TextButton
import androidx.compose.material3.IconButton
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.ExposedDropdownMenuAnchorType
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavController
import com.screwy.igloo.R
import com.screwy.igloo.data.stripPlatformPrefix
import com.screwy.igloo.data.entity.BookmarkItem
import com.screwy.igloo.media.MediaResolvers
import com.screwy.igloo.media.MediaUri
import com.screwy.igloo.ui.UiStateSwitch
import com.screwy.igloo.ui.component.Avatar
import com.screwy.igloo.ui.component.BookmarkSheet
import com.screwy.igloo.ui.component.MediaCellArtwork
import com.screwy.igloo.ui.component.MediaTypeBadge
import com.screwy.igloo.ui.component.ScrollToBottomFab
import com.screwy.igloo.ui.component.ScrollToTopFab
import com.screwy.igloo.ui.component.TimestampBadge
import com.screwy.igloo.ui.component.displayLabel
import com.screwy.igloo.ui.component.normalizeHandle
import com.screwy.igloo.ui.component.scrollArrowVisibility
import com.screwy.igloo.ui.nav.IglooNavigationSource
import com.screwy.igloo.ui.nav.WideVerticalGridMinCellWidthDp
import com.screwy.igloo.ui.nav.rememberIglooAdaptiveLayout
import com.screwy.igloo.ui.nav.rememberIglooNavigator
import com.screwy.igloo.ui.theme.iglooColors
import kotlinx.coroutines.launch
import org.koin.androidx.compose.koinViewModel
import org.koin.compose.koinInject
import java.time.LocalDate
import java.time.ZoneOffset

/**
 * Bookmarks tab: vertical thumbnail grid with a category-chip filter row on top.
 * Each tile shows the post thumbnail with a small avatar bug and opens playable
 * items in the shared in-place moments-style overlay.
 */
@Composable
fun BookmarksRoute(
    navController: NavController,
    modifier: Modifier = Modifier,
) {
    val vm: BookmarksViewModel = koinViewModel()
    val scope = rememberCoroutineScope()
    val items by vm.items.collectAsStateWithLifecycle()
    val categories by vm.categories.collectAsStateWithLifecycle()
    val bookmarkCategories by vm.bookmarkCategories.collectAsStateWithLifecycle()
    val counts by vm.counts.collectAsStateWithLifecycle()
    val authorHandles by vm.authorHandles.collectAsStateWithLifecycle()
    val taggedHandles by vm.taggedHandles.collectAsStateWithLifecycle()
    val labelCounts by vm.labelCounts.collectAsStateWithLifecycle()
    val selectedFilter by vm.selectedFilter.collectAsStateWithLifecycle()
    val uiState by vm.uiState.collectAsStateWithLifecycle()
    val pendingBookmark by vm.pendingBookmark.collectAsStateWithLifecycle()
    val gridState = rememberLazyGridState()
    val navigator = rememberIglooNavigator(navController)
    val adaptiveLayout = rememberIglooAdaptiveLayout()
    val gridCells = if (adaptiveLayout.isWide) {
        GridCells.Adaptive(WideVerticalGridMinCellWidthDp.dp)
    } else {
        GridCells.Fixed(3)
    }
    var filtersOpen by rememberSaveable { mutableStateOf(false) }

    val scrollFabsEnabled = items.isNotEmpty() && pendingBookmark == null
    val scrollArrows by remember(items.size, scrollFabsEnabled) {
        derivedStateOf {
            scrollArrowVisibility(
                showScrollFabs = scrollFabsEnabled,
                itemCount = items.size,
                visibleItemCount = gridState.layoutInfo.visibleItemsInfo.size,
                firstVisibleItemIndex = gridState.firstVisibleItemIndex,
                firstVisibleItemScrollOffset = gridState.firstVisibleItemScrollOffset,
            )
        }
    }

    UiStateSwitch(state = uiState, modifier = modifier) {
        Box(modifier = Modifier.fillMaxSize()) {
            LazyVerticalGrid(
                columns = gridCells,
                state = gridState,
                modifier = Modifier.fillMaxSize(),
                contentPadding = PaddingValues(horizontal = 8.dp, vertical = 8.dp),
                horizontalArrangement = Arrangement.spacedBy(6.dp),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                // Chip row spans the full 3-col width so the grid scrolls with its filter.
                item(span = { GridItemSpan(maxLineSpan) }) {
                    CategoryChipRow(
                        categories = categories,
                        counts = counts,
                        selected = selectedFilter,
                        onSelectAll = { vm.selectAll(); scope.launch { gridState.scrollToItem(0) } },
                        onSelectCategory = { vm.selectCategory(it); scope.launch { gridState.scrollToItem(0) } },
                        onOpenFilters = { filtersOpen = true },
                    )
                }
                gridItems(
                    items = items,
                    key = { it.bookmark.videoId },
                ) { item ->
                    BookmarkTile(
                        item = item,
                        authorDisplayName = bookmarkAuthorDisplayName(item),
                        authorHandle = bookmarkAuthorHandle(item, bookmarkChannelId(item)),
                        onAuthorClick = { channelId ->
                            navigator.openChannel(channelId, IglooNavigationSource.Bookmarks)
                        },
                        onClick = {
                            if (opensBookmarkInMomentsOverlay(item)) {
                                navigator.openShorts(
                                    playlistType = "bookmarks",
                                    playlistId = bookmarkPlaylistId(selectedFilter),
                                    videoId = item.bookmark.videoId,
                                    source = IglooNavigationSource.Bookmarks,
                                )
                            }
                        },
                    )
                }
            }

            when {
                scrollArrows.showTop -> ScrollToTopFab(
                    onClick = { scope.launch { gridState.scrollToItem(0) } },
                    modifier = Modifier
                        .align(Alignment.BottomEnd)
                        .padding(12.dp),
                )
                scrollArrows.showBottom -> ScrollToBottomFab(
                    onClick = {
                        scope.launch { gridState.scrollToItem(items.lastIndex + 1) }
                    },
                    modifier = Modifier
                        .align(Alignment.BottomEnd)
                        .padding(12.dp),
                )
            }

            if (filtersOpen) {
                BookmarkFilterPopup(
                    categories = categories,
                    authorHandles = authorHandles,
                    taggedHandles = taggedHandles,
                    labels = labelCounts,
                    selected = selectedFilter,
                    onApply = {
                        vm.selectFilter(it)
                        filtersOpen = false
                        scope.launch { gridState.scrollToItem(0) }
                    },
                    onDismiss = { filtersOpen = false },
                )
            }

        }
    }

    pendingBookmark?.let { target ->
        BookmarkSheet(
            target = target,
            categories = bookmarkCategories,
            onConfirm = vm::confirmBookmark,
            onRemove = vm::removePendingBookmark,
            onDismiss = vm::dismissBookmarkSheet,
            onCreateCategory = vm::createCategory,
        )
    }
}

/** All + one chip per category, showing counts. Horizontal scroll for overflow. */
@Composable
private fun CategoryChipRow(
    categories: List<com.screwy.igloo.data.entity.BookmarkCategoryEntity>,
    counts: Map<Long, Int>,
    selected: BookmarkFilter,
    onSelectAll: () -> Unit,
    onSelectCategory: (Long) -> Unit,
    onOpenFilters: () -> Unit,
) {
    val totalCount = counts.values.sum()
    val allLabel = stringResource(R.string.moments_label_all)
    LazyRow(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 6.dp),
        contentPadding = PaddingValues(horizontal = 4.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        item(key = "__all__") {
            CategoryChip(
                label = if (totalCount > 0) {
                    stringResource(R.string.bookmarks_all_with_count, totalCount)
                } else {
                    allLabel
                },
                selected = !selected.active,
                onClick = onSelectAll,
            )
        }
        items(
            items = categories,
            key = { it.categoryId },
        ) { cat ->
            val count = counts[cat.categoryId] ?: 0
            CategoryChip(
                label = if (count > 0) "${cat.name} ($count)" else cat.name,
                selected = selected.categoryId == cat.categoryId,
                onClick = { onSelectCategory(cat.categoryId) },
            )
        }
        item(key = "__filters__") {
            val colors = MaterialTheme.iglooColors
            Box(
                modifier = Modifier
                    .clip(RoundedCornerShape(18.dp))
                    .background(if (selected.active) colors.primary.copy(alpha = 0.18f) else Color.Transparent)
                    .border(1.dp, if (selected.active) colors.primary else colors.onSurfaceFaint, RoundedCornerShape(18.dp))
                    .clickable(onClick = onOpenFilters)
                    .padding(horizontal = 14.dp, vertical = 8.dp),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    imageVector = Icons.Default.FilterAlt,
                    contentDescription = stringResource(R.string.bookmark_filters),
                    tint = if (selected.active) MaterialTheme.iglooColors.primary else MaterialTheme.iglooColors.onSurface,
                    modifier = Modifier.size(16.dp),
                )
            }
        }
    }
}

@Composable
private fun CategoryChip(
    label: String,
    selected: Boolean,
    onClick: () -> Unit,
) {
    val colors = MaterialTheme.iglooColors
    val bg = if (selected) colors.primary.copy(alpha = 0.18f) else Color.Transparent
    val border = if (selected) colors.primary else colors.onSurfaceFaint
    Box(
        modifier = Modifier
            .clip(RoundedCornerShape(18.dp))
            .background(bg)
            .border(1.dp, border, RoundedCornerShape(18.dp))
            .clickable(onClick = onClick)
            .padding(horizontal = 14.dp, vertical = 8.dp),
        contentAlignment = Alignment.Center,
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            Text(
                text = label,
                style = MaterialTheme.typography.labelMedium,
                color = if (selected) colors.primary else colors.onSurface,
                fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun BookmarkFilterPopup(
    categories: List<com.screwy.igloo.data.entity.BookmarkCategoryEntity>,
    authorHandles: List<String>,
    taggedHandles: List<String>,
    labels: List<BookmarkLabelCount>,
    selected: BookmarkFilter,
    onApply: (BookmarkFilter) -> Unit,
    onDismiss: () -> Unit,
) {
    var categoryId by rememberSaveable { mutableStateOf(selected.categoryId) }
    var postedBy by rememberSaveable { mutableStateOf(selected.postedBy) }
    var tagged by rememberSaveable { mutableStateOf(selected.tagged) }
    var label by rememberSaveable { mutableStateOf(selected.label) }
    var noLabel by rememberSaveable { mutableStateOf(selected.noLabel) }
    var query by rememberSaveable { mutableStateOf(selected.query) }
    var savedFromDay by rememberSaveable { mutableStateOf(selected.savedFromDay) }
    var savedThroughDay by rememberSaveable { mutableStateOf(selected.savedThroughDay) }
    var oldestFirst by rememberSaveable { mutableStateOf(selected.oldestFirst) }
    var pickingDate by rememberSaveable { mutableStateOf<String?>(null) }
    val noLabelText = stringResource(R.string.bookmark_filter_no_label)
    Dialog(onDismissRequest = onDismiss, properties = DialogProperties(usePlatformDefaultWidth = false)) {
        Surface(
            modifier = Modifier.padding(16.dp).widthIn(max = 720.dp).fillMaxWidth().heightIn(max = 720.dp),
            color = MaterialTheme.iglooColors.surface,
            shape = RoundedCornerShape(16.dp),
        ) {
            Column(Modifier.imePadding()) {
                Row(
                    modifier = Modifier.fillMaxWidth().padding(8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    IconButton(onClick = onDismiss) {
                        Icon(Icons.Default.Close, contentDescription = stringResource(R.string.action_close))
                    }
                    Text(stringResource(R.string.bookmark_filters), style = MaterialTheme.typography.titleLarge)
                }
                Column(
                    modifier = Modifier.weight(1f, fill = false).verticalScroll(rememberScrollState()).padding(16.dp),
                    verticalArrangement = Arrangement.spacedBy(16.dp),
                ) {
                    OutlinedTextField(
                        value = query,
                        onValueChange = { query = it },
                        placeholder = { Text(stringResource(R.string.bookmark_search_text)) },
                        leadingIcon = { Icon(Icons.Default.Search, contentDescription = null) },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    HorizontalDivider(color = MaterialTheme.iglooColors.onSurfaceFaint)
                    BookmarkFilterField(
                        title = stringResource(R.string.bookmark_filter_category),
                        selectedValue = categoryId?.toString().orEmpty(),
                        options = categories.map { it.categoryId.toString() to it.name },
                        onSelect = { categoryId = it.toLongOrNull() },
                    )
                    BookmarkFilterField(
                        title = stringResource(R.string.bookmark_label),
                        selectedValue = if (noLabel) "empty" else label?.let { "label:$it" }.orEmpty(),
                        options = labels.map { row ->
                            if (row.label == null) "empty" to noLabelText else "label:${row.label}" to row.label
                        },
                        onSelect = {
                            noLabel = it == "empty"
                            label = if (it.startsWith("label:")) it.removePrefix("label:") else null
                        },
                    )
                    HorizontalDivider(color = MaterialTheme.iglooColors.onSurfaceFaint)
                    BookmarkFilterField(
                        title = stringResource(R.string.bookmark_filter_posted_by),
                        selectedValue = postedBy,
                        options = authorHandles.map { it to it },
                        onSelect = { postedBy = it },
                    )
                    BookmarkFilterField(
                        title = stringResource(R.string.bookmark_filter_tagged),
                        selectedValue = tagged,
                        options = taggedHandles.map { it to it },
                        onSelect = { tagged = it },
                    )
                    HorizontalDivider(color = MaterialTheme.iglooColors.onSurfaceFaint)
                    Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        BookmarkDateField(
                            title = stringResource(R.string.bookmark_saved_from),
                            day = savedFromDay,
                            onClick = { pickingDate = "from" },
                            modifier = Modifier.weight(1f),
                        )
                        BookmarkDateField(
                            title = stringResource(R.string.bookmark_saved_through),
                            day = savedThroughDay,
                            onClick = { pickingDate = "through" },
                            modifier = Modifier.weight(1f),
                        )
                    }
                    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text(stringResource(R.string.bookmark_sort), style = MaterialTheme.typography.labelMedium)
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            CategoryChip(
                                label = stringResource(R.string.bookmark_sort_newest),
                                selected = !oldestFirst,
                                onClick = { oldestFirst = false },
                            )
                            CategoryChip(
                                label = stringResource(R.string.bookmark_sort_oldest),
                                selected = oldestFirst,
                                onClick = { oldestFirst = true },
                            )
                        }
                    }
                }
                Row(
                    modifier = Modifier.fillMaxWidth().padding(16.dp),
                    horizontalArrangement = Arrangement.spacedBy(12.dp, Alignment.End),
                ) {
                    OutlinedButton(modifier = Modifier.width(112.dp).height(44.dp), onClick = {
                        categoryId = null; postedBy = ""; tagged = ""; label = null; noLabel = false
                        query = ""; savedFromDay = null; savedThroughDay = null; oldestFirst = false
                    }) {
                        Text(stringResource(R.string.bookmark_filter_reset))
                    }
                    Button(modifier = Modifier.width(112.dp).height(44.dp), onClick = { onApply(BookmarkFilter(categoryId, postedBy, tagged, label, noLabel, query.trim(), savedFromDay, savedThroughDay, oldestFirst)) }) {
                        Text(stringResource(R.string.bookmark_filter_apply))
                    }
                }
            }
        }
    }
    pickingDate?.let { field ->
        val day = if (field == "from") savedFromDay else savedThroughDay
        val pickerState = rememberDatePickerState(
            initialSelectedDateMillis = day?.let { LocalDate.ofEpochDay(it).atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli() },
            selectableDates = object : SelectableDates {
                override fun isSelectableDate(utcTimeMillis: Long): Boolean {
                    val candidate = utcTimeMillis / 86_400_000L
                    return if (field == "from") savedThroughDay == null || candidate <= savedThroughDay!!
                    else savedFromDay == null || candidate >= savedFromDay!!
                }
            },
        )
        DatePickerDialog(
            onDismissRequest = { pickingDate = null },
            confirmButton = {
                TextButton(onClick = {
                    val picked = pickerState.selectedDateMillis?.let { it / 86_400_000L }
                    if (field == "from") savedFromDay = picked else savedThroughDay = picked
                    pickingDate = null
                }) { Text(stringResource(R.string.bookmark_filter_apply)) }
            },
            dismissButton = {
                TextButton(onClick = {
                    if (field == "from") savedFromDay = null else savedThroughDay = null
                    pickingDate = null
                }) { Text(stringResource(R.string.action_clear)) }
            },
        ) { DatePicker(state = pickerState) }
    }
}

@Composable
private fun BookmarkDateField(title: String, day: Long?, onClick: () -> Unit, modifier: Modifier) {
    Column(modifier, verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(title, style = MaterialTheme.typography.labelMedium)
        OutlinedButton(onClick = onClick, modifier = Modifier.fillMaxWidth()) {
            Text(day?.let { LocalDate.ofEpochDay(it).toString() } ?: stringResource(R.string.moments_label_all))
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun BookmarkFilterField(
    title: String,
    selectedValue: String,
    options: List<Pair<String, String>>,
    onSelect: (String) -> Unit,
) {
    val selectedName = options.firstOrNull { it.first == selectedValue }?.second.orEmpty()
    var query by remember(selectedName) { mutableStateOf(selectedName) }
    var expanded by remember { mutableStateOf(false) }
    var searching by remember { mutableStateOf(false) }
    val matches = if (searching) options.filter { it.second.contains(query.trim(), ignoreCase = true) } else options
    ExposedDropdownMenuBox(
        expanded = expanded,
        onExpandedChange = {
            expanded = it
            searching = false
            if (!it) query = selectedName
        },
    ) {
        OutlinedTextField(
            value = query,
            onValueChange = {
                query = it
                searching = true
                expanded = true
                if (it.isEmpty()) onSelect("")
            },
            label = { Text(title) },
            placeholder = { Text(stringResource(R.string.moments_label_all)) },
            singleLine = true,
            trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(expanded) },
            modifier = Modifier.fillMaxWidth().menuAnchor(ExposedDropdownMenuAnchorType.PrimaryEditable),
        )
        ExposedDropdownMenu(
            expanded = expanded,
            onDismissRequest = { expanded = false; query = selectedName },
        ) {
            DropdownMenuItem(
                text = { Text(stringResource(R.string.moments_label_all)) },
                onClick = { onSelect(""); query = ""; expanded = false },
            )
            matches.forEach { (value, name) ->
                DropdownMenuItem(
                    text = { Text(name) },
                    onClick = { onSelect(value); query = name; expanded = false },
                )
            }
            if (matches.isEmpty()) {
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.bookmark_filter_no_matches)) },
                    onClick = {},
                    enabled = false,
                )
            }
        }
    }
}

/**
 * One grid cell — portrait tile, small avatar bug top-left, [MediaTypeBadge]
 * bottom-left, [TimestampBadge] bottom-right. Matches MomentsGrid / AllMomentsRoute /
 * TikTok-channel cell shape so the three vertical grids look identical to the user.
 */
@Composable
private fun BookmarkTile(
    item: BookmarkItem,
    authorDisplayName: String?,
    authorHandle: String,
    onAuthorClick: (String) -> Unit,
    onClick: () -> Unit,
) {
    val colors = MaterialTheme.iglooColors
    val resolvers: MediaResolvers = koinInject()

    val ownerKind = bookmarkOwnerKind(item) ?: return
    val mediaOwnerId = bookmarkMediaOwnerId(item)
    val channelId = item.resolvedChannelId
	val thumbUri by resolvers.thumbnailForPostFlow(mediaOwnerId, ownerKind)
		.collectAsState(initial = MediaUri.Missing)
	val displayThumbUri = thumbUri

    Box(
        modifier = Modifier
            .fillMaxWidth()
            .aspectRatio(9f / 16f)
            .clip(RoundedCornerShape(8.dp))
            .background(colors.surfaceVariant)
            .clickable(onClick = onClick),
    ) {
        MediaCellArtwork(
            thumbnailUri = displayThumbUri,
            contentDescription = null,
        )

        val normalizedHandle = normalizeHandle(authorHandle)
        val authorLabel = displayLabel(
            primary = authorDisplayName,
            handle = normalizedHandle,
            fallback = channelId?.let(::stripPlatformPrefix),
        )
        if (channelId != null && authorLabel.isNotBlank()) {
            Row(
                modifier = Modifier
                    .align(Alignment.TopStart)
                    .fillMaxWidth()
                    .clickable { onAuthorClick(channelId) }
                    .background(
                        Brush.verticalGradient(
                            colors = listOf(Color.Black.copy(alpha = 0.55f), Color.Transparent),
                        ),
                    )
                    .padding(horizontal = 6.dp, vertical = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                Avatar(channelId = channelId, size = 16.dp)
                Text(
                    text = authorLabel,
                    style = MaterialTheme.typography.labelSmall.copy(fontWeight = FontWeight.Medium),
                    color = Color.White,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }

        // Shared overlays — same shape across all 3 vertical grids.
        MediaTypeBadge(bookmarkMediaType(item))
        TimestampBadge(bookmarkPublishedAt(item))
    }
}
