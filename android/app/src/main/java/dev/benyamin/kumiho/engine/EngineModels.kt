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
    val accountRotation: Boolean = false,
    val rotationThreshold: Int = 10,
    val allowLocked: Boolean = false,
    val relayUrl: String? = null,
    val relayTokenSet: Boolean = false,
)

/** One persisted favorite or recent location (`api.LocationRef`). */
data class LocationRef(
    val countryCode: String,
    val cityCode: String?,
    val server: String,
)

/**
 * One fresh latency sample (`api.CachedPing`). A null [rttMs] means the probe
 * ran and the server did not answer; an absent entry means it was never
 * measured.
 */
data class CachedPing(
    val rttMs: Double?,
    val measuredAt: Instant,
)

/**
 * The complete locations reply (`api.LocationsResult`), including the pinned
 * refs, cached latency and the locked-list view state.
 */
data class LocationsResult(
    val countries: List<Country>,
    val favorites: List<LocationRef>,
    val recents: List<LocationRef>,
    val latency: Map<String, CachedPing>,
    val allowLocked: Boolean,
)

/** One completed ping run (`api.PingDone`); [error] is a run-level failure. */
data class PingDone(
    val total: Int,
    val answered: Int,
    val error: String?,
)

/** One control-plane relay probe (`api.RelayTestResult`). */
data class RelayTest(
    val ok: Boolean,
    val status: Int,
    val latencyMs: Double?,
    val error: String?,
)

/** One Mozilla control-plane host probe (`api.RegionHostStatus`). */
data class RegionHost(
    val host: String,
    val status: String,
    val detail: String?,
)

/**
 * The blocked-region preflight (`api.RegionCheckResult`). [blocked] lists the
 * hosts that answered HTTP 451; [browserBlocked] is the sign-in page. A guide
 * is required when either is set.
 */
data class RegionCheck(
    val hosts: List<RegionHost>,
    val blocked: List<String>,
    val browserBlocked: Boolean,
    val relayConfigured: Boolean,
) {
    val needsGuide: Boolean get() = blocked.isNotEmpty() || browserBlocked
}

/** The reply to prepare (`api.PrepareResult`); it never carries the pass token. */
data class PrepareResult(
    val email: String?,
    val expiresAt: Long,
    val quota: Quota?,
    val locations: Int,
    val servers: Int,
    val serverListRefreshed: Boolean,
    val profileRefreshed: Boolean,
    val relayUsed: Boolean,
)

/**
 * The location tree (`serverlist.Country`/`City`/`Server`). `REC` arrives as a
 * normal country whose code is `rec`; the UI decides how to present it.
 */
data class Country(
    val code: String,
    val name: String,
    val cities: List<City>,
    val locked: Boolean = false,
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
    val method: String? = null,
    val url: String? = null,
    val loginId: String? = null,
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
