package com.screwy.igloo.moments

import com.screwy.igloo.ui.nav.RouteRegistry
import java.io.File
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class MomentsGraphBackStackTest {
    @Test
    fun momentsGraphContentRoutesAreRecognized() {
        assertTrue(isMomentsGraphContentRoute(RouteRegistry.Moments.route))
        assertTrue(isMomentsGraphContentRoute(RouteRegistry.AllMoments.route))
        assertFalse(isMomentsGraphContentRoute(RouteRegistry.Feed.route))
        assertFalse(isMomentsGraphContentRoute(RouteRegistry.Bookmarks.route))
        assertFalse(isMomentsGraphContentRoute(null))
    }

    @Test
    fun momentsHostsUseGuardedGraphLookup() {
        listOf("MomentsRoute.kt", "AllMomentsHost.kt").forEach { filename ->
            val text = source("main/java/com/screwy/igloo/moments/$filename")
            assertTrue(
                "$filename should skip outgoing compositions after moments-graph is popped",
                text.contains("rememberMomentsGraphBackStackEntry(navController) ?: return"),
            )
            assertFalse(
                "$filename should not query moments-graph directly",
                text.contains("getBackStackEntry(RouteRegistry.MomentsGraphRoute)"),
            )
        }
    }

    private fun source(relative: String): String {
        val userDir = System.getProperty("user.dir").orEmpty()
        val root = generateSequence(File(userDir).absoluteFile) { it.parentFile }
            .firstOrNull { File(it, "app/src/$relative").isFile }
            ?: error("Could not locate Android source root from $userDir")
        return File(root, "app/src/$relative").readText()
    }
}
