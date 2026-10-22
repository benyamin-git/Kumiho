package dev.benyamin.kumiho.vpn

import android.annotation.SuppressLint
import android.app.PendingIntent
import android.content.ActivityNotFoundException
import android.content.Intent
import android.graphics.drawable.Icon
import android.os.Build
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.KumihoApplication
import dev.benyamin.kumiho.MainActivity
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.permissions.PermissionController

/**
 * The Quick Settings tile (D11/D47): one tap connects the saved selection or
 * disconnects, without adding a launcher shortcut.
 *
 * The tile is not app UI: it renders the pure [TileState] reducer from the
 * process-local [VpnStatusCache] the coordinator fills. It never claims a
 * connection the service did not report, never connects while signed out or
 * when the service is gone, and ignores a second tap while a connect is in
 * flight. Missing VPN consent goes through the system dialog with a pending
 * request, unlocking first when the device is locked (D51).
 */
class QuickConnectTileService : TileService() {

    override fun onTileAdded() {
        super.onTileAdded()
        render()
    }

    override fun onStartListening() {
        super.onStartListening()
        resumePendingConsent()
        render()
    }

    override fun onClick() {
        super.onClick()
        val container = (application as? KumihoApplication)?.container ?: return
        when (presentation().action) {
            TileAction.DISCONNECT -> {
                container.vpnServiceCoordinator.disconnect()
                render()
            }
            TileAction.CONNECT -> requestConnect(container)
            TileAction.OPEN_APP -> openApp()
            TileAction.NONE -> Unit
        }
    }

    /** Writes the current snapshot onto the system tile; safe when there is none. */
    private fun render() {
        val tile = qsTile ?: return
        val presentation = presentation()
        tile.label = getString(R.string.tile_label)
        // Subtitle is API 29+; on 26-28 the active/inactive state and label are
        // all the platform renders, so nothing is lost by skipping it.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            tile.subtitle = subtitle(presentation.subtitle)
        }
        tile.icon = Icon.createWithResource(this, R.drawable.ic_tile)
        tile.state = when {
            !presentation.available -> Tile.STATE_UNAVAILABLE
            presentation.active -> Tile.STATE_ACTIVE
            else -> Tile.STATE_INACTIVE
        }
        tile.updateTile()
    }

    private fun presentation(): TilePresentation {
        val consentGranted = PermissionController.vpnPrepareIntent(this) == null
        return TileState.reduce(VpnStatusCache.current(), consentGranted)
    }

    /**
     * Connects the engine's saved selection (or REC). Full mode needs Android's
     * VPN consent first: show the system dialog, then connect on the next
     * listening pass only when consent is really granted. Proxy-only sessions
     * have no tile-connect path; the options sheet owns that choice.
     */
    private fun requestConnect(container: AppContainer) {
        val consent = PermissionController.vpnPrepareIntent(this)
        if (consent == null) {
            startConnect(container)
            return
        }
        val ask = Runnable {
            VpnStatusCache.markConsentPending()
            try {
                showTileActivity(consent)
            } catch (_: ActivityNotFoundException) {
                VpnStatusCache.clearConsentPending()
            }
            render()
        }
        runWhenUnlocked(ask)
    }

    private fun startConnect(container: AppContainer) {
        // The cache makes a duplicate tap a no-op until the service answers.
        VpnStatusCache.markConnectRequested()
        container.vpnServiceCoordinator.connect(ConnectRequest())
        render()
    }

    private fun openApp() {
        val intent = Intent(this, MainActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)
        runWhenUnlocked {
            try {
                showTileActivity(intent)
            } catch (_: ActivityNotFoundException) {
                // Nothing to open; the next listening pass keeps the tile honest.
            }
        }
    }

    /**
     * Shows an activity from the tile, collapsing the shade.
     *
     * Android 14 replaced the `Intent` overload with a `PendingIntent` one and
     * made the old one throw for apps targeting 34+, so newer releases get the
     * `PendingIntent` and 26-33 keep the original call. Lint's deprecation
     * check does not model the version guard, hence the local suppression.
     */
    @Suppress("DEPRECATION")
    @SuppressLint("StartActivityAndCollapseDeprecated")
    private fun showTileActivity(intent: Intent) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startActivityAndCollapse(
                PendingIntent.getActivity(
                    this,
                    0,
                    intent,
                    PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
                ),
            )
        } else {
            startActivityAndCollapse(intent)
        }
    }

    /**
     * `startActivityAndCollapse` cannot wait for the consent dialog, so a
     * granted consent is picked up the next time the tile listens: if the
     * request that prompted the dialog is still pending, connect then. A
     * denial clears the pending request once the dialog has had time to close.
     */
    private fun resumePendingConsent() {
        if (!VpnStatusCache.hasPendingConsent()) return
        val container = (application as? KumihoApplication)?.container ?: return
        if (PermissionController.vpnPrepareIntent(this) == null) {
            VpnStatusCache.clearConsentPending()
            startConnect(container)
        } else if (VpnStatusCache.consentPendingExpired()) {
            VpnStatusCache.clearConsentPending()
        }
    }

    /** Tile actions that show UI must wait for the lock screen to go away. */
    private fun runWhenUnlocked(block: Runnable) {
        if (isLocked) unlockAndRun(block) else block.run()
    }

    private fun subtitle(subtitle: TileSubtitle): String = when (subtitle) {
        TileSubtitle.NotConnected -> getString(R.string.tile_subtitle_off)
        TileSubtitle.Connecting -> getString(R.string.tile_subtitle_connecting)
        is TileSubtitle.Connected -> subtitle.location?.let {
            getString(R.string.tile_subtitle_connected_location, it)
        } ?: getString(R.string.tile_subtitle_connected)
        TileSubtitle.ProxyOnly -> getString(R.string.tile_subtitle_proxy_only)
        TileSubtitle.Reconnecting -> getString(R.string.tile_subtitle_reconnecting)
        TileSubtitle.Waiting -> getString(R.string.tile_subtitle_waiting)
        TileSubtitle.SignInRequired -> getString(R.string.tile_subtitle_sign_in)
        TileSubtitle.PermissionNeeded -> getString(R.string.tile_subtitle_permission)
        TileSubtitle.Unavailable -> getString(R.string.tile_subtitle_unavailable)
        TileSubtitle.Error -> getString(R.string.tile_subtitle_error)
    }
}
