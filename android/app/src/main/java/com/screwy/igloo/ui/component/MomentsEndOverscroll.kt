package com.screwy.igloo.ui.component

import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.animate
import androidx.compose.animation.core.spring
import androidx.compose.foundation.OverscrollEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.input.nestedscroll.NestedScrollSource
import androidx.compose.ui.node.DelegatableNode
import androidx.compose.ui.unit.Velocity
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

internal class MomentsEndOverscroll(
    private val scope: CoroutineScope,
    private val canScrollForward: () -> Boolean,
    private val nativeEffect: OverscrollEffect?,
) : OverscrollEffect {
    var distance by mutableFloatStateOf(0f)
        private set
    var limit = 1f
    private var pullDistance = 0f
    private var returnJob: Job? = null

    override val node: DelegatableNode = nativeEffect?.node ?: object : Modifier.Node() {}

    override val isInProgress: Boolean
        get() = distance > 0f || nativeEffect?.isInProgress == true

    fun reset() {
        returnJob?.cancel()
        pullDistance = 0f
        distance = 0f
    }

    override fun applyToScroll(
        delta: Offset,
        source: NestedScrollSource,
        performScroll: (Offset) -> Offset,
    ): Offset {
        if (distance == 0f && (delta.y >= 0f || canScrollForward())) {
            return nativeEffect?.applyToScroll(delta, source, performScroll) ?: performScroll(delta)
        }
        if (source != NestedScrollSource.UserInput) return performScroll(delta)
        returnJob?.cancel()
        var remaining = delta
        var relaxed = Offset.Zero
        if (distance > 0f && delta.y > 0f) {
            val consumed = minOf(delta.y, pullDistance)
            pullDistance -= consumed
            relaxed = Offset(0f, consumed)
            remaining -= relaxed
        }
        val consumed = performScroll(remaining)
        val unused = remaining - consumed
        val endPull = if (!canScrollForward() && unused.y < 0f) unused.y else 0f
        pullDistance -= endPull
        distance = limit * pullDistance / (limit + pullDistance)
        return relaxed + consumed + Offset(0f, endPull)
    }

    override suspend fun applyToFling(
        velocity: Velocity,
        performFling: suspend (Velocity) -> Velocity,
    ) {
        if (distance == 0f) {
            if (nativeEffect != null) nativeEffect.applyToFling(velocity, performFling)
            else performFling(velocity)
            return
        }
        val consumed = performFling(velocity)
        returnJob?.cancel()
        val job = scope.launch {
            animate(
                initialValue = distance,
                targetValue = 0f,
                initialVelocity = -(velocity - consumed).y,
                animationSpec = spring(
                    dampingRatio = Spring.DampingRatioNoBouncy,
                    stiffness = Spring.StiffnessMediumLow,
                ),
            ) { value, _ ->
                distance = value.coerceIn(0f, limit * 0.99f)
                pullDistance = limit * distance / (limit - distance)
            }
            pullDistance = 0f
            distance = 0f
        }
        returnJob = job
        job.join()
    }
}
