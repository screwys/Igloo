package com.screwy.igloo.ui.component

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.Dp
import com.screwy.igloo.ui.theme.iglooColors

@Composable
fun CenteredTabs(
    tabs: List<Pair<String, String>>,
    selected: String,
    onSelected: (String) -> Unit,
    modifier: Modifier = Modifier,
    verticalPadding: Dp = 8.dp,
) {
    val colors = MaterialTheme.iglooColors
    val shape = RoundedCornerShape(999.dp)
    Box(modifier.padding(vertical = verticalPadding), contentAlignment = Alignment.Center) {
        Row(
            modifier = Modifier.clip(shape).background(colors.surfaceElevated)
                .border(1.dp, colors.border, shape).horizontalScroll(rememberScrollState())
                .selectableGroup().padding(4.dp),
            horizontalArrangement = Arrangement.spacedBy(4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            tabs.forEach { (key, label) ->
                val active = selected == key
                Box(
                    modifier = Modifier.clip(shape)
                        .background(if (active) colors.primary else colors.surfaceElevated)
                        .selectable(selected = active, role = Role.Tab, onClick = { onSelected(key) })
                        .heightIn(min = 48.dp).padding(horizontal = 16.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(label, style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.SemiBold,
                        color = if (active) colors.onPrimary else colors.onSurface)
                }
            }
        }
    }
}
