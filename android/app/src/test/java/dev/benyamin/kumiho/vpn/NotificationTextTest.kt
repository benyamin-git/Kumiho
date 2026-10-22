package dev.benyamin.kumiho.vpn

import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.engine.ByteCounters
import dev.benyamin.kumiho.engine.EngineStatus
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The foreground notification's pure text rules (D30): every UI state has its
 * own word, a session shows uptime and up/down totals, a missing value is
 * absent rather than a fake zero, and the notification never offers Connect.
 */
class NotificationTextTest {

    private fun status(
        state: String = EngineStates.CONNECTED,
        location: String? = null,
        server: String? = null,
        since: Long = 0L,
        up: Long? = null,
        down: Long? = null,
        proxyOnly: Boolean = false,
        authenticated: Boolean = true,
    ) = EngineStatus(
        state = state,
        authenticated = authenticated,
        email = if (authenticated) "user@example.com" else null,
        since = since,
        tokenExpires = 0L,
        passExpires = 0L,
        killSwitch = true,
        autoconnect = true,
        location = location,
        server = server,
        proxyOnly = proxyOnly,
        quota = null,
        dns = null,
        ipv6 = null,
        exitIp = null,
        exitCountry = null,
        totals = if (up == null && down == null) null else ByteCounters(up = up ?: 0L, down = down ?: 0L),
        rates = null,
    )

    @Test
    fun `every state has its own notification word`() {
        val expected = mapOf(
            VpnUiState.OFF to R.string.notification_state_off,
            VpnUiState.CONNECTED to R.string.notification_state_connected,
            VpnUiState.PROXY_ONLY to R.string.notification_state_proxy_only,
            VpnUiState.CONNECTING to R.string.notification_state_connecting,
            VpnUiState.RECONNECTING to R.string.notification_state_reconnecting,
            VpnUiState.WAITING to R.string.notification_state_waiting,
            VpnUiState.NOT_SIGNED_IN to R.string.notification_state_not_signed_in,
            VpnUiState.ERROR to R.string.notification_state_error,
            VpnUiState.SERVICE_UNAVAILABLE to R.string.notification_state_unavailable,
            VpnUiState.PERMISSION_NEEDED to R.string.notification_state_permission_needed,
        )
        assertEquals("every UI state is covered", VpnUiState.entries.size, expected.size)
        VpnUiState.entries.forEach { state ->
            assertEquals(state.name, expected.getValue(state), NotificationText.stateRes(state))
        }
    }

    @Test
    fun `a connected session shows its location, uptime and totals`() {
        val since = 1_700_000_000L
        val content = NotificationText.content(
            state = VpnUiState.CONNECTED,
            status = status(
                location = "DE",
                since = since,
                up = 12_897_485L,
                down = 47_817_523L,
            ),
            nowSeconds = since + 3_720L,
        )

        assertEquals(R.string.notification_state_connected, content.stateRes)
        assertEquals("DE", content.location)
        assertEquals("1h 2m", content.uptime)
        assertEquals("12.3 MiB", content.up)
        assertEquals("45.6 MiB", content.down)
        assertFalse(content.proxyOnly)
    }

    @Test
    fun `proxy only never claims a tunnel`() {
        val content = NotificationText.content(
            state = VpnUiState.PROXY_ONLY,
            status = status(state = EngineStates.PROXY_ONLY, proxyOnly = true, location = "NL"),
            nowSeconds = 1_700_000_100L,
        )

        assertEquals(R.string.notification_state_proxy_only, content.stateRes)
        assertTrue(content.proxyOnly)
    }

    @Test
    fun `a state without a session hides uptime and traffic`() {
        // The engine may still carry the previous session's counters while
        // reconnecting; the notification only shows them while a session is up.
        val oldNumbers = status(since = 1_700_000_000L, up = 1_024L, down = 2_048L)
        listOf(
            VpnUiState.CONNECTING,
            VpnUiState.RECONNECTING,
            VpnUiState.WAITING,
            VpnUiState.ERROR,
            VpnUiState.OFF,
        ).forEach { state ->
            val content = NotificationText.content(state, oldNumbers, 1_700_000_100L)
            assertNull("$state uptime", content.uptime)
            assertNull("$state up", content.up)
            assertNull("$state down", content.down)
        }
    }

    @Test
    fun `a session without numbers shows nothing fake`() {
        val content = NotificationText.content(
            state = VpnUiState.CONNECTED,
            status = status(location = null, server = null),
            nowSeconds = 1_700_000_000L,
        )

        assertNull(content.location)
        assertNull(content.uptime)
        assertNull(content.up)
        assertNull(content.down)
    }

    @Test
    fun `zero totals are real values`() {
        val content = NotificationText.content(
            state = VpnUiState.CONNECTED,
            status = status(up = 0L, down = 0L),
            nowSeconds = 1_700_000_000L,
        )

        assertEquals("0 B", content.up)
        assertEquals("0 B", content.down)
    }

    @Test
    fun `a recommended selection falls back to the endpoint host`() {
        assertEquals(
            "edge.example.net",
            NotificationText.location(status(location = "rec", server = "edge.example.net:443")),
        )
        assertEquals(
            "edge.example.net",
            NotificationText.location(status(location = null, server = "edge.example.net:443")),
        )
        assertNull(NotificationText.location(status(location = "rec", server = null)))
        assertNull(NotificationText.location(null))
    }

    @Test
    fun `uptime formatting matches the dashboard`() {
        assertEquals("0s", NotificationText.uptime(100L, 100L))
        assertEquals("42s", NotificationText.uptime(100L, 142L))
        assertEquals("3m 20s", NotificationText.uptime(100L, 300L))
        assertEquals("2h 5m", NotificationText.uptime(100L, 7_600L))
        assertEquals("0s", NotificationText.uptime(200L, 100L))
        assertNull(NotificationText.uptime(0L, 100L))
    }

    @Test
    fun `bytes use binary units`() {
        assertEquals("0 B", NotificationText.humanBytes(0L))
        assertEquals("512 B", NotificationText.humanBytes(512L))
        assertEquals("1.0 KiB", NotificationText.humanBytes(1_024L))
        assertEquals("1.5 MiB", NotificationText.humanBytes(1_572_864L))
        assertEquals("12.3 GiB", NotificationText.humanBytes(13_207_024_436L))
    }

    @Test
    fun `only a live session keeps the minute ticker`() {
        assertTrue(NotificationText.shouldTick(VpnUiState.CONNECTED))
        assertTrue(NotificationText.shouldTick(VpnUiState.PROXY_ONLY))
        VpnUiState.entries
            .filter { it != VpnUiState.CONNECTED && it != VpnUiState.PROXY_ONLY }
            .forEach { assertFalse("$it must not tick", NotificationText.shouldTick(it)) }
    }

    @Test
    fun `the status notification has no connect action`() {
        assertEquals(
            listOf(NotificationAction.DISCONNECT, NotificationAction.OPEN),
            NotificationText.ACTIONS,
        )
        assertEquals(2, NotificationAction.entries.size)
        assertFalse(
            "D30: the notification must never offer Connect",
            NotificationAction.entries.any { it.name == "CONNECT" },
        )
    }

    @Test
    fun `the two channels keep status quiet and alerts noticeable`() {
        assertEquals("connection", NotificationFactory.CHANNEL_ID)
        assertEquals("alerts", NotificationFactory.ALERT_CHANNEL_ID)
        assertTrue(
            "alerts must outrank the silent status channel",
            NotificationFactory.ALERT_IMPORTANCE > NotificationFactory.STATUS_IMPORTANCE,
        )
        assertEquals(NotificationFactory.FOREGROUND_ID, 1)
        assertEquals("the sign-in prompt keeps its id", NotificationFactory.SIGN_IN_ID, 2)
        assertEquals("errors get their own id", 3, NotificationFactory.ERROR_ID)
    }
}
