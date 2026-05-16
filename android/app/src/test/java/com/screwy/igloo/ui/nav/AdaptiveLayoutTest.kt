package com.screwy.igloo.ui.nav

import android.content.pm.ActivityInfo
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AdaptiveLayoutTest {

    @Test
    fun width_breakpoint_switches_to_wide_at_six_hundred_dp() {
        assertEquals(IglooLayoutClass.Compact, iglooLayoutClassForWidthDp(599))
        assertEquals(IglooLayoutClass.Wide, iglooLayoutClassForWidthDp(600))
    }

    @Test
    fun sidebar_width_expands_at_eight_forty_dp() {
        assertEquals(240, wideSidebarWidthDp(600))
        assertEquals(240, wideSidebarWidthDp(839))
        assertEquals(280, wideSidebarWidthDp(840))
    }

    @Test
    fun wide_sidebar_applies_to_non_overlay_detail_routes_only_when_policy_allows() {
        assertTrue(routeUsesWideSidebar(RouteRegistry.Player.route))
        assertTrue(routeUsesWideSidebar(RouteRegistry.Thread.route))
        assertFalse(routeUsesWideSidebar(RouteRegistry.Media.route))
        assertFalse(routeUsesWideSidebar(RouteRegistry.Login.route))
    }

    @Test
    fun moments_stage_keeps_nine_by_sixteen_and_clamps_width() {
        assertEquals(
            MomentsStageSizeDp(widthDp = 430, heightDp = 764),
            wideMomentsStageSizeDp(1200, 900),
        )
        assertEquals(
            MomentsStageSizeDp(widthDp = 337, heightDp = 599),
            wideMomentsStageSizeDp(1200, 600),
        )
        assertEquals(
            MomentsStageSizeDp(widthDp = 300, heightDp = 533),
            wideMomentsStageSizeDp(300, 900),
        )
    }

    @Test
    fun route_orientation_keeps_compact_non_player_portrait_and_wide_user_oriented() {
        assertEquals(
            ActivityInfo.SCREEN_ORIENTATION_PORTRAIT,
            routeRequestedOrientation(RouteRegistry.Feed.route, wideLayout = false),
        )
        assertEquals(
            ActivityInfo.SCREEN_ORIENTATION_FULL_USER,
            routeRequestedOrientation(RouteRegistry.Feed.route, wideLayout = true),
        )
        assertEquals(
            null,
            routeRequestedOrientation(RouteRegistry.Player.route, wideLayout = false),
        )
    }
}
