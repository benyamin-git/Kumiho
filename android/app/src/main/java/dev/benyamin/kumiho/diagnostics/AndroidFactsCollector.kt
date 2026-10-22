package dev.benyamin.kumiho.diagnostics

import android.content.Context
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.os.Build
import android.os.PowerManager
import androidx.core.app.NotificationManagerCompat
import dev.benyamin.kumiho.permissions.PermissionController
import dev.benyamin.kumiho.vpn.VpnServiceCoordinator
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.Socket
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * Collects the Android facts for the engine's doctor. Everything that needs an
 * Android API lives here; the wire model and its JSON stay pure in
 * [AndroidFacts].
 *
 * The engine reports the checks, so this collector never decides whether
 * something is wrong. A failed probe reads as false/absent, which the Go checks
 * interpret honestly.
 */
class AndroidFactsCollector(
    private val context: Context,
    private val coordinator: VpnServiceCoordinator,
) {
    suspend fun collect(): AndroidFacts = withContext(Dispatchers.IO) {
        coordinator.refreshPolicy()
        val snapshot = coordinator.state.value
        val engine = coordinator.awaitEngine()
        val settings = runCatching { engine.settings() }.getOrNull()
        val status = snapshot.status ?: runCatching { engine.refreshStatus() }.getOrNull()
        val connectivity = context.getSystemService(Context.CONNECTIVITY_SERVICE)
            as? ConnectivityManager
        val vpnLink = connectivity?.vpnLinkProperties()
        val activeCaps = connectivity?.activeNetwork?.let(connectivity::getNetworkCapabilities)
        val validatedNonVpn = activeCaps?.hasCapability(
            NetworkCapabilities.NET_CAPABILITY_VALIDATED,
        ) == true && !activeCaps.hasTransport(NetworkCapabilities.TRANSPORT_VPN)

        AndroidFacts(
            device = deviceName(),
            vpnPermission = PermissionController.vpnPrepareIntent(context) == null,
            alwaysOn = snapshot.alwaysOn,
            lockdown = snapshot.lockdown,
            notifications = NotificationManagerCompat.from(context).areNotificationsEnabled(),
            batteryOptimized = isBatteryOptimized(),
            network = validatedNonVpn,
            dnsServers = vpnLink?.dnsServers.orEmpty().mapNotNull { it.hostAddress },
            privateDnsMode = vpnLink.privateDnsMode(),
            socksPortListening = isListening(settings?.socksPort),
            httpProxyPortListening = settings?.httpProxy == true &&
                isListening(settings.httpProxyPort),
            serviceRunning = snapshot.engineReady,
            engineState = status?.state.orEmpty(),
            sdk = Build.VERSION.SDK_INT,
        )
    }

    private fun deviceName(): String {
        val parts = listOf(Build.MANUFACTURER, Build.MODEL)
            .mapNotNull { it?.trim()?.takeIf(String::isNotEmpty) }
            .distinct()
        return parts.joinToString(" ").ifEmpty { "android" }
    }

    private fun isBatteryOptimized(): Boolean {
        val power = context.getSystemService(Context.POWER_SERVICE) as? PowerManager
            ?: return true
        return !power.isIgnoringBatteryOptimizations(context.packageName)
    }

    private fun isListening(port: Int?): Boolean {
        if (port == null || port !in 1..65535) return false
        return try {
            Socket().use { socket ->
                socket.connect(
                    InetSocketAddress(InetAddress.getByName(LOOPBACK), port),
                    PROBE_TIMEOUT_MS,
                )
                true
            }
        } catch (_: Exception) {
            false
        }
    }

    private fun ConnectivityManager.vpnLinkProperties(): LinkProperties? {
        val active = activeNetwork
        if (active.isVpn(this)) return getLinkProperties(active)
        @Suppress("DEPRECATION") // allNetworks is the documented source in the plan.
        return allNetworks.firstOrNull { it.isVpn(this) }?.let(::getLinkProperties)
    }

    private fun Network?.isVpn(manager: ConnectivityManager): Boolean =
        this != null &&
            manager.getNetworkCapabilities(this)?.hasTransport(
                NetworkCapabilities.TRANSPORT_VPN,
            ) == true

    private fun LinkProperties?.privateDnsMode(): String {
        if (this == null) return AndroidFacts.PRIVATE_DNS_OFF
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            if (privateDnsServerName != null) return AndroidFacts.PRIVATE_DNS_HOSTNAME
            if (isPrivateDnsActive) return AndroidFacts.PRIVATE_DNS_OPPORTUNISTIC
        }
        return AndroidFacts.PRIVATE_DNS_OFF
    }

    private companion object {
        const val LOOPBACK = "127.0.0.1"
        const val PROBE_TIMEOUT_MS = 250
    }
}
