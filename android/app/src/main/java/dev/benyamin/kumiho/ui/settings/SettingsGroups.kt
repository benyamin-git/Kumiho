package dev.benyamin.kumiho.ui.settings

import androidx.annotation.StringRes
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.engine.Settings

/** The four sections of the Settings list, in display order. */
enum class SettingsGroup(@StringRes val titleRes: Int) {
    APP(R.string.settings_group_app),
    CONNECTION(R.string.settings_group_connection),
    ANDROID_VPN(R.string.settings_group_android_vpn),
    ADVANCED(R.string.settings_group_advanced),
}

/** How a row is edited. [READ_ONLY] rows render Android state, never engine values. */
enum class SettingKind { BOOLEAN, ENUM, INTEGER, TEXT, CIDR_LIST, READ_ONLY }

/**
 * When a change takes effect. It is user-visible copy (the hint says it), but
 * keeping it structured lets the mapping tests pin the desktop parity.
 */
enum class ApplyTiming { IMMEDIATE, NEXT_CONNECT, OS_MANAGED }

/** An inclusive integer range, mirroring `internal/settings.Config.Validate`. */
data class SettingRange(val min: Int, val max: Int) {
    operator fun contains(value: Int): Boolean = value in min..max
}

/**
 * One settings row. Labels and hints are resource ids so every user-visible
 * word lives in `strings.xml`; the screen resolves them.
 */
data class SettingSpec(
    val key: String,
    val group: SettingsGroup,
    val kind: SettingKind,
    @StringRes val labelRes: Int,
    @StringRes val hintRes: Int,
    val options: List<String> = emptyList(),
    val range: SettingRange? = null,
    @StringRes val placeholderRes: Int? = null,
    val applyTiming: ApplyTiming = ApplyTiming.NEXT_CONNECT,
    /** Another row key that must be "on" before this row is editable. */
    val dependsOn: String? = null,
) {
    /** Read-only rows are Android state; they are never written through `settings_set`. */
    val readOnly: Boolean get() = kind == SettingKind.READ_ONLY
}

/** A validated value ready for `settings_set`, or a reset of the same row. */
data class SettingChange(
    val key: String,
    val value: String,
    val reset: Boolean = false,
)

/** Why a value could not be sent; the screen turns this into copy. */
enum class SettingError {
    READ_ONLY,
    NOT_A_NUMBER,
    OUT_OF_RANGE,
    NOT_AN_OPTION,
    INVALID_CIDR,
    IPV6_NOT_SUPPORTED,
    CIDR_TOO_BROAD,
}

/** A client-side validation outcome. */
sealed interface SettingInput {
    data class Valid(val value: String) : SettingInput

    data class Invalid(
        val error: SettingError,
        val range: SettingRange? = null,
        val detail: String? = null,
    ) : SettingInput
}

/**
 * The single ordered catalog of all twenty-one engine rows plus their exact
 * groups. Every key appears once; [SettingsMapperTest] enforces that, and the
 * screen renders [specsIn] so a duplicate would be visible immediately.
 */
object SettingsCatalog {

    val specs: List<SettingSpec> = listOf(
        // Connection: the everyday tunnel settings.
        SettingSpec(
            key = "mtu",
            group = SettingsGroup.CONNECTION,
            kind = SettingKind.INTEGER,
            labelRes = R.string.settings_label_mtu,
            hintRes = R.string.settings_hint_mtu,
            range = SettingRange(576, 65535),
        ),
        SettingSpec(
            key = "socks_port",
            group = SettingsGroup.CONNECTION,
            kind = SettingKind.INTEGER,
            labelRes = R.string.settings_label_socks_port,
            hintRes = R.string.settings_hint_socks_port,
            range = SettingRange(1, 65535),
        ),
        SettingSpec(
            key = "exit_check",
            group = SettingsGroup.CONNECTION,
            kind = SettingKind.BOOLEAN,
            labelRes = R.string.settings_label_exit_check,
            hintRes = R.string.settings_hint_exit_check,
        ),
        SettingSpec(
            key = "doh_provider",
            group = SettingsGroup.CONNECTION,
            kind = SettingKind.ENUM,
            labelRes = R.string.settings_label_doh_provider,
            hintRes = R.string.settings_hint_doh_provider,
            options = listOf("automatic", "cloudflare", "google", "quad9", "off"),
        ),
        SettingSpec(
            key = "dns_upstream",
            group = SettingsGroup.CONNECTION,
            kind = SettingKind.TEXT,
            labelRes = R.string.settings_label_dns_upstream,
            hintRes = R.string.settings_hint_dns_upstream,
            placeholderRes = R.string.settings_placeholder_provider_default,
        ),
        SettingSpec(
            key = "allow_lan",
            group = SettingsGroup.CONNECTION,
            kind = SettingKind.BOOLEAN,
            labelRes = R.string.settings_label_allow_lan,
            hintRes = R.string.settings_hint_allow_lan,
        ),
        SettingSpec(
            key = "quota_poll_minutes",
            group = SettingsGroup.CONNECTION,
            kind = SettingKind.INTEGER,
            labelRes = R.string.settings_label_quota_poll_minutes,
            hintRes = R.string.settings_hint_quota_poll_minutes,
            range = SettingRange(1, 1440),
        ),
        SettingSpec(
            key = "account_rotation",
            group = SettingsGroup.CONNECTION,
            kind = SettingKind.BOOLEAN,
            labelRes = R.string.settings_label_account_rotation,
            hintRes = R.string.settings_hint_account_rotation,
            // The Account screen is the primary surface; this is the mirror.
            applyTiming = ApplyTiming.NEXT_CONNECT,
        ),
        SettingSpec(
            key = "rotation_threshold",
            group = SettingsGroup.CONNECTION,
            kind = SettingKind.INTEGER,
            labelRes = R.string.settings_label_rotation_threshold,
            hintRes = R.string.settings_hint_rotation_threshold,
            range = SettingRange(1, 50),
            applyTiming = ApplyTiming.NEXT_CONNECT,
            dependsOn = "account_rotation",
        ),

        // Android VPN: read-only OS state, never the engine values (A10).
        SettingSpec(
            key = "kill_switch",
            group = SettingsGroup.ANDROID_VPN,
            kind = SettingKind.READ_ONLY,
            labelRes = R.string.settings_label_kill_switch,
            hintRes = R.string.settings_hint_kill_switch,
            applyTiming = ApplyTiming.OS_MANAGED,
        ),
        SettingSpec(
            key = "autoconnect",
            group = SettingsGroup.ANDROID_VPN,
            kind = SettingKind.READ_ONLY,
            labelRes = R.string.settings_label_autoconnect,
            hintRes = R.string.settings_hint_autoconnect,
            applyTiming = ApplyTiming.OS_MANAGED,
        ),

        // Advanced: collapsed by default, low-level or proxy-only behavior.
        SettingSpec(
            key = "edge_address",
            group = SettingsGroup.ADVANCED,
            kind = SettingKind.TEXT,
            labelRes = R.string.settings_label_edge_address,
            hintRes = R.string.settings_hint_edge_address,
            placeholderRes = R.string.settings_placeholder_resolve,
        ),
        SettingSpec(
            key = "upstream_proxy",
            group = SettingsGroup.ADVANCED,
            kind = SettingKind.TEXT,
            labelRes = R.string.settings_label_upstream_proxy,
            hintRes = R.string.settings_hint_upstream_proxy,
            placeholderRes = R.string.settings_placeholder_direct,
        ),
        SettingSpec(
            key = "exclude_cidrs",
            group = SettingsGroup.ADVANCED,
            kind = SettingKind.CIDR_LIST,
            labelRes = R.string.settings_label_exclude_cidrs,
            hintRes = R.string.settings_hint_exclude_cidrs,
            placeholderRes = R.string.settings_placeholder_none,
        ),
        SettingSpec(
            key = "tls_split",
            group = SettingsGroup.ADVANCED,
            kind = SettingKind.ENUM,
            labelRes = R.string.settings_label_tls_split,
            hintRes = R.string.settings_hint_tls_split,
            options = listOf("off", "first-byte", "sni"),
        ),
        SettingSpec(
            key = "http_proxy",
            group = SettingsGroup.ADVANCED,
            kind = SettingKind.BOOLEAN,
            labelRes = R.string.settings_label_http_proxy,
            hintRes = R.string.settings_hint_http_proxy,
        ),
        SettingSpec(
            key = "http_proxy_port",
            group = SettingsGroup.ADVANCED,
            kind = SettingKind.INTEGER,
            labelRes = R.string.settings_label_http_proxy_port,
            hintRes = R.string.settings_hint_http_proxy_port,
            range = SettingRange(1, 65535),
        ),
        SettingSpec(
            key = "solve_challenges",
            group = SettingsGroup.ADVANCED,
            kind = SettingKind.BOOLEAN,
            labelRes = R.string.settings_label_solve_challenges,
            hintRes = R.string.settings_hint_solve_challenges,
            applyTiming = ApplyTiming.IMMEDIATE,
        ),
        SettingSpec(
            key = "keepalive_profile",
            group = SettingsGroup.ADVANCED,
            kind = SettingKind.ENUM,
            labelRes = R.string.settings_label_keepalive_profile,
            hintRes = R.string.settings_hint_keepalive_profile,
            options = listOf("standard", "slow"),
        ),
        SettingSpec(
            key = "log_level",
            group = SettingsGroup.ADVANCED,
            kind = SettingKind.ENUM,
            labelRes = R.string.settings_label_log_level,
            hintRes = R.string.settings_hint_log_level,
            options = listOf("debug", "info", "warn", "error"),
            applyTiming = ApplyTiming.IMMEDIATE,
        ),
        SettingSpec(
            key = "redact_targets",
            group = SettingsGroup.ADVANCED,
            kind = SettingKind.BOOLEAN,
            labelRes = R.string.settings_label_redact_targets,
            hintRes = R.string.settings_hint_redact_targets,
            applyTiming = ApplyTiming.IMMEDIATE,
        ),
    )

    /** Fast lookup; a key outside the catalog is a programming error in the UI. */
    private val byKeyMap: Map<String, SettingSpec> = specs.associateBy { it.key }

    fun byKey(key: String): SettingSpec? = byKeyMap[key]

    fun specsIn(group: SettingsGroup): List<SettingSpec> = specs.filter { it.group == group }

    /** The twenty-one engine keys, in catalog order; the read-only two included. */
    val engineKeys: List<String> = specs.map { it.key }

    /** Every key that can be sent through `settings_set`. */
    val settableKeys: List<String> = specs.filterNot { it.readOnly }.map { it.key }

    /** The two Android-owned rows. They are never serialized. */
    val readOnlyKeys: List<String> = specs.filter { it.readOnly }.map { it.key }

    /** The user-visible label for an enum option, or null for an unknown value. */
    @StringRes
    fun optionLabelRes(option: String): Int? = when (option) {
        "debug" -> R.string.settings_option_debug
        "info" -> R.string.settings_option_info
        "warn" -> R.string.settings_option_warn
        "error" -> R.string.settings_option_error
        "automatic" -> R.string.settings_option_automatic
        "cloudflare" -> R.string.settings_option_cloudflare
        "google" -> R.string.settings_option_google
        "quad9" -> R.string.settings_option_quad9
        "off" -> R.string.settings_option_off
        "first-byte" -> R.string.settings_option_first_byte
        "sni" -> R.string.settings_option_sni
        "standard" -> R.string.settings_option_standard
        "slow" -> R.string.settings_option_slow
        else -> null
    }

    /** The user-visible label for a theme id, or null for an unknown id. */
    @StringRes
    fun themeLabelRes(theme: String): Int? = when (theme) {
        "light" -> R.string.settings_theme_light
        "dark" -> R.string.settings_theme_dark
        "oled" -> R.string.settings_theme_oled
        else -> null
    }

    /** The user-visible label for an accent id, or null for an unknown id. */
    @StringRes
    fun accentLabelRes(accent: String): Int? = when (accent) {
        "blue" -> R.string.settings_accent_blue
        "teal" -> R.string.settings_accent_teal
        "green" -> R.string.settings_accent_green
        "orange" -> R.string.settings_accent_orange
        "rose" -> R.string.settings_accent_rose
        "violet" -> R.string.settings_accent_violet
        else -> null
    }
}

/**
 * Pure mapping between the engine snapshot and the rows: current values,
 * override marks, client validation and the exact `settings_set` values.
 *
 * `kill_switch` and `autoconnect` deliberately map to no value and refuse to
 * serialize: Android renders its own OS state for those rows (A10).
 */
object SettingsMapper {

    /** The display value of [spec] from the engine snapshot; read-only rows are empty. */
    fun valueOf(spec: SettingSpec, settings: Settings): String = when (spec.kind) {
        SettingKind.READ_ONLY -> ""
        SettingKind.BOOLEAN -> when (spec.key) {
            "exit_check" -> onOff(settings.exitCheck)
            "allow_lan" -> onOff(settings.allowLan)
            "http_proxy" -> onOff(settings.httpProxy)
            "solve_challenges" -> onOff(settings.solveChallenges)
            "redact_targets" -> onOff(settings.redactTargets)
            "account_rotation" -> onOff(settings.accountRotation)
            else -> ""
        }
        SettingKind.ENUM -> when (spec.key) {
            "doh_provider" -> settings.dohProvider
            "tls_split" -> settings.tlsSplit
            "keepalive_profile" -> settings.keepaliveProfile
            "log_level" -> settings.logLevel
            else -> ""
        }
        SettingKind.INTEGER -> when (spec.key) {
            "mtu" -> settings.mtu.toString()
            "socks_port" -> settings.socksPort.toString()
            "quota_poll_minutes" -> settings.quotaPollMinutes.toString()
            "rotation_threshold" -> settings.rotationThreshold.toString()
            "http_proxy_port" -> settings.httpProxyPort.toString()
            else -> ""
        }
        SettingKind.TEXT -> when (spec.key) {
            "dns_upstream" -> settings.dnsUpstream.orEmpty()
            "edge_address" -> settings.edgeAddress.orEmpty()
            "upstream_proxy" -> settings.upstreamProxy.orEmpty()
            else -> ""
        }
        SettingKind.CIDR_LIST -> settings.excludeCidrs.joinToString(", ")
    }

    /** Whether the engine snapshot carries a runtime override for [spec]. */
    fun isOverridden(spec: SettingSpec, settings: Settings): Boolean =
        !spec.readOnly && settings.overridden.contains(spec.key)

    /** The reset request: an empty value plus `reset = true`, never sent for read-only rows. */
    fun reset(spec: SettingSpec): SettingChange? =
        if (spec.readOnly) null else SettingChange(spec.key, value = "", reset = true)

    /** Validates [raw] and returns the exact string `settings_set` should carry. */
    fun serialize(spec: SettingSpec, raw: String): SettingInput {
        if (spec.readOnly) return SettingInput.Invalid(SettingError.READ_ONLY)
        return when (spec.kind) {
            SettingKind.BOOLEAN -> when (raw.trim().lowercase()) {
                "on", "true", "yes" -> SettingInput.Valid("on")
                "off", "false", "no" -> SettingInput.Valid("off")
                else -> SettingInput.Invalid(SettingError.NOT_AN_OPTION)
            }
            SettingKind.ENUM -> enumValue(spec, raw)
            SettingKind.INTEGER -> integerValue(spec, raw)
            SettingKind.TEXT -> SettingInput.Valid(raw.trim())
            SettingKind.CIDR_LIST -> cidrList(raw)
            SettingKind.READ_ONLY -> SettingInput.Invalid(SettingError.READ_ONLY)
        }
    }

    private fun onOff(value: Boolean): String = if (value) "on" else "off"

    private fun enumValue(spec: SettingSpec, raw: String): SettingInput {
        val value = raw.trim()
        return if (value in spec.options) {
            SettingInput.Valid(value)
        } else {
            SettingInput.Invalid(SettingError.NOT_AN_OPTION)
        }
    }

    private fun integerValue(spec: SettingSpec, raw: String): SettingInput {
        val range = spec.range ?: return SettingInput.Invalid(SettingError.NOT_A_NUMBER)
        val value = raw.trim().toIntOrNull()
            ?: return SettingInput.Invalid(SettingError.NOT_A_NUMBER)
        if (value !in range) {
            return SettingInput.Invalid(SettingError.OUT_OF_RANGE, range = range)
        }
        return SettingInput.Valid(value.toString())
    }

    /**
     * Parses a comma-separated IPv4 list through [CidrValidator]. Spaces and
     * empty items are dropped, a bare address becomes `/32`, host bits are
     * masked, and `/0` or a union covering all of IPv4 is rejected before it
     * can reach `settings_set`.
     */
    fun cidrList(raw: String): SettingInput = CidrValidator.validate(raw)
}
