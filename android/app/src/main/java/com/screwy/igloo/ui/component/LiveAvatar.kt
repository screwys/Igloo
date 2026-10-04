package com.screwy.igloo.ui.component

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
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
    val liveColor = MaterialTheme.iglooColors.error
    Box(modifier = modifier.size(size)) {
        Avatar(
            channelId = channelId,
            size = size,
            modifier = avatarModifier.then(if (live) Modifier.border(3.dp, liveColor, CircleShape) else Modifier),
            onClick = onClick,
            showPendingBadge = showPendingBadge,
        )
        if (live) {
            Text(
                text = "LIVE",
                color = Color.White,
                fontSize = 9.sp,
                fontWeight = FontWeight.Bold,
                modifier = Modifier.align(Alignment.BottomCenter).offset(y = 4.dp)
                    .background(liveColor, RoundedCornerShape(3.dp)).padding(horizontal = 4.dp, vertical = 1.dp),
            )
        }
    }
}
