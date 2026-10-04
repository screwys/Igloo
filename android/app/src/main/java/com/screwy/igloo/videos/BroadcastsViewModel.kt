package com.screwy.igloo.videos

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.screwy.igloo.R
import com.screwy.igloo.auth.AuthRepo
import com.screwy.igloo.data.IglooDatabase
import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.data.entity.ChannelDisplay
import com.screwy.igloo.data.entity.displayOrName
import com.screwy.igloo.net.BroadcastsApi
import com.screwy.igloo.net.Reachability
import com.screwy.igloo.ui.UiEffect
import com.screwy.igloo.ui.UiEffects
import com.screwy.igloo.ui.nav.RouteRegistry
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.isActive
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.Json
import java.net.URI
import java.security.MessageDigest

class BroadcastsViewModel(
    private val db: IglooDatabase,
    private val prefs: PreferencesRepo,
    private val auth: AuthRepo,
    private val api: BroadcastsApi,
    private val reachability: Reachability,
    private val uiEffects: UiEffects,
) : ViewModel() {
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true }
    private val cache = MutableStateFlow(BroadcastCache())
    private val cacheMutex = Mutex()
    private var accountKey: String? = null
    private var refreshJob: Job? = null
    private var playbackJob: Job? = null
    private val refreshing = MutableStateFlow(false)
    private val failed = MutableStateFlow(false)
    private val playback = MutableStateFlow<BroadcastPlayback?>(null)
    private val preparingPlayback = MutableStateFlow(false)

    val isRefreshing = refreshing.asStateFlow()
    val syncFailed = failed.asStateFlow()
    val activePlayback = playback.asStateFlow()
    val isPreparingPlayback = preparingPlayback.asStateFlow()
    val broadcastsEnabled = cache.map { it.broadcastsEnabled }
        .stateIn(viewModelScope, SharingStarted.Eagerly, true)
    val accounts: StateFlow<List<ChannelDisplay>> = db.channelReadDao().allFlow()
        .stateIn(viewModelScope, SharingStarted.Eagerly, emptyList())

    val broadcasts: StateFlow<List<BroadcastCard>> = combine(cache, accounts) { value, channels ->
        if (!value.broadcastsEnabled) emptyList() else broadcastCards(value.broadcasts, channels)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    init {
        viewModelScope.launch {
            combine(auth.serverUrlFlow, auth.usernameFlow) { url, username -> broadcastAccountKey(url, username) }
                .distinctUntilChanged().collectLatest { key ->
                    closePlayback()
                    refreshJob?.cancel()
                    refreshing.value = false
                    failed.value = false
                    cacheMutex.withLock {
                        val saved = prefs.broadcastCache(key)?.let {
                            runCatching { json.decodeFromString<BroadcastCache>(it) }.getOrNull()
                        } ?: BroadcastCache()
                        if (currentAccountKey() != key) return@collectLatest
                        accountKey = key
                        cache.value = saved
                    }
                    startRefresh(key)
                }
        }
        viewModelScope.launch {
            reachability.state.collectLatest { state ->
                if (state == Reachability.State.Online && failed.value) {
                    accountKey?.let(::startRefresh)
                }
            }
        }
    }

    fun refresh() {
        val key = accountKey ?: return
        startRefresh(key)
    }

    private fun startRefresh(key: String) {
        refreshJob?.cancel()
        refreshJob = viewModelScope.launch { refreshCache(key) }
    }

    fun playBroadcast(card: BroadcastCard) {
        val key = accountKey ?: return
        if (currentAccountKey() != key) return
        val baseUrl = auth.serverUrlSync().trimEnd('/')
        closePlayback()
        playbackJob = viewModelScope.launch {
            preparingPlayback.value = true
            try {
                if (card.broadcast.liveStatus !in listOf("is_live", "is_upcoming") && db.videoDao().getById(card.id) != null) {
                    if (currentAccountKey() != key) return@launch
                    uiEffects.emit(UiEffect.NavigateTo(RouteRegistry.playerRoute(card.id)))
                    return@launch
                }
                if (currentAccountKey() != key) return@launch
                val response = api.stream(card.id, baseUrl)
                if (currentAccountKey() != key) return@launch
                response.error_message?.takeIf { it.isNotBlank() }?.let {
                    uiEffects.emit(UiEffect.Toast(it))
                    return@launch
                }
                val path = response.manifest_url ?: response.media_url ?: error("Missing stream URL")
                playback.value = BroadcastPlayback(card.title, api.absoluteUrl(path, baseUrl), when (response.manifest_type) {
                    "hls" -> "application/x-mpegURL"
                    "dash" -> "application/dash+xml"
                    else -> response.media_type
                })
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                if (currentAccountKey() == key) uiEffects.emit(UiEffect.ToastRes(R.string.broadcast_playback_failed))
            } finally {
                if (currentCoroutineContext().isActive) preparingPlayback.value = false
            }
        }
    }

    fun closePlayback() {
        playbackJob?.cancel()
        preparingPlayback.value = false
        playback.value = null
    }

    private suspend fun refreshCache(key: String) {
        if (currentAccountKey() != key || accountKey != key) return
        val baseUrl = auth.serverUrlSync().trimEnd('/')
        refreshing.value = true
        try {
            if (currentAccountKey() != key) return
            val response = api.broadcasts(baseUrl)
            cacheMutex.withLock {
                if (accountKey != key || currentAccountKey() != key) return@withLock
                val next = cache.value.copy(broadcasts = response.broadcasts.map {
                    it.copy(thumbnailUrl = api.absoluteUrl(it.thumbnailUrl, baseUrl))
                },
                    broadcastsEnabled = response.broadcasts_enabled)
                cache.value = next
                prefs.setBroadcastCache(key, json.encodeToString(next))
                failed.value = false
            }
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            if (currentAccountKey() == key) failed.value = true
        } finally {
            if (currentCoroutineContext().isActive && currentAccountKey() == key) refreshing.value = false
        }
    }

    private fun currentAccountKey(): String = broadcastAccountKey(auth.serverUrlSync(), auth.usernameSync())

    private fun broadcastCards(broadcasts: List<Broadcast>, channels: List<ChannelDisplay>): List<BroadcastCard> {
        val byId = channels.associateBy { it.channel.channelId }
        val cards = broadcasts.map { row ->
            BroadcastCard(row.videoId, row.channelId, byId[row.channelId]?.displayOrName.orEmpty(), row.title, row)
        }
        return cards.sortedWith(compareBy<BroadcastCard> {
                when (it.broadcast.liveStatus) { "is_live" -> 0; "is_upcoming" -> 1; else -> 2 }
            }.thenByDescending {
                if (it.broadcast.liveStatus == "is_live") it.broadcast.concurrentViewCount ?: Long.MIN_VALUE else Long.MIN_VALUE
            }.thenBy {
                if (it.broadcast.liveStatus == "is_upcoming") it.broadcast.startsAtMs.takeIf { time -> time > 0 } ?: Long.MAX_VALUE else Long.MAX_VALUE
            }.thenByDescending { it.broadcast.publishedAtMs }.thenBy { it.broadcast.sourceRank }
                .thenBy { it.channelId }.thenBy { it.id })
    }
}

private fun broadcastAccountKey(serverUrl: String, username: String?): String {
    val url = AuthRepo.normalizeServerUrl(serverUrl)
    val normalized = runCatching {
        val uri = URI(url)
        URI(uri.scheme.lowercase(), null, uri.host.lowercase(), uri.port,
            uri.path.trimEnd('/'), null, null).toString()
    }.getOrDefault(url)
    return MessageDigest.getInstance("SHA-256")
        .digest((normalized + "\n" + username.orEmpty()).toByteArray())
        .joinToString("") { "%02x".format(it) }
}
