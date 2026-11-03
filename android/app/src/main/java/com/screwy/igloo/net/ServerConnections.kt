package com.screwy.igloo.net

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import com.screwy.igloo.auth.AuthRepo
import io.ktor.client.engine.HttpClientEngine
import io.ktor.client.engine.okhttp.OkHttp
import io.ktor.client.request.HttpRequest
import io.ktor.client.request.HttpRequestData
import io.ktor.client.request.HttpResponseData
import io.ktor.http.Url
import io.ktor.util.AttributeKey
import io.ktor.utils.io.InternalAPI
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.atomic.AtomicLong
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import okhttp3.Call
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.OkHttpClient
import okhttp3.Response
import okhttp3.ResponseBody
import okio.ForwardingSource
import okio.buffer

/** Chooses a route without changing the server URL used for account identity. */
class ServerConnections(
    context: Context,
    private val auth: AuthRepo,
    scope: CoroutineScope,
    foreground: ForegroundLifecycleFlow,
) {
    private val connectivity = context.getSystemService(ConnectivityManager::class.java)
    private val revision = AtomicLong()
    private val mutex = Mutex()
    private val networkEngines = mutableMapOf<Network, HttpClientEngine>()
    @Volatile private var selected: Selection? = null
    private val routeKey = AttributeKey<Selection>("IglooServerRoute")

    private data class Selection(
        val revision: Long,
        val main: HttpUrl,
        val local: HttpUrl?,
        val network: Network?,
    ) {
        val url: HttpUrl get() = if (network != null) local ?: main else main
    }

    private val callback = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) { revision.incrementAndGet() }
        override fun onLost(network: Network) {
            revision.incrementAndGet()
            synchronized(networkEngines) { networkEngines.remove(network)?.close() }
        }
        override fun onLinkPropertiesChanged(network: Network, properties: android.net.LinkProperties) {
            revision.incrementAndGet()
        }
        override fun onCapabilitiesChanged(network: Network, capabilities: NetworkCapabilities) {
            revision.incrementAndGet()
        }
    }

    init {
        // Observe physical networks as well as the VPN's default network.
        connectivity.registerNetworkCallback(
            NetworkRequest.Builder().clearCapabilities().build(), callback,
        )
        scope.launch {
            foreground.flow.collect { if (it) revision.incrementAndGet() }
        }
    }

    fun matches(url: String): Boolean {
        val candidate = url.toHttpUrlOrNull() ?: return false
        return configuredUrls().any { candidate.belongsTo(it) }
    }

    private fun configuredUrls(): List<HttpUrl> = listOf(
        auth.serverUrlSync(), auth.localServerUrlFlow.value,
    ).mapNotNull { it.toHttpUrlOrNull() }

    private suspend fun select(): Selection = mutex.withLock {
        var result: Selection
        do {
            val version = revision.get()
            val main = auth.serverUrlSync().toHttpUrlOrNull()
                ?: throw IOException("Invalid server URL")
            val local = auth.localServerUrlFlow.value.toHttpUrlOrNull()
            selected?.takeIf {
                it.revision == version && it.main == main && it.local == local
            }?.let { return@withLock it }
            val preferred = local ?: main
            val network = withContext(Dispatchers.IO) {
                val lanAddress = local != null || runCatching {
                    java.net.InetAddress.getAllByName(main.host).any {
                        it.isSiteLocalAddress || it.isLinkLocalAddress || it.isLoopbackAddress || it.isAnyLocalAddress
                    }
                }.getOrDefault(false)
                if (lanAddress) localNetworks().firstOrNull { probe(preferred, it) } else null
            }
            result = Selection(version, main, local, network)
        } while (result.revision != revision.get())
        selected = result
        android.util.Log.i("Igloo/Net", "server_route=" + if (result.network == null) "main" else "local")
        result
    }

    @Suppress("DEPRECATION")
    private fun localNetworks(): List<Network> = connectivity.allNetworks.filter { network ->
        val caps = connectivity.getNetworkCapabilities(network) ?: return@filter false
        !caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN) &&
            (caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) ||
                caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET))
    }

    private fun probe(base: HttpUrl, network: Network): Boolean {
        val url = base.newBuilder().addPathSegments("api/health/live").build()
        val connection = try {
            network.openConnection(URL(url.toString())) as HttpURLConnection
        } catch (_: IOException) {
            return false
        }
        return try {
            connection.connectTimeout = 750
            connection.readTimeout = 750
            connection.instanceFollowRedirects = false
            connection.responseCode == 200 && connection.inputStream.bufferedReader().use {
                isIglooHealthBody(it.readText())
            }
        } catch (_: IOException) {
            false
        } finally {
            connection.disconnect()
        }
    }

    private val direct = OkHttpClient()
    private var boundNetwork: Network? = null
    private var boundClient: OkHttpClient? = null

    @Synchronized
    private fun transport(network: Network?): OkHttpClient {
        if (network == null) return direct
        if (boundNetwork != network) {
            boundClient = OkHttpClient.Builder()
                .socketFactory(network.socketFactory)
                .dns { host -> network.getAllByName(host).toList() }
                .build()
            boundNetwork = network
        }
        return boundClient!!
    }

    private fun target(url: HttpUrl, route: Selection): HttpUrl {
        val source = configuredUrls().filter { url.belongsTo(it) }.maxByOrNull { it.encodedPath.length } ?: return url
        val suffix = url.encodedPath.removePrefix(source.encodedPath.trimEnd('/'))
        return route.url.newBuilder()
            .encodedPath(route.url.encodedPath.trimEnd('/') + suffix)
            .encodedQuery(url.encodedQuery)
            .build()
    }

    fun transportFailed(request: HttpRequest) {
        request.attributes.getOrNull(routeKey)?.let(::transportFailed)
    }

    private fun transportFailed(route: Selection) {
        if (selected != route || route.network == null) return
        selected = route.copy(network = null)
    }

    /** Ktor keeps ownership of request cancellation, timeouts, and response streaming. */
    @OptIn(InternalAPI::class)
    fun engine(): HttpClientEngine {
        val defaultEngine = OkHttp.create { preconfigured = direct }
        return object : HttpClientEngine {
            override val dispatcher get() = defaultEngine.dispatcher
            override val config get() = defaultEngine.config
            override val coroutineContext get() = defaultEngine.coroutineContext
            override val supportedCapabilities get() = defaultEngine.supportedCapabilities
            override suspend fun execute(data: HttpRequestData): HttpResponseData {
                if (!matches(data.url.toString())) return defaultEngine.execute(data)
                val route = select()
                data.attributes.put(routeKey, route)
                val engine = route.network?.let { network ->
                    synchronized(networkEngines) {
                        networkEngines.getOrPut(network) { OkHttp.create { preconfigured = transport(network) } }
                    }
                } ?: defaultEngine
                val request = HttpRequestData(
                    Url(target(data.url.toString().toHttpUrlOrNull()!!, route).toString()),
                    data.method, data.headers, data.body, data.executionContext, data.attributes,
                )
                return engine.execute(request)
            }

            override fun close() {
                synchronized(networkEngines) { networkEngines.values.forEach { it.close() } }
                defaultEngine.close()
            }
        }
    }

    /** Media3 opens each URL through the same selection as API and image requests. */
    fun mediaCallFactory(): Call.Factory = Call.Factory { request ->
        val route = if (matches(request.url.toString())) runBlocking { select() } else null
        val call = transport(route?.network).newCall(
            if (route == null) request else request.newBuilder().url(target(request.url, route)).build(),
        )
        object : Call by call {
            override fun execute(): Response = try {
                val response = call.execute()
                val body = response.body
                if (route == null) response else {
                    val source = object : ForwardingSource(body.source()) {
                        override fun read(sink: okio.Buffer, byteCount: Long): Long = try {
                            super.read(sink, byteCount)
                        } catch (error: IOException) {
                            if (!call.isCanceled()) transportFailed(route)
                            throw error
                        }
                    }.buffer()
                    response.newBuilder().body(object : ResponseBody() {
                        override fun contentType() = body.contentType()
                        override fun contentLength() = body.contentLength()
                        override fun source() = source
                    }).build()
                }
            } catch (error: IOException) {
                if (route != null && !call.isCanceled()) transportFailed(route)
                throw error
            }
        }
    }
}

private fun HttpUrl.belongsTo(base: HttpUrl): Boolean =
    scheme == base.scheme && host == base.host && port == base.port &&
        (encodedPath == base.encodedPath.trimEnd('/') ||
            encodedPath.startsWith(base.encodedPath.trimEnd('/') + "/"))
