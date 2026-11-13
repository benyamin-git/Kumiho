package dev.benyamin.kumiho.widget

import dev.benyamin.kumiho.vpn.TileAction
import dev.benyamin.kumiho.vpn.TileSnapshot
import dev.benyamin.kumiho.vpn.VpnUiState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * The widget presentation is the tile reducer plus the one fact only a wide
 * widget shows: the country. These tests pin every service state so a new
 * state cannot silently render as a lie.
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
    }

    @Test
    fun `connected shows the uppercase country and hides the pseudo-country`() {
        val germany = WidgetState.reduce(TileSnapshot(VpnUiState.CONNECTED, location = "de"), true)
        assertEquals("DE", germany.location)

        val recommended = WidgetState.reduce(TileSnapshot(VpnUiState.CONNECTED, location = "rec"), true)
        assertNull(recommended.location)
    }

    @Test
    fun `location stays visible while reconnecting waiting and error`() {
        for (state in listOf(VpnUiState.RECONNECTING, VpnUiState.WAITING, VpnUiState.ERROR)) {
            val view = WidgetState.reduce(TileSnapshot(state, location = "jp"), consentGranted = true)
            assertEquals("$state location", "JP", view.location)
        }

        val connecting = WidgetState.reduce(TileSnapshot(VpnUiState.CONNECTING, location = "jp"), true)
        assertNull(connecting.location)
    }

    @Test
    fun `proxy-only never shows a location`() {
        val view = WidgetState.reduce(TileSnapshot(VpnUiState.PROXY_ONLY, location = "DE"), true)

        assertNull(view.location)
    }
}
