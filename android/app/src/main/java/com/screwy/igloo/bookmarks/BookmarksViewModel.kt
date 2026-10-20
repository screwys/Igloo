package com.screwy.igloo.bookmarks

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.screwy.igloo.channel.ChannelRouteResolver
import com.screwy.igloo.data.IglooDatabase
import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.data.entity.BookmarkCategoryEntity
import com.screwy.igloo.data.entity.BookmarkItem
import com.screwy.igloo.data.stripPlatformPrefix
import com.screwy.igloo.outbox.OutboxKind
import com.screwy.igloo.outbox.OutboxWriter
import com.screwy.igloo.ui.UiState
import com.screwy.igloo.ui.component.BookmarkCategoryDisplay
import com.screwy.igloo.ui.component.BookmarkPayload
import com.screwy.igloo.ui.component.BookmarkState
import com.screwy.igloo.ui.component.BookmarkTarget
import com.screwy.igloo.ui.component.parseStoredHandles
import com.screwy.igloo.ui.component.parseStoredMediaIndices
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import kotlinx.serialization.Serializable
import java.time.LocalDate
import java.time.ZoneId

@Serializable
data class BookmarkFilter(
    val categoryId: Long? = null,
    val account: String = "",
    val label: String? = null,
    val noLabel: Boolean = false,
    val query: String = "",
    val savedFromDay: Long? = null,
    val savedThroughDay: Long? = null,
    val oldestFirst: Boolean = false,
) {
    val active: Boolean get() = categoryId != null || account.isNotEmpty() || label != null || noLabel || query.isNotBlank() || savedFromDay != null || savedThroughDay != null || oldestFirst
}

data class BookmarkLabelCount(val label: String?, val count: Int)

/** Bookmarks route state holder for the filtered grid. */
class BookmarksViewModel(
    private val db: IglooDatabase,
    private val outboxWriter: OutboxWriter,
    private val prefs: PreferencesRepo,
) : ViewModel() {

    /**
     * Player toggles mirrored from [PreferencesRepo] so the bookmarks-overlay
     * [com.screwy.igloo.ui.component.MomentsPlayer] reads + writes the same auto-swipe / mute bit
     * as the Moments tab — one app-wide setting, not three per-screen copies.
     */
    val autoplayEnabled: StateFlow<Boolean> =
        prefs
            .autoplay()
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000L),
                initialValue = PreferencesRepo.Defaults.AUTOPLAY,
            )

    val muted: StateFlow<Boolean> =
        prefs
            .muteDefault()
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000L),
                initialValue = PreferencesRepo.Defaults.MUTE_DEFAULT,
            )

    fun setAutoplayEnabled(enabled: Boolean) {
        viewModelScope.launch { prefs.setAutoplay(enabled) }
    }

    fun setMuted(enabled: Boolean) {
        viewModelScope.launch { prefs.setMuteDefault(enabled) }
    }

    private val selectedBookmarkFilter = MutableStateFlow<BookmarkFilter>(BookmarkFilter())
    val selectedFilter: StateFlow<BookmarkFilter> = selectedBookmarkFilter.asStateFlow()

    fun selectAll() {
        selectedBookmarkFilter.value = BookmarkFilter()
    }

    fun selectCategory(id: Long) {
        selectedBookmarkFilter.value = selectedBookmarkFilter.value.copy(categoryId = id)
    }

    fun selectFilter(filter: BookmarkFilter) {
        selectedBookmarkFilter.value = filter
    }

    private val allItems: StateFlow<List<BookmarkItem>?> =
        db.bookmarkReadDao()
            .bookmarksFlow()
			.map<List<BookmarkItem>, List<BookmarkItem>?> { rows -> visibleBookmarkItems(rows) }
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000L),
                initialValue = null,
            )

    val categories: StateFlow<List<BookmarkCategoryEntity>> =
        db.bookmarkCategoryDao()
            .allFlow()
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000L),
                initialValue = emptyList(),
            )

    val bookmarkCategories: StateFlow<List<BookmarkCategoryDisplay>> =
        categories
            .map { rows -> rows.map { BookmarkCategoryDisplay(it.categoryId, it.name) } }
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000L),
                initialValue = emptyList(),
            )

    /** Per-category counts (including id=0 "uncategorized") so chips can show `name (N)`. */
    val counts: StateFlow<Map<Long, Int>> =
        allItems
            .map { items -> items.orEmpty().groupingBy { it.bookmark.categoryId }.eachCount() }
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000L),
                initialValue = emptyMap(),
            )

    val labelCounts: StateFlow<List<BookmarkLabelCount>> =
        allItems
            .map { items -> bookmarkLabelCounts(items.orEmpty()) }
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000L),
                initialValue = emptyList(),
            )

    val accounts: StateFlow<List<String>> =
        allItems.map { items ->
            items.orEmpty().flatMap(::bookmarkAccountHandles)
                .map(String::lowercase).distinct().sorted()
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000L), emptyList())

    val items: StateFlow<List<BookmarkItem>> =
        combine(allItems, selectedBookmarkFilter) { list, filter ->
                filterBookmarkItems(list.orEmpty(), filter)
            }
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000L),
                initialValue = emptyList(),
            )

    val uiState: StateFlow<UiState<Unit>> =
        combine(allItems, selectedBookmarkFilter) { list, _ ->
                when {
                    list == null -> UiState.Loading
                    list.isEmpty() -> UiState.Empty
                    else -> UiState.Data(Unit)
                }
            }
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000L),
                initialValue = UiState.Loading,
            )

    private val _pendingBookmark = MutableStateFlow<BookmarkTarget?>(null)
    val pendingBookmark: StateFlow<BookmarkTarget?> = _pendingBookmark.asStateFlow()

    fun removeBookmark(videoId: String) {
        viewModelScope.launch {
            outboxWriter.enqueue(
                OutboxKind.Bookmark(videoId = videoId, action = OutboxKind.Action.Clear)
            )
        }
    }

    fun requestBookmarkSheet(item: BookmarkItem) {
        val existingMediaIndices = parseStoredMediaIndices(item.bookmark.mediaIndices)
        _pendingBookmark.value =
            BookmarkTarget(
                itemId = item.bookmark.videoId,
                authorHandle =
                    item.feedAuthorHandle
                        ?: item.resolvedChannelSourceId
                        ?: item.video?.channelId?.let(::stripPlatformPrefix)
                        ?: item.bookmark.videoId,
                mediaCount = item.video?.slideCount?.coerceAtLeast(0) ?: 0,
                currentBookmark =
                    BookmarkState(
                        categoryId = item.bookmark.categoryId,
                        customTitle = item.bookmark.customTitle,
                        mediaIndices = existingMediaIndices,
                        accountHandles = parseStoredHandles(item.bookmark.accountHandles),
                    ),
                defaultTitle = defaultTitle(item),
                defaultMediaIndices = existingMediaIndices,
                sourceHandle = item.feedSourceHandle,
                quoteAuthorHandle = item.feedQuoteAuthorHandle,
                bodyText = item.feedItem?.bodyText,
                isRetweet = item.feedItem?.isRetweet == true,
            )
    }

    fun dismissBookmarkSheet() {
        _pendingBookmark.value = null
    }

    fun confirmBookmark(payload: BookmarkPayload) {
        val target = _pendingBookmark.value ?: return
        _pendingBookmark.value = null
        viewModelScope.launch {
            outboxWriter.enqueue(
                OutboxKind.Bookmark(
                    videoId = target.itemId,
                    action = OutboxKind.Action.Set,
                    categoryId = payload.categoryId,
                    customTitle = payload.customTitle,
                    accountHandles = payload.accountHandles?.joinToString(","),
                    mediaIndices = payload.serializedMediaIndices,
                    combineImages = payload.combineImages,
                )
            )
        }
    }

    fun removePendingBookmark() {
        val target = _pendingBookmark.value ?: return
        _pendingBookmark.value = null
        removeBookmark(target.itemId)
    }

    fun createCategory(name: String) {
        viewModelScope.launch {
            val provisionalId = -System.currentTimeMillis()
            outboxWriter.enqueue(
                OutboxKind.CreateCategory(name = name, provisionalId = provisionalId)
            )
        }
    }

    suspend fun canonicalUrlFor(item: BookmarkItem): String {
        item.feedItem
            ?.canonicalUrl
            ?.takeIf { it.isNotBlank() }
            ?.let {
                return it
            }
        return item.video?.canonicalUrl.orEmpty()
    }

    suspend fun routeForMention(handle: String, fallbackPlatform: String): String =
        ChannelRouteResolver.routeForHandle(
            db = db,
            rawHandle = handle,
            fallbackPlatform = fallbackPlatform,
        )

    private fun defaultTitle(item: BookmarkItem): String? =
        item.feedItem?.bodyText?.lineSequence()?.firstOrNull { it.isNotBlank() }
            ?: item.video?.title?.takeIf { !it.isNullOrBlank() }
            ?: item.video?.description?.lineSequence()?.firstOrNull { it.isNotBlank() }
}

internal fun normalizeBookmarkLabel(label: String?): String? =
    label?.trim()?.takeIf { it.isNotEmpty() }

internal fun bookmarkLabelCounts(items: List<BookmarkItem>): List<BookmarkLabelCount> =
    items
        .groupingBy { normalizeBookmarkLabel(it.bookmark.customTitle) }
        .eachCount()
        .map { (label, count) -> BookmarkLabelCount(label = label, count = count) }
        .sortedWith(
            compareByDescending<BookmarkLabelCount> { it.count }
                .thenBy { it.label.orEmpty().lowercase() }
        )

internal fun bookmarkAccountHandles(item: BookmarkItem): List<String> {
    val author = listOf(item.feedAuthorHandle, item.resolvedChannelSourceId, item.resolvedChannelId)
        .firstOrNull { !it.isNullOrBlank() }
    return (parseStoredHandles(item.bookmark.accountHandles).orEmpty() + listOfNotNull(author))
        .map { it.trim().removePrefix("@").lowercase() }
        .filter(String::isNotEmpty)
        .distinct()
}

internal fun filterBookmarkItems(
    items: List<BookmarkItem>,
    filter: BookmarkFilter,
): List<BookmarkItem> {
    val zone = ZoneId.systemDefault()
    val savedFrom = filter.savedFromDay?.let { LocalDate.ofEpochDay(it).atStartOfDay(zone).toInstant().toEpochMilli() }
    val savedBefore = filter.savedThroughDay?.let { LocalDate.ofEpochDay(it).plusDays(1).atStartOfDay(zone).toInstant().toEpochMilli() }
    val words = filter.query.trim().split(Regex("\\s+")).filter(String::isNotEmpty)
    val matching = items.filter { item ->
        val text = if (words.isEmpty()) "" else listOfNotNull(
            item.bookmark.customTitle, item.bookmark.accountHandles,
            item.video?.title, item.video?.description,
            item.feedItem?.bodyText, item.feedItem?.quoteBodyText,
        ).joinToString(" ")
        (savedFrom == null || item.bookmark.bookmarkedAt >= savedFrom) &&
            (savedBefore == null || item.bookmark.bookmarkedAt < savedBefore) &&
            words.all { text.contains(it, ignoreCase = true) } &&
            (filter.categoryId == null || item.bookmark.categoryId == filter.categoryId) &&
            (filter.account.isBlank() || bookmarkAccountHandles(item).any {
                it.equals(filter.account.trim().removePrefix("@"), ignoreCase = true)
            }) &&
            when {
                filter.noLabel -> normalizeBookmarkLabel(item.bookmark.customTitle) == null
                filter.label != null -> normalizeBookmarkLabel(item.bookmark.customTitle) == normalizeBookmarkLabel(filter.label)
                else -> true
            }
    }
    return if (filter.oldestFirst) matching.asReversed() else matching
}
