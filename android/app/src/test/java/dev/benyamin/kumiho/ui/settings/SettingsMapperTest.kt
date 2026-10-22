package dev.benyamin.kumiho.ui.settings

import dev.benyamin.kumiho.engine.Settings
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The settings catalog and the pure mapping: all nineteen engine keys exactly
 * once, their groups and kinds, the current values, override marks, Reset and
 * the exact strings `settings_set` receives.
 *
 * No Android and no engine: [SettingsMapper] is a pure function of the snapshot.
 */
class SettingsMapperTest {

    // ---- the catalog -----------------------------------------------------

    @Test
    fun `the catalog lists all nineteen engine keys exactly once`() {
        val expected = setOf(
            "socks_port", "mtu", "exit_check", "doh_provider", "dns_upstream",
            "edge_address", "upstream_proxy", "exclude_cidrs", "allow_lan",
            "log_level", "redact_targets", "kill_switch", "autoconnect",
            "quota_poll_minutes", "tls_split", "http_proxy", "http_proxy_port",
            "solve_challenges", "keepalive_profile",
        )
        assertEquals(19, SettingsCatalog.specs.size)
        assertEquals(expected, SettingsCatalog.engineKeys.toSet())
        assertEquals(19, SettingsCatalog.engineKeys.size)
        assertEquals(17, SettingsCatalog.settableKeys.size)
        assertEquals(setOf("kill_switch", "autoconnect"), SettingsCatalog.readOnlyKeys.toSet())
    }

    @Test
    fun `every key sits in its exact group`() {
        assertEquals(
            listOf(
                "mtu", "socks_port", "exit_check", "doh_provider", "dns_upstream",
                "allow_lan", "quota_poll_minutes",
            ),
            SettingsCatalog.specsIn(SettingsGroup.CONNECTION).map { it.key },
        )
        assertEquals(
            listOf("kill_switch", "autoconnect"),
            SettingsCatalog.specsIn(SettingsGroup.ANDROID_VPN).map { it.key },
        )
        assertEquals(
            listOf(
                "edge_address", "upstream_proxy", "exclude_cidrs", "tls_split",
                "http_proxy", "http_proxy_port", "solve_challenges",
                "keepalive_profile", "log_level", "redact_targets",
            ),
            SettingsCatalog.specsIn(SettingsGroup.ADVANCED).map { it.key },
        )
        assertTrue(SettingsCatalog.specsIn(SettingsGroup.APP).isEmpty())
    }

    @Test
    fun `each key has the kind the editor expects`() {
        val kinds = SettingsCatalog.specs.associate { it.key to it.kind }
        assertEquals(SettingKind.INTEGER, kinds["mtu"])
        assertEquals(SettingKind.INTEGER, kinds["socks_port"])
        assertEquals(SettingKind.INTEGER, kinds["quota_poll_minutes"])
        assertEquals(SettingKind.INTEGER, kinds["http_proxy_port"])
        assertEquals(SettingKind.BOOLEAN, kinds["exit_check"])
        assertEquals(SettingKind.BOOLEAN, kinds["allow_lan"])
        assertEquals(SettingKind.BOOLEAN, kinds["http_proxy"])
        assertEquals(SettingKind.BOOLEAN, kinds["solve_challenges"])
        assertEquals(SettingKind.BOOLEAN, kinds["redact_targets"])
        assertEquals(SettingKind.ENUM, kinds["doh_provider"])
        assertEquals(SettingKind.ENUM, kinds["tls_split"])
        assertEquals(SettingKind.ENUM, kinds["keepalive_profile"])
        assertEquals(SettingKind.ENUM, kinds["log_level"])
        assertEquals(SettingKind.TEXT, kinds["dns_upstream"])
        assertEquals(SettingKind.TEXT, kinds["edge_address"])
        assertEquals(SettingKind.TEXT, kinds["upstream_proxy"])
        assertEquals(SettingKind.CIDR_LIST, kinds["exclude_cidrs"])
        assertEquals(SettingKind.READ_ONLY, kinds["kill_switch"])
        assertEquals(SettingKind.READ_ONLY, kinds["autoconnect"])
    }

    @Test
    fun `apply timing follows the engine's behavior`() {
        val immediate = SettingsCatalog.specs
            .filter { it.applyTiming == ApplyTiming.IMMEDIATE }
            .map { it.key }
            .toSet()
        assertEquals(setOf("solve_challenges", "log_level", "redact_targets"), immediate)
        assertEquals(
            ApplyTiming.OS_MANAGED,
            SettingsCatalog.byKey("kill_switch")?.applyTiming,
        )
        assertEquals(ApplyTiming.OS_MANAGED, SettingsCatalog.byKey("autoconnect")?.applyTiming)
        assertEquals(ApplyTiming.NEXT_CONNECT, SettingsCatalog.byKey("mtu")?.applyTiming)
    }

    // ---- current values --------------------------------------------------

    @Test
    fun `every settable key maps its current value and the read-only pair stays empty`() {
        val snapshot = sample()
        val expected = mapOf(
            "mtu" to "1400",
            "socks_port" to "1080",
            "exit_check" to "on",
            "doh_provider" to "quad9",
            "dns_upstream" to "9.9.9.9",
            "allow_lan" to "off",
            "quota_poll_minutes" to "15",
            "edge_address" to "203.0.113.7",
            "upstream_proxy" to "http://proxy.example:3128",
            "exclude_cidrs" to "10.0.0.0/8, 192.168.1.0/24",
            "tls_split" to "first-byte",
            "http_proxy" to "on",
            "http_proxy_port" to "8080",
            "solve_challenges" to "off",
            "keepalive_profile" to "slow",
            "log_level" to "debug",
            "redact_targets" to "on",
            "kill_switch" to "",
            "autoconnect" to "",
        )
        for ((key, value) in expected) {
            val spec = requireNotNull(SettingsCatalog.byKey(key))
            assertEquals(key, value, SettingsMapper.valueOf(spec, snapshot))
        }
    }

    @Test
    fun `an empty optional text is empty rather than guessed`() {
        val snapshot = sample(dnsUpstream = null, edgeAddress = null, upstreamProxy = null)
        assertEquals("", SettingsMapper.valueOf(SettingsCatalog.byKey("dns_upstream")!!, snapshot))
        assertEquals("", SettingsMapper.valueOf(SettingsCatalog.byKey("edge_address")!!, snapshot))
        assertEquals("", SettingsMapper.valueOf(SettingsCatalog.byKey("upstream_proxy")!!, snapshot))
    }

    // ---- overrides and reset --------------------------------------------

    @Test
    fun `overrides mark only settable keys and reset carries reset true`() {
        val snapshot = sample(overridden = listOf("mtu", "log_level", "kill_switch"))
        assertTrue(SettingsMapper.isOverridden(SettingsCatalog.byKey("mtu")!!, snapshot))
        assertTrue(SettingsMapper.isOverridden(SettingsCatalog.byKey("log_level")!!, snapshot))
        assertFalse(
            "an Android-owned row is never an engine override",
            SettingsMapper.isOverridden(SettingsCatalog.byKey("kill_switch")!!, snapshot),
        )
        assertFalse(SettingsMapper.isOverridden(SettingsCatalog.byKey("socks_port")!!, snapshot))

        assertNull(SettingsMapper.reset(SettingsCatalog.byKey("kill_switch")!!))
        assertNull(SettingsMapper.reset(SettingsCatalog.byKey("autoconnect")!!))
        for (key in SettingsCatalog.settableKeys) {
            val change = SettingsMapper.reset(SettingsCatalog.byKey(key)!!)
            assertNotNull(change)
            assertEquals(key, change!!.key)
            assertEquals("", change.value)
            assertTrue(change.reset)
        }
    }

    // ---- serialization ---------------------------------------------------

    @Test
    fun `booleans serialize to exactly on and off`() {
        val exit = SettingsCatalog.byKey("exit_check")!!
        assertEquals(SettingInput.Valid("on"), SettingsMapper.serialize(exit, "on"))
        assertEquals(SettingInput.Valid("on"), SettingsMapper.serialize(exit, " TRUE "))
        assertEquals(SettingInput.Valid("on"), SettingsMapper.serialize(exit, "yes"))
        assertEquals(SettingInput.Valid("off"), SettingsMapper.serialize(exit, "off"))
        assertEquals(SettingInput.Valid("off"), SettingsMapper.serialize(exit, "No"))
        assertEquals(
            SettingInput.Invalid(SettingError.NOT_AN_OPTION),
            SettingsMapper.serialize(exit, "maybe"),
        )
    }

    @Test
    fun `integer ranges mirror the engine's bounds`() {
        assertValid("socks_port", "1")
        assertValid("socks_port", "65535")
        assertValid("socks_port", " 443 ")
        assertOutOfRange("socks_port", "0", 1, 65535)
        assertOutOfRange("socks_port", "65536", 1, 65535)

        assertValid("mtu", "576")
        assertValid("mtu", "65535")
        assertOutOfRange("mtu", "575", 576, 65535)
        assertOutOfRange("mtu", "65536", 576, 65535)

        assertValid("quota_poll_minutes", "1")
        assertValid("quota_poll_minutes", "1440")
        assertOutOfRange("quota_poll_minutes", "0", 1, 1440)
        assertOutOfRange("quota_poll_minutes", "1441", 1, 1440)

        assertValid("http_proxy_port", "1")
        assertValid("http_proxy_port", "65535")
        assertOutOfRange("http_proxy_port", "70000", 1, 65535)

        assertEquals(
            SettingInput.Invalid(SettingError.NOT_A_NUMBER),
            SettingsMapper.serialize(SettingsCatalog.byKey("mtu")!!, "abc"),
        )
        assertEquals(
            SettingInput.Invalid(SettingError.NOT_A_NUMBER),
            SettingsMapper.serialize(SettingsCatalog.byKey("mtu")!!, "1500.5"),
        )
    }

    @Test
    fun `enums accept exactly their members and reject everything else`() {
        val doh = SettingsCatalog.byKey("doh_provider")!!
        for (option in listOf("automatic", "cloudflare", "google", "quad9", "off")) {
            assertValid("doh_provider", option)
        }
        assertEquals(
            SettingInput.Invalid(SettingError.NOT_AN_OPTION),
            SettingsMapper.serialize(doh, "adguard"),
        )

        for (option in listOf("off", "first-byte", "sni")) {
            assertValid("tls_split", option)
        }
        assertEquals(
            SettingInput.Invalid(SettingError.NOT_AN_OPTION),
            SettingsMapper.serialize(SettingsCatalog.byKey("tls_split")!!, "second-byte"),
        )

        for (option in listOf("standard", "slow")) {
            assertValid("keepalive_profile", option)
        }
        for (option in listOf("debug", "info", "warn", "error")) {
            assertValid("log_level", option)
        }
        assertEquals(
            SettingInput.Invalid(SettingError.NOT_AN_OPTION),
            SettingsMapper.serialize(SettingsCatalog.byKey("log_level")!!, "trace"),
        )
    }

    @Test
    fun `CIDR lists normalize, drop empty items and reject IPv6`() {
        val spec = SettingsCatalog.byKey("exclude_cidrs")!!
        assertEquals(
            SettingInput.Valid("10.0.0.0/8, 192.168.1.5/32"),
            SettingsMapper.serialize(spec, " 10.0.0.0/8 ,, 192.168.1.5 "),
        )
        assertEquals(SettingInput.Valid(""), SettingsMapper.serialize(spec, "  "))
        assertEquals(
            SettingInput.Valid("0.0.0.0/0"),
            SettingsMapper.serialize(spec, "0.0.0.0/0"),
        )
        assertEquals(
            SettingInput.Invalid(SettingError.IPV6_NOT_SUPPORTED),
            SettingsMapper.serialize(spec, "fd00::/8"),
        )
        assertEquals(
            SettingInput.Invalid(SettingError.INVALID_CIDR, detail = "10.0.0.256"),
            SettingsMapper.serialize(spec, "10.0.0.256"),
        )
        assertEquals(
            SettingInput.Invalid(SettingError.INVALID_CIDR, detail = "10.0.0.0/33"),
            SettingsMapper.serialize(spec, "10.0.0.0/33"),
        )
        assertEquals(
            SettingInput.Invalid(SettingError.INVALID_CIDR, detail = "example.com"),
            SettingsMapper.serialize(spec, "example.com"),
        )
    }

    @Test
    fun `text values are trimmed and read-only rows refuse to serialize`() {
        assertEquals(
            SettingInput.Valid("9.9.9.9"),
            SettingsMapper.serialize(SettingsCatalog.byKey("dns_upstream")!!, " 9.9.9.9 "),
        )
        assertEquals(
            SettingInput.Valid(""),
            SettingsMapper.serialize(SettingsCatalog.byKey("upstream_proxy")!!, "   "),
        )
        for (key in SettingsCatalog.readOnlyKeys) {
            assertEquals(
                SettingInput.Invalid(SettingError.READ_ONLY),
                SettingsMapper.serialize(SettingsCatalog.byKey(key)!!, "on"),
            )
        }
    }

    // ---- helpers ---------------------------------------------------------

    private fun assertValid(key: String, raw: String) {
        val result = SettingsMapper.serialize(SettingsCatalog.byKey(key)!!, raw)
        assertEquals(key, SettingInput.Valid(raw.trim()), result)
    }

    private fun assertOutOfRange(key: String, raw: String, min: Int, max: Int) {
        val result = SettingsMapper.serialize(SettingsCatalog.byKey(key)!!, raw)
        assertEquals(
            key,
            SettingInput.Invalid(SettingError.OUT_OF_RANGE, range = SettingRange(min, max)),
            result,
        )
    }

    private fun sample(
        dnsUpstream: String? = "9.9.9.9",
        edgeAddress: String? = "203.0.113.7",
        upstreamProxy: String? = "http://proxy.example:3128",
        overridden: List<String> = emptyList(),
    ) = Settings(
        socksPort = 1080,
        mtu = 1400,
        exitCheck = true,
        dohProvider = "quad9",
        dnsUpstream = dnsUpstream,
        edgeAddress = edgeAddress,
        upstreamProxy = upstreamProxy,
        excludeCidrs = listOf("10.0.0.0/8", "192.168.1.0/24"),
        allowLan = false,
        logLevel = "debug",
        redactTargets = true,
        killSwitch = "on",
        autoconnect = "off",
        killSwitchEffective = true,
        autoconnectEffective = false,
        quotaPollMinutes = 15,
        overridden = overridden,
        tlsSplit = "first-byte",
        httpProxy = true,
        httpProxyPort = 8080,
        solveChallenges = false,
        keepaliveProfile = "slow",
    )
}
