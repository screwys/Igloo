package com.screwy.igloo.ui.component

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import com.screwy.igloo.R
import com.screwy.igloo.ui.theme.iglooColors

internal fun storyProgressWindow(items: List<MomentItem>, currentIndex: Int): StoryProgressWindow {
    if (items.isEmpty() || currentIndex !in items.indices) return StoryProgressWindow(index = 0, count = 0)
    val channelId = items[currentIndex].channelId
    var start = currentIndex
    while (start > 0 && items[start - 1].channelId == channelId) {
        start -= 1
    }
    var end = currentIndex
    while (end < items.lastIndex && items[end + 1].channelId == channelId) {
        end += 1
    }
    return StoryProgressWindow(
        index = currentIndex - start,
        count = end - start + 1,
    )
}

@Composable
internal fun StoryProgressControl(
    currentPage: Int,
    pageCount: Int,
    modifier: Modifier = Modifier,
) {
    if (pageCount <= 0) return
    Column(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = 12.dp, vertical = 4.dp),
        verticalArrangement = Arrangement.spacedBy(5.dp),
        horizontalAlignment = Alignment.End,
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            repeat(pageCount) { index ->
                val alpha = when {
                    index < currentPage -> 0.82f
                    index == currentPage -> 1f
                    else -> 0.34f
                }
                val color = Color.White.copy(alpha = alpha)
                Box(
                    modifier = Modifier
                        .weight(1f)
                        .height(3.dp)
                        .clip(RoundedCornerShape(999.dp))
                        .background(color),
                )
            }
        }
    }
}

@Composable
internal fun MomentsTabControl(
    activeTab: String,
    onTabSelected: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    val allLabel = stringResource(R.string.shorts_tab_all)
    val followingLabel = stringResource(R.string.shorts_tab_following)
    val storiesLabel = stringResource(R.string.shorts_tab_stories)
    CompositionLocalProvider(LocalLayoutDirection provides LayoutDirection.Ltr) {
        Row(
            modifier = modifier
                .padding(horizontal = 16.dp, vertical = 4.dp),
            horizontalArrangement = Arrangement.spacedBy(16.dp),
            verticalAlignment = Alignment.Top,
        ) {
            MomentsTabPill("all", allLabel, activeTab == "all", onTabSelected)
            MomentsTabPill("following", followingLabel, activeTab == "following", onTabSelected)
            MomentsTabPill("stories", storiesLabel, activeTab == "stories", onTabSelected)
        }
    }
}

@Composable
private fun MomentsTabPill(
    tab: String,
    label: String,
    active: Boolean,
    onTabSelected: (String) -> Unit,
) {
    Box(
        modifier = Modifier
            .width(116.dp)
            .height(34.dp)
            .clickable { onTabSelected(tab) }
            .padding(horizontal = 2.dp),
    ) {
        Text(
            text = label,
            color = if (active) Color.White else Color.White.copy(alpha = 0.68f),
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            style = MaterialTheme.typography.titleSmall.copy(
                fontWeight = if (active) FontWeight.Bold else FontWeight.SemiBold,
                shadow = DropShadow,
            ),
            modifier = Modifier.align(Alignment.TopCenter),
        )
        if (active) {
            Box(
                modifier = Modifier
                    .align(Alignment.BottomCenter)
                    .size(5.dp)
                    .clip(CircleShape)
                    .background(MaterialTheme.iglooColors.primary),
            )
        }
    }
}
