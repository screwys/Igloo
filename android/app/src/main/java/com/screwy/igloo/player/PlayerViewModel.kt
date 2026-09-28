package com.screwy.igloo.player

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.screwy.igloo.channel.ChannelRouteResolver
import com.screwy.igloo.data.IglooDatabase
import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.data.entity.ChannelEntity
import com.screwy.igloo.data.entity.SponsorBlockSegmentEntity
import com.screwy.igloo.data.entity.VideoCommentEntity
import com.screwy.igloo.data.entity.VideoEntity
import com.screwy.igloo.data.entity.WatchHistoryEntity
import com.screwy.igloo.media.MediaResolvers
import com.screwy.igloo.media.MediaUri
import com.screwy.igloo.media.OwnerKind
import com.screwy.igloo.sync.SyncCoordinator
import com.screwy.igloo.ui.UiEffect
import com.screwy.igloo.ui.UiEffects
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/**
 * Long-form YouTube player state holder.
 *
 * State flows (all Room-backed so the UI re-renders on any write without extra
 * glue):
 *  - [video] — the `videos` row for this id.
 *  - [channel] — the owning channel (for description header).
 *  - [comments] — `video_comments` rows, server presentation order.
 *  - [segments] — SponsorBlock segments for the scrubber overlay.
 *  - [subtitlePath] — local VTT path resolved from the retained subtitle row.
 *    Nice-to-have — nulls when no verified local subtitle is available yet.
 *  - [streamUri] — the playable URI, local if cached else remote (resolver rules).
 *  - [watchHistory] — resume position + duration for the last-known sync.
 *
 * PlaybackService saves playback progress through the outbox.
 */
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
class PlayerViewModel(
    private val videoId: String,
    private val db: IglooDatabase,
    private val prefs: PreferencesRepo,
    private val scheduler: SyncCoordinator,
    private val uiEffects: UiEffects,
    private val resolvers: MediaResolvers,
) : ViewModel() {

    private val _isRefreshingComments = MutableStateFlow(false)
    val isRefreshingComments: StateFlow<Boolean> = _isRefreshingComments.asStateFlow()

    /** Current DeArrow mode — drives title + thumbnail resolver at render sites. */
    val dearrowMode: StateFlow<String> = prefs.dearrowMode().stateIn(
        scope = viewModelScope,
        started = SharingStarted.WhileSubscribed(5_000L),
        initialValue = PreferencesRepo.Defaults.DEARROW_MODE,
    )

    /** The `videos` row. Null until Room's first emission. */
    val video: StateFlow<VideoEntity?> = db.videoDao()
        .getByIdFlow(videoId)
        .stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000L),
            initialValue = null,
        )

    /**
     * Owning channel row — the route renders its `name` in the description block
     * and uses `channelId` for the "tap author → channel" nav. Flipped through
     * `flatMapLatest` so we stay reactive to video row changes (e.g., re-sync
     * flipping channel_id would rebind the channel flow).
     */
    val channel: StateFlow<ChannelEntity?> = video
        .flatMapLatest { v ->
            if (v == null) flow<ChannelEntity?> { emit(null) }
            else db.channelDao().getByIdFlow(v.channelId)
        }
        .stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000L),
            initialValue = null,
        )

    /** Comments bundled with the mirrored video. */
    val comments: StateFlow<List<VideoCommentEntity>> = db.videoCommentDao()
        .forVideoFlow(videoId)
        .stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000L),
            initialValue = emptyList(),
        )

    /** SponsorBlock segments for scrubber painting + skip-to-end taps. */
    val segments: StateFlow<List<SponsorBlockSegmentEntity>> = db.sponsorBlockSegmentDao()
        .forVideoFlow(videoId)
        .stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000L),
            initialValue = emptyList(),
        )

    /** Local VTT path for the subtitle overlay. Prefer current Sync assets. */
    val subtitlePath: StateFlow<String?> = localAssetPathFlow("subtitle")

    val previewSpritePath: StateFlow<String?> = localAssetPathFlow("preview_sprite")

    val previewTrackJsonPath: StateFlow<String?> = localAssetPathFlow("preview_track_json")

    val subtitleIsAuto: StateFlow<Boolean?> = db.androidSyncDao()
		.assetsForOwnerFlow("youtube_video", videoId)
        .map { rows -> rows.firstOrNull { it.assetKind == "subtitle" }?.subtitleIsAuto }
        .stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000L),
            initialValue = null,
        )

    /**
     * Playable URI. Re-resolved when Sync or the retained inventory fallback changes.
     */
    val streamUri: StateFlow<MediaUri> = resolvers.videoStreamFlow(videoId, OwnerKind.YouTubeVideo)
        .stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000L),
            initialValue = MediaUri.Missing,
        )

    val thumbnailUri: StateFlow<MediaUri> = resolvers.thumbnailForPostFlow(videoId, OwnerKind.YouTubeVideo)
        .stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000L),
            initialValue = MediaUri.Missing,
        )

    /** Watch-history for resume-position (in seconds per the server contract). */
    val watchHistory: StateFlow<WatchHistoryEntity?> = db.watchHistoryDao()
        .getByIdFlow(videoId)
        .stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000L),
            initialValue = null,
        )

    fun refreshComments() {
        viewModelScope.launch {
            _isRefreshingComments.value = true
            scheduler.triggerAll()
            delay(1_000L)
            _isRefreshingComments.value = false
        }
    }

    fun resolveMentionAndNavigate(handle: String) {
        viewModelScope.launch {
            val route = ChannelRouteResolver.routeForHandle(
                db = db,
                rawHandle = handle,
                fallbackPlatform = "youtube",
            )
            uiEffects.emit(UiEffect.NavigateTo(route))
        }
    }

    private fun localAssetPathFlow(assetKind: String): StateFlow<String?> =
		db.androidSyncDao().assetsForOwnerFlow("youtube_video", videoId)
			.map { rows ->
				rows.firstOrNull { it.assetKind == assetKind }
					?.takeIf { !it.localPath.isNullOrBlank() }
					?.localPath
					?.takeIf { it.isNotBlank() }
            }
            .stateIn(
            scope = viewModelScope,
            started = SharingStarted.WhileSubscribed(5_000L),
            initialValue = null,
        )
}
