package com.screwy.igloo.moments

import com.screwy.igloo.net.ForegroundLifecycleFlow
import com.screwy.igloo.net.MomentsApi
import com.screwy.igloo.net.Reachability
import com.screwy.igloo.net.TikTokLive
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.stateIn

@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
class TikTokLives(
    api: MomentsApi,
    reachability: Reachability,
    foreground: ForegroundLifecycleFlow,
    scope: CoroutineScope,
) {
    val current: StateFlow<List<TikTokLive>> = combine(reachability.state, foreground.flow) { state, active ->
        active && state !is Reachability.State.Offline
    }.flatMapLatest { online ->
        if (!online) flowOf(emptyList()) else flow {
            while (true) {
                try {
                    emit(api.lives())
                } catch (cancelled: CancellationException) {
                    throw cancelled
                } catch (_: Exception) {
                    emit(emptyList())
                }
                delay(60_000)
            }
        }
    }.stateIn(scope, SharingStarted.WhileSubscribed(5_000), emptyList())
}
