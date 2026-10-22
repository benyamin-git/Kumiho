package dev.benyamin.kumiho.vpn

import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.ui.settings.SplitTunnelUiState
import dev.benyamin.kumiho.ui.settings.splitReconnectRequired
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The split contract end to end on the JVM: the Go `networkConfig` JSON is
 * parsed without recomputation, the committed provider snapshot is what the
 * builder sees, the split/route lists follow the API level, and the reconnect
 * banner only claims a live full tunnel.
 *
 * This complements [VpnInterfaceSpecTest], which owns the exhaustive split and
 * route matrix; here the input comes from the engine's own JSON shape.
 */
class SplitInterfaceSpecTest {

    private val appPackage = "dev.benyamin.kumiho.debug"

    private fun goJson(
        excludes: List<String> = listOf("10.0.0.0/8", "172.16.0.0/12"),
        routes: List<String> = listOf("11.0.0.0/8", "0.0.0.0/8"),
    ): VpnConfig {
        val excludesArray = JSONArray()
        excludes.forEach(excludesArray::put)
        val routesArray = JSONArray()
        routes.forEach(routesArray::put)
        val json = JSONObject()
            .put("mtu", 8500)
            .put("allow_lan", true)
            .put("exclude_cidrs", JSONArray().put("10.8.0.1/32"))
            .put("ipv4_excludes", excludesArray)
            .put("ipv4_routes", routesArray)
            .toString()
        return VpnConfig.parse(json)
    }

    /** The narrow provider seam; the real one is DataStore-backed. */
    private class FakeProvider(var settings: SplitSettings = SplitSettings()) : SplitSettingsProvider {
        override fun snapshot(): SplitSettings = settings
    }

    // ---- engine JSON to interface ---------------------------------------

    @Test
    fun `the Go exclusions are used verbatim on API 33 and never exclude DNS or IPv6`() {
        val config = goJson()
        val spec = buildVpnInterfaceSpec(
            apiLevel = 33,
            config = config,
            split = FakeProvider().snapshot(),
            appPackage = appPackage,
        )

        assertEquals(listOf("10.0.0.0/8", "172.16.0.0/12"), spec.excludedRoutes)
        assertEquals(
            listOf(VpnInterfaceSpec.DEFAULT_ROUTE_IPV4, VpnInterfaceSpec.DEFAULT_ROUTE_IPV6),
            spec.routes,
        )
        assertEquals(listOf(VpnInterfaceSpec.DNS_HOST), spec.dnsServers)
        assertFalse(spec.excludedRoutes.contains("${VpnInterfaceSpec.DNS_HOST}/32"))
        assertTrue(spec.excludedRoutes.none { it.contains(':') })
    }

    @Test
    fun `API 26 to 32 uses the Go complement, never the raw exclusions`() {
        for (api in listOf(26, 30, 32)) {
            val spec = buildVpnInterfaceSpec(
                apiLevel = api,
                config = goJson(),
                split = FakeProvider().snapshot(),
                appPackage = appPackage,
            )
            assertEquals(
                listOf("0.0.0.0/8", "11.0.0.0/8", VpnInterfaceSpec.DEFAULT_ROUTE_IPV6).sorted(),
                spec.routes,
            )
            assertTrue("API $api has no excluded-route list", spec.excludedRoutes.isEmpty())
            assertTrue(spec.routes.none { it.contains(':') && it != VpnInterfaceSpec.DEFAULT_ROUTE_IPV6 })
        }
    }

    // ---- the provider snapshot -----------------------------------------

    @Test
    fun `the builder sees the provider's latest snapshot at connect time`() {
        val provider = FakeProvider(
            SplitSettings(SplitMode.EXCLUDE, setOf("com.example.a")),
        )
        val before = buildVpnInterfaceSpec(33, goJson(), provider.snapshot(), appPackage)
        assertEquals(
            listOf("com.example.a", appPackage).sorted(),
            before.disallowedApps,
        )

        provider.settings = SplitSettings(SplitMode.INCLUDE, setOf("com.example.b"))
        val after = buildVpnInterfaceSpec(33, goJson(), provider.snapshot(), appPackage)
        assertEquals(listOf("com.example.b"), after.allowedApps)
        assertEquals(emptyList<String>(), after.disallowedApps)
    }

    @Test
    fun `each mode maps to one non-mixed app policy`() {
        val off = buildVpnInterfaceSpec(
            33,
            goJson(),
            SplitSettings(SplitMode.OFF),
            appPackage,
        )
        assertEquals(emptyList<String>(), off.allowedApps)
        assertEquals(listOf(appPackage), off.disallowedApps)

        val exclude = buildVpnInterfaceSpec(
            33,
            goJson(),
            SplitSettings(SplitMode.EXCLUDE, setOf("com.example.a")),
            appPackage,
        )
        assertEquals(emptyList<String>(), exclude.allowedApps)
        assertEquals(listOf("com.example.a", appPackage).sorted(), exclude.disallowedApps)

        val include = buildVpnInterfaceSpec(
            33,
            goJson(),
            SplitSettings(SplitMode.INCLUDE, setOf("com.example.a")),
            appPackage,
        )
        assertEquals(listOf("com.example.a"), include.allowedApps)
        assertEquals(emptyList<String>(), include.disallowedApps)
    }

    @Test
    fun `an empty Include both warns and falls back to disallowing Kumiho`() {
        val ui = SplitTunnelUiState(
            settings = SplitSettings(SplitMode.INCLUDE),
            loading = false,
        )
        assertTrue(ui.warnEmptyInclude)

        val spec = buildVpnInterfaceSpec(33, goJson(), ui.settings, appPackage)
        assertEquals(emptyList<String>(), spec.allowedApps)
        assertEquals(listOf(appPackage), spec.disallowedApps)
    }

    @Test
    fun `Kumiho is omitted from Include even if it was somehow selected`() {
        val spec = buildVpnInterfaceSpec(
            33,
            goJson(),
            SplitSettings(SplitMode.INCLUDE, setOf(appPackage, "com.example.a")),
            appPackage,
        )
        assertEquals(listOf("com.example.a"), spec.allowedApps)
        assertFalse(spec.allowedApps.contains(appPackage))
    }

    // ---- the reconnect banner ------------------------------------------

    @Test
    fun `a live full tunnel requires a reconnect while Off or proxy-only does not`() {
        assertFalse(splitReconnectRequired(ServiceState(binding = true)))
        assertFalse(splitReconnectRequired(statusService("IDLE")))
        assertFalse(splitReconnectRequired(unauthenticatedService()))
        assertFalse(splitReconnectRequired(statusService("PROXY_ONLY")))
        assertFalse(splitReconnectRequired(statusService("FATAL")))

        assertTrue(splitReconnectRequired(statusService("CONNECTED")))
        assertTrue(splitReconnectRequired(statusService("CONNECTING")))
        assertTrue(splitReconnectRequired(statusService("RECONNECTING")))
        assertTrue(splitReconnectRequired(statusService("WAITING_NETWORK")))
    }

    private fun unauthenticatedService() = statusService("UNAUTHENTICATED").let { service ->
        service.copy(status = service.status?.copy(authenticated = false))
    }

    private fun statusService(state: String) = ServiceState(
        binding = true,
        engineReady = true,
        status = EngineStatus(
            state = state,
            authenticated = true,
            email = "user@example.test",
            since = 0,
            tokenExpires = 0,
            passExpires = 0,
            killSwitch = false,
            autoconnect = false,
            location = null,
            server = null,
            proxyOnly = state == "PROXY_ONLY",
            quota = null,
            dns = null,
            ipv6 = null,
            exitIp = null,
            exitCountry = null,
            totals = null,
            rates = null,
        ),
    )
}
