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
}
