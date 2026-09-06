package com.screwy.igloo.ui.component

import android.content.Context
import android.view.View
import androidx.test.core.app.ApplicationProvider
import android.graphics.Bitmap
import android.graphics.Rect
import android.text.style.ClickableSpan
import android.widget.ImageView
import com.screwy.igloo.data.entity.FeedItemEntity
import com.screwy.igloo.data.entity.OutboxEntity
import com.screwy.igloo.data.entity.ThreadedFeedRow
import com.screwy.igloo.feed.FeedMediaCellDescriptor
import com.screwy.igloo.feed.FeedMediaCellModel
import com.screwy.igloo.media.MediaUri
import com.screwy.igloo.outbox.OutboxKind
import com.screwy.igloo.outbox.pendingFeedActionOverrides
import java.io.File
import java.io.FileOutputStream
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], manifest = Config.NONE)
class NativeMainFeedSurfaceTest {
    @Test
    fun nativeFeedPrimaryActionsPutThreadBeforeSavedActions() {
        assertEquals(
            listOf(
                NativeFeedPrimaryAction.Reply,
                NativeFeedPrimaryAction.Like,
                NativeFeedPrimaryAction.Bookmark,
                NativeFeedPrimaryAction.Share,
                NativeFeedPrimaryAction.External,
            ),
            NativeFeedPrimaryActions,
        )
    }

    @Test
    fun accountBadgeStaysBesideHandleAndLeavesRoomForFollow() {
        val header = NativeIdentityHeaderViews(ApplicationProvider.getApplicationContext<Context>())
        header.accountBadge.text = "🇺🇸 📱 ⓘ"
        header.accountBadge.visibility = View.VISIBLE
        header.follow.text = "Follow"
        header.date.text = "· 2h"
        fun layout(name: String) {
            header.name.text = name
            header.meta.text = name
            header.root.measure(View.MeasureSpec.makeMeasureSpec(500, View.MeasureSpec.EXACTLY),
                View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED))
            header.root.layout(0, 0, 500, header.root.measuredHeight)
        }
        layout("Sample")
        assertEquals(header.meta.right + dp(6), header.accountBadge.left)
        assertTrue(header.follow.right <= (header.follow.parent as View).width)
        layout("A very long display name ".repeat(20))
        assertEquals(header.meta.right + dp(6), header.accountBadge.left)
        assertTrue(header.date.right <= (header.date.parent as View).width)
        assertTrue(header.follow.right <= (header.follow.parent as View).width)
    }

    @Test
    fun nativeInlineVideoSelectorChoosesClosestVisibleCandidateDuringScroll() {
        val selected =
            chooseNativeInlineVideoCandidate(
                candidates =
                    listOf(
                        NativeInlineVideoCandidate(
                            key = "upper",
                            streamUri = MediaUri.Remote("https://example.test/upper.mp4"),
                            visibleFraction = 0.9f,
                            centerDistancePx = 480f,
                        ),
                        NativeInlineVideoCandidate(
                            key = "center",
                            streamUri = MediaUri.Remote("https://example.test/center.mp4"),
                            visibleFraction = 0.3f,
                            centerDistancePx = 10f,
                        ),
                    )
            )

        assertEquals("center", selected?.key)
    }

    @Test
    fun nativeInlineVideoSameUriHandoffDoesNotPrepareANewStream() {
        val uri = MediaUri.Remote("https://example.test/same.mp4")
        val selected =
            NativeInlineVideoCandidate(
                key = "new-holder",
                streamUri = uri,
                visibleFraction = 1f,
                centerDistancePx = 0f,
            )

        assertEquals(
            NativeInlineVideoSwitchDecision.HandoffSameStream,
            nativeInlineVideoSwitchDecision(
                activeKey = "old-holder",
                activeStreamUri = uri,
                selected = selected,
            ),
        )
    }

    @Test
    fun nativeStableAspectRatioPreservesKnownImageGeometry() {
        assertEquals(0.1f, nativeStableSingleMediaAspectRatio(cell(0.1f)), 0.0001f)
        assertEquals(1.25f, nativeStableSingleMediaAspectRatio(cell(1.25f)), 0.0001f)
        assertEquals(9f, nativeStableSingleMediaAspectRatio(cell(9f)), 0.0001f)
        assertEquals(1f, nativeStableSingleMediaAspectRatio(cell(1.7f, known = false)), 0.0001f)
        assertEquals(
            16f / 9f,
            nativeStableSingleMediaAspectRatio(cell(1f, known = false, isVideo = true)),
            0.0001f,
        )
    }

    @Test
    fun nativeSingleMediaDimensionsKeepWideQuoteImagesCompact() {
        val aspectRatio = 3.5f

        assertEquals(
            NativeMediaDimensions(widthPx = 1109, heightPx = 317),
            nativeSingleMediaDimensions(
                maxWidthPx = 1112,
                aspectRatio = aspectRatio,
                maxHeightPx = 1680,
            ),
        )
        assertEquals(
            NativeMediaDimensions(widthPx = 1064, heightPx = 304),
            nativeSingleMediaDimensions(
                maxWidthPx = 1064,
                aspectRatio = aspectRatio,
                maxHeightPx = 1680,
            ),
        )
        assertEquals(
            1064,
            nativeQuoteMediaGridWidthPx(mediaGridWidthPx = 1112, horizontalPaddingPx = 24),
        )
    }

    @Test
    fun nativeSingleMediaHeightLimitPreservesItsAspectRatio() {
        assertEquals(
            NativeMediaDimensions(widthPx = 130, heightPx = NativeSingleMediaMaxHeightDp),
            nativeSingleMediaDimensions(
                maxWidthPx = 400,
                aspectRatio = 0.25f,
                maxHeightPx = NativeSingleMediaMaxHeightDp,
            ),
        )
    }

    @Test
    fun nativeStableAspectRatioDoesNotProbeLocalImageDuringBind() {
        val image = File.createTempFile("igloo-feed-aspect", ".png").also { it.deleteOnExit() }
        val bitmap = Bitmap.createBitmap(220, 110, Bitmap.Config.ARGB_8888)
        FileOutputStream(image).use { output ->
            assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, output))
        }
        bitmap.recycle()

        val model =
            FeedMediaCellModel(
                descriptor = cell(1f, known = false),
                previewItem = MediaItem.Image(MediaUri.Local(image), aspectRatio = 1f),
            )

        assertEquals(1f, nativeStableSingleMediaAspectRatio(model), 0.0001f)
    }

    @Test
    fun nativeMediaScaleTypeUsesWebFeedRules() {
        assertEquals(
            ImageView.ScaleType.FIT_START,
            nativeMediaScaleTypeFor(cell(0.7f), isSingle = true),
        )
        assertEquals(ImageView.ScaleType.CENTER_CROP, nativeMediaScaleTypeFor(cell(1.25f)))
        assertEquals(
            ImageView.ScaleType.FIT_CENTER,
            nativeMediaScaleTypeFor(cell(1.25f), preserveFullImage = true),
        )
        assertEquals(
            ImageView.ScaleType.CENTER_CROP,
            nativeMediaScaleTypeFor(cell(1.25f, isVideo = true)),
        )
        assertEquals(
            ImageView.ScaleType.CENTER_CROP,
            nativeMediaScaleTypeFor(cell(1.25f, isVideo = true), isSingle = true),
        )
        assertEquals(
            ImageView.ScaleType.CENTER_CROP,
            nativeMediaScaleTypeFor(cell(1f, known = false), isSingle = true),
        )
    }

    @Test
    fun nativeTranslationPillMatchesWebTimestampContract() {
        val active =
            nativeTranslationPillFor(
                item =
                    FeedItemEntity(
                        tweetId = "tweet-1",
                        bodyText = "サンプル本文",
                        lang = "ja",
                        bodyTranslation = "hello",
                        bodySourceLang = "Japanese",
                    ),
                active = true,
                enabled = true,
            )
        val inactive =
            nativeTranslationPillFor(
                item =
                    FeedItemEntity(
                        tweetId = "tweet-2",
                        bodyText = "hola",
                        lang = "es",
                        bodySourceLang = "Spanish",
                    ),
                active = false,
                enabled = false,
            )
        val english =
            nativeTranslationPillFor(
                item = FeedItemEntity(tweetId = "tweet-3", bodyText = "hello", lang = "en"),
                active = false,
                enabled = false,
            )

        assertEquals(
            NativeTranslationPill(sourceLangLabel = "Japanese", active = true, enabled = true),
            active,
        )
        assertEquals(
            NativeTranslationPill(sourceLangLabel = "Spanish", active = false, enabled = false),
            inactive,
        )
        assertEquals(null, english)
    }

    @Test
    fun nativeTranslationPillsArePerTextField() {
        val englishBody =
            nativeTranslationPillForText(
                lang = "en",
                sourceLang = null,
                text = "plain parent text",
                active = false,
                enabled = false,
            )
        val translatedQuote =
            nativeTranslationPillForText(
                lang = "fr",
                sourceLang = "French",
                text = "texte cite",
                active = true,
                enabled = true,
            )

        assertEquals(null, englishBody)
        assertEquals(
            NativeTranslationPill(sourceLangLabel = "French", active = true, enabled = true),
            translatedQuote,
        )
    }

    @Test
    fun nativeTranslationPillUsesTranslatorLanguageLabel() {
        val translatedBody =
            nativeTranslationPillForText(
                lang = "kr",
                sourceLang = "Korean",
                text = "안녕하세요",
                active = true,
                enabled = true,
            )

        assertEquals(
            NativeTranslationPill(sourceLangLabel = "Korean", active = true, enabled = true),
            translatedBody,
        )
    }

    @Test
    fun nativeVisibleFractionUsesCurrentViewportWithoutSettleDelay() {
        assertEquals(1f, nativeVisibleHeightFraction(Rect(0, 100, 100, 300), 600), 0.0001f)
        assertEquals(0.5f, nativeVisibleHeightFraction(Rect(0, -100, 100, 100), 600), 0.0001f)
        assertEquals(0f, nativeVisibleHeightFraction(Rect(0, 700, 100, 900), 600), 0.0001f)
    }

    @Test
    fun nativeThreadCapsuleOnlyShowsWhenPreviewOmitsPosts() {
        assertFalse(nativeThreadCapsuleVisible(emptyList<String>()))
        assertFalse(nativeThreadCapsuleVisible(listOf("root")))
        assertTrue(nativeThreadCapsuleVisible(listOf("root", "parent")))
    }

    @Test
    fun nativeFeedScrollRestoreFindsTheSamePostAfterRecreation() {
        val items =
            listOf(
                NativeFeedAdapterItem.Post(threadedRow("tweet-1"), post("tweet-1")),
                NativeFeedAdapterItem.Post(threadedRow("tweet-2"), post("tweet-2")),
                NativeFeedAdapterItem.Post(threadedRow("tweet-3"), post("tweet-3")),
            )

        assertEquals(
            1,
            nativeFeedRestoreAdapterIndex(
                items,
                NativeFeedScrollAnchor(rowId = "tweet-2", offsetPx = -42),
            ),
        )
    }

    @Test
    fun nativeFeedMediaWindowKeepsTheExistingViewportMarginWithinTheFeed() {
        assertEquals(0..5, nativeFeedMediaWindow(0, 1, postCount = 200))
        assertEquals(38..54, nativeFeedMediaWindow(40, 50, postCount = 200))
        assertEquals(197..199, nativeFeedMediaWindow(199, 199, postCount = 200))
        assertEquals(null, nativeFeedMediaWindow(0, 0, postCount = 0))
    }

    @Test
    fun nativeMediaRowsPreserveProportionsAndOverflowInsteadOfShrinking() {
        assertEquals(
            NativeMediaDimensions(540, 360),
            nativeMultiMediaCellDimensions(List(4) { 1.5f }, 0, 360, maxHeightPx = 520),
        )
        assertEquals(
            NativeMediaDimensions(468, 520),
            nativeMultiMediaCellDimensions(listOf(0.3f, 0.9f, 0.3f), 1, 1000, maxHeightPx = 520),
        )
    }

    @Test
    fun nativeMediaRowsFitUpToFifteenPercentOverflow() {
        val ratios = List(3) { 0.5f }
        // The preferred row is 1000px wide, including its two gaps.
        assertEquals(
            NativeMediaDimensions(282, 564),
            nativeMultiMediaCellDimensions(ratios, 0, 850, maxHeightPx = 664, gapPx = 2),
        )
        assertEquals(
            NativeMediaDimensions(332, 664),
            nativeMultiMediaCellDimensions(ratios, 0, 849, maxHeightPx = 664, gapPx = 2),
        )
        val cells = ratios.indices.map { index ->
            nativeMultiMediaCellDimensions(ratios, index, 900, maxHeightPx = 664, gapPx = 2)
        }
        assertTrue(cells.sumOf { it.widthPx } + 4 <= 900)
        cells.forEach { assertEquals(it.heightPx / 2, it.widthPx) }
    }

    @Test
    fun nativeMultiMediaCellsUseTheirRenderedGridDimensions() {
        val pair = nativeMultiMediaCellDimensions(List(2) { 0.75f }, 0, 840, maxHeightPx = 520)
        val four = nativeMultiMediaCellDimensions(List(4) { 0.75f }, 0, 840, maxHeightPx = 520)
        assertEquals(pair, four)
        assertEquals(
            NativeMediaDimensions(widthPx = 498, heightPx = 498),
            nativeMultiMediaCellDimensions(
                cellAspectRatios = listOf(1f, 1f),
                cellIndex = 0,
                gridWidthPx = 1_000,
                maxHeightPx = 520,
                gapPx = 4,
            ),
        )
        assertEquals(
            NativeMediaDimensions(widthPx = 300, heightPx = 520),
            nativeMultiMediaCellDimensions(
                cellAspectRatios = listOf(624f / 1080f, 623f / 1080f, 624f / 1080f),
                cellIndex = 0,
                gridWidthPx = 1_000,
                maxHeightPx = 520,
            ),
        )
        assertEquals(
            NativeMediaDimensions(widthPx = 520, heightPx = 520),
            nativeMultiMediaCellDimensions(
                cellAspectRatios = listOf(1f, 1f, 1f, 1f),
                cellIndex = 3,
                gridWidthPx = 1_000,
                maxHeightPx = 520,
            ),
        )
    }

    @Test
    fun like_and_bookmark_updates_do_not_replace_feed_media_content() {
        val original = adapterPost("sample_post")
        val liked =
            adapterPost(
                tweetId = "sample_post",
                rowTransform = { row -> row.copy(isLiked = 1, likedAt = 1_000L) },
            )
        val changedBody =
            adapterPost(
                tweetId = "sample_post",
                rowTransform = { row ->
                    row.copy(item = row.item.copy(bodyText = "updated content"))
                },
            )

        assertTrue(nativeFeedLikeBookmarkOnlyChange(original, liked))
        assertFalse(nativeFeedLikeBookmarkOnlyChange(original, changedBody))
    }

    @Test
    fun thread_action_payload_includes_the_visible_ancestor_state() {
        val ancestor = feedRow("sample_ancestor")
        val leaf = feedRow("sample_reply")
        val original =
            adapterPost("sample_reply").copy(
                threaded = ThreadedFeedRow(row = leaf, chain = listOf(ancestor)),
                chainPosts = listOf(post("sample_ancestor")),
            )
        val likedAncestor = ancestor.copy(isLiked = 1, likedAt = 1_000L)
        val updated =
            original.copy(
                threaded = original.threaded.copy(chain = listOf(likedAncestor)),
                chainPosts =
                    listOf(
                        post("sample_ancestor").let { post ->
                            post.copy(
                                row = likedAncestor,
                                actions = post.actions.copy(isLiked = true),
                            )
                        }
                    ),
            )

        assertTrue(nativeFeedLikeBookmarkOnlyChange(original, updated))
        assertEquals(
            listOf("sample_ancestor" to true),
            nativeThreadActionPosts(updated).map { it.row.item.tweetId to it.actions.isLiked },
        )
    }

    @Test
    fun pending_clear_updates_actions_without_changing_feed_media_rows() {
        val target =
            feedRow("post_1").copy(
                item = FeedItemEntity(tweetId = "post_1", contentHash = "shared_content"),
                isLiked = 1,
                likedAt = 10L,
                isBookmarked = 1,
                bookmarkCategoryId = 2L,
                bookmarkCustomTitle = "Saved",
                bookmarkedAt = 10L,
            )
        val alias =
            feedRow("post_alias").copy(
                item = FeedItemEntity(tweetId = "post_alias", contentHash = "shared_content"),
                isLiked = 1,
                isBookmarked = 1,
                bookmarkCategoryId = 2L,
                bookmarkCustomTitle = "Saved",
                bookmarkedAt = 10L,
            )
        val rows = listOf(ThreadedFeedRow(target, emptyList()), ThreadedFeedRow(alias, emptyList()))

        val updated =
            nativeFeedRowsWithPendingActionOverrides(
                rows,
                pendingFeedActionOverrides(
                    listOf(
                        OutboxEntity(
                            kind = OutboxKind.CODE_LIKE,
                            itemId = "post_1",
                            payloadJson = """{"action":"clear"}""",
                        ),
                        OutboxEntity(
                            kind = OutboxKind.CODE_BOOKMARK,
                            itemId = "post_1",
                            payloadJson = """{"action":"clear"}""",
                        ),
                    )
                ),
            )

        assertEquals(target.item, updated[0].row.item)
        assertEquals(0, updated[0].row.isLiked)
        assertNull(updated[0].row.likedAt)
        assertEquals(0, updated[0].row.isBookmarked)
        assertNull(updated[0].row.bookmarkCategoryId)
        assertEquals(1, updated[1].row.isLiked)
        assertEquals(0, updated[1].row.isBookmarked)
    }

    @Test
    fun media_model_updates_do_not_rebind_post_text_or_actions() {
        val original = adapterPost("sample_post")
        val mediaReady =
            original.copy(
                post =
                    original.post.copy(
                        media =
                            original.post.media.copy(
                                grid = original.post.media.grid.copy(inventoryLoaded = false)
                            )
                    )
            )
        val changedBody =
            adapterPost(
                tweetId = "sample_post",
                rowTransform = { row -> row.copy(item = row.item.copy(bodyText = "updated content")) },
            )
        val threadedOriginal = original.copy(chainPosts = listOf(original.post))
        val threadedMediaReady = mediaReady.copy(chainPosts = listOf(mediaReady.post))

        assertTrue(nativeFeedMediaOnlyChange(original, mediaReady))
        assertFalse(nativeFeedMediaOnlyChange(original, changedBody))
        assertFalse(nativeFeedMediaOnlyChange(threadedOriginal, threadedMediaReady))
    }

    @Test
    fun nativeBodyClampUsesLengthThreshold() {
        assertTrue(nativeShouldClampBody("x".repeat(421)))
        assertFalse(nativeShouldClampBody("short"))
    }

    @Test
    fun nativeChannelHeaderUsesHeroGeometryWithoutBlankBand() {
        assertEquals(NativeChannelHeaderBannerHeightDp, NativeChannelHeaderBannerFrameHeightDp)
        assertEquals(NativeChannelHeaderAvatarSizeDp / 2, NativeChannelHeaderAvatarOverlapDp)
        assertTrue(NativeChannelHeaderBannerFrameHeightDp > 148)
        assertTrue(NativeChannelHeaderAvatarSizeDp > 92)
        assertTrue(NativeChannelHeaderActionRowHeightDp > NativeChannelHeaderAvatarOverlapDp)
        assertTrue(NativeChannelHeaderActionRowHeightDp <= NativeChannelHeaderAvatarOverlapDp + 8)
        assertTrue(NativeChannelHeaderFollowHeightDp <= 44)
        assertTrue(NativeChannelHeaderIconButtonSizeDp <= 40)
        assertTrue(NativeChannelHeaderNameTextSp <= 28f)
        assertTrue(NativeChannelHeaderBioTextSp <= 16f)
        assertTrue(NativeChannelHeaderMetaTextSp <= 17f)
    }

    @Test
    fun nativeClickableTextMarksMentionAndUrlSpans() {
        val text =
            clickableText(
                raw = "Ins: @example https://example.test",
                linkColor = 0xFF00FF,
                onMentionClick = {},
                onUrlClick = {},
            )

        assertEquals(2, text.getSpans(0, text.length, ClickableSpan::class.java).size)
    }

    @Test
    fun bareProfileWebsitesOpenAsHttpsUrls() {
        assertEquals("https://youtube.com/@example", externalUrlForIntent("youtube.com/@example"))
        assertEquals("https://example.com:443/path", externalUrlForIntent("example.com:443/path"))
        assertEquals("mailto:test@example.com", externalUrlForIntent("mailto:test@example.com"))
        assertEquals(
            "https://example.test/path",
            externalUrlForIntent(" https://example.test/path "),
        )
    }

    private fun cell(
        aspectRatio: Float,
        known: Boolean = true,
        isVideo: Boolean = false,
    ): FeedMediaCellDescriptor =
        FeedMediaCellDescriptor(
            isVideo = isVideo,
            aspectRatio = aspectRatio,
            aspectRatioKnown = known,
        )

    private fun threadedRow(tweetId: String) =
        com.screwy.igloo.data.entity.ThreadedFeedRow(row = feedRow(tweetId), chain = emptyList())

    private fun post(tweetId: String): com.screwy.igloo.feed.SocialPostModel {
        val row = feedRow(tweetId)
        return com.screwy.igloo.feed.SocialPostModel(
            row = row,
            author =
                com.screwy.igloo.feed.SocialProfileModel(
                    channelId = "twitter_alice",
                    handle = "alice",
                    displayName = "Alice",
                ),
            actions =
                com.screwy.igloo.feed.SocialActionState(
                    isLiked = false,
                    isBookmarked = false,
                    isAuthorFollowed = false,
                    isAuthorStarred = false,
                ),
            media =
                com.screwy.igloo.feed.SocialMediaModel(
                    ownerId = tweetId,
                    grid =
                        com.screwy.igloo.feed.FeedMediaGridModel(
                            ownerId = tweetId,
                            cells = emptyList(),
                            inventoryLoaded = true,
                        ),
                ),
            quoteMedia = null,
        )
    }

    private fun adapterPost(
        tweetId: String,
        rowTransform: (com.screwy.igloo.data.entity.FeedRow) ->
            com.screwy.igloo.data.entity.FeedRow = { it },
    ): NativeFeedAdapterItem.Post {
        val row = rowTransform(feedRow(tweetId))
        val post =
            post(tweetId).copy(
                row = row,
                actions =
                    post(tweetId)
                        .actions
                        .copy(isLiked = row.isLiked == 1, isBookmarked = row.isBookmarked == 1),
            )
        return NativeFeedAdapterItem.Post(
            threaded = com.screwy.igloo.data.entity.ThreadedFeedRow(row, emptyList()),
            post = post,
        )
    }

    private fun feedRow(tweetId: String) =
        com.screwy.igloo.data.entity.FeedRow(
            item = FeedItemEntity(tweetId = tweetId),
            channelName = "Alice",
            channelPlatform = "twitter",
            authorHandle = "alice",
            isLiked = 0,
            likedAt = null,
            isBookmarked = 0,
            bookmarkCategoryId = null,
            bookmarkCustomTitle = null,
            bookmarkedAt = null,
            channelIsFollowed = 0,
            channelIsStarred = 0,
        )
}
