package dev.benyamin.kumiho.vpn

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The interface contract the Go route helper depends on: the authoritative
 * lists are used as given (R12), API 33+ excludes routes while API 26–32 uses
 * the complement, IPv6 is always captured, and the split lists never mix.
 */
class VpnInterfaceSpecTest {

    private val appPackage = "dev.benyamin.kumiho"

    private fun config(
        mtu: Int = 8500,
        allowLan: Boolean = true,
        excludeCidrs: List<String> = emptyList(),
        ipv4Excludes: List<String> = listOf("172.16.0.0/12", "10.0.0.0/8"),
        ipv4Routes: List<String> = listOf("11.0.0.0/8", "0.0.0.0/8"),
    ) = VpnConfig(
        mtu = mtu,
        allowLan = allowLan,
        excludeCidrs = excludeCidrs,
        ipv4Excludes = ipv4Excludes,
        ipv4Routes = ipv4Routes,
    )

    private fun spec(
        apiLevel: Int,
        config: VpnConfig = config(),
        split: SplitSettings = SplitSettings(),
    ) = buildVpnInterfaceSpec(apiLevel, config, split, appPackage)

    // ---- routes ---------------------------------------------------------

    @Test
    fun api26UsesTheComplementRoutesAndNoExclusions() {
        for (api in listOf(26, 30, 32)) {
            val result = spec(api)
            assertEquals(
                listOf("0.0.0.0/8", "11.0.0.0/8", "::/0"),
                result.routes,
            )
            assertEquals(emptyList<String>(), result.excludedRoutes)
        }
    }

    @Test
    fun api33UsesTheDefaultRouteAndExcludes() {
        for (api in listOf(33, 34)) {
            val result = spec(api)
            assertEquals(
                listOf(VpnInterfaceSpec.DEFAULT_ROUTE_IPV4, VpnInterfaceSpec.DEFAULT_ROUTE_IPV6),
                result.routes,
            )
            assertEquals(listOf("10.0.0.0/8", "172.16.0.0/12"), result.excludedRoutes)
        }
    }

    @Test
    fun routeModesAreNeverMixed() {
        val old = spec(32)
        assertTrue(old.excludedRoutes.isEmpty())
        assertFalse(old.routes.contains(VpnInterfaceSpec.DEFAULT_ROUTE_IPV4))

        val modern = spec(33)
        assertEquals(
            listOf(VpnInterfaceSpec.DEFAULT_ROUTE_IPV4, VpnInterfaceSpec.DEFAULT_ROUTE_IPV6),
            modern.routes,
        )
        assertTrue(modern.excludedRoutes.isNotEmpty())
    }

    @Test
    fun ipv6IsAlwaysCapturedAndNeverExcluded() {
        val modern = spec(33)
        assertTrue(modern.routes.contains(VpnInterfaceSpec.DEFAULT_ROUTE_IPV6))
        assertTrue(modern.excludedRoutes.none { it.contains(':') })

        val legacy = spec(26, config(ipv4Routes = listOf("0.0.0.0/1", "128.0.0.0/1")))
        assertTrue(legacy.routes.contains(VpnInterfaceSpec.DEFAULT_ROUTE_IPV6))
        assertTrue(legacy.excludedRoutes.isEmpty())
    }

    @Test
    fun dnsHostIsPresentAndNeverExcluded() {
        for (api in listOf(26, 32, 33, 34)) {
            val result = spec(api)
            assertEquals(listOf(VpnInterfaceSpec.DNS_HOST), result.dnsServers)
            assertFalse(result.excludedRoutes.contains(VpnInterfaceSpec.DNS_HOST))
            assertFalse(result.excludedRoutes.contains("${VpnInterfaceSpec.DNS_HOST}/32"))
        }
    }

    @Test
    fun c1DnsHostStaysRoutedWhenTheUserExcludesItsPrefix() {
        // allow_lan and the raw user CIDR both cover 10.8.0.1, but the Go list
        // (authoritative, C1 already applied) does not. Kotlin must not
        // subtract or recompute anything, and must not emit the host.
        val result = spec(
            33,
            config(
                allowLan = true,
                excludeCidrs = listOf("10.8.0.1/32"),
                ipv4Excludes = listOf("10.0.0.0/8"),
            ),
        )
        assertEquals(listOf("10.0.0.0/8"), result.excludedRoutes)
        assertEquals(listOf(VpnInterfaceSpec.DNS_HOST), result.dnsServers)
        assertFalse(result.excludedRoutes.contains("${VpnInterfaceSpec.DNS_HOST}/32"))
    }

    @Test
    fun theAuthoritativeListsAreNotRecomputed() {
        val excludes = listOf("203.0.113.7/32", "198.51.100.0/24")
        val routes = listOf("0.0.0.0/2", "64.0.0.0/2", "128.0.0.0/2")
        val modern = spec(34, config(ipv4Excludes = excludes, ipv4Routes = routes))
        assertEquals(excludes.sorted(), modern.excludedRoutes)

        val legacy = spec(32, config(ipv4Excludes = excludes, ipv4Routes = routes))
        assertEquals((routes.sorted() + VpnInterfaceSpec.DEFAULT_ROUTE_IPV6).sorted(), legacy.routes)
    }

    @Test
    fun listsAreSortedDeterministically() {
        val first = spec(33, config(ipv4Excludes = listOf("172.16.0.0/12", "10.0.0.0/8")))
        val second = spec(33, config(ipv4Excludes = listOf("10.0.0.0/8", "172.16.0.0/12")))
        assertEquals(first, second)
        assertEquals(listOf("10.0.0.0/8", "172.16.0.0/12"), first.excludedRoutes)
    }

    @Test
    fun addressesAreFixed() {
        val result = spec(33)
        assertEquals(
            listOf(VpnInterfaceSpec.TUN_ADDRESS_IPV4, VpnInterfaceSpec.TUN_ADDRESS_IPV6),
            result.addresses,
        )
    }

    @Test
    fun allowBypassIsOffByDefaultAndCopiedWhenRequested() {
        assertFalse(spec(33).allowBypass)
        assertTrue(
            buildVpnInterfaceSpec(
                apiLevel = 33,
                config = config(),
                split = SplitSettings(),
                appPackage = appPackage,
                allowBypass = true,
            ).allowBypass,
        )
    }

    // ---- split tunnel ---------------------------------------------------

    @Test
    fun splitOffBlocksOnlyKumiho() {
        val result = spec(33)
        assertEquals(emptyList<String>(), result.allowedApps)
        assertEquals(listOf(appPackage), result.disallowedApps)
    }

    @Test
    fun splitExcludeBlocksTheSelectionPlusKumiho() {
        val result = spec(
            33,
            split = SplitSettings(
                mode = SplitMode.EXCLUDE,
                packages = setOf("com.example.b", "com.example.a"),
            ),
        )
        assertEquals(emptyList<String>(), result.allowedApps)
        assertEquals(
            listOf("com.example.a", "com.example.b", appPackage),
            result.disallowedApps,
        )
    }

    @Test
    fun splitIncludeAllowsOnlyTheSelection() {
        val result = spec(
            33,
            split = SplitSettings(
                mode = SplitMode.INCLUDE,
                packages = setOf("com.example.b", "com.example.a"),
            ),
        )
        assertEquals(listOf("com.example.a", "com.example.b"), result.allowedApps)
        assertEquals(emptyList<String>(), result.disallowedApps)
    }

    @Test
    fun kumihoIsNeverAllowedEvenWhenSelected() {
        val result = spec(
            33,
            split = SplitSettings(
                mode = SplitMode.INCLUDE,
                packages = setOf(appPackage, "com.example.a"),
            ),
        )
        assertEquals(listOf("com.example.a"), result.allowedApps)
        assertFalse(result.allowedApps.contains(appPackage))
        assertEquals(emptyList<String>(), result.disallowedApps)
    }

    @Test
    fun emptyIncludeFallsBackToDisallowingKumiho() {
        val result = spec(33, split = SplitSettings(mode = SplitMode.INCLUDE))
        assertEquals(emptyList<String>(), result.allowedApps)
        assertEquals(listOf(appPackage), result.disallowedApps)
    }

    @Test
    fun allowedAndDisallowedAreNeverBothNonEmpty() {
        for (mode in SplitMode.entries) {
            for (packages in listOf(emptySet(), setOf("com.example.a"), setOf(appPackage))) {
                val result = spec(33, split = SplitSettings(mode, packages))
                assertFalse(
                    "mode=$mode packages=$packages",
                    result.allowedApps.isNotEmpty() && result.disallowedApps.isNotEmpty(),
                )
            }
        }
    }

    @Test
    fun splitListsAreSortedAndDeduplicated() {
        val result = spec(
            26,
            split = SplitSettings(
                mode = SplitMode.EXCLUDE,
                packages = setOf("com.example.c", "com.example.a", "com.example.a"),
            ),
        )
        assertEquals(
            listOf("com.example.a", "com.example.c", appPackage),
            result.disallowedApps,
        )
    }

    // ---- config parsing -------------------------------------------------

    @Test
    fun parseReadsTheGoShape() {
        val raw = JSONObject()
            .put("mtu", 8500)
            .put("allow_lan", true)
            .put("exclude_cidrs", JSONArray().put("203.0.113.0/24"))
            .put("ipv4_excludes", JSONArray().put("10.0.0.0/8"))
            .put("ipv4_routes", JSONArray().put("11.0.0.0/8"))
            .toString()
        val parsed = VpnConfig.parse(raw)
        assertEquals(8500, parsed.mtu)
        assertTrue(parsed.allowLan)
        assertEquals(listOf("203.0.113.0/24"), parsed.excludeCidrs)
        assertEquals(listOf("10.0.0.0/8"), parsed.ipv4Excludes)
        assertEquals(listOf("11.0.0.0/8"), parsed.ipv4Routes)
    }

    @Test
    fun parseToleratesUnknownKeysAndMissingInformationalCidrs() {
        val raw = JSONObject()
            .put("mtu", 1500)
            .put("allow_lan", false)
            .put("ipv4_excludes", JSONArray())
            .put("ipv4_routes", JSONArray().put("0.0.0.0/0"))
            .put("future_key", JSONObject().put("nested", 1))
            .put("another", JSONArray().put("x"))
            .toString()
        val parsed = VpnConfig.parse(raw)
        assertEquals(emptyList<String>(), parsed.excludeCidrs)
        assertFalse(parsed.allowLan)
    }

    @Test
    fun parseRejectsInvalidJson() {
        assertThrows(IllegalArgumentException::class.java) { VpnConfig.parse("{not json") }
        assertThrows(IllegalArgumentException::class.java) { VpnConfig.parse("[]") }
        assertThrows(IllegalArgumentException::class.java) { VpnConfig.parse("") }
    }

    @Test
    fun parseRejectsMissingRequiredKeys() {
        val complete = JSONObject()
            .put("mtu", 1500)
            .put("allow_lan", true)
            .put("ipv4_excludes", JSONArray())
            .put("ipv4_routes", JSONArray())
        for (key in listOf("mtu", "allow_lan", "ipv4_excludes", "ipv4_routes")) {
            val broken = JSONObject(complete.toString())
            broken.remove(key)
            assertThrows("missing $key", IllegalArgumentException::class.java) {
                VpnConfig.parse(broken.toString())
            }
        }
    }

    @Test
    fun parseRejectsWrongTypes() {
        val cases = listOf(
            JSONObject().put("mtu", "1500").put("allow_lan", true)
                .put("ipv4_excludes", JSONArray()).put("ipv4_routes", JSONArray()),
            JSONObject().put("mtu", 1500).put("allow_lan", "yes")
                .put("ipv4_excludes", JSONArray()).put("ipv4_routes", JSONArray()),
            JSONObject().put("mtu", 1500).put("allow_lan", true)
                .put("ipv4_excludes", "10.0.0.0/8").put("ipv4_routes", JSONArray()),
            JSONObject().put("mtu", 1500).put("allow_lan", true)
                .put("ipv4_excludes", JSONArray().put(10)).put("ipv4_routes", JSONArray()),
            JSONObject().put("mtu", 1500).put("allow_lan", true)
                .put("ipv4_excludes", JSONArray()).put("ipv4_routes", JSONArray().put(" ")),
            JSONObject().put("mtu", 1500.5).put("allow_lan", true)
                .put("ipv4_excludes", JSONArray()).put("ipv4_routes", JSONArray()),
        )
        for ((index, fields) in cases.withIndex()) {
            assertThrows("case $index", IllegalArgumentException::class.java) {
                VpnConfig.parse(fields.toString())
            }
        }
    }

    @Test
    fun parseValidatesTheMtuRange() {
        val fields = JSONObject()
            .put("allow_lan", true)
            .put("ipv4_excludes", JSONArray())
            .put("ipv4_routes", JSONArray())
        for (mtu in listOf(VpnConfig.MIN_MTU - 1, VpnConfig.MAX_MTU + 1, 0, -1)) {
            assertThrows("mtu $mtu", IllegalArgumentException::class.java) {
                VpnConfig.parse(JSONObject(fields.toString()).put("mtu", mtu).toString())
            }
        }
        assertEquals(
            VpnConfig.MIN_MTU,
            VpnConfig.parse(
                JSONObject(fields.toString()).put("mtu", VpnConfig.MIN_MTU).toString(),
            ).mtu,
        )
        assertEquals(
            VpnConfig.MAX_MTU,
            VpnConfig.parse(
                JSONObject(fields.toString()).put("mtu", VpnConfig.MAX_MTU).toString(),
            ).mtu,
        )
    }
}
