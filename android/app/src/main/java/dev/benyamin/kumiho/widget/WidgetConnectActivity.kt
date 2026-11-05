package dev.benyamin.kumiho.widget

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.result.contract.ActivityResultContracts
import dev.benyamin.kumiho.permissions.PermissionController
import dev.benyamin.kumiho.vpn.TileAction
import dev.benyamin.kumiho.vpn.VpnStatusCache

/**
 * The transparent target for every widget toggle. It settles the action
 * without showing UI: a plain connect or disconnect runs immediately, and a
 * missing VPN consent opens the system dialog first.
 *
 * Widget taps come from the launcher, so this is also the path that avoids
 * background-activity and foreground-service start restrictions.
 */
class WidgetConnectActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // shortcut: a rotation while the system consent dialog is up drops the
        // tap; the user taps the widget again.
        if (savedInstanceState != null) {
            finish()
            return
        }

        val prepare = PermissionController.vpnPrepareIntent(this)
        val action = WidgetState.reduce(VpnStatusCache.current(), consentGranted = prepare == null).action
        if (action == TileAction.CONNECT && prepare != null) {
            registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
                if (result.resultCode == RESULT_OK) WidgetActions.toggle(this@WidgetConnectActivity)
                finish()
            }.launch(prepare)
            return
        }
        WidgetActions.toggle(this)
        finish()
    }
}
