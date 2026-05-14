package com.screwy.igloo.ui.component

import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.BrokenImage
import androidx.compose.material3.Icon
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.media3.exoplayer.ExoPlayer
import coil3.compose.AsyncImage
import com.screwy.igloo.R
import com.screwy.igloo.data.dao.AndroidSyncDao
import com.screwy.igloo.data.dao.MediaInventoryDao
import com.screwy.igloo.data.entity.AndroidSyncAssetEntity
import com.screwy.igloo.data.entity.MediaInventoryEntity
import com.screwy.igloo.media.MediaUri
import com.screwy.igloo.net.ServerBaseUrlProvider
import java.io.File
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import org.koin.compose.koinInject

@Composable
internal fun MomentImageSurface(
    videoId: String,
    thumbnailUri: MediaUri,
    isActive: Boolean,
    autoSwipe: Boolean,
    onAutoAdvance: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val baseUrlProvider: ServerBaseUrlProvider = koinInject()
    val mediaInventoryDao: MediaInventoryDao = koinInject()
    val syncDao: AndroidSyncDao = koinInject()
    val baseUrl = baseUrlProvider.baseUrl()
    val slideUris by momentSlideMediaFlow(
        mediaInventoryDao = mediaInventoryDao,
        syncDao = syncDao,
        baseUrl = baseUrl,
        videoId = videoId,
        fallbackSlideCount = 1,
    ).collectAsState(initial = emptyList())

    MomentStillImage(
        mediaUri = slideUris.firstOrNull() ?: thumbnailUri,
        contentDescription = stringResource(R.string.content_description_moment_image),
        modifier = modifier,
    )

    LaunchedEffect(videoId, isActive, autoSwipe) {
        if (!isActive || !autoSwipe) return@LaunchedEffect
        delay(MOMENT_STILL_ADVANCE_DELAY_MS)
        onAutoAdvance()
    }
}

@Composable
internal fun MomentSlideshowSurface(
    videoId: String,
    slideCount: Int,
    thumbnailUri: MediaUri,
    isActive: Boolean,
    autoSwipe: Boolean,
    onAutoAdvance: () -> Unit,
    manualAdvanceTick: Int = 0,
    onManualAdvanceAtEnd: () -> Unit = {},
    modifier: Modifier = Modifier,
) {
    val baseUrlProvider: ServerBaseUrlProvider = koinInject()
    val mediaInventoryDao: MediaInventoryDao = koinInject()
    val syncDao: AndroidSyncDao = koinInject()
    val baseUrl = baseUrlProvider.baseUrl()
    val slideUris by momentSlideMediaFlow(
        mediaInventoryDao = mediaInventoryDao,
        syncDao = syncDao,
        baseUrl = baseUrl,
        videoId = videoId,
        fallbackSlideCount = slideCount,
    ).collectAsState(initial = emptyList())
    val effectiveSlideUris = remember(slideUris, thumbnailUri) {
        if (slideUris.isNotEmpty()) slideUris else listOf(thumbnailUri)
    }
    val effectiveSlideCount = effectiveSlideUris.size.coerceAtLeast(1)
    val pagerState = rememberPagerState(pageCount = { effectiveSlideCount })

    LaunchedEffect(manualAdvanceTick, effectiveSlideCount) {
        if (manualAdvanceTick == 0 || effectiveSlideCount <= 0) return@LaunchedEffect
        val page = pagerState.currentPage
        if (page < effectiveSlideCount - 1) {
            pagerState.animateScrollToPage(
                page = page + 1,
                animationSpec = tween(
                    durationMillis = AUTO_SWIPE_SCROLL_DURATION_MS,
                    easing = FastOutSlowInEasing,
                ),
            )
        } else {
            onManualAdvanceAtEnd()
        }
    }

    Box(
        modifier = modifier
            .fillMaxSize()
            .background(Color.Black),
    ) {
        if (effectiveSlideUris.isNotEmpty()) {
            HorizontalPager(
                state = pagerState,
                modifier = Modifier.fillMaxSize(),
            ) { page ->
                MomentStillImage(
                    mediaUri = effectiveSlideUris[page],
                    contentDescription = stringResource(R.string.content_description_slide_number, page + 1),
                    modifier = Modifier.fillMaxSize(),
                )
            }
        } else {
            MomentStillImage(
                mediaUri = thumbnailUri,
                contentDescription = stringResource(R.string.content_description_slide_number, 1),
                modifier = Modifier.fillMaxSize(),
            )
        }

        if (effectiveSlideCount > 1) {
            MomentSlideDots(
                currentPage = pagerState.currentPage,
                pageCount = effectiveSlideCount,
                modifier = Modifier
                    .align(Alignment.BottomCenter)
                    .padding(bottom = 96.dp),
            )
        }
    }

    LaunchedEffect(videoId, slideCount, isActive, autoSwipe, effectiveSlideCount) {
        if (!isActive || effectiveSlideCount <= 0) return@LaunchedEffect
        while (true) {
            val pageAtStart = pagerState.currentPage
            delay(MOMENT_SLIDESHOW_ADVANCE_DELAY_MS)
            if (!isActive) return@LaunchedEffect
            if (pagerState.isScrollInProgress || pagerState.currentPage != pageAtStart) {
                continue
            }

            if (pageAtStart < effectiveSlideCount - 1) {
                pagerState.animateScrollToPage(
                    page = pageAtStart + 1,
                    animationSpec = tween(
                        durationMillis = AUTO_SWIPE_SCROLL_DURATION_MS,
                        easing = FastOutSlowInEasing,
                    ),
                )
            } else if (autoSwipe) {
                onAutoAdvance()
                return@LaunchedEffect
            } else {
                pagerState.animateScrollToPage(
                    page = 0,
                    animationSpec = tween(
                        durationMillis = AUTO_SWIPE_SCROLL_DURATION_MS,
                        easing = FastOutSlowInEasing,
                    ),
                )
            }
        }
    }
}

@Composable
private fun MomentStillImage(
    mediaUri: MediaUri,
    contentDescription: String,
    modifier: Modifier = Modifier,
) {
    Box(
        modifier = modifier
            .fillMaxSize()
            .background(Color.Black),
        contentAlignment = Alignment.Center,
    ) {
        when (mediaUri) {
            is MediaUri.Local -> AsyncImage(
                model = mediaUri.file,
                contentDescription = contentDescription,
                modifier = Modifier.fillMaxSize(),
                contentScale = momentFitWidthContentScale(),
            )
            is MediaUri.Remote -> AsyncImage(
                model = rememberRemoteImageModel(mediaUri.url),
                contentDescription = contentDescription,
                modifier = Modifier.fillMaxSize(),
                contentScale = momentFitWidthContentScale(),
            )
            MediaUri.Missing -> Icon(
                imageVector = Icons.Default.BrokenImage,
                contentDescription = stringResource(R.string.content_description_missing_media),
                tint = Color.White.copy(alpha = 0.70f),
                modifier = Modifier.size(40.dp),
            )
        }
    }
}

private fun momentSlideMediaFlow(
    mediaInventoryDao: MediaInventoryDao,
    syncDao: AndroidSyncDao,
    baseUrl: String,
    videoId: String,
    fallbackSlideCount: Int,
) = combine(
    mediaInventoryDao.forOwnerFlow(videoId),
    syncDao.latestVerifiedAssetsForOwnerFlow(videoId, listOf("post_media")),
) { rows, syncRows ->
    resolveMomentSlideUris(
        rows = rows,
        baseUrl = baseUrl,
        videoId = videoId,
        fallbackSlideCount = fallbackSlideCount,
        syncRows = syncRows,
    )
}
    .distinctUntilChanged()

internal fun momentAudioUriFlow(
    mediaInventoryDao: MediaInventoryDao,
    syncDao: AndroidSyncDao,
    baseUrl: String,
    videoId: String,
) = combine(
    mediaInventoryDao.forOwnerFlow(videoId),
    syncDao.latestVerifiedAssetsForOwnerFlow(videoId, listOf("post_audio", "audio")),
) { rows, syncRows ->
    resolveMomentAudioUri(
        rows = rows,
        baseUrl = baseUrl,
        videoId = videoId,
        syncRows = syncRows,
    )
}
    .distinctUntilChanged()

internal fun resolveMomentSlideUris(
    rows: List<MediaInventoryEntity>,
    baseUrl: String,
    videoId: String,
    fallbackSlideCount: Int,
    syncRows: List<AndroidSyncAssetEntity> = emptyList(),
): List<MediaUri> {
    val syncSlideRows = syncRows
        .asSequence()
        .filter(::isMomentSyncSlideAsset)
        .sortedBy(::momentSyncSlideIndex)
        .toList()
    if (syncSlideRows.isNotEmpty()) {
        return syncSlideRows.map { row -> momentSyncAssetToMediaUri(row, baseUrl) }
    }

    val slideRows = rows
        .asSequence()
        .filter { row ->
            row.assetKind == "post_media" && row.serverUrl.contains("/api/media/slide/")
        }
        .sortedBy(::momentSlideIndex)
        .toList()
    if (slideRows.isNotEmpty()) {
        return slideRows.map { row -> momentInventoryRowToMediaUri(row, baseUrl) }
    }

    val fallbackCount = fallbackSlideCount.coerceAtLeast(0)
    if (fallbackCount == 0) return emptyList()
    return List(fallbackCount) { index ->
        momentSlideUrl(baseUrl, videoId, index)
            ?.let(MediaUri::Remote)
            ?: MediaUri.Missing
    }
}

internal fun resolveMomentAudioUri(
    rows: List<MediaInventoryEntity>,
    baseUrl: String,
    videoId: String,
    syncRows: List<AndroidSyncAssetEntity> = emptyList(),
): MediaUri {
    val syncAudioRow = syncRows.firstOrNull { row ->
        row.assetKind == "audio" ||
            row.assetKind == "post_audio" ||
            row.serverUrl.contains("/api/media/audio/")
    }
    if (syncAudioRow != null) {
        return momentSyncAssetToMediaUri(syncAudioRow, baseUrl)
    }

    val audioRow = rows.firstOrNull { row ->
        row.assetKind == "audio" ||
            row.assetKind == "post_audio" ||
            row.serverUrl.contains("/api/media/audio/")
    }
    if (audioRow != null) {
        return momentInventoryRowToMediaUri(audioRow, baseUrl)
    }
    return momentAudioUrl(baseUrl, videoId)
        ?.let(MediaUri::Remote)
        ?: MediaUri.Missing
}

private fun momentSlideIndex(row: MediaInventoryEntity): Int =
    row.serverUrl.substringAfterLast('/').toIntOrNull()
        ?: row.assetId.substringAfterLast('_').toIntOrNull()
        ?: Int.MAX_VALUE

private fun isMomentSyncSlideAsset(row: AndroidSyncAssetEntity): Boolean {
    if (row.assetKind != "post_media") return false
    val contentType = row.contentType?.trim()?.lowercase().orEmpty()
    return contentType.isBlank() || contentType.startsWith("image/")
}

private fun momentSyncSlideIndex(row: AndroidSyncAssetEntity): Int =
    row.mediaIndex

internal fun momentInventoryRowToMediaUri(
    row: MediaInventoryEntity,
    baseUrl: String,
): MediaUri {
    if (row.state == "cached" && !row.localPath.isNullOrBlank()) {
        val file = File(row.localPath)
        if (file.exists()) return MediaUri.Local(file)
    }

    val root = baseUrl.trim().trimEnd('/')
    if (root.isBlank()) return MediaUri.Missing
    return MediaUri.Remote(root + row.serverUrl)
}

private fun momentSyncAssetToMediaUri(
    row: AndroidSyncAssetEntity,
    @Suppress("UNUSED_PARAMETER") baseUrl: String,
): MediaUri {
    if (row.state == "verified" && !row.localPath.isNullOrBlank()) {
        val file = File(row.localPath)
        if (file.exists()) return MediaUri.Local(file)
    }

    return MediaUri.Missing
}

internal fun prepareMomentAudio(
    player: ExoPlayer,
    loadedKey: String?,
    videoId: String,
    audioUri: MediaUri,
): String? {
    val targetKey = momentAudioLoadKey(videoId, audioUri) ?: return clearMomentAudio(player)
    if (loadedKey == targetKey) return loadedKey

    when (audioUri) {
        is MediaUri.Local -> player.setMediaItem(MediaItem.fromUri(audioUri.file.toURI().toString()))
        is MediaUri.Remote -> player.setMediaItem(MediaItem.fromUri(audioUri.url))
        MediaUri.Missing -> return clearMomentAudio(player)
    }
    player.repeatMode = Player.REPEAT_MODE_ONE
    player.prepare()
    return targetKey
}

private fun momentAudioLoadKey(videoId: String, audioUri: MediaUri): String? = when (audioUri) {
    is MediaUri.Local -> "local:$videoId:${audioUri.file.absolutePath}"
    is MediaUri.Remote -> "remote:$videoId:${audioUri.url}"
    MediaUri.Missing -> null
}

internal fun clearMomentAudio(player: ExoPlayer): String? {
    player.playWhenReady = false
    player.pause()
    player.clearMediaItems()
    return null
}
