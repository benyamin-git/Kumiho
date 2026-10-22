package dev.benyamin.kumiho.vpn

import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.ui.dashboard.DashboardPhase
import dev.benyamin.kumiho.ui.dashboard.DashboardState
import dev.benyamin.kumiho.ui.settings.splitReconnectRequired
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Proxy-only is the same foreground service without a TUN interface (D33).
 * The T9/T12 path already exists; this test locks the Kotlin contract the path
 * depends on: no interface spec, no VPN consent, no split/Always-on claims, and
 * an honest "Proxy only" state instead of a tunnel.
 */
class ProxyOnlyStateTest {

    @Test
    fun `a proxy only request never builds a Vpn interface spec`() {
        val proxyOnly = ConnectRequest(proxyOnly = true)
        val full = ConnectRequest()

        assertTrue(proxyOnly.proxyOnly)
        assertFalse(
            "Go must never call Configure/AcquireFD, so Kotlin must not ask for consent or a spec",
            proxyOnly.requiresTunInterface,
        )
        assertTrue("full mode still needs the interface", full.requiresTunInterface)
    }

    @Test
    fun `proxy only is its own state, never connected`() {
        assertEquals(VpnUiState.PROXY_ONLY, VpnStateReducer.reduce(service(EngineStates.PROXY_ONLY)))
        assertEquals(
            VpnUiState.PROXY_ONLY,
            VpnStateReducer.reduceForService(service(EngineStates.PROXY_ONLY)),
        )

        val state = DashboardState.fromService(service(EngineStates.PROXY_ONLY))
        assertEquals(DashboardPhase.PROXY_ONLY, state.phase)
        assertTrue("the service still passes traffic locally", state.phase.active)
        assertFalse("proxy-only is not a filled tunnel", state.powerConnected)
    }

    @Test
    fun `proxy only makes no split reconnect claim`() {
        assertFalse(splitReconnectRequired(service(EngineStates.PROXY_ONLY)))
        assertTrue("a real tunnel still asks for the reconnect", splitReconnectRequired(service(EngineStates.CONNECTED)))
    }

    @Test
    fun `proxy only never auto-connects on an Android restart`() {
        assertEquals(
            AlwaysOnDecision.NONE,
            AlwaysOnPolicy.decide(
                alwaysOn = true,
                authenticated = true,
                state = EngineStates.PROXY_ONLY,
                freshStart = true,
            ),
        )
    }

    private fun service(state: String) = ServiceState(
        binding = true,
        engineReady = true,
        status = EngineStatus(
            state = state,
            authenticated = true,
            email = "user@example.com",
            since = 1_700_000_000L,
            tokenExpires = 0L,
            passExpires = 0L,
            killSwitch = true,
            autoconnect = true,
            location = "NL",
            server = "edge.example.net:443",
            proxyOnly = state == EngineStates.PROXY_ONLY,
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
