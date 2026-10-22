package dev.benyamin.kumiho.diagnostics

import org.json.JSONArray
import org.json.JSONObject

/**
 * The Android state the engine's doctor cannot see for itself. The JSON keys
 * are the exact wire names of `mobile/doctor.go`'s `doctorFacts`; there is no
 * Kotlin duplicate of the checks, only the facts.
 *
 * The default is the honest "nothing known" state: no permission, no network,
 * no listeners, SDK 0. The collector fills it from Android on a background
 * dispatcher; tests build one directly.
 */
data class AndroidFacts(
    val device: String = "android",
    val vpnPermission: Boolean = false,
    val alwaysOn: Boolean = false,
    val lockdown: Boolean = false,
    val notifications: Boolean = false,
    val batteryOptimized: Boolean = true,
    val network: Boolean = false,
    val dnsServers: List<String> = emptyList(),
    val privateDnsMode: String = PRIVATE_DNS_OFF,
    val socksPortListening: Boolean = false,
    val httpProxyPortListening: Boolean = false,
    val serviceRunning: Boolean = false,
    val engineState: String = "",
    val sdk: Int = 0,
) {
    /** The exact `doctorFacts` envelope; no extra key may appear here. */
    fun toJson(): JSONObject = JSONObject().apply {
        put("device", device)
        put("vpn_permission", vpnPermission)
        put("always_on", alwaysOn)
        put("lockdown", lockdown)
        put("notifications", notifications)
        put("battery_optimized", batteryOptimized)
        put("network", network)
        put("dns_servers", JSONArray(dnsServers))
        put("private_dns_mode", privateDnsMode)
        put("socks_port_listening", socksPortListening)
        put("http_proxy_port_listening", httpProxyPortListening)
        put("service_running", serviceRunning)
        put("engine_state", engineState)
        put("sdk", sdk)
    }

    companion object {
        const val PRIVATE_DNS_HOSTNAME = "hostname"
        const val PRIVATE_DNS_OPPORTUNISTIC = "opportunistic"
        const val PRIVATE_DNS_OFF = "off"
    }
}
