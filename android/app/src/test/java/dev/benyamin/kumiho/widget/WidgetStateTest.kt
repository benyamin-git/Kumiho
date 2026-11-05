package dev.benyamin.kumiho.widget

import dev.benyamin.kumiho.engine.Quota
import dev.benyamin.kumiho.vpn.TileAction
import dev.benyamin.kumiho.vpn.TileSnapshot
import dev.benyamin.kumiho.vpn.VpnUiState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The widget presentation is the tile reducer plus the two facts only a wide
 * widget shows: the country and the remaining quota. These tests pin every
 * service state so a new state cannot silently render as a lie.
 */
class WidgetStateTest {

    @Test
    fun `every service state maps to its label and action`() {
        data class Case(
            val state: VpnUiState,
            val consent: Boolean,
            val label: WidgetLabel,
            val action: TileAction,
            val active: Boolean,
        )

        val cases = listOf(
            Case(VpnUiState.OFF, true, WidgetLabel.NOT_CONNECTED, TileAction.CONNECT, false),
            Case(VpnUiState.OFF, false, WidgetLabel.PERMISSION_NEEDED, TileAction.CONNECT, false),
            Case(VpnUiState.CONNECTED, true, WidgetLabel.CONNECTED, TileAction.DISCONNECT, true),
            Case(VpnUiState.PROXY_ONLY, true, WidgetLabel.PROXY_ONLY, TileAction.DISCONNECT, true),
            Case(VpnUiState.CONNECTING, true, WidgetLabel.CONNECTING, TileAction.DISCONNECT, true),
            Case(VpnUiState.RECONNECTING, true, WidgetLabel.RECONNECTING, TileAction.DISCONNECT, true),
            Case(VpnUiState.WAITING, true, WidgetLabel.WAITING, TileAction.DISCONNECT, true),
            Case(VpnUiState.NOT_SIGNED_IN, true, WidgetLabel.SIGN_IN_REQUIRED, TileAction.OPEN_APP, false),
            Case(VpnUiState.ERROR, true, WidgetLabel.ERROR, TileAction.CONNECT, false),
            Case(VpnUiState.ERROR, false, WidgetLabel.PERMISSION_NEEDED, TileAction.CONNECT, false),
            Case(VpnUiState.PERMISSION_NEEDED, true, WidgetLabel.PERMISSION_NEEDED, TileAction.CONNECT, false),
            Case(VpnUiState.SERVICE_UNAVAILABLE, true, WidgetLabel.UNAVAILABLE, TileAction.OPEN_APP, false),
        )

        for (case in cases) {
            val view = WidgetState.reduce(TileSnapshot(case.state), case.consent)
            assertEquals("${case.state} label", case.label, view.label)
            assertEquals("${case.state} action", case.action, view.action)
            assertEquals("${case.state} active", case.active, view.active)
        }
    }

    @Test
    fun `no snapshot is unavailable`() {
        val view = WidgetState.reduce(null, consentGranted = true)

        assertEquals(WidgetLabel.UNAVAILABLE, view.label)
        assertEquals(TileAction.OPEN_APP, view.action)
        assertNull(view.location)
        assertEquals(WidgetQuota.Hidden, view.quota)
    }

    @Test
    fun `connected shows the uppercase country and hides the pseudo-country`() {
        val germany = WidgetState.reduce(TileSnapshot(VpnUiState.CONNECTED, location = "de"), true)
        assertEquals("DE", germany.location)

        val recommended = WidgetState.reduce(TileSnapshot(VpnUiState.CONNECTED, location = "rec"), true)
        assertNull(recommended.location)
    }

    @Test
    fun `proxy-only never shows a location`() {
        val view = WidgetState.reduce(
            TileSnapshot(VpnUiState.PROXY_ONLY, location = "DE", quota = Quota(100, 25, null)),
            true,
        )

        assertNull(view.location)
        assertEquals(WidgetQuota.Percent(25), view.quota)
    }

    @Test
    fun `quota is a percentage only when both facts are reported`() {
        fun connected(quota: Quota?) = WidgetState.reduce(
            TileSnapshot(VpnUiState.CONNECTED, location = "DE", quota = quota),
            true,
        )

        assertEquals(WidgetQuota.Percent(25), connected(Quota(100, 25, null)).quota)
        assertEquals(WidgetQuota.Unknown, connected(Quota(null, 25, null)).quota)
        assertEquals(WidgetQuota.Unknown, connected(Quota(100, null, null)).quota)
        assertEquals(WidgetQuota.Unknown, connected(Quota(0, 10, null)).quota)
        assertEquals(WidgetQuota.Percent(0), connected(Quota(100, 0, null)).quota)
        assertEquals(WidgetQuota.Percent(100), connected(Quota(100, 250, null)).quota)
    }

    @Test
    fun `quota is hidden while a connect is in flight and when the tunnel is off`() {
        val connecting = WidgetState.reduce(
            TileSnapshot(VpnUiState.CONNECTED, quota = Quota(100, 25, null), connectInFlight = true),
            true,
        )
        assertEquals(WidgetLabel.CONNECTING, connecting.label)
        assertEquals(TileAction.NONE, connecting.action)
        assertEquals(WidgetQuota.Hidden, connecting.quota)

        val off = WidgetState.reduce(TileSnapshot(VpnUiState.OFF, quota = Quota(100, 25, null)), true)
        assertEquals(WidgetQuota.Hidden, off.quota)
    }

    @Test
    fun `quota is visible in every state that reports a session`() {
        for (state in listOf(
            VpnUiState.CONNECTED,
            VpnUiState.RECONNECTING,
            VpnUiState.WAITING,
            VpnUiState.PROXY_ONLY,
            VpnUiState.ERROR,
        )) {
            val view = WidgetState.reduce(
                TileSnapshot(state, quota = Quota(100, 30, null)),
                consentGranted = true,
            )
            assertEquals("$state quota", WidgetQuota.Percent(30), view.quota)
        }

        val signedOut = WidgetState.reduce(
            TileSnapshot(VpnUiState.NOT_SIGNED_IN, quota = Quota(100, 30, null)),
            true,
        )
        assertEquals(WidgetQuota.Hidden, signedOut.quota)
        assertFalse(signedOut.active)
        assertTrue(signedOut.action == TileAction.OPEN_APP)
    }
}
