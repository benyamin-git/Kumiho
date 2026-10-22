package dev.benyamin.kumiho.diagnostics

import org.json.JSONArray
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The wire model the engine's `doctor` expects. The key set is asserted
 * exactly: an extra or renamed key would silently make a check read a fallback.
 */
class AndroidFactsTest {

    @Test
    fun `the default facts are honest and complete`() {
        val facts = AndroidFacts()

        assertEquals("android", facts.device)
        assertEquals(false, facts.vpnPermission)
        assertEquals(false, facts.alwaysOn)
        assertEquals(false, facts.lockdown)
        assertEquals(false, facts.notifications)
        assertEquals(true, facts.batteryOptimized)
        assertEquals(false, facts.network)
        assertEquals(emptyList<String>(), facts.dnsServers)
        assertEquals("off", facts.privateDnsMode)
        assertEquals(false, facts.socksPortListening)
        assertEquals(false, facts.httpProxyPortListening)
        assertEquals(false, facts.serviceRunning)
        assertEquals("", facts.engineState)
        assertEquals(0, facts.sdk)
    }

    @Test
    fun `toJson emits exactly the doctorFacts keys with their types`() {
        val facts = AndroidFacts(
            device = "Google Pixel 8",
            vpnPermission = true,
            alwaysOn = true,
            lockdown = false,
            notifications = true,
            batteryOptimized = false,
            network = true,
            dnsServers = listOf("10.8.0.2", "1.1.1.1"),
            privateDnsMode = AndroidFacts.PRIVATE_DNS_HOSTNAME,
            socksPortListening = true,
            httpProxyPortListening = true,
            serviceRunning = true,
            engineState = "connected",
            sdk = 36,
        )

        val json = facts.toJson()

        assertEquals(
            setOf(
                "device",
                "vpn_permission",
                "always_on",
                "lockdown",
                "notifications",
                "battery_optimized",
                "network",
                "dns_servers",
                "private_dns_mode",
                "socks_port_listening",
                "http_proxy_port_listening",
                "service_running",
                "engine_state",
                "sdk",
            ),
            json.keys().asSequence().toSet(),
        )
        assertEquals("Google Pixel 8", json.getString("device"))
        assertTrue(json.getBoolean("vpn_permission"))
        assertTrue(json.getBoolean("always_on"))
        assertEquals(false, json.getBoolean("lockdown"))
        assertTrue(json.getBoolean("notifications"))
        assertEquals(false, json.getBoolean("battery_optimized"))
        assertTrue(json.getBoolean("network"))
        assertEquals(
            JSONArray(listOf("10.8.0.2", "1.1.1.1")).toString(),
            json.getJSONArray("dns_servers").toString(),
        )
        assertEquals("hostname", json.getString("private_dns_mode"))
        assertTrue(json.getBoolean("socks_port_listening"))
        assertTrue(json.getBoolean("http_proxy_port_listening"))
        assertTrue(json.getBoolean("service_running"))
        assertEquals("connected", json.getString("engine_state"))
        assertEquals(36, json.getInt("sdk"))
    }
}
