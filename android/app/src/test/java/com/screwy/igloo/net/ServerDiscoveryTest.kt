package com.screwy.igloo.net

import java.net.InetAddress
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ServerDiscoveryTest {

    @Test fun candidateBaseUrlsForHosts_usesIglooHttpAndHttpsPorts() {
        assertEquals(
            listOf(
                "http://192.168.1.10:5001",
                "https://192.168.1.10:8443",
            ),
            candidateBaseUrlsForHosts(listOf("192.168.1.10")),
        )
    }

    @Test fun parseIglooDiscoveryAddress_readsUdpResponseAddress() {
        assertEquals(
            "http://192.168.1.50:5001",
            parseIglooDiscoveryAddress(
                """{"product":"Igloo","name":"Igloo","address":"http://192.168.1.50:5001/"}""",
                fallbackHost = "192.168.1.99",
            ),
        )
    }

    @Test fun parseIglooDiscoveryAddress_usesSenderWhenAddressMissing() {
        assertEquals(
            "http://192.168.1.99:5001",
            parseIglooDiscoveryAddress("""{"product":"Igloo","name":"Igloo"}""", fallbackHost = "192.168.1.99"),
        )
    }

    @Test fun isIglooHealthBody_acceptsLiveHealthAndLegacyOkEnvelope() {
        assertTrue(isIglooHealthBody("""{"status":"live"}"""))
        assertTrue(isIglooHealthBody("""{"ok":true,"status":"healthy"}"""))
        assertFalse(isIglooHealthBody("""{"status":"degraded"}"""))
    }

    @Test fun ipv4LanCandidates_capsBroadNetworksToContaining24AndSkipsOwnAddress() {
        val candidates = ipv4LanCandidates(
            Ipv4LanAddress(
                address = InetAddress.getByName("192.168.4.20") as java.net.Inet4Address,
                prefixLength = 16,
            ),
        )

        assertEquals(253, candidates.size)
        assertTrue(candidates.contains("192.168.4.1"))
        assertTrue(candidates.contains("192.168.4.254"))
        assertFalse(candidates.contains("192.168.4.20"))
        assertFalse(candidates.contains("192.168.3.254"))
    }

    @Test fun ipv4BroadcastAddress_capsBroadNetworksToContaining24() {
        assertEquals(
            "192.168.4.255",
            ipv4BroadcastAddress(
                Ipv4LanAddress(
                    address = InetAddress.getByName("192.168.4.20") as java.net.Inet4Address,
                    prefixLength = 16,
                ),
            ),
        )
    }
}
