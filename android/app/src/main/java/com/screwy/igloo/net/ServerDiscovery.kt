package com.screwy.igloo.net

import android.content.Context
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import java.io.IOException
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.HttpURLConnection
import java.net.Inet4Address
import java.net.InetAddress
import java.net.SocketTimeoutException
import java.net.URL
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.sync.Semaphore
import kotlinx.coroutines.sync.withPermit
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

interface ServerDiscovery {
    suspend fun discover(): List<String>
}

/**
 * Best-effort LAN discovery for the login screen. Like Jellyfin-style clients,
 * this first asks the LAN for servers through UDP broadcast, then falls back to
 * generic hostnames, emulator host addresses, gateways, DNS servers, and the
 * Wi-Fi/Ethernet/VPN IPv4 /24s Android exposes.
 */
class LanServerDiscovery(
    context: Context,
    private val dispatcher: CoroutineDispatcher = Dispatchers.IO,
    private val probe: suspend (String) -> Boolean = { baseUrl -> probeIglooHealth(baseUrl) },
) : ServerDiscovery {

    private val connectivity =
        context.applicationContext.getSystemService(ConnectivityManager::class.java)

    override suspend fun discover(): List<String> = withContext(dispatcher) {
        val urls = (udpDiscoverBaseUrls() + candidateBaseUrls()).distinct()
        val lock = Any()
        val found = linkedSetOf<String>()
        coroutineScope {
            val semaphore = Semaphore(DISCOVERY_PARALLELISM)
            urls.map { url ->
                async {
                    semaphore.withPermit {
                        if (probe(url)) {
                            synchronized(lock) { found += url }
                        }
                    }
                }
            }.awaitAll()
        }
        found.toList()
    }

    private fun udpDiscoverBaseUrls(): List<String> {
        val broadcastHosts = udpBroadcastAddresses()
        if (broadcastHosts.isEmpty()) return emptyList()

        return runCatching {
            val found = linkedSetOf<String>()
            DatagramSocket().use { socket ->
                socket.broadcast = true
                socket.soTimeout = UDP_DISCOVERY_TIMEOUT_MS
                val message = IGLOO_DISCOVERY_MESSAGE.toByteArray(Charsets.UTF_8)
                broadcastHosts.forEach { host ->
                    runCatching {
                        val packet = DatagramPacket(
                            message,
                            message.size,
                            InetAddress.getByName(host),
                            IGLOO_DISCOVERY_PORT,
                        )
                        socket.send(packet)
                    }
                }

                while (true) {
                    val buffer = ByteArray(UDP_DISCOVERY_BUFFER_BYTES)
                    val packet = DatagramPacket(buffer, buffer.size)
                    try {
                        socket.receive(packet)
                    } catch (_: SocketTimeoutException) {
                        break
                    } catch (_: IOException) {
                        break
                    }
                    val body = String(packet.data, 0, packet.length, Charsets.UTF_8)
                    val url = parseIglooDiscoveryAddress(body, packet.address.hostAddress.orEmpty())
                    if (url != null) found += url
                }
            }
            found.toList()
        }.getOrDefault(emptyList())
    }

    private fun udpBroadcastAddresses(): List<String> {
        val hosts = linkedSetOf("255.255.255.255")
        discoveryNetworks().forEach networkLoop@{ network ->
            val capabilities = connectivity.getNetworkCapabilities(network) ?: return@networkLoop
            if (!capabilities.isLocalDiscoveryNetwork()) return@networkLoop
            val links = connectivity.getLinkProperties(network) ?: return@networkLoop
            links.linkAddresses.forEach linkLoop@{ link ->
                val address = link.address as? Inet4Address ?: return@linkLoop
                if (!address.isDiscoveryAddress()) return@linkLoop
                ipv4BroadcastAddress(Ipv4LanAddress(address, link.prefixLength))?.let { hosts += it }
            }
        }
        return hosts.toList()
    }

    private fun candidateBaseUrls(): List<String> {
        val hosts = linkedSetOf<String>()
        hosts += DIRECT_DISCOVERY_HOSTS
        discoveryNetworks().forEach networkLoop@{ network ->
            val capabilities = connectivity.getNetworkCapabilities(network) ?: return@networkLoop
            if (!capabilities.isLocalDiscoveryNetwork()) return@networkLoop
            val links = connectivity.getLinkProperties(network) ?: return@networkLoop
            hosts += directHosts(links)
            hosts += links.linkAddresses
                .mapNotNull { link ->
                    val address = link.address as? Inet4Address ?: return@mapNotNull null
                    if (!address.isDiscoveryAddress()) return@mapNotNull null
                    Ipv4LanAddress(address, link.prefixLength)
                }
                .flatMap { ipv4LanCandidates(it) }
        }
        return candidateBaseUrlsForHosts(hosts.toList())
    }

    private fun discoveryNetworks(): List<Network> {
        val active = connectivity.activeNetwork
        return buildList {
            if (active != null) add(active)
            allConnectivityNetworks().forEach { network ->
                if (network != active) add(network)
            }
        }
    }

    @Suppress("DEPRECATION")
    private fun allConnectivityNetworks(): Array<Network> =
        connectivity.allNetworks

    private fun directHosts(links: LinkProperties): List<String> {
        val hosts = linkedSetOf<String>()
        links.routes
            .mapNotNull { it.gateway as? Inet4Address }
            .filter { it.isDiscoveryAddress() }
            .mapTo(hosts) { it.hostAddress.orEmpty() }
        links.dnsServers
            .mapNotNull { it as? Inet4Address }
            .filter { it.isDiscoveryAddress() }
            .mapTo(hosts) { it.hostAddress.orEmpty() }
        return hosts.filter { it.isNotBlank() }
    }

    private fun NetworkCapabilities.isLocalDiscoveryNetwork(): Boolean =
        hasTransport(NetworkCapabilities.TRANSPORT_WIFI) ||
            hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) ||
            hasTransport(NetworkCapabilities.TRANSPORT_VPN)

    private fun Inet4Address.isDiscoveryAddress(): Boolean =
        !isAnyLocalAddress && !isLoopbackAddress && !isLinkLocalAddress && !isMulticastAddress

    private companion object {
        const val DISCOVERY_PARALLELISM = 32
        const val IGLOO_DISCOVERY_MESSAGE = "who is IglooServer?"
        const val IGLOO_DISCOVERY_PORT = 5001
        const val UDP_DISCOVERY_TIMEOUT_MS = 650
        const val UDP_DISCOVERY_BUFFER_BYTES = 1024
        val DIRECT_DISCOVERY_HOSTS = listOf(
            "igloo.local",
            "igloo",
            "igloo.home.arpa",
            "igloo.lan",
            "10.0.2.2",
            "10.0.3.2",
        )
    }
}

internal data class Ipv4LanAddress(
    val address: Inet4Address,
    val prefixLength: Int,
)

internal fun candidateBaseUrlsForHosts(hosts: List<String>): List<String> =
    hosts.flatMap { host ->
        listOf("http://$host:5001", "https://$host:8443")
    }

internal fun ipv4LanCandidates(link: Ipv4LanAddress): List<String> {
    if (link.prefixLength > 30) return emptyList()
    val prefix = link.prefixLength.coerceAtLeast(24)
    val own = link.address.toIpv4Long()
    val network = own and ipv4Mask(prefix)
    val broadcast = network or (ipv4Mask(prefix) xor 0xffffffffL)
    if (broadcast <= network + 1) return emptyList()
    return ((network + 1) until broadcast)
        .filter { it != own }
        .map(::ipv4String)
}

internal fun ipv4BroadcastAddress(link: Ipv4LanAddress): String? {
    if (link.prefixLength > 30) return null
    val prefix = link.prefixLength.coerceAtLeast(24)
    val network = link.address.toIpv4Long() and ipv4Mask(prefix)
    return ipv4String(network or (ipv4Mask(prefix) xor 0xffffffffL))
}

internal fun parseIglooDiscoveryAddress(body: String, fallbackHost: String): String? =
    runCatching {
        val obj = Json.parseToJsonElement(body).jsonObject
        val product = obj["product"]?.jsonPrimitive?.contentOrNull
        if (!product.equals("Igloo", ignoreCase = true)) return@runCatching null
        val address = obj["address"]?.jsonPrimitive?.contentOrNull
            ?: obj["Address"]?.jsonPrimitive?.contentOrNull
        normalizeDiscoveryAddress(address, fallbackHost)
    }.getOrNull()

internal fun normalizeDiscoveryAddress(address: String?, fallbackHost: String): String? {
    val raw = address?.trim().orEmpty()
    if (raw.isEmpty() && fallbackHost.isBlank()) return null
    val candidate = if (raw.isNotEmpty()) raw else "http://$fallbackHost:5001"
    if (candidate.isBlank()) return null
    val withScheme = if (candidate.startsWith("http://", ignoreCase = true) ||
        candidate.startsWith("https://", ignoreCase = true)
    ) {
        candidate
    } else {
        "http://$candidate"
    }
    return withScheme.trimEnd('/')
}

internal fun probeIglooHealth(baseUrl: String): Boolean {
    val conn = runCatching {
        URL("$baseUrl/api/health/live").openConnection() as HttpURLConnection
    }.getOrElse { return false }
    return try {
        conn.connectTimeout = 450
        conn.readTimeout = 450
        conn.instanceFollowRedirects = false
        if (conn.responseCode !in 200..299) return false
        val buffer = ByteArray(512)
        val read = conn.inputStream.use { it.read(buffer) }
        if (read <= 0) return false
        val body = String(buffer, 0, read, Charsets.UTF_8)
        isIglooHealthBody(body)
    } catch (_: Throwable) {
        false
    } finally {
        conn.disconnect()
    }
}

internal fun isIglooHealthBody(body: String): Boolean =
    runCatching {
        val obj = Json.parseToJsonElement(body).jsonObject
        obj["ok"]?.jsonPrimitive?.booleanOrNull == true ||
            obj["status"]?.jsonPrimitive?.contentOrNull in setOf("live", "healthy")
    }.getOrDefault(false)

private fun ipv4Mask(prefix: Int): Long =
    (0xffffffffL shl (32 - prefix)) and 0xffffffffL

private fun Inet4Address.toIpv4Long(): Long =
    address.fold(0L) { acc, byte -> (acc shl 8) or (byte.toInt() and 0xff).toLong() }

private fun ipv4String(value: Long): String =
    listOf(24, 16, 8, 0).joinToString(".") { shift ->
        (((value shr shift) and 0xff).toString())
    }
