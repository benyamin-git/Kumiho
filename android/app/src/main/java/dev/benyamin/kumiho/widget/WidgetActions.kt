package dev.benyamin.kumiho.widget

import android.content.Context
import android.content.Intent
import dev.benyamin.kumiho.KumihoApplication
import dev.benyamin.kumiho.MainActivity
import dev.benyamin.kumiho.vpn.ConnectRequest
import dev.benyamin.kumiho.vpn.TileAction
import dev.benyamin.kumiho.vpn.VpnStatusCache

/** The actions a widget tap can take; consent is settled before [toggle]. */
object WidgetActions {
    fun toggle(context: Context) {
        val app = context.applicationContext as? KumihoApplication ?: return
        val action = WidgetState.reduce(VpnStatusCache.current(), consentGranted = true).action
        when (action) {
            TileAction.DISCONNECT -> app.container.vpnServiceCoordinator.disconnect()
            TileAction.CONNECT -> app.container.vpnServiceCoordinator.connect(ConnectRequest())
            TileAction.OPEN_APP -> open(context)
            TileAction.NONE -> Unit
        }
    }

    fun open(context: Context) {
        context.startActivity(
            Intent(context, MainActivity::class.java)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
        )
    }
}
