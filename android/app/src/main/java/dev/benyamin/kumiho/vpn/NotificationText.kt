package dev.benyamin.kumiho.vpn

import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.engine.EngineStatus
import java.util.Locale

/** One status-notification action; there is deliberately no Connect (D30). */
enum class NotificationAction { DISCONNECT, OPEN }

/**
 * The pure content of the foreground status notification (D30): the state
 * word, the location the engine has persisted, the session uptime and the
 * up/down totals. The factory resolves [stateRes] to copy and never invents a
 * value the engine did not report.
 *
 * A proxy-only session is explicitly not a tunnel: [proxyOnly] follows the
 * state, and the state word says "Proxy only".
 */
data class NotificationContent(
    val state: VpnUiState,
    val stateRes: Int,
    val location: String?,
    val uptime: String?,
    val up: String?,
    val down: String?,
) {
    val proxyOnly: Boolean get() = state == VpnUiState.PROXY_ONLY
}

/**
 * The notification's pure text rules. No Android and no resources: the
 * formatting and the "is there anything honest to show?" decisions are
 * JVM-tested in `NotificationTextTest`, and [NotificationFactory] only maps
 * them onto the channel and the builder.
 */
object NotificationText {
    /** The two actions the ongoing notification offers, in order (D30). */
    val ACTIONS: List<NotificationAction> = listOf(
        NotificationAction.DISCONNECT,
        NotificationAction.OPEN,
    )

    /**
     * Whether the minute ticker must keep running. Only a live session has an
     * uptime and traffic to age; every other state updates on change alone.
     */
    fun shouldTick(state: VpnUiState): Boolean =
        state == VpnUiState.CONNECTED || state == VpnUiState.PROXY_ONLY

    /** The state word resource for [state]; one entry per UI state. */
    fun stateRes(state: VpnUiState): Int = when (state) {
        VpnUiState.OFF -> R.string.notification_state_off
        VpnUiState.CONNECTED -> R.string.notification_state_connected
        VpnUiState.PROXY_ONLY -> R.string.notification_state_proxy_only
        VpnUiState.CONNECTING -> R.string.notification_state_connecting
        VpnUiState.RECONNECTING -> R.string.notification_state_reconnecting
        VpnUiState.WAITING -> R.string.notification_state_waiting
        VpnUiState.NOT_SIGNED_IN -> R.string.notification_state_not_signed_in
        VpnUiState.ERROR -> R.string.notification_state_error
        VpnUiState.SERVICE_UNAVAILABLE -> R.string.notification_state_unavailable
        VpnUiState.PERMISSION_NEEDED -> R.string.notification_state_permission_needed
    }

    /**
     * Builds the content for one status snapshot. Uptime and totals are shown
     * only while a session is up; a connecting or reconnecting state must not
     * present the previous session's numbers as current.
     */
    fun content(
        state: VpnUiState,
        status: EngineStatus?,
        nowSeconds: Long,
    ): NotificationContent {
        val sessionUp = state == VpnUiState.CONNECTED || state == VpnUiState.PROXY_ONLY
        return NotificationContent(
            state = state,
            stateRes = stateRes(state),
            location = location(status),
            uptime = if (sessionUp) uptime(status?.since ?: 0L, nowSeconds) else null,
            up = if (sessionUp) status?.totals?.let { humanBytes(it.up) } else null,
            down = if (sessionUp) status?.totals?.let { humanBytes(it.down) } else null,
        )
    }

    /**
     * The country code the engine persisted, or the endpoint's host when the
     * engine has no named selection. `rec` is the engine's pseudo-country, not
     * a place, so it falls through to the server.
     */
    fun location(status: EngineStatus?): String? {
        val code = status?.location?.trim().orEmpty()
        if (code.isNotEmpty() && !code.equals(RECOMMENDED, ignoreCase = true)) {
            return code.uppercase(Locale.ROOT)
        }
        return hostOf(status?.server)
    }

    /**
     * Session uptime from the engine's `since` (Unix seconds), matching the
     * dashboard: `42s`, `3m 20s`, `2h 5m`. Null without a start time; a future
     * timestamp reads as zero instead of a negative duration.
     */
    fun uptime(since: Long, nowSeconds: Long): String? {
        if (since <= 0L) return null
        val elapsed = (nowSeconds - since).coerceAtLeast(0L)
        return when {
            elapsed < 60 -> "${elapsed}s"
            elapsed < 3600 -> "${elapsed / 60}m ${elapsed % 60}s"
            else -> "${elapsed / 3600}h ${(elapsed % 3600) / 60}m"
        }
    }

    /**
     * A byte count in binary units, exactly as the dashboard writes it:
     * `0 B`, `512 B`, `1.0 KiB`, `12.3 MiB`.
     */
    fun humanBytes(bytes: Long): String {
        if (bytes < UNIT) return "$bytes B"
        var divisor = UNIT
        var exponent = 0
        var value = bytes / UNIT
        while (value >= UNIT) {
            divisor *= UNIT
            exponent++
            value /= UNIT
        }
        val scaled = bytes.toDouble() / divisor.toDouble()
        val suffix = "KMGTPE"[exponent]
        return String.format(Locale.ROOT, "%.1f %ciB", scaled, suffix)
    }

    /** The host part of the engine's `host:port` server string. */
    fun hostOf(server: String?): String? {
        val value = server?.trim().orEmpty()
        if (value.isEmpty()) return null
        val colon = value.lastIndexOf(':')
        if (colon > 0 && value.indexOf(':') == colon) {
            val port = value.substring(colon + 1)
            if (port.isNotEmpty() && port.all { it.isDigit() }) {
                return value.substring(0, colon)
            }
        }
        return value
    }

    private const val UNIT = 1024L
    private const val RECOMMENDED = "rec"
}
