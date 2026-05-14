package com.screwy.igloo

import android.view.ViewGroup
import androidx.compose.ui.platform.ComposeView
import androidx.test.core.app.ActivityScenario
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class MainActivitySmokeTest {

    @Test
    fun appLaunchAttachesComposeRoot() {
        ActivityScenario.launch(MainActivity::class.java).use { scenario ->
            scenario.onActivity { activity ->
                val content = activity.findViewById<ViewGroup>(android.R.id.content)
                assertTrue("Expected Activity content view to contain Compose.", content.childCount > 0)

                val root = content.getChildAt(0)
                assertTrue("Expected MainActivity to attach a Compose root.", root is ComposeView)
                assertTrue("Expected Compose root to be attached to the window.", root.isAttachedToWindow)
            }
        }
    }
}
