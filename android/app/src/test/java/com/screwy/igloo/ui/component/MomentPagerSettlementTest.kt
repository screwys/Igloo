package com.screwy.igloo.ui.component

import androidx.compose.foundation.layout.size
import androidx.compose.foundation.pager.VerticalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.material3.Text
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import com.screwy.igloo.media.OwnerKind
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class MomentPagerSettlementTest {
    @get:Rule val composeRule = createComposeRule()

    @Test
    fun inserting_rows_before_the_current_video_does_not_report_another_settlement() {
        var items by
            mutableStateOf(
                listOf(
                    momentItem("older"),
                    momentItem("active"),
                    momentItem("newer"),
                )
            )
        val viewedVideoIds = mutableListOf<String>()
        val cursorVideoIds = mutableListOf<String>()

        composeRule.setContent {
            val pagerItems = rememberMomentPagerSessionItems(items)
            val pagerState = rememberPagerState(initialPage = 1, pageCount = { pagerItems.size })
            val settlementTracker = remember { MomentPagerSettlementTracker() }
            MomentPagerSettlementEffect(
                pagerState = pagerState,
                items = pagerItems,
                settlementTracker = settlementTracker,
                onSettled = { item, origin ->
                    viewedVideoIds += item.videoId
                    if (origin == MomentPagerSettlementOrigin.Navigation) {
                        cursorVideoIds += item.videoId
                    }
                },
            )
            VerticalPager(
                state = pagerState,
                key = { page -> pagerItems[page].videoId },
                modifier = Modifier.size(width = 360.dp, height = 640.dp),
            ) { page ->
                Text(pagerItems[page].videoId)
            }
        }

        composeRule.runOnIdle {
            assertEquals(listOf("active"), viewedVideoIds)
            assertEquals(emptyList<String>(), cursorVideoIds)
        }
        composeRule.runOnIdle { items = listOf(momentItem("backfill")) + items }
        composeRule.onNodeWithText("active").assertIsDisplayed()
        composeRule.runOnIdle {
            assertEquals(listOf("active"), viewedVideoIds)
            assertEquals(emptyList<String>(), cursorVideoIds)
        }
    }

    @Test
    fun passive_start_target_records_a_view_but_only_later_navigation_records_the_cursor() {
        val items = listOf(momentItem("older"), momentItem("restored"), momentItem("newer"))
        val settlementTracker = MomentPagerSettlementTracker()
        lateinit var pagerState: androidx.compose.foundation.pager.PagerState
        lateinit var scope: CoroutineScope
        val viewedVideoIds = mutableListOf<String>()
        val cursorVideoIds = mutableListOf<String>()

        composeRule.setContent {
            pagerState = rememberPagerState(initialPage = 0, pageCount = { items.size })
            scope = rememberCoroutineScope()
            MomentPagerSettlementEffect(
                pagerState = pagerState,
                items = items,
                settlementTracker = settlementTracker,
                onSettled = { item, origin ->
                    viewedVideoIds += item.videoId
                    if (origin == MomentPagerSettlementOrigin.Navigation) {
                        cursorVideoIds += item.videoId
                    }
                },
            )
            VerticalPager(
                state = pagerState,
                key = { page -> items[page].videoId },
                modifier = Modifier.size(width = 360.dp, height = 640.dp),
            ) { page ->
                Text(items[page].videoId)
            }
        }

        composeRule.runOnIdle {
            assertEquals(listOf("older"), viewedVideoIds)
            assertEquals(emptyList<String>(), cursorVideoIds)
            settlementTracker.expectPassiveStartTarget("restored")
            scope.launch { pagerState.scrollToPage(1) }
        }
        composeRule.onNodeWithText("restored").assertIsDisplayed()
        composeRule.runOnIdle {
            assertEquals(listOf("older", "restored"), viewedVideoIds)
            assertEquals(emptyList<String>(), cursorVideoIds)
            scope.launch { pagerState.scrollToPage(2) }
        }
        composeRule.onNodeWithText("newer").assertIsDisplayed()
        composeRule.runOnIdle {
            assertEquals(listOf("older", "restored", "newer"), viewedVideoIds)
            assertEquals(listOf("newer"), cursorVideoIds)
        }
    }

    @Test
    fun removing_the_current_video_advances_to_the_next_video() {
        var items by
            mutableStateOf(
                listOf(
                    momentItem("older"),
                    momentItem("active"),
                    momentItem("newer"),
                )
            )

        composeRule.setContent {
            val pagerItems = rememberMomentPagerSessionItems(items)
            val pagerState = rememberPagerState(initialPage = 1, pageCount = { pagerItems.size })
            VerticalPager(
                state = pagerState,
                key = { page -> pagerItems[page].videoId },
                modifier = Modifier.size(width = 360.dp, height = 640.dp),
            ) { page ->
                Text(pagerItems[page].videoId)
            }
        }

        composeRule.onNodeWithText("active").assertIsDisplayed()
        composeRule.runOnIdle { items = items.filterNot { it.videoId == "active" } }
        composeRule.onNodeWithText("newer").assertIsDisplayed()
    }

    @Test
    fun clamping_after_tail_removal_is_passive_reconciliation() {
        var items by
            mutableStateOf(
                listOf(
                    momentItem("older"),
                    momentItem("newer"),
                    momentItem("active_tail"),
                )
            )
        val origins = mutableListOf<Pair<String, MomentPagerSettlementOrigin>>()
        val cursorVideoIds = mutableListOf<String>()

        composeRule.setContent {
            val pagerItems = rememberMomentPagerSessionItems(items)
            val pagerState = rememberPagerState(initialPage = 2, pageCount = { pagerItems.size })
            val settlementTracker = remember { MomentPagerSettlementTracker() }
            MomentPagerSettlementEffect(
                pagerState = pagerState,
                items = pagerItems,
                settlementTracker = settlementTracker,
                onSettled = { item, origin ->
                    origins += item.videoId to origin
                    if (origin == MomentPagerSettlementOrigin.Navigation) {
                        cursorVideoIds += item.videoId
                    }
                },
            )
            VerticalPager(
                state = pagerState,
                key = { page -> pagerItems[page].videoId },
                modifier = Modifier.size(width = 360.dp, height = 640.dp),
            ) { page ->
                Text(pagerItems[page].videoId)
            }
        }

        composeRule.onNodeWithText("active_tail").assertIsDisplayed()
        composeRule.runOnIdle { items = items.dropLast(1) }
        composeRule.onNodeWithText("newer").assertIsDisplayed()
        composeRule.runOnIdle {
            assertEquals(
                listOf(
                    "active_tail" to MomentPagerSettlementOrigin.Restore,
                    "newer" to MomentPagerSettlementOrigin.Reconciliation,
                ),
                origins,
            )
            assertEquals(emptyList<String>(), cursorVideoIds)
        }
    }

    private fun momentItem(videoId: String): MomentItem =
        MomentItem(
            videoId = videoId,
            channelId = "tiktok_sample",
            authorHandle = "@sample",
            description = videoId,
            likeCount = null,
            isLiked = false,
            isBookmarked = false,
            ownerKind = OwnerKind.TikTokVideo,
        )
}
