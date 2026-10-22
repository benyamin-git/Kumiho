package dev.benyamin.kumiho.vpn

import android.content.Context
import android.content.pm.PackageManager
import android.net.ConnectivityManager
import android.net.IpPrefix
import android.net.NetworkCapabilities
import android.net.VpnService
import android.os.Build
import dev.benyamin.kumiho.go.mobile.DeviceBridge
import java.net.InetAddress

/**
 * The Kotlin half of the VpnService handshake.
 *
 * The service owns one instance for the engine's whole lifetime. Go calls
 * [configure] with the authoritative route plan before a full-tunnel connect,
 * then [acquireFD] once, on the engine's connect goroutine. The descriptor is
 * detached from its `ParcelFileDescriptor` and handed to Go, which adopts it
 * and closes it; the bridge never keeps a copy.
 *
 * [configure] and [acquireFD] share one lock: a configure that lands while a
 * previous connection is being replaced cannot tear the builder input in half,
 * and the split snapshot is read inside the same critical section.
 */
class KumihoDeviceBridge(
    private val service: VpnService,
    private val splitSettings: SplitSettingsProvider,
) : DeviceBridge {
    private val lock = Any()
    private var config: VpnConfig? = null

    /** Parses and stores the network plan; bad JSON throws to Go. */
    override fun configure(configJSON: String) {
        val parsed = VpnConfig.parse(configJSON)
        synchronized(lock) {
            config = parsed
        }
    }

    /** Builds, establishes and detaches the tunnel interface descriptor. */
    override fun acquireFD(mtu: Long): Long {
        require(mtu in VpnConfig.MIN_MTU.toLong()..VpnConfig.MAX_MTU.toLong()) {
            "mtu out of range: $mtu"
        }
        val spec = synchronized(lock) {
            val current = config
                ?: throw IllegalStateException("configure() must be called before acquireFD()")
            buildVpnInterfaceSpec(
                apiLevel = Build.VERSION.SDK_INT,
                config = current,
                split = splitSettings.snapshot(),
                appPackage = service.packageName,
            )
        }

        val builder = service.Builder()
            .setSession(SESSION_NAME)
            .setMtu(mtu.toInt())

        for (address in spec.addresses) {
            val (host, length) = splitCidr(address)
            builder.addAddress(host, length)
        }
        for (server in spec.dnsServers) {
            builder.addDnsServer(server)
        }
        for (route in spec.routes) {
            val (host, length) = splitCidr(route)
            builder.addRoute(host, length)
        }
        // API 33+ excludes are the Go list verbatim (R12); older releases get
        // the precomputed complement routes instead and no exclusion list.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            for (excluded in spec.excludedRoutes) {
                val (host, length) = splitCidr(excluded)
                builder.excludeRoute(IpPrefix(InetAddress.getByName(host), length))
            }
        }
        for (pkg in spec.allowedApps) {
            addApplication(builder, pkg, allow = true)
        }
        for (pkg in spec.disallowedApps) {
            addApplication(builder, pkg, allow = false)
        }

        val descriptor = builder.establish()
            ?: throw IllegalStateException(
                "establish() returned null; the VPN consent or interface is missing",
            )
        return try {
            descriptor.detachFd().toLong()
        } finally {
            descriptor.close()
        }
    }

    /**
     * True when a validated, non-VPN network has internet access. The VPN
     * interface itself is never treated as the underlying network.
     */
    @Suppress("DEPRECATION") // allNetworks is the documented source in the plan.
    override fun networkAvailable(): Boolean {
        val manager = service.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager
            ?: return false
        return try {
            manager.allNetworks.any { network ->
                val capabilities = manager.getNetworkCapabilities(network) ?: return@any false
                !capabilities.hasTransport(NetworkCapabilities.TRANSPORT_VPN) &&
                    capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) &&
                    capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)
            }
        } catch (_: SecurityException) {
            false
        }
    }

    /**
     * Splits a numeric CIDR (`10.8.0.2/32`, `::/0`) into the address and
     * prefix length the builder's string overloads take. The values come from
     * Go's `netip.Prefix.String()`, never from a hostname.
     */
    private fun splitCidr(cidr: String): Pair<String, Int> {
        val slash = cidr.indexOf('/')
        require(slash > 0 && slash < cidr.lastIndex) { "invalid route prefix: $cidr" }
        val host = cidr.substring(0, slash)
        val length = cidr.substring(slash + 1).toIntOrNull()
            ?: throw IllegalArgumentException("invalid route prefix: $cidr")
        val maxLength = if (host.contains(':')) 128 else 32
        require(length in 0..maxLength) { "invalid route prefix: $cidr" }
        return host to length
    }

    private fun addApplication(builder: VpnService.Builder, packageName: String, allow: Boolean) {
        try {
            if (allow) {
                builder.addAllowedApplication(packageName)
            } else {
                builder.addDisallowedApplication(packageName)
            }
        } catch (_: PackageManager.NameNotFoundException) {
            // An app uninstalled between the snapshot and the build is simply
            // skipped; the rest of the split tunnel still applies.
        }
    }

    private companion object {
        const val SESSION_NAME = "Kumiho"
    }
}
