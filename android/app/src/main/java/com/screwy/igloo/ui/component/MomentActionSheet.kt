package com.screwy.igloo.ui.component

import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import android.content.pm.PackageManager
import android.util.Rational
import androidx.activity.ComponentActivity
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.VolumeOff
import androidx.compose.material.icons.automirrored.filled.VolumeUp
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.PersonRemove
import androidx.compose.material.icons.filled.PictureInPictureAlt
import androidx.compose.material.icons.filled.Repeat
import androidx.compose.material.icons.filled.Share
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.Alignment
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.core.app.PictureInPictureParamsCompat
import com.screwy.igloo.R
import com.screwy.igloo.data.IglooDatabase
import com.screwy.igloo.data.platformKeyFromChannelId
import com.screwy.igloo.data.stripPlatformPrefix
import org.koin.compose.koinInject

internal data class MomentActionAvailability(
    val canToggleReposts: Boolean,
    val canToggleMute: Boolean,
    val canUnfollowChannel: Boolean,
    val canVisitAuthor: Boolean,
    val canVisitReposter: Boolean,
)

internal fun momentActionAvailability(item: MomentItem): MomentActionAvailability {
    val isRepost = item.repostIntroduced && !item.reposterChannelId.isNullOrBlank()
    return MomentActionAvailability(
        canToggleReposts = isRepost,
        canToggleMute = !item.isAuthorFollowed && item.channelId.isNotBlank(),
        canUnfollowChannel = isRepost || item.isAuthorFollowed,
        canVisitAuthor = item.channelId.isNotBlank(),
        canVisitReposter = isRepost,
    )
}

internal fun momentUnfollowTarget(item: MomentItem): String? =
    when {
        !momentActionAvailability(item).canUnfollowChannel -> null
        item.repostIntroduced && !item.reposterChannelId.isNullOrBlank() -> item.reposterChannelId
        else -> item.channelId.takeIf { it.isNotBlank() }
    }

/** A display target for the account affected by a moment action. */
internal data class MomentActionAccountLabels(
    val reposter: String,
    val author: String,
)

/**
 * Keeps action labels meaningful across TikTok, Instagram, and X identifiers.
 *
 * Prefer handles from the item or canonical channel ID so every row in this menu names accounts
 * the same way. Display names remain a fallback only when no usable handle is available.
 */
internal fun momentActionAccountLabels(item: MomentItem): MomentActionAccountLabels {
    val reposterId = item.reposterChannelId.orEmpty().trim()
    val reposter =
        momentAccountHandleLabel(
                platform = platformKeyFromChannelId(reposterId),
                raw = stripPlatformPrefix(reposterId),
            )
            ?: item.repostAuthorLabel?.trim()?.takeIf { it.isNotBlank() }
            ?: stripPlatformPrefix(reposterId)
    val author =
        momentAccountHandleLabel(
            platform = platformKeyFromChannelId(item.channelId),
            raw = item.authorHandle,
        )
            ?: momentAccountHandleLabel(
                platform = platformKeyFromChannelId(item.channelId),
                raw = stripPlatformPrefix(item.channelId),
            )
            ?: item.authorDisplayName?.trim()?.takeIf { it.isNotBlank() }
            ?: stripPlatformPrefix(item.channelId)
    return MomentActionAccountLabels(reposter = reposter, author = author)
}

private fun momentAccountHandleLabel(platform: String, raw: String?): String? =
    platformHandleCandidate(platform, raw).takeIf { it.isNotBlank() }?.let { "@$it" }

internal fun momentUnfollowAuthorLabel(item: MomentItem): String =
    item.authorDisplayName?.trim()?.takeIf { it.isNotBlank() }
        ?: item.authorHandle.trim().takeIf { it.isNotBlank() }
        ?: item.channelId

internal fun momentMiniPlayerAvailable(item: MomentItem, pictureInPictureSupported: Boolean): Boolean =
    pictureInPictureSupported &&
        momentMediaMode(item.mediaKind, item.slideCount) == MomentMediaMode.Video

@Composable
@OptIn(ExperimentalMaterial3Api::class)
internal fun MomentActionSheet(
    item: MomentItem,
    onDismissRequest: () -> Unit,
    onRepostsEnabledChanged: (channelId: String, enabled: Boolean) -> Unit,
    onChannelMutedChanged: (channelId: String, muted: Boolean) -> Unit,
    onUnfollowChannel: (channelId: String) -> Unit,
    onShare: (MomentItem) -> Unit,
    onVisitChannel: (channelId: String) -> Unit,
) {
    val actions = momentActionAvailability(item)
    val context = LocalContext.current
    val miniPlayerActivity = remember(context) { context.findMomentComponentActivity() }
    val miniPlayerAvailable =
        remember(miniPlayerActivity, item.mediaKind, item.slideCount) {
            momentMiniPlayerAvailable(
                item = item,
                pictureInPictureSupported =
                    miniPlayerActivity?.packageManager?.hasSystemFeature(
                        PackageManager.FEATURE_PICTURE_IN_PICTURE
                    ) == true,
            )
        }

    val db: IglooDatabase = koinInject()
    val reposterChannelId = item.reposterChannelId.orEmpty()
    val unfollowChannelId = momentUnfollowTarget(item)
    val reposterSetting =
        if (actions.canToggleReposts) {
            db.channelSettingDao()
                .getByIdFlow(reposterChannelId)
                .collectAsState(initial = null)
                .value
        } else {
            null
        }
    val mutedAuthor =
        if (actions.canToggleMute) {
            db.mutedChannelDao()
                .getByIdFlow(item.channelId)
                .collectAsState(initial = null)
                .value
        } else {
            null
        }
    val repostsEnabled = reposterSetting?.includeReposts != 0
    val authorMuted = mutedAuthor != null
    val accountLabels = momentActionAccountLabels(item)
    val unfollowLabel =
        if (actions.canVisitReposter) accountLabels.reposter else accountLabels.author
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = false)
    var showUnfollowConfirmation by remember(item.videoId, reposterChannelId) { mutableStateOf(false) }

    ModalBottomSheet(
        onDismissRequest = onDismissRequest,
        sheetState = sheetState,
    ) {
        // Do not let this context menu claim player-sized vertical space. With the partial sheet
        // state above, it wraps to compact rows on phones as well.
        Column(modifier = Modifier.fillMaxWidth().padding(bottom = 12.dp)) {
            if (actions.canToggleReposts) {
                MomentActionRow(
                    icon = Icons.Default.Repeat,
                    label =
                        stringResource(
                            if (repostsEnabled) R.string.action_turn_off_reposts_for_account
                            else R.string.action_turn_on_reposts_for_account,
                            accountLabels.reposter,
                        ),
                    onClick = {
                        onDismissRequest()
                        onRepostsEnabledChanged(reposterChannelId, !repostsEnabled)
                    },
                )
            }
            if (miniPlayerAvailable) {
                MomentActionRow(
                    icon = Icons.Default.PictureInPictureAlt,
                    label = stringResource(R.string.mini_player_title),
                    onClick = {
                        onDismissRequest()
                        miniPlayerActivity?.enterPictureInPictureMode(
                            PictureInPictureParamsCompat.Builder()
                                .setEnabled(true)
                                .setAspectRatio(Rational(9, 16))
                                .setTitle(momentAuthorLabel(item))
                                .build()
                        )
                    },
                )
            }
            MomentActionRow(
                icon = Icons.Default.Share,
                label = stringResource(R.string.action_share),
                onClick = {
                    onDismissRequest()
                    onShare(item)
                },
            )
            if (actions.canVisitReposter) {
                MomentActionRow(
                    icon = Icons.Default.Person,
                    label =
                        stringResource(
                            R.string.action_visit_profile_of_account,
                            accountLabels.reposter,
                        ),
                    onClick = {
                        onDismissRequest()
                        onVisitChannel(reposterChannelId)
                    },
                )
            }
            if (actions.canVisitAuthor && item.channelId != reposterChannelId) {
                MomentActionRow(
                    icon = Icons.Default.Person,
                    label =
                        stringResource(
                            R.string.action_visit_profile_of_account,
                            accountLabels.author,
                        ),
                    onClick = {
                        onDismissRequest()
                        onVisitChannel(item.channelId)
                    },
                )
            }
            if (actions.canToggleMute) {
                MomentActionRow(
                    icon =
                        if (authorMuted) Icons.AutoMirrored.Filled.VolumeUp
                        else Icons.AutoMirrored.Filled.VolumeOff,
                    label =
                        stringResource(
                            if (authorMuted) R.string.action_unmute_account_label
                            else R.string.action_mute_account_label,
                            accountLabels.author,
                        ),
                    onClick = {
                        onDismissRequest()
                        onChannelMutedChanged(item.channelId, !authorMuted)
                    },
                )
            }
            if (unfollowChannelId != null) {
                MomentActionRow(
                    icon = Icons.Default.PersonRemove,
                    label = stringResource(R.string.action_unfollow_account_label, unfollowLabel),
                    onClick = { showUnfollowConfirmation = true },
                )
            }
        }
    }
    if (showUnfollowConfirmation) {
        MomentUnfollowConfirmation(
            accountLabel = unfollowLabel,
            onDismissRequest = { showUnfollowConfirmation = false },
            onConfirm = {
                showUnfollowConfirmation = false
                onDismissRequest()
                unfollowChannelId?.let(onUnfollowChannel)
            },
        )
    }
}

@Composable
internal fun MomentUnfollowConfirmation(
    accountLabel: String,
    onDismissRequest: () -> Unit,
    onConfirm: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismissRequest,
        title = { Text(stringResource(R.string.confirm_unfollow_account_title)) },
        text = { Text(stringResource(R.string.confirm_unfollow_channel_delete_media_body, accountLabel)) },
        confirmButton = {
            TextButton(onClick = onConfirm) {
                Text(stringResource(R.string.action_unfollow))
            }
        },
        dismissButton = {
            TextButton(onClick = onDismissRequest) {
                Text(stringResource(R.string.action_cancel))
            }
        },
    )
}

@Composable
private fun MomentActionRow(icon: ImageVector, label: String, onClick: () -> Unit) {
    Row(
        modifier =
            Modifier
                .fillMaxWidth()
                .heightIn(min = 48.dp)
                .clickable(onClick = onClick)
                .padding(horizontal = 24.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            imageVector = icon,
            contentDescription = null,
            modifier = Modifier.size(22.dp),
        )
        Text(
            text = label,
            style = MaterialTheme.typography.bodyLarge,
            modifier = Modifier.padding(start = 16.dp),
        )
    }
}

private tailrec fun Context.findMomentComponentActivity(): ComponentActivity? =
    when (this) {
        is ComponentActivity -> this
        is ContextWrapper -> baseContext.findMomentComponentActivity()
        is Activity -> null
        else -> null
    }
