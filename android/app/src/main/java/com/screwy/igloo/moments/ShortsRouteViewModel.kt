package com.screwy.igloo.moments

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.screwy.igloo.bookmarks.bookmarkFilterFromPlaylistId
import com.screwy.igloo.bookmarks.bookmarkMomentPlaylistItems
import com.screwy.igloo.channel.ChannelRouteResolver
import com.screwy.igloo.data.IglooDatabase
import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.data.entity.MomentsCursorEntity
import com.screwy.igloo.data.entity.StoryChannelItem
import com.screwy.igloo.data.stripPlatformPrefix
import com.screwy.igloo.media.ownerKindFromAssetOwnerKind
import com.screwy.igloo.net.ServerBaseUrlProvider
import com.screwy.igloo.outbox.OutboxKind
import com.screwy.igloo.outbox.OutboxWriter
import com.screwy.igloo.ui.UiEffect
import com.screwy.igloo.ui.UiEffects
import com.screwy.igloo.ui.UiState
import com.screwy.igloo.ui.component.BookmarkCategoryDisplay
import com.screwy.igloo.ui.component.BookmarkPayload
import com.screwy.igloo.ui.component.BookmarkState
import com.screwy.igloo.ui.component.BookmarkTarget
import com.screwy.igloo.ui.component.storyRingState
import com.screwy.igloo.ui.component.toBookmarkState
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import com.screwy.igloo.data.entity.MomentItem as DbMomentItem
import com.screwy.igloo.ui.component.MomentItem as PlayerMomentItem

@OptIn(ExperimentalCoroutinesApi::class)
class ShortsRouteViewModel(
    private val playlistSpec: ShortsPlaylistSpec,
    startVideoId: String,
    initialSelectionExplicit: Boolean = true,
    private val db: IglooDatabase,
    private val outboxWriter: OutboxWriter,
    private val prefs: PreferencesRepo,
    private val uiEffects: UiEffects,
    baseUrlProvider: ServerBaseUrlProvider,
    private val savedStateHandle: SavedStateHandle,
) : ViewModel() {
    private data class RepostMeta(
        val authorLabel: String,
        val otherCount: Int,
    )
    private data class StartSelection(
        val videoId: String,
        val index: Int,
    )
    private data class PendingInitialSelectionResolution(
        val item: PlayerMomentItem? = null,
        val cursorSuperseded: Boolean = false,
    )
    private enum class InitialSelectionKind {
        Explicit,
        Passive,
    }

    private val baseUrl = baseUrlProvider.baseUrl()
    private val initialVideoId = startVideoId.trim()
    private val initialSelectionKind = claimInitialMomentsSelection(initialSelectionExplicit)
    private var initialMomentsSelectionPending = initialSelectionKind != null
    private val activeVideoId =
        MutableStateFlow(
            if (!playlistSpec.recordsMomentsCursor || initialSelectionKind != null) {
                initialVideoId
            } else {
                ""
            }
        )
    private val momentsCursorScope: String =
        if (playlistSpec.type == ShortsPlaylistType.Moments) "following" else "all"
    private val recordsMomentViews: Boolean =
        playlistSpec.type != ShortsPlaylistType.Bookmarks
    private val bookmarkFilter = bookmarkFilterFromPlaylistId(playlistSpec.playlistId)
    private val rawScopedResumeCursor: Flow<MomentsCursorEntity?> =
        if (playlistSpec.recordsMomentsCursor) {
            db.momentsCursorDao().flow(momentsCursorScope)
        } else {
            flowOf(null)
        }
    private val scopedResumeCursor: Flow<MomentsCursorEntity?> =
        rawScopedResumeCursor.flatMapLatest { cursor ->
            val cursorVideoId = cursor?.videoId?.trim().orEmpty()
            if (cursor == null || cursor.sortAtMs > 0L || cursorVideoId.isEmpty()) {
                flowOf(cursor)
            } else {
                db.momentReadDao()
                    .momentSortAtFlow(cursorVideoId, momentsCursorScope)
                    .map { currentSortAtMs ->
                        cursor.copy(sortAtMs = currentSortAtMs?.takeIf { it > 0L } ?: 0L)
                    }
            }
        }

    private val storyStatusByChannel: StateFlow<Map<String, StoryChannelItem>> =
        prefs.storiesWindowHours()
            .flatMapLatest { hours ->
                db.momentReadDao().storyStatusesFlow(storyCutoffMillis(hours))
            }
            .map { rows -> rows.associateBy { it.channelId } }
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000L),
                initialValue = emptyMap(),
            )
    val storyChannels: StateFlow<List<StoryChannelItem>> =
        prefs.storiesWindowHours()
            .flatMapLatest { hours ->
                db.momentReadDao().storyChannelsFlow(storyCutoffMillis(hours))
            }
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000L),
                initialValue = emptyList(),
            )

    private val rawItems: StateFlow<List<PlayerMomentItem>?> =
        combine(playlistFlow(), storyStatusByChannel) { rows, storyStatuses ->
            rows.map { item ->
                val storyStatus = storyStatuses[item.channelId]
                item.copy(
                    storyRingState =
                        storyRingState(
                            storyStatus?.storyCount ?: 0,
                            storyStatus?.unseenCount ?: 0,
                        ),
                    storyFirstVideoId = storyStatus?.startVideoId().orEmpty(),
                )
            }
        }
        .map<List<PlayerMomentItem>, List<PlayerMomentItem>?> { it }
        .stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000L),
            initialValue = null,
        )

    /**
     * A route argument can be an explicit selection, a passive tab handoff, or a restored
     * back-stack entry. Claim it once in entry-owned saved state before observing Room so
     * reconstruction cannot turn either kind into a new explicit selection.
     */
    private fun claimInitialMomentsSelection(explicit: Boolean): InitialSelectionKind? {
        if (!playlistSpec.recordsMomentsCursor || initialVideoId.isEmpty()) return null
        if (savedStateHandle.get<Boolean>(InitialMomentsSelectionClaimedKey) == true) return null
        savedStateHandle[InitialMomentsSelectionClaimedKey] = true
        return if (explicit) InitialSelectionKind.Explicit else InitialSelectionKind.Passive
    }

    /**
     * A passive handoff resolves against the first Room playlist and never writes a cursor. An
     * explicit selection may publish only while this route is visible. Its initial cursor is the
     * causal baseline: any later cursor wins, even if the requested Room row appears afterward.
     */
    internal suspend fun consumePendingInitialMomentsSelection(
        onBaselineCaptured: () -> Unit = {},
    ) {
        if (!initialMomentsSelectionPending) return
        try {
            if (initialSelectionKind == InitialSelectionKind.Passive) {
                val initialItems = rawItems.filterNotNull().first()
                if (initialItems.none { it.videoId == initialVideoId }) activeVideoId.value = ""
                initialMomentsSelectionPending = false
                return
            }

            val baselineCursor = db.momentsCursorDao().get(momentsCursorScope)
            onBaselineCaptured()
            val resolution =
                combine(rawItems.filterNotNull(), rawScopedResumeCursor) { currentItems, cursor ->
                    PendingInitialSelectionResolution(
                        item = currentItems.firstOrNull { it.videoId == initialVideoId },
                        cursorSuperseded = cursor != baselineCursor,
                    )
                }
                    .first { it.item != null || it.cursorSuperseded }
            val requestedItem = resolution.item ?: return
            if (!initialMomentsSelectionPending) return
            val recorded =
                outboxWriter.recordMomentsCursorIfUnchanged(
                    expectedCursor = baselineCursor,
                    videoId = requestedItem.videoId,
                    positionMs = 0L,
                    scope = momentsCursorScope,
                    sortAtMs = requestedItem.sortAtMs.takeIf { it > 0L } ?: requestedItem.publishedAt,
                )
            if (recorded) initialMomentsSelectionPending = false
        } finally {
            cancelPendingInitialMomentsSelection()
        }
    }

    internal fun cancelPendingInitialMomentsSelection() {
        if (!initialMomentsSelectionPending) return
        initialMomentsSelectionPending = false
        if (activeVideoId.value == initialVideoId) activeVideoId.value = ""
    }

    val items: StateFlow<List<PlayerMomentItem>> = rawItems
        .map { it.orEmpty() }
        .stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000L),
            initialValue = emptyList(),
        )

    private val startSelection: StateFlow<StartSelection> =
        combine(items, activeVideoId, scopedResumeCursor) { currentItems, targetId, cursor ->
            resolveStartSelection(currentItems, targetId, cursor)
        }
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000L),
                initialValue = StartSelection(initialVideoId, 0),
            )

    val currentVideoId: StateFlow<String> = startSelection
        .map { it.videoId }
        .stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000L),
            initialValue = initialVideoId,
        )

    val startIndex: StateFlow<Int> = startSelection
        .map { it.index }
        .stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000L),
            initialValue = 0,
        )

    val uiState: StateFlow<UiState<Unit>> = rawItems
        .map { list ->
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

    val autoplayEnabled: StateFlow<Boolean> = prefs.autoplay().stateIn(
        scope = viewModelScope,
        started = SharingStarted.WhileSubscribed(5_000L),
        initialValue = PreferencesRepo.Defaults.AUTOPLAY,
    )

    val muted: StateFlow<Boolean> = prefs.muteDefault().stateIn(
        scope = viewModelScope,
        started = SharingStarted.WhileSubscribed(5_000L),
        initialValue = PreferencesRepo.Defaults.MUTE_DEFAULT,
    )

    private val _pendingBookmark = MutableStateFlow<BookmarkTarget?>(null)
    val pendingBookmark: StateFlow<BookmarkTarget?> = _pendingBookmark.asStateFlow()
    private val _pendingMomentActions = MutableStateFlow<PlayerMomentItem?>(null)
    val pendingMomentActions: StateFlow<PlayerMomentItem?> = _pendingMomentActions.asStateFlow()

    val bookmarkCategories: StateFlow<List<BookmarkCategoryDisplay>> =
        db.bookmarkCategoryDao().allFlow()
            .map { entities -> entities.map { BookmarkCategoryDisplay(it.categoryId, it.name) } }
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000L), emptyList())

    fun setAutoplayEnabled(enabled: Boolean) {
        viewModelScope.launch { prefs.setAutoplay(enabled) }
    }

    fun setMuted(enabled: Boolean) {
        viewModelScope.launch { prefs.setMuteDefault(enabled) }
    }

    fun onIndexChange(item: PlayerMomentItem) {
        initialMomentsSelectionPending = false
        activeVideoId.value = item.videoId
        if (!playlistSpec.recordsMomentsCursor) return
        viewModelScope.launch {
            recordMomentsCursor(item)
        }
    }

    private suspend fun recordMomentsCursor(item: PlayerMomentItem) {
        outboxWriter.recordMomentsCursor(
            item.videoId,
            0L,
            momentsCursorScope,
            item.sortAtMs.takeIf { it > 0L } ?: item.publishedAt,
        )
    }

    fun onViewEvent(videoId: String) {
        if (!recordsMomentViews) return
        viewModelScope.launch {
            outboxWriter.enqueue(OutboxKind.MomentView(videoId = videoId))
        }
    }

    private fun resolveStartSelection(
        items: List<PlayerMomentItem>,
        requestedVideoId: String,
        cursor: MomentsCursorEntity?,
    ): StartSelection {
        if (items.isEmpty()) return StartSelection(requestedVideoId, 0)
        val requested = requestedVideoId.trim()
        val requestedIndex = items.indexOfFirst { it.videoId == requested }
        if (requestedIndex >= 0) return StartSelection(requested, requestedIndex)

        val cursorRow = cursor
        val cursorVideoId = cursorRow?.videoId?.trim().orEmpty()
        if (cursorVideoId.isNotEmpty()) {
            val cursorIndex =
                shortsStartIndex(
                    items.map { ShortsStartItem(it.videoId, it.sortAtMs) },
                    cursorVideoId,
                    fallbackSortAtMs = cursorRow?.sortAtMs?.takeIf { it > 0L },
                )
            return StartSelection(items[cursorIndex].videoId, cursorIndex)
        }
        val passiveRouteIndex = items.indexOfFirst { it.videoId == initialVideoId }
        if (passiveRouteIndex >= 0) {
            return StartSelection(initialVideoId, passiveRouteIndex)
        }
        return StartSelection(items.first().videoId, 0)
    }

    fun toggleBookmark(item: PlayerMomentItem) {
        viewModelScope.launch {
            val current = db.bookmarkDao().getById(item.videoId)
            val action = if (current != null || item.isBookmarked) {
                OutboxKind.Action.Clear
            } else {
                OutboxKind.Action.Set
            }
            outboxWriter.enqueue(
                OutboxKind.Bookmark(
                    videoId = item.videoId,
                    action = action,
                ),
            )
        }
    }

    fun requestBookmarkSheet(item: PlayerMomentItem) {
        viewModelScope.launch {
            _pendingBookmark.value = bookmarkTargetForMoment(
                item = item,
                currentBookmark = db.bookmarkDao().getById(item.videoId)?.toBookmarkState(),
            )
        }
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
                    mediaIndices = payload.mediaIndices?.joinToString(","),
                ),
            )
        }
    }

    fun removePendingBookmark() {
        val target = _pendingBookmark.value ?: return
        _pendingBookmark.value = null
        viewModelScope.launch {
            outboxWriter.enqueue(
                OutboxKind.Bookmark(
                    videoId = target.itemId,
                    action = OutboxKind.Action.Clear,
                ),
            )
        }
    }

    fun createCategory(name: String) {
        viewModelScope.launch {
            val provisionalId = -System.currentTimeMillis()
            outboxWriter.enqueue(OutboxKind.CreateCategory(name = name, provisionalId = provisionalId))
        }
    }

    fun resolveMentionAndNavigate(handle: String) {
        viewModelScope.launch {
            uiEffects.emit(
                UiEffect.NavigateTo(
                    ChannelRouteResolver.routeForHandle(
                        db = db,
                        rawHandle = handle,
                        fallbackPlatform = "tiktok",
                    ),
                ),
            )
        }
    }

    fun followChannel(channelId: String) {
        viewModelScope.launch {
            outboxWriter.enqueue(
                OutboxKind.Follow(channelId = channelId, action = OutboxKind.Action.Set),
            )
        }
    }

    fun unfollowChannel(channelId: String) {
        viewModelScope.launch {
            outboxWriter.enqueue(
                OutboxKind.Follow(channelId = channelId, action = OutboxKind.Action.Clear),
            )
        }
    }

    fun requestMomentActions(item: PlayerMomentItem) {
        _pendingMomentActions.value = item
    }

    fun dismissMomentActions() {
        _pendingMomentActions.value = null
    }

    fun setRepostsEnabled(channelId: String, enabled: Boolean) {
        viewModelScope.launch {
            outboxWriter.enqueue(
                OutboxKind.ChannelSetting(
                    channelId = channelId,
                    settingField = "include_reposts",
                    value = if (enabled) 1L else 0L,
                )
            )
        }
    }

    fun setChannelMuted(channelId: String, muted: Boolean) {
        viewModelScope.launch {
            outboxWriter.enqueue(
                OutboxKind.Mute(
                    channelId = channelId,
                    action = if (muted) OutboxKind.Action.Set else OutboxKind.Action.Clear,
                )
            )
        }
    }

    @OptIn(ExperimentalCoroutinesApi::class)
    private fun playlistFlow() = when (playlistSpec.type) {
        ShortsPlaylistType.Moments -> db.momentReadDao()
            .playerMomentsFollowingFlow()
            .map { rows -> rows.map(::toPlayerMomentItem) }
        ShortsPlaylistType.AllMoments -> db.momentReadDao()
            .playerMomentsAllFlow()
            .map { rows -> rows.map(::toPlayerMomentItem) }
        ShortsPlaylistType.Channel -> db.momentReadDao()
            .channelMomentsFlow(playlistSpec.playlistId)
            .map { rows -> rows.map(::toPlayerMomentItem) }
        ShortsPlaylistType.Story -> prefs.storiesWindowHours()
            .flatMapLatest { hours ->
                db.momentReadDao().storyPlaylistFlow(
                    channelId = playlistSpec.playlistId,
                    cutoffMs = storyCutoffMillis(hours),
                )
            }
            .map { rows -> rows.map(::toPlayerMomentItem) }
        ShortsPlaylistType.StoryTray -> prefs.storiesWindowHours()
            .flatMapLatest { hours ->
                flow {
                    val rows = db.momentReadDao()
                        .storyTrayPlaylistFlow(cutoffMs = storyCutoffMillis(hours))
                        .first()
                    emit(rotateStoryTrayPlaylist(rows, initialVideoId))
                }
            }
            .map { rows -> rows.map(::toPlayerMomentItem) }
        ShortsPlaylistType.Bookmarks -> db.bookmarkReadDao()
            .bookmarksFlow()
			.map { rows -> bookmarkMomentPlaylistItems(rows, bookmarkFilter) }
    }

    private fun bookmarkTargetForMoment(
        item: PlayerMomentItem,
        currentBookmark: BookmarkState? = null,
    ): BookmarkTarget =
        BookmarkTarget(
            itemId = item.videoId,
            authorHandle = item.authorHandle,
            mediaCount = item.slideCount.coerceAtLeast(0),
            currentBookmark = currentBookmark,
            defaultTitle = item.description.lineSequence().firstOrNull(),
            bodyText = item.description,
        )

    private fun toPlayerMomentItem(row: DbMomentItem): PlayerMomentItem {
        val video = row.video
        val handle = row.channelSourceId?.takeIf { it.isNotBlank() }
            ?: stripPlatformPrefix(video.channelId)
        val repost = repostMeta(row)
        return PlayerMomentItem(
            videoId = video.videoId,
            channelId = video.channelId,
            canonicalUrl = video.canonicalUrl.orEmpty(),
            authorDisplayName = row.channelName?.takeIf { it.isNotBlank() },
            authorHandle = if (handle.isNotBlank()) "@$handle" else "",
            description = momentDisplayText(video.description, video.title),
            likeCount = null,
            isLiked = false,
            isBookmarked = false,
            mediaKind = video.mediaKind,
            slideCount = video.slideCount,
			ownerKind = ownerKindFromAssetOwnerKind(video.ownerKind),
            publishedAt = video.publishedAt,
            isAuthorFollowed = row.channelIsFollowed == 1,
            repostIntroduced = row.repostIntroduced == 1,
            reposterChannelId = row.reposterChannelId?.takeIf { it.isNotBlank() },
            repostAuthorLabel = repost?.authorLabel,
            repostOtherCount = repost?.otherCount ?: 0,
            sortAtMs = row.effectiveMomentAtMs.takeIf { it > 0L } ?: video.publishedAt,
        )
    }

    private fun storyCutoffMillis(hours: Int): Long =
        System.currentTimeMillis() - PreferencesRepo.Defaults.normalizeStoriesWindowHours(hours) * 3_600_000L

    private fun rotateStoryTrayPlaylist(
        rows: List<DbMomentItem>,
        startVideoId: String,
    ): List<DbMomentItem> {
        val anchorChannelId = rows.firstOrNull { it.video.videoId == startVideoId }?.video?.channelId
            ?: return rows
        val anchorIndex = rows.indexOfFirst { it.video.channelId == anchorChannelId }
        if (anchorIndex <= 0) return rows
        return rows.drop(anchorIndex) + rows.take(anchorIndex)
    }

    private fun StoryChannelItem.startVideoId(): String =
        firstUnseenVideoId.takeIf { it.isNotBlank() } ?: firstVideoId

    private fun repostMeta(row: DbMomentItem): RepostMeta? {
        if (row.repostIntroduced != 1) return null
        return RepostMeta(
            authorLabel = row.repostAuthorLabel?.takeIf { it.isNotBlank() } ?: return null,
            otherCount = (row.repostCount - 1).coerceAtLeast(0),
        )
    }
}

private const val InitialMomentsSelectionClaimedKey =
    "igloo.shorts.initial_moments_selection_claimed"
