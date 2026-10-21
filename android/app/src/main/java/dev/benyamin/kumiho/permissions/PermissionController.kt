package dev.benyamin.kumiho.permissions

import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Build
import android.provider.Settings
import androidx.core.content.ContextCompat

/**
 * The permission and system-settings reads the setup flow (T10) and the
 * service share. ActivityResult launchers stay in T10; this object only
 * produces intents and answers questions.
 */
object PermissionController {
    /** The consent intent, or null when Kumiho already holds VPN consent. */
    fun vpnPrepareIntent(context: Context): Intent? = VpnService.prepare(context)

    /** The system Always-on VPN / lockdown screen. */
    fun vpnSettingsIntent(): Intent = Intent(Settings.ACTION_VPN_SETTINGS)

    /** This app's notification settings. */
    fun notificationSettingsIntent(context: Context): Intent =
        Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS)
            .putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName)

    /** The battery-optimization list; Kumiho is not exempted behind the user. */
    fun batterySettingsIntent(): Intent = Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS)

    /** True when notifications may be posted (always true below API 33). */
    fun hasNotificationPermission(context: Context): Boolean =
        Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU ||
            ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) ==
            PackageManager.PERMISSION_GRANTED

    /**
     * Whether the OS is configured to start this VPN on boot. Android exposes
     * this only on a `VpnService` instance, and only from API 29; older
     * releases report false (the user-facing setup screen still links to the
     * OS setting).
     */
    fun isAlwaysOn(service: VpnService): Boolean =
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q && service.isAlwaysOn()

    /** Whether the OS runs this VPN in lockdown (block traffic if it drops). */
    fun isLockdownEnabled(service: VpnService): Boolean =
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q && service.isLockdownEnabled()
}
