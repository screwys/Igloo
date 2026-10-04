package com.screwy.igloo.ui.component

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.PlatformTextStyle
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.screwy.igloo.ui.theme.iglooColors

@Composable
internal fun LiveAvatar(
    channelId: String,
    size: Dp,
    live: Boolean,
    modifier: Modifier = Modifier,
    avatarModifier: Modifier = Modifier,
    onClick: (() -> Unit)? = null,
    showPendingBadge: Boolean = false,
) {
    val colors = MaterialTheme.iglooColors
    val liveColor = colors.primary
    Box(modifier = modifier.size(size).drawBehind {
        if (live) {
            drawCircle(
                color = liveColor,
                radius = this.size.minDimension / 2 + 1.5.dp.toPx(),
                style = Stroke(width = 3.dp.toPx()),
            )
        }
    }) {
        Avatar(
            channelId = channelId,
            size = size,
            modifier = avatarModifier,
            onClick = onClick,
            showPendingBadge = showPendingBadge,
        )
        if (live) {
            Text(
                text = "LIVE",
                color = Color.White,
                style = TextStyle(
                    fontSize = 9.sp,
                    lineHeight = 11.sp,
                    fontWeight = FontWeight.Bold,
                    platformStyle = PlatformTextStyle(includeFontPadding = false),
                ),
                modifier = Modifier.align(Alignment.BottomCenter).offset(y = 6.dp)
                    .background(liveColor, RoundedCornerShape(4.dp))
                    .border(2.dp, colors.background, RoundedCornerShape(4.dp))
                    .padding(horizontal = 7.dp, vertical = 3.dp),
            )
        }
    }
}
