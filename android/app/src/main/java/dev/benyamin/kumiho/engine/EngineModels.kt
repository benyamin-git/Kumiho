package dev.benyamin.kumiho.engine

import java.time.Instant

/**
 * The client's mirror of `internal/api.Status`. Every field is immutable; the
 * JSON mapping lives in [EngineJson].
 */
data class EngineStatus(
    val state: String,
    val authenticated: Boolean,
    val email: String?,
    val since: Long,
    val tokenExpires: Long,
    val passExpires: Long,
    val killSwitch: Boolean,
    val autoconnect: Boolean,
    val location: String?,
    val server: String?,
    val proxyOnly: Boolean,
    val quota: Quota?,
    val dns: String?,
    val ipv6: String?,
    val exitIp: String?,
    val exitCountry: String?,
    val totals: ByteCounters?,
    val rates: ByteRates?,
)

/**
 * The account's proxy quota. A null field means the engine did not report it;
 * `0` is a real value (for example an exhausted remaining count), so the two
 * are deliberately distinct.
 */
data class Quota(
    val limit: Long?,
    val remaining: Long?,
    val reset: Long?,
)

/** Per-direction byte totals for the current tunnel session. */
data class ByteCounters(
    val up: Long,
    val down: Long,
)

/** Per-direction bytes per second (2 s engine samples). */
data class ByteRates(
    val up: Double,
    val down: Double,
)

/** The runtime settings snapshot (`api.SettingsView`). */
data class Settings(
    val socksPort: Int,
    val mtu: Int,
    val exitCheck: Boolean,
    val dohProvider: String,
    val dnsUpstream: String?,
    val edgeAddress: String?,
    val upstreamProxy: String?,
    val excludeCidrs: List<String>,
    val allowLan: Boolean,
    val logLevel: String,
    val redactTargets: Boolean,
    val killSwitch: String,
    val autoconnect: String,
    val killSwitchEffective: Boolean,
    val autoconnectEffective: Boolean,
    val quotaPollMinutes: Int,
    val overridden: List<String>,
    val tlsSplit: String,
    val httpProxy: Boolean,
    val httpProxyPort: Int,
    val solveChallenges: Boolean,
    val keepaliveProfile: String,
)

/**
 * The location tree (`serverlist.Country`/`City`/`Server`). `REC` arrives as a
 * normal country whose code is `rec`; the UI decides how to present it.
 */
data class Country(
    val code: String,
    val name: String,
    val cities: List<City>,
)

/** One city inside a [Country]. */
data class City(
    val code: String,
    val name: String,
    val servers: List<Server>,
)

/** One proxy node inside a [City]. */
data class Server(
    val hostname: String,
    val port: Int,
    val quarantined: Boolean,
    val protocols: List<Protocol>,
)

/** One protocol record attached to a [Server]. */
data class Protocol(
    val name: String,
    val host: String,
    val port: Int,
    val scheme: String,
)

/** The Account screen snapshot (`api.AccountInfo`). */
data class Account(
    val email: String?,
    val uid: String?,
    val subscribed: Boolean,
    val maxBytes: Long?,
    val limitedBandwidth: Boolean,
    val quotaRemaining: Long?,
    val tokenExpires: Long,
    val error: String?,
)

/** A log severity, ordered as the engine's `logging.Level`. */
enum class LogLevel(val wire: String) {
    DEBUG("debug"),
    INFO("info"),
    WARN("warn"),
    ERROR("error"),
    ;

    companion object {
        /** Parses a wire level; an absent or unknown level falls back to info. */
        fun fromWire(value: String?): LogLevel = when (value?.trim()?.lowercase()) {
            "debug" -> DEBUG
            "warn", "warning" -> WARN
            "error" -> ERROR
            else -> INFO
        }
    }
}

/** One log record (`logging.Entry`); [message] is the wire `msg`. */
data class LogEntry(
    val time: Instant,
    val level: LogLevel,
    val tag: String,
    val message: String,
)

/** The reply to every login step (`api.LoginState`). */
data class LoginState(
    val step: String,
    val email: String?,
    val verificationMethod: String?,
    val verificationReason: String?,
    val error: String?,
)

/** One latency measurement; a null [rttMs] means the probe failed. */
data class PingResult(
    val host: String,
    val rttMs: Double?,
)

/** One advisory engine notice (`mobile.notice`). */
data class Notice(
    val type: String,
    val state: String?,
    val message: String?,
)

/** A doctor check outcome; unknown values are preserved as [UNKNOWN]. */
enum class DoctorStatus(val wire: String) {
    OK("ok"),
    WARN("warn"),
    FAIL("fail"),
    SKIP("skip"),
    UNKNOWN("unknown"),
    ;

    companion object {
        fun fromWire(value: String?): DoctorStatus =
            entries.firstOrNull { it.wire == value?.trim()?.lowercase() } ?: UNKNOWN
    }
}

/** One preflight diagnostic (`doctor.Check`). */
data class DoctorCheck(
    val name: String,
    val status: DoctorStatus,
    val detail: String,
    val remediation: String?,
)

/** One doctor run (`doctor.Report`). */
data class DoctorReport(
    val version: String,
    val host: String,
    val os: String,
    val time: String,
    val checks: List<DoctorCheck>,
    val failed: Int,
    val warned: Int,
    val skipped: Int,
)
