package dev.benyamin.kumiho.widget

import dev.benyamin.kumiho.ui.account.AccountFormat
import dev.benyamin.kumiho.vpn.TileAction
import dev.benyamin.kumiho.vpn.TileSnapshot
import dev.benyamin.kumiho.vpn.TileState
import dev.benyamin.kumiho.vpn.TileSubtitle
import dev.benyamin.kumiho.vpn.VpnUiState

/** The two home-screen widget shapes. */
enum class WidgetSize { TOGGLE_1X1, STATUS_2X1 }

/** The state word a widget shows; the provider resolves it to copy. */
enum class WidgetLabel {
    NOT_CONNECTED,
    CONNECTED,
    CONNECTING,
    PROXY_ONLY,
    RECONNECTING,
    WAITING,
    SIGN_IN_REQUIRED,
    PERMISSION_NEEDED,
    UNAVAILABLE,
    ERROR,
}

/** The quota line: hidden when the tunnel is off, unknown when not reported. */
sealed interface WidgetQuota {
    data object Hidden : WidgetQuota

    data object Unknown : WidgetQuota

    data class Percent(val value: Int) : WidgetQuota
}

/**
 * What a widget renders. It reuses the tile reducer for truth, actions and
 * consent, then adds the country and quota the 2x1 surface shows.
 */
data class WidgetPresentation(
    val active: Boolean,
    val label: WidgetLabel,
    val location: String?,
    val quota: WidgetQuota,
    val action: TileAction,
)

object WidgetState {
    fun reduce(snapshot: TileSnapshot?, consentGranted: Boolean): WidgetPresentation {
        val tile = TileState.reduce(snapshot, consentGranted)
        return WidgetPresentation(
            active = tile.active,
            label = label(tile.subtitle),
            location = (tile.subtitle as? TileSubtitle.Connected)?.location,
            quota = quota(snapshot),
            action = tile.action,
        )
    }

    private fun label(subtitle: TileSubtitle): WidgetLabel = when (subtitle) {
        TileSubtitle.NotConnected -> WidgetLabel.NOT_CONNECTED
        is TileSubtitle.Connected -> WidgetLabel.CONNECTED
        TileSubtitle.Connecting -> WidgetLabel.CONNECTING
        TileSubtitle.ProxyOnly -> WidgetLabel.PROXY_ONLY
        TileSubtitle.Reconnecting -> WidgetLabel.RECONNECTING
        TileSubtitle.Waiting -> WidgetLabel.WAITING
        TileSubtitle.SignInRequired -> WidgetLabel.SIGN_IN_REQUIRED
        TileSubtitle.PermissionNeeded -> WidgetLabel.PERMISSION_NEEDED
        TileSubtitle.Unavailable -> WidgetLabel.UNAVAILABLE
        TileSubtitle.Error -> WidgetLabel.ERROR
    }

    private fun quota(snapshot: TileSnapshot?): WidgetQuota {
        if (snapshot == null || snapshot.connectInFlight) return WidgetQuota.Hidden
        val visible = when (snapshot.state) {
            VpnUiState.CONNECTED,
            VpnUiState.RECONNECTING,
            VpnUiState.WAITING,
            VpnUiState.PROXY_ONLY,
            VpnUiState.ERROR,
            -> true
            else -> false
        }
        if (!visible) return WidgetQuota.Hidden
        val percent = AccountFormat.quotaPercent(snapshot.quota) ?: return WidgetQuota.Unknown
        return WidgetQuota.Percent(percent)
    }
}
