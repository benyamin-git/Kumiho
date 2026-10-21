package dev.benyamin.kumiho.vpn

/**
 * The exact interface Kumiho asks `VpnService.Builder` to build. Pure data so
 * the route/split rules can be unit-tested on the JVM; the bridge applies it.
 */
data class VpnInterfaceSpec(
    val addresses: List<String>,
    val dnsServers: List<String>,
    val routes: List<String>,
    val excludedRoutes: List<String>,
    val allowedApps: List<String>,
    val disallowedApps: List<String>,
) {
    companion object {
        /** The engine's in-tunnel IPv4 address and DNS listener (C1). */
        const val TUN_ADDRESS_IPV4 = "10.8.0.2/32"

        /** IPv6 is captured (and then blackholed by the Go fd filter, C5). */
        const val TUN_ADDRESS_IPV6 = "fd00::2/128"

        /** The tunnel DNS server; it must never appear in an exclusion (C1). */
        const val DNS_HOST = "10.8.0.2"

        const val DEFAULT_ROUTE_IPV4 = "0.0.0.0/0"
        const val DEFAULT_ROUTE_IPV6 = "::/0"

        /** The first API level with `Builder.excludeRoute`. */
        const val EXCLUDE_ROUTE_API = 33
    }
}

/**
 * Builds the interface spec for the running platform.
 *
 * - API 33+: the default IPv4 route plus `ipv4_excludes` as `excludeRoute`
 *   entries. The Go complement (`ipv4_routes`) is never used here.
 * - API 26–32: `ipv4_routes` (already the complement) as normal routes; there
 *   is no excluded-route list.
 * - IPv6 always gets `::/0` and is never excluded; the Go filter drops it.
 * - Split: Off blocks only Kumiho (the control plane stays direct); Exclude
 *   blocks the selection plus Kumiho; Include allows only the selection.
 *   Kumiho is never in the allowed list. An empty Include falls back to
 *   Off-style disallowed-Kumiho so the tunnel is never app-less.
 *
 * Every list the engine reports is used verbatim (R12); only ordering is
 * normalized so the builder input is deterministic.
 */
fun buildVpnInterfaceSpec(
    apiLevel: Int,
    config: VpnConfig,
    split: SplitSettings,
    appPackage: String,
): VpnInterfaceSpec {
    require(appPackage.isNotBlank()) { "the app package is required" }

    val routes = if (apiLevel >= VpnInterfaceSpec.EXCLUDE_ROUTE_API) {
        listOf(VpnInterfaceSpec.DEFAULT_ROUTE_IPV4, VpnInterfaceSpec.DEFAULT_ROUTE_IPV6).sorted()
    } else {
        (config.ipv4Routes + VpnInterfaceSpec.DEFAULT_ROUTE_IPV6).sorted()
    }
    val excludedRoutes = if (apiLevel >= VpnInterfaceSpec.EXCLUDE_ROUTE_API) {
        config.ipv4Excludes.sorted()
    } else {
        emptyList()
    }

    val selected = split.packages.filter { it.isNotBlank() }
    val allowedApps: List<String>
    val disallowedApps: List<String>
    when (split.mode) {
        SplitMode.OFF -> {
            allowedApps = emptyList()
            disallowedApps = listOf(appPackage)
        }
        SplitMode.EXCLUDE -> {
            allowedApps = emptyList()
            disallowedApps = (selected + appPackage).distinct().sorted()
        }
        SplitMode.INCLUDE -> {
            val apps = selected.filter { it != appPackage }.distinct().sorted()
            if (apps.isEmpty()) {
                // An include list with no apps would route nothing; fall back
                // to excluding Kumiho itself so the tunnel still works.
                allowedApps = emptyList()
                disallowedApps = listOf(appPackage)
            } else {
                allowedApps = apps
                disallowedApps = emptyList()
            }
        }
    }

    return VpnInterfaceSpec(
        addresses = listOf(VpnInterfaceSpec.TUN_ADDRESS_IPV4, VpnInterfaceSpec.TUN_ADDRESS_IPV6),
        dnsServers = listOf(VpnInterfaceSpec.DNS_HOST),
        routes = routes,
        excludedRoutes = excludedRoutes,
        allowedApps = allowedApps,
        disallowedApps = disallowedApps,
    )
}
