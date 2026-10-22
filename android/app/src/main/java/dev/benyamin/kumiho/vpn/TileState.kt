package dev.benyamin.kumiho.vpn

import java.util.Locale

/**
 * The tile's process-local snapshot. [state] is the same ten-value UI state the
 * shell renders, so the tile cannot invent a state the service did not report;
 * the remaining fields are display facts from the engine status.
 */
data class TileSnapshot(
    val state: VpnUiState,
    val location: String? = null,
    val proxyOnly: Boolean = false,
    val connectInFlight: Boolean = false,
)

/** The subtitle the tile shows; the service resolves it to copy. */
sealed interface TileSubtitle {
    data object NotConnected : TileSubtitle

    data object Connecting : TileSubtitle

    /** Connected in [location], or with no named location when it is null. */
    data class Connected(val location: String?) : TileSubtitle

    data object ProxyOnly : TileSubtitle

    data object Reconnecting : TileSubtitle

    data object Waiting : TileSubtitle

    data object SignInRequired : TileSubtitle

    data object PermissionNeeded : TileSubtitle

    data object Unavailable : TileSubtitle

    data object Error : TileSubtitle
}

/** What a tap on the tile does in the current presentation. */
enum class TileAction {
    /** Loading, in flight, or nothing truthful to do. */
    NONE,
    CONNECT,
    DISCONNECT,

    /** Signed out or service unavailable: open the app instead of a doomed connect. */
    OPEN_APP,
}

/**
 * The tile's full presentation. [active] maps to `Tile.STATE_ACTIVE` and is
 * true only while the service reports a session (including connecting and
 * reconnecting); [available] is false only when the service is not reachable.
 */
data class TilePresentation(
    val active: Boolean,
    val available: Boolean,
    val subtitle: TileSubtitle,
    val action: TileAction,
    val needsConsent: Boolean = false,
)

/**
 * The pure Quick Settings tile reducer. It is tested on the JVM for every
 * service state; the Android {@link QuickConnectTileService} only maps the
 * result onto `Tile` and the cache.
 *
 * Truthfulness rules:
 * - active only for the service's own in-progress/up states;
 * - a signed-out or unavailable tile never offers a connect that must fail;
 * - an idle tile whose VPN consent is missing offers consent, not a lie;
 * - a connect already in flight ignores further taps.
 */
object TileState {
    /** `rec` is the engine's pseudo-country, not a place to show. */
    private const val RECOMMENDED = "rec"

    fun reduce(snapshot: TileSnapshot?, consentGranted: Boolean): TilePresentation {
        if (snapshot == null) return unavailable()
        if (snapshot.connectInFlight) {
            return TilePresentation(
                active = true,
                available = true,
                subtitle = TileSubtitle.Connecting,
                action = TileAction.NONE,
            )
        }
        return when (snapshot.state) {
            VpnUiState.CONNECTED -> TilePresentation(
                active = true,
                available = true,
                subtitle = TileSubtitle.Connected(location(snapshot.location)),
                action = TileAction.DISCONNECT,
            )
            VpnUiState.PROXY_ONLY -> TilePresentation(
                active = true,
                available = true,
                subtitle = TileSubtitle.ProxyOnly,
                action = TileAction.DISCONNECT,
            )
            VpnUiState.CONNECTING -> TilePresentation(
                active = true,
                available = true,
                subtitle = TileSubtitle.Connecting,
                action = TileAction.DISCONNECT,
            )
            VpnUiState.RECONNECTING -> TilePresentation(
                active = true,
                available = true,
                subtitle = TileSubtitle.Reconnecting,
                action = TileAction.DISCONNECT,
            )
            VpnUiState.WAITING -> TilePresentation(
                active = true,
                available = true,
                subtitle = TileSubtitle.Waiting,
                action = TileAction.DISCONNECT,
            )
            VpnUiState.OFF -> if (consentGranted) {
                TilePresentation(
                    active = false,
                    available = true,
                    subtitle = TileSubtitle.NotConnected,
                    action = TileAction.CONNECT,
                )
            } else {
                permissionNeeded()
            }
            VpnUiState.ERROR -> if (consentGranted) {
                TilePresentation(
                    active = false,
                    available = true,
                    subtitle = TileSubtitle.Error,
                    action = TileAction.CONNECT,
                )
            } else {
                permissionNeeded()
            }
            VpnUiState.PERMISSION_NEEDED -> permissionNeeded()
            VpnUiState.NOT_SIGNED_IN -> TilePresentation(
                active = false,
                available = true,
                subtitle = TileSubtitle.SignInRequired,
                action = TileAction.OPEN_APP,
            )
            VpnUiState.SERVICE_UNAVAILABLE -> unavailable()
        }
    }

    /** The state word the tile shows for a country code, if it has one. */
    private fun location(value: String?): String? {
        val code = value?.trim().orEmpty()
        if (code.isEmpty() || code.equals(RECOMMENDED, ignoreCase = true)) return null
        return code.uppercase(Locale.ROOT)
    }

    private fun permissionNeeded() = TilePresentation(
        active = false,
        available = true,
        subtitle = TileSubtitle.PermissionNeeded,
        action = TileAction.CONNECT,
        needsConsent = true,
    )

    private fun unavailable() = TilePresentation(
        active = false,
        available = false,
        subtitle = TileSubtitle.Unavailable,
        action = TileAction.OPEN_APP,
    )
}
