package com.screwy.igloo.net

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import java.net.HttpURLConnection
import java.net.Inet4Address
import java.net.URL
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.sync.Semaphore
import kotlinx.coroutines.sync.withPermit
import kotlinx.coroutines.withContext

interface ServerDiscovery {
    suspend fun discover(): List<String>
}

/**
 * Best-effort LAN discovery for the login screen. It scans the active Wi-Fi,
 * Ethernet, or VPN IPv4 /24 for Igloo's native HTTP port and common HTTPS proxy
 * port, then verifies candidates through the unauthenticated liveness endpoint.
 */
class LanServerDiscovery(
    context: Context,
    private val dispatcher: CoroutineDispatcher = Dispatchers.IO,
    private val probe: suspend (String) -> Boolean = { baseUrl -> probeIglooHealth(baseUrl) },
) : ServerDiscovery {

    private val connectivity =
        context.applicationContext.getSystemService(ConnectivityManager::class.java)

    override suspend fun discover(): List<String> = withContext(dispatcher) {
        val urls = candidateBaseUrls()
        if (urls.isEmpty()) return@withContext emptyList()

        val found = linkedSetOf<String>()
        val lock = Any()
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
        urls.filter { it in found }
    }

    private fun candidateBaseUrls(): List<String> {
        val network = connectivity.activeNetwork ?: return emptyList()
        val capabilities = connectivity.getNetworkCapabilities(network) ?: return emptyList()
        if (!capabilities.isLocalDiscoveryNetwork()) return emptyList()
        val links = connectivity.getLinkProperties(network) ?: return emptyList()
        val hosts = links.linkAddresses
            .mapNotNull { link ->
                val address = link.address as? Inet4Address ?: return@mapNotNull null
                if (!address.isDiscoveryAddress()) return@mapNotNull null
                Ipv4LanAddress(address, link.prefixLength)
            }
            .flatMap { ipv4LanCandidates(it) }
            .distinct()
        return candidateBaseUrlsForHosts(hosts)
    }

    private fun NetworkCapabilities.isLocalDiscoveryNetwork(): Boolean =
        hasTransport(NetworkCapabilities.TRANSPORT_WIFI) ||
            hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) ||
            hasTransport(NetworkCapabilities.TRANSPORT_VPN)

    private fun Inet4Address.isDiscoveryAddress(): Boolean =
        !isAnyLocalAddress && !isLoopbackAddress && !isLinkLocalAddress && !isMulticastAddress

    private companion object {
        const val DISCOVERY_PARALLELISM = 32
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
    val mask = (0xffffffffL shl (32 - prefix)) and 0xffffffffL
    val network = own and mask
    val broadcast = network or (mask xor 0xffffffffL)
    if (broadcast <= network + 1) return emptyList()
    return ((network + 1) until broadcast)
        .filter { it != own }
        .map(::ipv4String)
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
        body.contains("\"ok\":true") || body.contains("\"ok\": true")
    } catch (_: Throwable) {
        false
    } finally {
        conn.disconnect()
    }
}

private fun Inet4Address.toIpv4Long(): Long =
    address.fold(0L) { acc, byte -> (acc shl 8) or (byte.toInt() and 0xff).toLong() }

private fun ipv4String(value: Long): String =
    listOf(24, 16, 8, 0).joinToString(".") { shift ->
        (((value shr shift) and 0xff).toString())
    }
