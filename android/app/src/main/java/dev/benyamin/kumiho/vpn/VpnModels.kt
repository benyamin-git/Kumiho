package dev.benyamin.kumiho.vpn

import dev.benyamin.kumiho.engine.EngineStatus
import org.json.JSONArray
import org.json.JSONException
import org.json.JSONObject

/**
 * The connect-time network plan Go hands to the bridge (`networkConfig` in
 * `mobile/routes.go`).
 *
 * [ipv4Excludes] and [ipv4Routes] are authoritative and are used exactly as
 * given (R12): Kotlin never recomputes, subtracts or complements them. The Go
 * side already applies C1 (the tunnel DNS host is never excluded) and the D49
 * LAN rules. [excludeCidrs] is the user's raw setting and is informational
 * only.
 */
data class VpnConfig(
    val mtu: Int,
    val allowLan: Boolean,
    val excludeCidrs: List<String>,
    val ipv4Excludes: List<String>,
    val ipv4Routes: List<String>,
) {
    companion object {
        /** The same range `internal/settings` validates. */
        const val MIN_MTU = 576
        const val MAX_MTU = 65535

        /**
         * Parses the bridge JSON strictly. Unknown keys are tolerated so a
         * newer engine can add fields; the required keys and their types are
         * enforced, and [IllegalArgumentException] is thrown for anything
         * malformed (gomobile turns that into an error on the Go side).
         */
        fun parse(json: String): VpnConfig {
            val fields = try {
                JSONObject(json)
            } catch (error: JSONException) {
                throw IllegalArgumentException("invalid VPN network config: ${error.message}", error)
            }
            val mtu = requiredInt(fields, "mtu")
            require(mtu in MIN_MTU..MAX_MTU) { "mtu out of range: $mtu" }
            return VpnConfig(
                mtu = mtu,
                allowLan = requiredBoolean(fields, "allow_lan"),
                excludeCidrs = stringList(fields, "exclude_cidrs", required = false),
                ipv4Excludes = stringList(fields, "ipv4_excludes", required = true),
                ipv4Routes = stringList(fields, "ipv4_routes", required = true),
            )
        }

        private fun requiredInt(fields: JSONObject, key: String): Int {
            val value = fields.opt(key)
            if (value == null || value == JSONObject.NULL) {
                throw IllegalArgumentException("missing $key")
            }
            if (value !is Number) {
                throw IllegalArgumentException("$key must be a number")
            }
            val number = value.toDouble()
            if (!number.isFinite() || number != Math.floor(number)) {
                throw IllegalArgumentException("$key must be a whole number")
            }
            if (number < Int.MIN_VALUE || number > Int.MAX_VALUE) {
                throw IllegalArgumentException("$key is out of range: $value")
            }
            return number.toInt()
        }

        private fun requiredBoolean(fields: JSONObject, key: String): Boolean {
            val value = fields.opt(key)
            if (value == null || value == JSONObject.NULL) {
                throw IllegalArgumentException("missing $key")
            }
            if (value !is Boolean) {
                throw IllegalArgumentException("$key must be a boolean")
            }
            return value
        }

        private fun stringList(fields: JSONObject, key: String, required: Boolean): List<String> {
            val value = fields.opt(key)
            if (value == null || value == JSONObject.NULL) {
                if (required) throw IllegalArgumentException("missing $key")
                return emptyList()
            }
            if (value !is JSONArray) {
                throw IllegalArgumentException("$key must be an array")
            }
            val out = ArrayList<String>(value.length())
            for (index in 0 until value.length()) {
                val item = value.opt(index)
                if (item !is String || item.isBlank()) {
                    throw IllegalArgumentException("$key[$index] must be a non-blank string")
                }
                out += item
            }
            return out
        }
    }
}

/** How the split tunnel treats the selected packages. */
enum class SplitMode { OFF, EXCLUDE, INCLUDE }

/** The current split-tunnel selection; Off until T14 replaces the provider. */
data class SplitSettings(
    val mode: SplitMode = SplitMode.OFF,
    val packages: Set<String> = emptySet(),
)

/** The ten states the UI renders (T11's state word mapping). */
enum class VpnUiState {
    OFF,
    CONNECTED,
    PROXY_ONLY,
    CONNECTING,
    RECONNECTING,
    WAITING,
    NOT_SIGNED_IN,
    ERROR,
    SERVICE_UNAVAILABLE,
    PERMISSION_NEEDED,
}

/**
 * Everything the UI needs to know about the VPN service. [binding] is the
 * app's own binding, not a tunnel state; [status] is the engine's latest
 * advisory snapshot (call `refreshStatus` for the source of truth).
 */
data class ServiceState(
    val binding: Boolean = false,
    val engineReady: Boolean = false,
    val status: EngineStatus? = null,
    val alwaysOn: Boolean = false,
    val lockdown: Boolean = false,
    val error: String? = null,
    val permissionNeeded: Boolean = false,
)

/** One connect request; a null location keeps the saved selection or REC. */
data class ConnectRequest(
    val locationCode: String? = null,
    val cityCode: String? = null,
    val server: String? = null,
    val proxyOnly: Boolean = false,
)

/** What a restarted (Always-on) service should do. */
enum class AlwaysOnDecision { AUTO_CONNECT, SIGN_IN_NOTIFICATION, NONE }

/** The engine's wire state names (`internal/engine/engine.go`). */
internal object EngineStates {
    const val UNAUTHENTICATED = "UNAUTHENTICATED"
    const val IDLE = "IDLE"
    const val CONNECTING = "CONNECTING"
    const val CONNECTED = "CONNECTED"
    const val RECONNECTING = "RECONNECTING"
    const val WAITING_NETWORK = "WAITING_NETWORK"
    const val PROXY_ONLY = "PROXY_ONLY"
    const val FATAL = "FATAL"
}

/** A short, non-empty message for a failure; never a stack trace. */
internal fun messageOf(error: Throwable): String =
    error.message?.takeIf { it.isNotBlank() } ?: error::class.simpleName ?: "unknown error"
