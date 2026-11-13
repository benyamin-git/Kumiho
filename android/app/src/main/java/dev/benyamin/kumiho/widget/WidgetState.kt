package dev.benyamin.kumiho.widget

import dev.benyamin.kumiho.vpn.TileAction
import dev.benyamin.kumiho.vpn.TileSnapshot
import dev.benyamin.kumiho.vpn.TileState
import dev.benyamin.kumiho.vpn.TileSubtitle

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

/**
 * What a widget renders. It reuses the tile reducer for truth, actions and
 * consent, then adds the country the 2x1 surface shows.
 */
data class WidgetPresentation(
    val active: Boolean,
    val label: WidgetLabel,
    val location: String?,
    val action: TileAction,
)

object WidgetState {
    fun reduce(snapshot: TileSnapshot?, consentGranted: Boolean): WidgetPresentation {
        val tile = TileState.reduce(snapshot, consentGranted)
        val label = label(tile.subtitle)
        return WidgetPresentation(
            active = tile.active,
            label = label,
            location = if (label in LOCATION_STATES) TileState.location(snapshot?.location) else null,
            action = tile.action,
        )
    }

    /** States whose 2x1 line names the country the engine last reported. */
    private val LOCATION_STATES = setOf(
        WidgetLabel.CONNECTED,
        WidgetLabel.RECONNECTING,
        WidgetLabel.WAITING,
        WidgetLabel.ERROR,
    )

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

}
