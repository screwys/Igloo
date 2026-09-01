package com.screwy.igloo.moments

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.screwy.igloo.R
import com.screwy.igloo.channel.ChannelRouteResolver
import com.screwy.igloo.data.IglooDatabase
import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.data.entity.MomentItem as DbMomentItem
import com.screwy.igloo.data.entity.MomentsCursorEntity
import com.screwy.igloo.data.entity.StoryChannelItem
import com.screwy.igloo.data.entity.durationMs
import com.screwy.igloo.data.stripPlatformPrefix
import com.screwy.igloo.media.MediaResolvers
import com.screwy.igloo.media.ownerKindFromAssetOwnerKind
import com.screwy.igloo.outbox.OutboxKind
import com.screwy.igloo.outbox.OutboxWriter
import com.screwy.igloo.sync.SyncCoordinator
import com.screwy.igloo.ui.UiEffect
import com.screwy.igloo.ui.UiEffects
import com.screwy.igloo.ui.UiState
import com.screwy.igloo.ui.component.BookmarkCategoryDisplay
import com.screwy.igloo.ui.component.BookmarkPayload
import com.screwy.igloo.ui.component.BookmarkState
import com.screwy.igloo.ui.component.BookmarkTarget
import com.screwy.igloo.ui.component.MomentItem as PlayerMomentItem
import com.screwy.igloo.ui.component.MomentThumbnailItem
import com.screwy.igloo.ui.component.StoryRingState
import com.screwy.igloo.ui.component.storyRingState
import com.screwy.igloo.ui.component.toBookmarkState
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.onStart
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import kotlinx.coroutines.yield

internal data class ScopedShortsSnapshot<T>(
    val scope: String,
    val rows: List<T>?,
    val selection: VisibleShortsSelection,
)

internal data class MomentsPlayerRouteState(
    val scope: String,
    val items: List<PlayerMomentItem>,
    val selection: VisibleShortsSelection,
    val uiState: UiState<Unit>,
)

internal fun <T> momentsPlayerRouteState(
    snapshot: ScopedShortsSnapshot<T>,
    itemsForRows: (List<T>) -> List<PlayerMomentItem>,
): MomentsPlayerRouteState {
    val rows = snapshot.rows
    val items = rows?.let(itemsForRows).orEmpty()
    val uiState: UiState<Unit> =
        when {
            rows == null -> UiState.Loading
            rows.isEmpty() -> UiState.Empty
            else -> UiState.Data(Unit)
        }
    if (uiState is UiState.Data) {
        check(items.any { item -> item.videoId == snapshot.selection.videoId }) {
            "moments player selection must belong to its items"
        }
    }
    return MomentsPlayerRouteState(
        scope = snapshot.scope,
        items = items,
        selection = snapshot.selection,
        uiState = uiState,
    )
}

internal data class MomentsGridRouteState(
    val scope: String,
    val items: List<MomentThumbnailItem>,
    val startIndex: Int,
    val storyChannels: List<MomentsViewModel.StoryChannelUiItem>,
    val uiState: UiState<Unit>,
)

internal fun <T> momentsGridRouteState(
    snapshot: ScopedShortsSnapshot<T>,
    storyChannels: List<MomentsViewModel.StoryChannelUiItem>,
    itemsForRows: (List<T>) -> List<MomentThumbnailItem>,
): MomentsGridRouteState {
    val rows = snapshot.rows
    val items = rows?.let(itemsForRows).orEmpty()
    val uiState: UiState<Unit> =
        when {
            rows == null -> UiState.Loading
            snapshot.scope == "stories" -> UiState.Data(Unit)
            rows.isEmpty() -> UiState.Empty
            else -> UiState.Data(Unit)
        }
    if (uiState is UiState.Data && snapshot.scope != "stories") {
        check(items.any { item -> item.videoId == snapshot.selection.videoId }) {
            "moments grid selection must belong to its items"
        }
    }
    return MomentsGridRouteState(
        scope = snapshot.scope,
        items = items,
        startIndex = if (snapshot.scope == "stories") 0 else snapshot.selection.index,
        storyChannels = storyChannels,
        uiState = uiState,
    )
}

@OptIn(ExperimentalCoroutinesApi::class)
internal fun <T> scopedShortsSnapshotFlow(
    activeTab: Flow<String>,
    rowsForScope: (String) -> Flow<List<T>>,
    cursorForScope: (String) -> Flow<MomentsCursorEntity?>,
    startItem: (T, String) -> ShortsStartItem,
    scopeForTab: (String) -> String = ::momentsPlayerScope,
    selectionOverride: Flow<String?> = flowOf(null),
): Flow<ScopedShortsSnapshot<T>> =
    activeTab
        .map(scopeForTab)
        .distinctUntilChanged()
        .flatMapLatest { scope ->
            combine(
                rowsForScope(scope),
                cursorForScope(scope),
                selectionOverride,
            ) { rows, cursor, overrideVideoId ->
                val startItems = rows.map { startItem(it, scope) }
                val exactOverride =
                    overrideVideoId?.takeIf { id -> startItems.any { item -> item.videoId == id } }
                ScopedShortsSnapshot(
                    scope = scope,
                    rows = rows,
                    selection =
                        visibleShortsSelection(
                            items = startItems,
                            requestedVideoId = exactOverride ?: cursor?.videoId,
                            fallbackSortAtMs =
                                cursor?.sortAtMs?.takeIf { exactOverride == null && it > 0L },
                            fallbackOrderPosition =
                                cursor?.orderPosition?.takeIf { exactOverride == null && it > 0L },
                        ),
                )
            }
                .onStart {
                    emit(
                        ScopedShortsSnapshot(
                            scope = scope,
                            rows = null,
                            selection = VisibleShortsSelection(videoId = null, index = 0),
                        )
                    )
                }
        }

internal fun momentsPlayerScope(tab: String): String =
    if (PreferencesRepo.Defaults.normalizeMomentsTab(tab) == "following") "following" else "all"

/**
 * Nav-graph-scoped ViewModel shared by `MomentsRoute` (the TikTok-style player) and
 * `AllMomentsRoute` (the 3-column grid). Both routes live in the `moments-graph` nested nav graph and
 * resolve this VM against that graph's `NavBackStackEntry` ViewModelStore. The grid thumbnails are
 * still resolved eagerly here because the all-moments grid is a thumbnail surface. The player list
 * is intentionally cheap: it emits metadata only, and the player resolves
 * stream/thumbnail/bookmark state lazily for the current and neighboring pages.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class MomentsViewModel(
    private val db: IglooDatabase,
    private val outboxWriter: OutboxWriter,
    private val prefs: PreferencesRepo,
    private val scheduler: SyncCoordinator,
    private val uiEffects: UiEffects,
    private val resolvers: MediaResolvers,
) : ViewModel() {
    private data class RepostMeta(val authorLabel: String, val otherCount: Int)

    data class StoryChannelUiItem(
        val channelId: String,
        val displayName: String,
        val handle: String,
        val count: Int,
        val unseenCount: Int,
        val latestAtMs: Long,
        val firstVideoId: String,
        val firstUnseenVideoId: String,
        val ringState: StoryRingState,
    ) {
        val startVideoId: String
            get() = firstUnseenVideoId.takeIf { it.isNotBlank() } ?: firstVideoId
    }

    private val sessionTabOverride = MutableStateFlow<String?>(null)
    private val _sessionVideoId = MutableStateFlow<String?>(null)
    internal val sessionVideoId: StateFlow<String?> = _sessionVideoId.asStateFlow()

    val activeTab: StateFlow<String> =
        combine(prefs.momentsDefaultTab(), sessionTabOverride) { defaultTab, override ->
                PreferencesRepo.Defaults.normalizeMomentsTab(override ?: defaultTab)
            }
            .distinctUntilChanged()
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000L),
                initialValue = PreferencesRepo.Defaults.MOMENTS_DEFAULT_TAB,
            )

    private val storyCutoffMillis: StateFlow<Long> =
        prefs
            .storiesWindowHours()
            .map { hours -> storyCutoffMillis(hours) }
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000L),
                initialValue = storyCutoffMillis(PreferencesRepo.Defaults.STORIES_WINDOW_HOURS),
            )

    private val storyStatusRows: StateFlow<List<StoryChannelItem>> =
        storyCutoffMillis
            .flatMapLatest { cutoff -> db.momentReadDao().storyStatusesFlow(cutoff) }
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000L),
                initialValue = emptyList(),
            )

    private val storyStatusByChannel: StateFlow<Map<String, StoryChannelItem>> =
        storyStatusRows
            .map { rows -> rows.associateBy { it.channelId } }
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000L),
                initialValue = emptyMap(),
            )

    val storyChannels: StateFlow<List<StoryChannelUiItem>> =
        storyCutoffMillis
            .flatMapLatest { cutoff -> db.momentReadDao().storyChannelsFlow(cutoff) }
            .map { rows -> rows.map(::toStoryChannelUiItem) }
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000L),
                initialValue = emptyList(),
            )

    /** Grid rows and their cursor switch together, with a loading boundary for each new tab. */
    private val gridScopeSnapshot: StateFlow<ScopedShortsSnapshot<DbMomentItem>> =
        scopedShortsSnapshotFlow(
            activeTab = activeTab,
            rowsForScope = { scope ->
                when (scope) {
                    "stories" -> flowOf(emptyList())
                    "following" -> db.momentReadDao().momentsFollowingFlow()
                    else -> db.momentReadDao().momentsAllFlow()
                }
            },
            cursorForScope = { scope ->
                if (scope == "stories") flowOf(null) else resolvedMomentsCursorFlow(scope)
            },
            startItem = { row, scope ->
                ShortsStartItem(
                    videoId = row.video.videoId,
                    sortAtMs = momentSortAtMs(row),
                    orderPosition = momentOrderPosition(row, scope),
                )
            },
            scopeForTab = { tab -> PreferencesRepo.Defaults.normalizeMomentsTab(tab) },
        )
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000L),
                initialValue =
                    ScopedShortsSnapshot(
                        scope = PreferencesRepo.Defaults.MOMENTS_DEFAULT_TAB,
                        rows = null,
                        selection = VisibleShortsSelection(videoId = null, index = 0),
                    ),
            )

    /**
     * Player rows deliberately ignore `moment_views`, because the player writes a view row on every
     * swipe. It still observes `videos` and `channels`, so new shorts, prunes, and channel/unfollow
     * effects continue to update the player.
     */
    private val playerScopeSnapshot: StateFlow<ScopedShortsSnapshot<DbMomentItem>> =
        scopedShortsSnapshotFlow(
            activeTab = activeTab,
            rowsForScope = { scope ->
                if (scope == "following") {
                    db.momentReadDao().playerMomentsFollowingFlow()
                } else {
                    db.momentReadDao().playerMomentsAllFlow()
                }
            },
            cursorForScope = ::resolvedMomentsCursorFlow,
            startItem = { row, scope ->
                ShortsStartItem(
                    videoId = row.video.videoId,
                    sortAtMs = momentSortAtMs(row),
                    orderPosition = momentOrderPosition(row, scope),
                )
            },
            selectionOverride = sessionVideoId,
        )
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.Eagerly,
                initialValue =
                    ScopedShortsSnapshot(
                        scope = momentsPlayerScope(PreferencesRepo.Defaults.MOMENTS_DEFAULT_TAB),
                        rows = null,
                        selection = VisibleShortsSelection(videoId = null, index = 0),
                    ),
            )

    /** One atomic grid presentation; stale cards are never clickable under a new tab label. */
    internal val gridRouteState: StateFlow<MomentsGridRouteState> =
        combine(gridScopeSnapshot, storyChannels) { snapshot, stories ->
            momentsGridRouteState(snapshot, stories) { rows ->
                rows.map(::toMomentThumbnailItem)
            }
        }
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.WhileSubscribed(5_000L),
                initialValue =
                    MomentsGridRouteState(
                        scope = PreferencesRepo.Defaults.MOMENTS_DEFAULT_TAB,
                        items = emptyList(),
                        startIndex = 0,
                        storyChannels = emptyList(),
                        uiState = UiState.Loading,
                    ),
            )

    /**
     * One atomic player presentation. Scope, rows, cursor selection, and loading state move
     * together so tab changes cannot pair a new playlist with the previous scope's selection.
     */
    internal val playerRouteState: StateFlow<MomentsPlayerRouteState> =
        combine(playerScopeSnapshot, storyStatusByChannel) { snapshot, storyStatuses ->
            momentsPlayerRouteState(snapshot) { rows ->
                rows.map { row -> toPlayerMomentItem(row, storyStatuses, snapshot.scope) }
            }
        }
            .stateIn(
                scope = viewModelScope,
                started = SharingStarted.Eagerly,
                initialValue =
                    MomentsPlayerRouteState(
                        scope = momentsPlayerScope(PreferencesRepo.Defaults.MOMENTS_DEFAULT_TAB),
                        items = emptyList(),
                        selection = VisibleShortsSelection(videoId = null, index = 0),
                        uiState = UiState.Loading,
                    ),
            )

    private fun resolvedMomentsCursorFlow(
        cursorScope: String,
    ): Flow<MomentsCursorEntity?> =
        db.momentsCursorDao()
            .flow(cursorScope)
            .flatMapLatest { cursor ->
                val cursorVideoId = cursor?.videoId?.trim().orEmpty()
                if (cursor == null || cursor.sortAtMs > 0L || cursorVideoId.isEmpty()) {
                    flowOf(cursor)
                } else {
                    db.momentReadDao()
                        .momentSortAtFlow(cursorVideoId, cursorScope)
                        .map { currentSortAtMs ->
                            cursor.copy(
                                sortAtMs = currentSortAtMs?.takeIf { it > 0L } ?: 0L,
                            )
                        }
                }
            }

    /** Global moments/bookmarks playback toggles from Preferences. */
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

    private val _isRefreshing = MutableStateFlow(false)
    val isRefreshing: StateFlow<Boolean> = _isRefreshing.asStateFlow()

    /** Non-null when the bookmark sheet is open for the carried target. */
    private val _pendingBookmark = MutableStateFlow<BookmarkTarget?>(null)
    val pendingBookmark: StateFlow<BookmarkTarget?> = _pendingBookmark.asStateFlow()
    private val _pendingMomentActions = MutableStateFlow<PlayerMomentItem?>(null)
    val pendingMomentActions: StateFlow<PlayerMomentItem?> = _pendingMomentActions.asStateFlow()

    /** Category chip rows — same stream FeedViewModel uses. */
    val bookmarkCategories: StateFlow<List<BookmarkCategoryDisplay>> =
        db.bookmarkCategoryDao()
            .allFlow()
            .map { entities -> entities.map { BookmarkCategoryDisplay(it.categoryId, it.name) } }
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000L), emptyList())

    /** Settled page changed — record the cursor so moments resume at the selected short. */
    fun onIndexChange(item: PlayerMomentItem) {
        val routeState = playerRouteState.value
        val requestedScope = momentsPlayerScope(sessionTabOverride.value ?: activeTab.value)
        if (routeState.scope != requestedScope) return
        if (routeState.items.none { current -> current.videoId == item.videoId }) return
        _sessionVideoId.value = item.videoId
        viewModelScope.launchMomentsCursorWrite(
            outboxWriter = outboxWriter,
            videoId = item.videoId,
            sortAtMs = item.sortAtMs.takeIf { it > 0L } ?: item.publishedAt,
            orderPosition = item.orderPosition,
            activeTab = routeState.scope,
        )
    }

    fun selectGridMoment(videoId: String): Boolean {
        val snapshot = gridScopeSnapshot.value
        val row = snapshot.rows?.firstOrNull { item -> item.video.videoId == videoId } ?: return false
        if (snapshot.scope == "stories") return false

        _sessionVideoId.value = videoId
        val cursorScope = snapshot.scope
        val sortAtMs = momentSortAtMs(row)
        val orderPosition = momentOrderPosition(row, cursorScope)
        viewModelScope.launch {
            yield()
            outboxWriter.recordMomentsCursor(
                videoId = videoId,
                positionMs = 0L,
                scope = cursorScope,
                sortAtMs = sortAtMs,
                orderPosition = orderPosition,
            )
        }
        return true
    }

    /** One-per-video FIFO log of "this was shown on screen". */
    fun onViewEvent(videoId: String) {
        viewModelScope.launch { outboxWriter.enqueue(OutboxKind.MomentView(videoId = videoId)) }
    }

    fun setAutoplayEnabled(enabled: Boolean) {
        viewModelScope.launch { prefs.setAutoplay(enabled) }
    }

    fun setMuted(enabled: Boolean) {
        viewModelScope.launch { prefs.setMuteDefault(enabled) }
    }

    fun setActiveTab(tab: String) {
        _sessionVideoId.value = null
        sessionTabOverride.value = PreferencesRepo.Defaults.normalizeMomentsTab(tab)
    }

    /**
     * Direct bookmark toggle — used when the row is already bookmarked (tap clears it) or from the
     * pager-level `onBookmarkToggle` hook. New-bookmark flow goes through [requestBookmarkSheet] so
     * the user can pick a category.
     */
    fun toggleBookmark(item: PlayerMomentItem) {
        val action = if (item.isBookmarked) OutboxKind.Action.Clear else OutboxKind.Action.Set
        viewModelScope.launch {
            outboxWriter.enqueue(OutboxKind.Bookmark(videoId = item.videoId, action = action))
        }
    }

    /**
     * User tapped bookmark on a not-yet-bookmarked moment — open the bookmark sheet so they can
     * pick a category + label before saving.
     */
    fun requestBookmarkSheet(item: PlayerMomentItem) {
        viewModelScope.launch {
            _pendingBookmark.value =
                bookmarkTargetForMoment(
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
                )
            )
        }
    }

    fun removePendingBookmark() {
        val target = _pendingBookmark.value ?: return
        _pendingBookmark.value = null
        viewModelScope.launch {
            outboxWriter.enqueue(
                OutboxKind.Bookmark(videoId = target.itemId, action = OutboxKind.Action.Clear)
            )
        }
    }

    private fun bookmarkTargetForMoment(
        item: PlayerMomentItem,
        currentBookmark: BookmarkState? = null,
    ): BookmarkTarget =
        BookmarkTarget(
            itemId = item.videoId,
            authorHandle = item.authorHandle,
            // Moments are single-media video posts; the multi-image picker row
            // is hidden when mediaCount <= 1.
            mediaCount = 0,
            currentBookmark = currentBookmark,
            defaultTitle = item.description.lineSequence().firstOrNull(),
            bodyText = item.description,
        )

    fun createCategory(name: String) {
        viewModelScope.launch {
            val provisionalId = -System.currentTimeMillis()
            outboxWriter.enqueue(
                OutboxKind.CreateCategory(name = name, provisionalId = provisionalId)
            )
        }
    }

    fun followChannel(channelId: String) {
        viewModelScope.launch {
            outboxWriter.enqueue(
                OutboxKind.Follow(channelId = channelId, action = OutboxKind.Action.Set)
            )
        }
    }

    fun unfollowChannel(channelId: String) {
        viewModelScope.launch {
            outboxWriter.enqueue(
                OutboxKind.Follow(channelId = channelId, action = OutboxKind.Action.Clear)
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

    /** Pull-to-refresh — kicks the shorts sync stream and holds the spinner briefly. */
    fun refresh() {
        viewModelScope.launch {
            _isRefreshing.value = true
            scheduler.triggerAll()
            delay(1_000L)
            _isRefreshing.value = false
        }
    }

    fun notifyUpToDate() {
        viewModelScope.launch { uiEffects.emit(UiEffect.ToastRes(R.string.status_up_to_date)) }
    }

    fun resolveMentionAndNavigate(handle: String) {
        viewModelScope.launch {
            val route =
                ChannelRouteResolver.routeForHandle(
                    db = db,
                    rawHandle = handle,
                    fallbackPlatform = "tiktok",
                )
            uiEffects.emit(UiEffect.NavigateTo(route))
        }
    }

    private fun toStoryChannelUiItem(row: StoryChannelItem): StoryChannelUiItem {
        val handle =
            row.channelSourceId?.takeIf { it.isNotBlank() } ?: stripPlatformPrefix(row.channelId)
        return StoryChannelUiItem(
            channelId = row.channelId,
            displayName =
                row.channelName?.takeIf { it.isNotBlank() } ?: handle.ifBlank { row.channelId },
            handle = if (handle.isNotBlank()) "@$handle" else "",
            count = row.storyCount,
            unseenCount = row.unseenCount,
            latestAtMs = row.latestAtMs,
            firstVideoId = row.firstVideoId,
            firstUnseenVideoId = row.firstUnseenVideoId,
            ringState = row.storyRingState(),
        )
    }

    private fun storyCutoffMillis(hours: Int): Long =
        System.currentTimeMillis() -
            PreferencesRepo.Defaults.normalizeStoriesWindowHours(hours) * 3_600_000L

    private fun StoryChannelItem?.storyRingState(): StoryRingState =
        storyRingState(this?.storyCount ?: 0, this?.unseenCount ?: 0)

    private fun StoryChannelItem.startVideoId(): String =
        firstUnseenVideoId.takeIf { it.isNotBlank() } ?: firstVideoId

    private fun momentHandle(channelSourceId: String?, channelId: String): String =
        channelSourceId?.takeIf { it.isNotBlank() } ?: stripPlatformPrefix(channelId)

    private fun toMomentThumbnailItem(row: DbMomentItem): MomentThumbnailItem {
        val video = row.video
        val handle = momentHandle(row.channelSourceId, video.channelId)
        return MomentThumbnailItem(
            videoId = video.videoId,
            channelId = video.channelId,
            ownerKind = ownerKindFromAssetOwnerKind(video.ownerKind),
            mediaKind = video.mediaKind,
            slideCount = video.slideCount,
            durationMs = video.durationMs(),
            publishedAt = video.publishedAt,
            isViewed = row.isViewed == 1,
            authorDisplayName = row.channelName?.takeIf { it.isNotBlank() },
            authorHandle = if (handle.isNotBlank()) "@$handle" else "",
        )
    }

    private fun toPlayerMomentItem(
        row: DbMomentItem,
        storyStatuses: Map<String, StoryChannelItem>,
        scope: String,
    ): PlayerMomentItem {
        val video = row.video
        val handle = momentHandle(row.channelSourceId, video.channelId)
        val storyStatus = storyStatuses[video.channelId]
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
            sortAtMs = momentSortAtMs(row),
            orderPosition = momentOrderPosition(row, scope),
            storyRingState = storyStatus.storyRingState(),
            storyFirstVideoId = storyStatus?.startVideoId().orEmpty(),
        )
    }

    private fun momentSortAtMs(row: DbMomentItem): Long =
        row.effectiveMomentAtMs.takeIf { it > 0L } ?: row.video.publishedAt

    private fun momentOrderPosition(row: DbMomentItem, scope: String): Long =
        if (scope == "following") row.video.momentsFollowingPosition else row.video.momentsAllPosition

    private fun repostMeta(row: DbMomentItem): RepostMeta? {
        if (row.repostIntroduced != 1) return null
        return RepostMeta(
            authorLabel = row.repostAuthorLabel?.takeIf { it.isNotBlank() } ?: return null,
            otherCount = (row.repostCount - 1).coerceAtLeast(0),
        )
    }
}

/** Captures the current tab before dispatch so a later tab switch cannot redirect this write. */
internal fun CoroutineScope.launchMomentsCursorWrite(
    outboxWriter: OutboxWriter,
    videoId: String,
    sortAtMs: Long,
    activeTab: String,
    orderPosition: Long = 0,
): Job {
    val cursorScope = PreferencesRepo.Defaults.normalizeMomentsTab(activeTab)
    return launch {
        outboxWriter.recordMomentsCursor(videoId, 0L, cursorScope, sortAtMs, orderPosition)
    }
}
