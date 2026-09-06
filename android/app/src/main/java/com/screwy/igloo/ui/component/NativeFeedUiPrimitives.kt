// SPDX-License-Identifier: AGPL-3.0-only
// RecyclerView list/header/video behavior is adapted from Flare's AGPL-3.0 timeline patterns.

package com.screwy.igloo.ui.component

import android.content.Context
import android.graphics.Color
import android.graphics.drawable.ColorDrawable
import android.graphics.drawable.GradientDrawable
import android.text.SpannableString
import android.text.Spanned
import android.text.TextPaint
import android.text.TextUtils
import android.text.style.ClickableSpan
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.widget.ImageButton
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.PopupWindow
import android.widget.ScrollView
import android.widget.TextView
import java.text.DateFormat
import java.text.NumberFormat
import java.time.Instant
import java.util.Date
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.width
import androidx.compose.ui.unit.dp
import com.screwy.igloo.R
import com.screwy.igloo.data.entity.FeedItemEntity
import com.screwy.igloo.feed.SocialPostModel
import java.util.Locale

internal fun clickableText(
    raw: String,
    linkColor: Int,
    urlColor: Int = linkColor,
    onMentionClick: (String) -> Unit,
    onUrlClick: (String) -> Unit,
): SpannableString {
    val spannable = SpannableString(raw)
    MentionRegex.findAll(raw).forEach { match ->
        val handle = match.groupValues[1]
        spannable.setSpan(
            object : ClickableSpan() {
                override fun onClick(widget: View) = onMentionClick(handle)
                override fun updateDrawState(ds: TextPaint) {
                    ds.color = linkColor
                    ds.isUnderlineText = false
                }
            },
            match.range.first,
            match.range.last + 1,
            Spanned.SPAN_EXCLUSIVE_EXCLUSIVE,
        )
    }
    UrlRegex.findAll(raw).forEach { match ->
        val url = match.value
        spannable.setSpan(
            object : ClickableSpan() {
                override fun onClick(widget: View) = onUrlClick(url)
                override fun updateDrawState(ds: TextPaint) {
                    ds.color = urlColor
                    ds.isUnderlineText = true
                }
            },
            match.range.first,
            match.range.last + 1,
            Spanned.SPAN_EXCLUSIVE_EXCLUSIVE,
        )
    }
    return spannable
}

internal fun nativeTranslationPillFor(
    item: FeedItemEntity,
    active: Boolean,
    enabled: Boolean,
): NativeTranslationPill? {
    if (!enabled && !nativeHasForeignLanguage(item)) return null
    return NativeTranslationPill(
        sourceLangLabel = nativeFirstSourceLanguageLabel(item),
        active = active,
        enabled = enabled,
    )
}

internal fun nativeTranslationPillForText(
    lang: String?,
    sourceLang: String?,
    text: String?,
    active: Boolean,
    enabled: Boolean,
): NativeTranslationPill? {
    if (!enabled && !nativeIsForeignOrUnknown(lang, text)) return null
    return NativeTranslationPill(
        sourceLangLabel = nativeSourceLanguageLabel(sourceLang),
        active = active,
        enabled = enabled,
    )
}

private fun nativeHasForeignLanguage(item: FeedItemEntity): Boolean =
    nativeIsForeignOrUnknown(item.lang, item.bodyText) ||
        nativeIsForeignOrUnknown(item.quoteLang, item.quoteBodyText)

private fun nativeFirstSourceLanguageLabel(item: FeedItemEntity): String =
    nativeSourceLanguageLabel(item.bodySourceLang).ifBlank {
        nativeSourceLanguageLabel(item.quoteSourceLang)
    }

private fun nativeIsForeignOrUnknown(lang: String?, text: String?): Boolean =
    !text.isNullOrBlank() && lang?.trim()?.lowercase(Locale.ROOT).let { it.isNullOrBlank() || it != "en" }

private fun nativeSourceLanguageLabel(sourceLang: String?): String {
    val raw = sourceLang?.trim().orEmpty()
    if (raw.isBlank() || raw.equals("und", ignoreCase = true) || raw.equals("unknown", ignoreCase = true)) return ""
    if (!nativeLooksLikeLanguageTag(raw)) return raw
    val tag = raw.trim().replace('_', '-')
    val display = Locale.forLanguageTag(tag).getDisplayName(Locale.ENGLISH).trim()
    if (display.isNotBlank() && !display.equals(tag, ignoreCase = true)) return display
    return raw
}

private fun nativeLooksLikeLanguageTag(value: String): Boolean =
    value.replace('_', '-').matches(Regex("^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$"))

internal fun nativeShouldClampBody(text: String): Boolean =
    text.length > 420 || text.count { it == '\n' } + 1 > NativeFeedBodyCollapsedLines

internal fun <T> nativeThreadPreviewAncestors(chain: List<T>): List<T> =
    chain.take(1)

internal fun <T> nativeThreadCapsuleVisible(chain: List<T>): Boolean =
    chain.size > nativeThreadPreviewAncestors(chain).size

internal fun NativeFeedPrimaryAction.iconRes(selected: Boolean): Int = when (this) {
    NativeFeedPrimaryAction.Reply -> R.drawable.ic_feed_reply_24
    NativeFeedPrimaryAction.External -> R.drawable.ic_feed_open_24
    NativeFeedPrimaryAction.Share -> R.drawable.ic_feed_share_24
    NativeFeedPrimaryAction.Like -> if (selected) R.drawable.ic_feed_favorite_24 else R.drawable.ic_feed_favorite_border_24
    NativeFeedPrimaryAction.Bookmark -> if (selected) R.drawable.ic_feed_bookmark_24 else R.drawable.ic_feed_bookmark_border_24
}

internal fun NativeFeedPrimaryAction.contentDescription(context: Context, post: SocialPostModel): String = when (this) {
    NativeFeedPrimaryAction.Reply -> context.getString(R.string.feed_open_thread)
    NativeFeedPrimaryAction.External -> context.getString(R.string.action_open_on_x)
    NativeFeedPrimaryAction.Share -> context.getString(R.string.action_share)
    NativeFeedPrimaryAction.Like -> context.getString(if (post.actions.isLiked) R.string.action_unlike else R.string.action_like)
    NativeFeedPrimaryAction.Bookmark -> context.getString(
        if (post.actions.isBookmarked) R.string.action_remove_bookmark else R.string.action_bookmark,
    )
}

internal fun showNativeFeedPopup(
    anchor: View,
    colors: NativeFeedColors,
    items: List<NativeFeedMenuItem>,
) {
    if (items.isEmpty()) return
    val context = anchor.context
    val menuWidth = dp(224)
    val content = LinearLayout(context).apply {
        orientation = LinearLayout.VERTICAL
        setPadding(dp(6), dp(6), dp(6), dp(6))
        background = roundedStroke(colors.surfaceElevated, colors.borderSubtle, dp(1), dp(10))
    }
    val popup = PopupWindow(content, menuWidth, ViewGroup.LayoutParams.WRAP_CONTENT, true).apply {
        isOutsideTouchable = true
        elevation = dp(10).toFloat()
        setBackgroundDrawable(ColorDrawable(Color.TRANSPARENT))
    }
    items.forEach { item ->
        content.addView(
            TextView(context).apply {
                text = item.label
                textSize = 15f
                maxLines = 1
                ellipsize = TextUtils.TruncateAt.END
                gravity = Gravity.CENTER_VERTICAL
                setIncludeFontPadding(false)
                setPadding(dp(10), 0, dp(10), 0)
                setTextColor(if (item.danger) colors.primary else colors.onSurface)
                background = roundedFill(Color.TRANSPARENT, dp(8))
                setOnClickListener {
                    popup.dismiss()
                    item.action()
                }
            },
            LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(40)),
        )
    }
    popup.showAsDropDown(anchor, anchor.width - menuWidth, dp(4))
}

internal fun accountBadgeText(context: Context): TextView = smallText(context).apply {
    gravity = Gravity.CENTER
    setPadding(dp(6), dp(2), dp(6), dp(2))
    maxLines = 1
    visibility = View.GONE
}

internal fun bindNativeAccountBadge(
    badge: TextView,
    displayName: String,
    region: String?,
    detailsJson: String?,
    colors: NativeFeedColors,
) {
    val details = parseFeedAccountDetails(detailsJson)
    badge.visibility = if (region.isNullOrBlank() && details == null) View.GONE else View.VISIBLE
    if (badge.visibility == View.GONE) {
        badge.setOnClickListener(null)
        return
    }
    badge.text = listOf(accountRegionFlag(region), accountSourceSymbol(details?.source.orEmpty()),
        if (details?.locationAccurate == false) "⚠" else "")
        .filter(String::isNotBlank).joinToString(" ").ifBlank { "🌐" }
    badge.setTextColor(colors.onSurfaceMuted)
    badge.background = roundedStroke(colors.surfaceVariant, colors.borderSubtle, dp(1), dp(10))
    badge.contentDescription = badge.context.getString(R.string.feed_account_details) + ", " + displayName
    badge.setOnClickListener { showNativeAccountDetails(badge, displayName, region, details, colors) }
}

private fun showNativeAccountDetails(
    anchor: View,
    displayName: String,
    region: String?,
    details: FeedAccountDetails?,
    colors: NativeFeedColors,
) {
    val context = anchor.context
    val content = LinearLayout(context).apply {
        orientation = LinearLayout.VERTICAL
        setPadding(dp(16), dp(12), dp(16), dp(12))
        background = roundedStroke(colors.surfaceElevated, colors.borderSubtle, dp(1), dp(10))
    }
    content.addView(bodyText(context).apply {
        text = context.getString(R.string.feed_account_details)
        setTypeface(typeface, android.graphics.Typeface.BOLD)
        setTextColor(colors.onSurface)
    })
    content.addView(smallText(context).apply { text = displayName; setTextColor(colors.onSurfaceMuted) })
    fun field(label: Int, value: String?) {
        if (value.isNullOrBlank()) return
        val row = LinearLayout(context).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
            setPadding(dp(10), dp(8), dp(10), dp(8))
            background = roundedFill(colors.surfaceVariant, dp(6))
        }
        row.addView(smallText(context).apply {
            text = context.getString(label)
            setTextColor(colors.onSurfaceMuted)
        }, LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f))
        row.addView(smallText(context).apply {
            text = value
            gravity = Gravity.END
            setTextColor(colors.onSurface)
            setTypeface(typeface, android.graphics.Typeface.BOLD)
            setTextIsSelectable(true)
        }, LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1.5f).apply {
            marginStart = dp(8)
        })
        content.addView(row, LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT,
            ViewGroup.LayoutParams.WRAP_CONTENT).apply { topMargin = dp(8) })
    }
    fun date(raw: String?): String? = raw?.let {
        runCatching { DateFormat.getDateInstance(DateFormat.MEDIUM).format(Date.from(Instant.parse(it))) }.getOrNull()
    }
    field(R.string.feed_account_region, region)
    field(R.string.feed_account_source, details?.source)
    if (details?.locationAccurate == false) {
        content.addView(bodyText(context).apply {
            text = context.getString(R.string.feed_account_location_uncertain)
            setTextColor(colors.onSurfaceMuted)
            setPadding(0, dp(12), 0, 0)
        })
    }
    field(R.string.feed_account_created_at, date(details?.createdAt))
    field(R.string.feed_account_verified_at, date(details?.verifiedAt))
    field(R.string.feed_account_username_changes, details?.usernameChanges?.takeIf { it >= 0 }?.let { NumberFormat.getIntegerInstance().format(it) })
    field(R.string.feed_account_verification, details?.verification?.takeIf(String::isNotBlank)?.let { value ->
        when (value.lowercase()) {
            "blue", "individual" -> context.getString(R.string.feed_account_verification_blue)
            "business", "organization" -> context.getString(R.string.feed_account_verification_business)
            "government" -> context.getString(R.string.feed_account_verification_government)
            else -> value
        }
    })
    field(R.string.feed_account_user_id, details?.userId)
    val width = minOf(dp(300), context.resources.displayMetrics.widthPixels - dp(24))
    val scroll = ScrollView(context).apply { addView(content) }
    scroll.measure(View.MeasureSpec.makeMeasureSpec(width, View.MeasureSpec.EXACTLY),
        View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED))
    PopupWindow(scroll, width, minOf(scroll.measuredHeight, context.resources.displayMetrics.heightPixels * 2 / 3), true).apply {
        isOutsideTouchable = true
        elevation = dp(10).toFloat()
        setBackgroundDrawable(ColorDrawable(Color.TRANSPARENT))
        showAsDropDown(anchor, 0, dp(4))
    }
}

internal fun actionIconButton(context: Context, colors: NativeFeedColors): ImageButton =
    ImageButton(context).apply {
        background = null
        scaleType = ImageView.ScaleType.CENTER
        setPadding(dp(10), dp(6), dp(10), dp(6))
        setColorFilter(colors.onSurfaceMuted)
        layoutParams = LinearLayout.LayoutParams(dp(44), dp(38))
    }

internal fun bodyText(context: Context): TextView =
    TextView(context).apply {
        textSize = 17f
        setIncludeFontPadding(false)
        setLineSpacing(0f, 1.22f)
        setPadding(0, dp(3), 0, dp(3))
    }

internal fun quoteText(context: Context): TextView =
    TextView(context).apply {
        textSize = 15f
        setIncludeFontPadding(false)
        setLineSpacing(0f, 1.18f)
        setPadding(0, dp(2), 0, dp(2))
    }

internal fun smallText(context: Context): TextView =
    TextView(context).apply {
        textSize = 14f
        maxLines = 1
        ellipsize = TextUtils.TruncateAt.END
        setPadding(0, dp(3), 0, dp(3))
    }

internal fun verticalSpacingLayoutParams(): LinearLayout.LayoutParams =
    LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT).apply {
        topMargin = dp(6)
        bottomMargin = dp(6)
    }

internal fun roundedFill(color: Int, radius: Int): GradientDrawable =
    GradientDrawable().apply {
        setColor(color)
        cornerRadius = radius.toFloat()
    }

internal fun roundedStroke(fill: Int, stroke: Int, strokeWidth: Int, radius: Int): GradientDrawable =
    GradientDrawable().apply {
        setColor(fill)
        setStroke(strokeWidth, stroke)
        cornerRadius = radius.toFloat()
    }

internal fun stableItemId(value: String): Long {
    var result = 1125899906842597L
    value.forEach { ch -> result = 31 * result + ch.code }
    return result
}

internal fun dp(value: Int): Int =
    (value * android.content.res.Resources.getSystem().displayMetrics.density).toInt()

private val MentionRegex = Regex("""(?<![A-Za-z0-9_])@([A-Za-z0-9_]{1,30})""")
private val UrlRegex = Regex("""https?://\S+""")
