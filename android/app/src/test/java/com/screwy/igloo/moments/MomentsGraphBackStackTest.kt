package com.screwy.igloo.moments

import com.screwy.igloo.ui.nav.RouteRegistry
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class MomentsGraphBackStackTest {
    @Test
    fun momentsGraphContentRoutesAreRecognized() {
        assertTrue(isMomentsGraphContentRoute(RouteRegistry.Moments.route))
        assertFalse(isMomentsGraphContentRoute("all-moments"))
        assertFalse(isMomentsGraphContentRoute(RouteRegistry.Feed.route))
        assertFalse(isMomentsGraphContentRoute(RouteRegistry.Bookmarks.route))
        assertFalse(isMomentsGraphContentRoute(null))
    }
}
