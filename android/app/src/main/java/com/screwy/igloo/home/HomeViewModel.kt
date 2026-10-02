package com.screwy.igloo.home

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.screwy.igloo.R
import com.screwy.igloo.auth.AuthRepo
import com.screwy.igloo.data.IglooDatabase
import com.screwy.igloo.data.PreferencesRepo
import com.screwy.igloo.data.Dearrow
import com.screwy.igloo.data.entity.ChannelDisplay
import com.screwy.igloo.data.entity.displayOrName
import com.screwy.igloo.data.dao.HomeVideoRow
import com.screwy.igloo.data.dao.HomeFeedRow
import com.screwy.igloo.feed.parseFeedMediaDescriptors
import com.screwy.igloo.net.HomeApi
import com.screwy.igloo.net.Reachability
import com.screwy.igloo.ui.UiEffect
import com.screwy.igloo.ui.UiEffects
import com.screwy.igloo.ui.nav.RouteRegistry
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.isActive
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.mapLatest
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.doubleOrNull
import java.net.URI
import java.security.MessageDigest
import java.util.UUID

@OptIn(ExperimentalCoroutinesApi::class)
class HomeViewModel(
    private val db: IglooDatabase,
    private val prefs: PreferencesRepo,
    private val auth: AuthRepo,
    private val api: HomeApi,
    private val reachability: Reachability,
    private val uiEffects: UiEffects,
) : ViewModel() {
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true }
    private val cache = MutableStateFlow(HomeCache())
    private val cacheMutex = Mutex()
    private val syncMutex = Mutex()
    private var accountKey: String? = null
    private var saveJob: Job? = null
    private var refreshJob: Job? = null
    private var playbackJob: Job? = null
    private val refreshing = MutableStateFlow(false)
    private val failed = MutableStateFlow(false)
    private val playback = MutableStateFlow<HomePlayback?>(null)
    private val preparingPlayback = MutableStateFlow(false)

    val layout: StateFlow<HomeLayout> = cache
        .map { it.layout }
        .stateIn(viewModelScope, SharingStarted.Eagerly, HomeLayout())
    val isRefreshing = refreshing.asStateFlow()
    val syncFailed = failed.asStateFlow()
    val activePlayback = playback.asStateFlow()
    val isPreparingPlayback = preparingPlayback.asStateFlow()
    val accounts: StateFlow<List<ChannelDisplay>> = db.channelReadDao().allFlow()
        .stateIn(viewModelScope, SharingStarted.Eagerly, emptyList())

    val content: StateFlow<List<HomeWidgetContent>> = combine(cache, accounts, prefs.dearrowMode(), prefs.storiesWindowHours()) { value, channels, mode, hours ->
        val cutoff = System.currentTimeMillis() - hours * 3_600_000L
        value.layout.widgets.map { widget -> widgetFlow(widget, value, channels, mode, cutoff) }
    }.flatMapLatest { widgets ->
        if (widgets.isEmpty()) flowOf(emptyList()) else combine(widgets) { it.toList() }
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    init {
        viewModelScope.launch {
            combine(auth.serverUrlFlow, auth.usernameFlow) { url, username -> homeAccountKey(url, username) }
                .distinctUntilChanged().collectLatest { key ->
                    closePlayback()
                    saveJob?.cancel()
                    refreshJob?.cancel()
                    refreshing.value = false
                    failed.value = false
                    cacheMutex.withLock {
                        val saved = prefs.homeCache(key)?.let {
                            runCatching { json.decodeFromString<HomeCache>(it) }.getOrNull()
                        } ?: HomeCache()
                        if (currentAccountKey() != key) return@collectLatest
                        accountKey = key
                        cache.value = saved
                    }
                    startRefresh(key)
                }
        }
        viewModelScope.launch {
            reachability.state.collectLatest { state ->
                if (state == Reachability.State.Online && (cache.value.dirty || failed.value)) {
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

    fun setLayout(layout: HomeLayout) = updateLayout { current ->
        current.copy(columns = layout.columns, spacing = layout.spacing)
    }

    private fun updateLayout(update: (HomeLayout) -> HomeLayout) {
        val key = accountKey ?: return
        if (currentAccountKey() != key) return
        val next = cache.value.copy(layout = update(cache.value.layout), dirty = true)
        cache.value = next
        val saved = prefs.persistHomeCache(key, json.encodeToString(next))
        saveJob?.cancel()
        saveJob = viewModelScope.launch {
            saved.join()
            delay(500)
            syncLayout(key)
        }
    }

    fun saveWidget(widget: HomeWidget) = updateLayout { current -> current.copy(
        widgets = current.widgets.map { if (it.id == widget.id) widget else it },
    ) }

    fun addWidget(type: String) = updateLayout { current -> current.copy(
        widgets = current.widgets + HomeWidget(
            id = UUID.randomUUID().toString(), type = type,
            layout = when (type) { "continue", "live" -> "feature"; "moments" -> "portraits"; "starred", "account" -> "editorial"; else -> "cards" },
            starredOnly = type == "starred" || type == "live",
            order = when (type) { "continue", "saved" -> "recent"; "live" -> "live"; else -> "newest" },
        ),
    ) }

    fun removeWidget(id: String) = updateLayout { current -> current.copy(
        widgets = current.widgets.filterNot { it.id == id },
    ) }

    fun duplicateWidget(widget: HomeWidget) = updateLayout { current -> current.copy(
        widgets = current.widgets + widget.copy(id = UUID.randomUUID().toString()),
    ) }

    fun moveWidget(id: String, delta: Int) = updateLayout { current ->
        val widgets = current.widgets.toMutableList()
        val from = widgets.indexOfFirst { it.id == id }
        val to = from + delta
        if (from in widgets.indices && to in widgets.indices) widgets.add(to, widgets.removeAt(from))
        current.copy(widgets = widgets)
    }

    fun playBroadcast(card: HomeCard) {
        val key = accountKey ?: return
        if (currentAccountKey() != key) return
        val baseUrl = auth.serverUrlSync().trimEnd('/')
        closePlayback()
        playbackJob = viewModelScope.launch {
            preparingPlayback.value = true
            try {
                if (card.broadcast?.liveStatus !in listOf("is_live", "is_upcoming") && db.videoDao().getById(card.id) != null) {
                    if (currentAccountKey() != key) return@launch
                    uiEffects.emit(UiEffect.NavigateTo(RouteRegistry.playerRoute(card.id)))
                    return@launch
                }
                if (currentAccountKey() != key) return@launch
                val response = api.stream(card.id, baseUrl)
                if (currentAccountKey() != key) return@launch
                val path = response.manifest_url ?: response.media_url ?: error("Missing stream URL")
                playback.value = HomePlayback(card.title, api.absoluteUrl(path, baseUrl), when (response.manifest_type) {
                    "hls" -> "application/x-mpegURL"
                    "dash" -> "application/dash+xml"
                    else -> response.media_type
                })
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                if (currentAccountKey() == key) uiEffects.emit(UiEffect.ToastRes(R.string.home_playback_failed))
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
            syncLayout(key)
            if (currentAccountKey() != key) return
            val response = api.broadcasts(baseUrl)
            cacheMutex.withLock {
                if (accountKey != key || currentAccountKey() != key) return@withLock
                val next = cache.value.copy(broadcasts = response.broadcasts.map {
                    it.copy(thumbnailUrl = api.absoluteUrl(it.thumbnailUrl, baseUrl))
                },
                    includeReposts = response.include_reposts, includeTagged = response.include_tagged)
                cache.value = next
                prefs.setHomeCache(key, json.encodeToString(next))
            }
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            if (currentAccountKey() == key) failed.value = true
        } finally {
            if (currentCoroutineContext().isActive && currentAccountKey() == key) refreshing.value = false
        }
    }

    private suspend fun syncLayout(key: String) = syncMutex.withLock sync@ {
        val snapshot = cacheMutex.withLock {
            if (accountKey != key || currentAccountKey() != key) return@sync
            cache.value
        }
        val baseUrl = auth.serverUrlSync().trimEnd('/')
        try {
            val remoteLayout = if (snapshot.dirty) {
                api.saveLayout(snapshot.layout, baseUrl)
                snapshot.layout
            } else api.layout(baseUrl)
            cacheMutex.withLock {
                if (accountKey != key || currentAccountKey() != key || cache.value.layout != snapshot.layout) return@withLock
                val next = cache.value.copy(layout = remoteLayout, dirty = false)
                cache.value = next
                prefs.setHomeCache(key, json.encodeToString(next))
            }
            if (currentAccountKey() == key) failed.value = false
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            if (currentAccountKey() == key) failed.value = true
        }
    }

    private fun currentAccountKey(): String = homeAccountKey(auth.serverUrlSync(), auth.usernameSync())

    private fun widgetFlow(widget: HomeWidget, home: HomeCache, channels: List<ChannelDisplay>, dearrowMode: String, storyCutoffMs: Long) =
        if (widget.type == "live") flowOf(HomeWidgetContent(widget, broadcastCards(widget, home.broadcasts, channels)))
        else combine(
            videoRows(widget, home.includeReposts, home.includeTagged, storyCutoffMs),
            feedRows(widget),
        ) { videos, posts ->
            val cards = videos.map { row ->
                val v = row.item.video
                val title = if (widget.type == "saved" && !row.customTitle.isNullOrBlank()) row.customTitle
                    else if (v.ownerKind == "tweet") ""
                    else Dearrow.resolveTitle(dearrowMode, v.title, v.dearrowTitle, v.dearrowTitleCasual)
                val text = v.description?.takeIf { it.isNotBlank() }
                    ?: v.title?.takeUnless { postTitlePlaceholder.matches(it) }.orEmpty()
                HomeCard(v.videoId, v.channelId, row.item.channelName.orEmpty(), row.platform,
                    title, text, row.sortAtMs, reposterName = row.reposterName.orEmpty(),
                    isBookmarked = row.isBookmarked != 0, video = row.item)
            } + posts.map { homeRow ->
                val row = homeRow.item
                val p = row.item
                HomeCard(p.tweetId, p.channelId.orEmpty(), row.channelName.orEmpty(), row.channelPlatform.orEmpty(),
                    if (widget.type == "saved") row.bookmarkCustomTitle?.takeIf { it.isNotBlank() } ?: p.articleTitle.orEmpty()
                    else p.articleTitle.orEmpty(), p.bodyText.orEmpty(),
                    homeRow.sortAtMs, reposterName = homeRow.reposterName.orEmpty(), feed = row)
            }
            val ordered = if (widget.order == "account") cards.sortedWith(
                compareBy<HomeCard> { it.channelName.lowercase() }.thenByDescending { it.sortAtMs }.thenByDescending { it.id },
            ) else cards.sortedWith(compareByDescending<HomeCard> { it.sortAtMs }.thenByDescending { it.id })
            HomeWidgetContent(widget, ordered.take(widget.count))
        }

    private fun videoRows(widget: HomeWidget, includeReposts: Boolean, includeTagged: Boolean, storyCutoffMs: Long) = db.homeReadDao().videosFlow(
        widget.type, widget.platforms.isEmpty(), widget.platforms,
        widget.channels.isEmpty(), widget.channels, widget.contentTypes.isEmpty(), widget.contentTypes,
        widget.starredOnly, includeReposts, includeTagged, storyCutoffMs, widget.order, if (widget.type == "moments") 32 else widget.count,
    ).mapLatest { firstPage ->
        if (widget.type != "moments") firstPage
        else {
            val moments = ArrayList<HomeVideoRow>()
            var page = firstPage
            var offset = 0
            while (true) {
                for (row in page) {
                    if (isMoment(row)) moments.add(row)
                    if (moments.size == widget.count) break
                }
                if (moments.size == widget.count || page.size < 32) break
                offset += page.size
                page = db.homeReadDao().videosPage(
                    widget.type, widget.platforms.isEmpty(), widget.platforms,
                    widget.channels.isEmpty(), widget.channels, widget.contentTypes.isEmpty(), widget.contentTypes,
                    widget.starredOnly, includeReposts, includeTagged, storyCutoffMs, widget.order, 32, offset,
                )
            }
            moments
        }
    }

    private fun isMoment(row: HomeVideoRow): Boolean {
        val video = row.item.video
        if (video.ownerKind != "youtube_video") return true
        val metadata = video.metadataJson?.takeIf { it.isNotBlank() }?.let {
            runCatching { json.parseToJsonElement(it).jsonObject }.getOrNull()
        }
        val duration = metadata?.get("duration")?.jsonPrimitive?.doubleOrNull ?: video.duration?.toDouble() ?: 0.0
        val width = metadata?.get("width")?.jsonPrimitive?.doubleOrNull ?: 0.0
        val height = metadata?.get("height")?.jsonPrimitive?.doubleOrNull ?: 0.0
        return (duration > 0 && duration <= 90) || (width > 0 && height > 0 && height / width > 1.3)
    }

    private fun feedRows(widget: HomeWidget) = db.homeReadDao().feedFlow(
        widget.type, widget.platforms.isEmpty(), widget.platforms, widget.channels.isEmpty(), widget.channels,
        widget.starredOnly, widget.order,
        if (widget.contentTypes.isEmpty() || "post" in widget.contentTypes) widget.count else 32,
    ).mapLatest { firstPage ->
        if (widget.contentTypes.isEmpty() || "post" in widget.contentTypes) firstPage
        else {
            val posts = ArrayList<HomeFeedRow>()
            var page = firstPage
            var offset = 0
            while (true) {
                for (row in page) {
                    val media = parseFeedMediaDescriptors(row.item.item.mediaJson)
                    val kind = when {
                        media.size > 1 -> "slideshow"
                        media.singleOrNull()?.type?.lowercase() in listOf("video", "gif", "animated_gif") -> "video"
                        media.size == 1 -> "image"
                        else -> "post"
                    }
                    if (kind in widget.contentTypes) posts.add(row)
                    if (posts.size == widget.count) break
                }
                if (posts.size == widget.count || page.size < 32) break
                offset += page.size
                page = db.homeReadDao().feedPage(
                    widget.type, widget.platforms.isEmpty(), widget.platforms, widget.channels.isEmpty(), widget.channels,
                    widget.starredOnly, widget.order, 32, offset,
                )
            }
            posts
        }
    }

    private fun broadcastCards(widget: HomeWidget, broadcasts: List<HomeBroadcast>, channels: List<ChannelDisplay>): List<HomeCard> {
        if (widget.platforms.isNotEmpty() && "youtube" !in widget.platforms) return emptyList()
        if (widget.contentTypes.isNotEmpty() && "video" !in widget.contentTypes) return emptyList()
        val byId = channels.associateBy { it.channel.channelId }
        val cards = broadcasts.filter { row ->
            (widget.liveStates.isEmpty() || row.liveStatus in widget.liveStates ||
                (widget.liveStates.any { it == "was_live" || it == "post_live" } &&
                    row.liveStatus !in listOf("is_live", "is_upcoming"))) &&
                (widget.channels.isEmpty() || row.channelId in widget.channels) &&
                (!widget.starredOnly || byId[row.channelId]?.isStarred == 1)
        }.map { row ->
            HomeCard(row.videoId, row.channelId, byId[row.channelId]?.displayOrName.orEmpty(), "youtube",
                row.title, "", row.startsAtMs.takeIf { it > 0 } ?: row.publishedAtMs, broadcast = row)
        }
        return when (widget.order) {
            "account" -> cards.sortedWith(compareBy<HomeCard> { it.channelName.lowercase() }
                .thenByDescending { it.sortAtMs }.thenBy { it.broadcast?.sourceRank }.thenBy { it.id })
            "newest", "recent" -> cards.sortedWith(compareByDescending<HomeCard> { it.sortAtMs }
                .thenBy { it.broadcast?.sourceRank }.thenBy { it.channelId }.thenBy { it.id })
            else -> cards.sortedWith(compareBy<HomeCard> {
                when (it.broadcast?.liveStatus) { "is_live" -> 0; "is_upcoming" -> 1; else -> 2 }
            }.thenByDescending {
                if (it.broadcast?.liveStatus == "is_live") it.broadcast.concurrentViewCount ?: Long.MIN_VALUE else Long.MIN_VALUE
            }.thenBy {
                if (it.broadcast?.liveStatus == "is_upcoming") it.broadcast.startsAtMs.takeIf { time -> time > 0 } ?: Long.MAX_VALUE else Long.MAX_VALUE
            }.thenByDescending { it.broadcast?.publishedAtMs }.thenBy { it.broadcast?.sourceRank }
                .thenBy { it.channelId }.thenBy { it.id })
        }.take(widget.count)
    }
}

private val postTitlePlaceholder = Regex("(?i)^x\\s+post\\s+'?\\d+'?$")

private fun homeAccountKey(serverUrl: String, username: String?): String {
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
