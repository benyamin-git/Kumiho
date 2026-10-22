package dev.benyamin.kumiho.vpn

import dev.benyamin.kumiho.engine.EngineStatus
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

/**
 * The Quick Settings tile reducer and its process-local cache. The tile is a
 * system surface, so the logic that decides "is it really connected?" and "what
 * does a tap do?" is pure and tested here for every service state.
 */
class TileStateTest {

    @Before
    fun resetCache() = VpnStatusCache.clear()

    @After
    fun clearCache() = VpnStatusCache.clear()

    // ---- reducer ---------------------------------------------------------

    @Test
    fun `disconnected offers one connect`() {
        val view = TileState.reduce(TileSnapshot(VpnUiState.OFF), consentGranted = true)

        assertFalse(view.active)
        assertTrue(view.available)
        assertEquals(TileSubtitle.NotConnected, view.subtitle)
        assertEquals(TileAction.CONNECT, view.action)
        assertFalse(view.needsConsent)
    }

    @Test
    fun `connecting is active and offers disconnect`() {
        val view = TileState.reduce(TileSnapshot(VpnUiState.CONNECTING), consentGranted = true)

        assertTrue(view.active)
        assertEquals(TileSubtitle.Connecting, view.subtitle)
        assertEquals(TileAction.DISCONNECT, view.action)
    }

    @Test
    fun `connected shows the location and offers disconnect`() {
        val view = TileState.reduce(
            TileSnapshot(VpnUiState.CONNECTED, location = "de"),
            consentGranted = true,
        )

        assertTrue(view.active)
        assertTrue(view.available)
        assertEquals(TileSubtitle.Connected("DE"), view.subtitle)
        assertEquals(TileAction.DISCONNECT, view.action)
    }

    @Test
    fun `connected without a place has no invented location`() {
        val view = TileState.reduce(
            TileSnapshot(VpnUiState.CONNECTED, location = "rec"),
            consentGranted = true,
        )

        assertEquals(TileSubtitle.Connected(null), view.subtitle)
    }

    @Test
    fun `proxy only is active but never a tunnel word`() {
        val view = TileState.reduce(
            TileSnapshot(VpnUiState.PROXY_ONLY, proxyOnly = true, location = "NL"),
            consentGranted = true,
        )

        assertTrue(view.active)
        assertEquals(TileSubtitle.ProxyOnly, view.subtitle)
        assertEquals(TileAction.DISCONNECT, view.action)
    }

    @Test
    fun `reconnecting and waiting are active`() {
        val reconnecting = TileState.reduce(
            TileSnapshot(VpnUiState.RECONNECTING),
            consentGranted = true,
        )
        val waiting = TileState.reduce(TileSnapshot(VpnUiState.WAITING), consentGranted = true)

        assertTrue(reconnecting.active)
        assertEquals(TileSubtitle.Reconnecting, reconnecting.subtitle)
        assertEquals(TileAction.DISCONNECT, reconnecting.action)
        assertTrue(waiting.active)
        assertEquals(TileSubtitle.Waiting, waiting.subtitle)
        assertEquals(TileAction.DISCONNECT, waiting.action)
    }

    @Test
    fun `a missing service is unavailable and never connects`() {
        listOf(
            TileState.reduce(null, consentGranted = true),
            TileState.reduce(TileSnapshot(VpnUiState.SERVICE_UNAVAILABLE), consentGranted = true),
        ).forEach { view ->
            assertFalse(view.active)
            assertFalse(view.available)
            assertEquals(TileSubtitle.Unavailable, view.subtitle)
            assertEquals(TileAction.OPEN_APP, view.action)
        }
    }

    @Test
    fun `signed out opens the app instead of a doomed connect`() {
        val view = TileState.reduce(TileSnapshot(VpnUiState.NOT_SIGNED_IN), consentGranted = true)

        assertFalse(view.active)
        assertTrue(view.available)
        assertEquals(TileSubtitle.SignInRequired, view.subtitle)
        assertEquals(TileAction.OPEN_APP, view.action)
    }

    @Test
    fun `missing consent asks for it before connecting`() {
        listOf(
            TileSnapshot(VpnUiState.OFF),
            TileSnapshot(VpnUiState.PERMISSION_NEEDED),
            TileSnapshot(VpnUiState.ERROR),
        ).forEach { snapshot ->
            val view = TileState.reduce(snapshot, consentGranted = false)
            assertFalse(view.active)
            assertEquals(TileSubtitle.PermissionNeeded, view.subtitle)
            assertEquals(TileAction.CONNECT, view.action)
            assertTrue("${snapshot.state} must request consent", view.needsConsent)
        }
    }

    @Test
    fun `an error retries a connect when consent is held`() {
        val view = TileState.reduce(TileSnapshot(VpnUiState.ERROR), consentGranted = true)

        assertEquals(TileSubtitle.Error, view.subtitle)
        assertEquals(TileAction.CONNECT, view.action)
        assertFalse(view.needsConsent)
    }

    @Test
    fun `a connect in flight ignores a second tap`() {
        val view = TileState.reduce(
            TileSnapshot(VpnUiState.OFF, connectInFlight = true),
            consentGranted = true,
        )

        assertTrue(view.active)
        assertEquals(TileSubtitle.Connecting, view.subtitle)
        assertEquals("no duplicate connect", TileAction.NONE, view.action)
    }

    // ---- cache -----------------------------------------------------------

    @Test
    fun `the cache keeps the latest service snapshot`() {
        VpnStatusCache.update(
            service(EngineStates.CONNECTED, location = "de"),
            nowMillis = 1_000L,
        )

        val snapshot = VpnStatusCache.current(nowMillis = 1_000L)
        assertEquals(VpnUiState.CONNECTED, snapshot?.state)
        assertEquals("de", snapshot?.location)
        assertFalse(snapshot?.connectInFlight == true)
    }

    @Test
    fun `clearing the cache makes the tile unavailable`() {
        VpnStatusCache.update(service(EngineStates.CONNECTED), nowMillis = 1_000L)
        VpnStatusCache.clear()

        assertNull(VpnStatusCache.current(nowMillis = 1_000L))
        val view = TileState.reduce(VpnStatusCache.current(1_000L), consentGranted = true)
        assertEquals(TileSubtitle.Unavailable, view.subtitle)
    }

    @Test
    fun `a requested connect stays in flight only until the service answers`() {
        VpnStatusCache.update(service(EngineStates.IDLE), nowMillis = 1_000L)
        VpnStatusCache.markConnectRequested(nowMillis = 1_000L)
        assertTrue(VpnStatusCache.current(nowMillis = 1_000L)?.connectInFlight == true)

        VpnStatusCache.update(service(EngineStates.CONNECTING), nowMillis = 1_500L)
        assertFalse(VpnStatusCache.current(nowMillis = 1_500L)?.connectInFlight == true)
        assertEquals(VpnUiState.CONNECTING, VpnStatusCache.current(1_500L)?.state)
    }

    @Test
    fun `a lost connect request expires instead of wedging the tile`() {
        VpnStatusCache.update(service(EngineStates.IDLE), nowMillis = 1_000L)
        VpnStatusCache.markConnectRequested(nowMillis = 1_000L)

        val expired = 1_000L + VpnStatusCache.CONNECT_IN_FLIGHT_MILLIS
        assertFalse(VpnStatusCache.current(nowMillis = expired)?.connectInFlight == true)
        val view = TileState.reduce(VpnStatusCache.current(expired), consentGranted = true)
        assertEquals(TileAction.CONNECT, view.action)
    }

    @Test
    fun `an error or sign-in answer clears the in-flight guard`() {
        VpnStatusCache.update(service(EngineStates.IDLE), nowMillis = 1_000L)
        VpnStatusCache.markConnectRequested(nowMillis = 1_000L)
        VpnStatusCache.update(
            service(EngineStates.FATAL, authenticated = true),
            nowMillis = 1_100L,
        )

        assertFalse(VpnStatusCache.current(1_100L)?.connectInFlight == true)
    }

    @Test
    fun `a consent prompt stays pending only while it can still be on screen`() {
        VpnStatusCache.markConsentPending(nowMillis = 1_000L)
        assertTrue(VpnStatusCache.hasPendingConsent())
        assertFalse(
            VpnStatusCache.consentPendingExpired(
                nowMillis = 1_000L + VpnStatusCache.CONSENT_SETTLE_MILLIS - 1L,
            ),
        )
        assertTrue(
            VpnStatusCache.consentPendingExpired(
                nowMillis = 1_000L + VpnStatusCache.CONSENT_SETTLE_MILLIS,
            ),
        )

        VpnStatusCache.clearConsentPending()
        assertFalse(VpnStatusCache.hasPendingConsent())
    }

    // ---- helpers ---------------------------------------------------------

    private fun service(
        state: String,
        location: String? = null,
        authenticated: Boolean = true,
    ) = ServiceState(
        binding = true,
        engineReady = true,
        status = EngineStatus(
            state = state,
            authenticated = authenticated,
            email = if (authenticated) "user@example.com" else null,
            since = 0L,
            tokenExpires = 0L,
            passExpires = 0L,
            killSwitch = true,
            autoconnect = true,
            location = location,
            server = null,
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
