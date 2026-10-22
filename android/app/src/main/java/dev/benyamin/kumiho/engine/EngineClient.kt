package dev.benyamin.kumiho.engine

import dev.benyamin.kumiho.go.mobile.DeviceBridge
import dev.benyamin.kumiho.go.mobile.Listener
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong
import java.util.logging.Logger
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject

/** Client-side log emitted while no engine process exists yet. */
private val engineLogger: Logger = Logger.getLogger("dev.benyamin.kumiho.engine")

/**
 * The single Kotlin-side owner of the engine conversation.
 *
 * [EngineClient] turns JSON envelopes into typed requests and typed responses,
 * and turns the four gomobile listener callbacks into observed flows. It is
 * pure JVM code: no `Context`, no Compose, no Android logging. T9 creates one
 * with a [GomobileEngineTransport] and a `KumihoDeviceBridge`; tests create one
 * with a fake transport and a fake bridge.
 *
 * Concurrency: the listener callbacks run on Go goroutines and only touch
 * thread-safe flow primitives (`StateFlow.value` and `MutableStateFlow.update`
 * plus non-suspending `tryEmit`), so a callback never blocks Go and never lets
 * an exception escape. [command], [start] and [close] move the blocking JNI
 * calls onto [dispatcher] (the IO pool by default). [close] is idempotent and
 * cancels the client's scope.
 */
class EngineClient(
    private val transport: EngineTransport,
    private val dispatcher: CoroutineDispatcher = Dispatchers.IO,
    private val nowMillis: () -> Long = System::currentTimeMillis,
    private val warn: (String) -> Unit = { engineLogger.warning(it) },
) {
    private val scope = CoroutineScope(SupervisorJob() + dispatcher)
    private val startLock = Mutex()
    private val closed = AtomicBoolean(false)
    private val started = AtomicBoolean(false)
    private val lastWarningAt = AtomicLong(Long.MIN_VALUE)

    private val statusState = MutableStateFlow<EngineStatus?>(null)

    /** The latest advisory status; null until the engine reports one. */
    val status: StateFlow<EngineStatus?> = statusState.asStateFlow()

    private val logsState = MutableStateFlow<List<LogEntry>>(emptyList())

    /** The newest [MAX_LOG_ENTRIES] streamed entries, oldest first. */
    val logs: StateFlow<List<LogEntry>> = logsState.asStateFlow()

    private val pingEvents = MutableSharedFlow<PingResult>(
        extraBufferCapacity = PING_BUFFER,
        onBufferOverflow = BufferOverflow.DROP_OLDEST,
    )

    /** One event per requested ping host; a slow collector drops the oldest. */
    val pings: SharedFlow<PingResult> = pingEvents.asSharedFlow()

    private val noticeEvents = MutableSharedFlow<Notice>(
        extraBufferCapacity = NOTICE_BUFFER,
        onBufferOverflow = BufferOverflow.DROP_OLDEST,
    )

    /** Advisory engine notices plus protocol failures the client detects. */
    val notices: SharedFlow<Notice> = noticeEvents.asSharedFlow()

    /**
     * Opens the engine for [paths] and wires an internal [Listener] adapter.
     * The first call wins; repeating it is a no-op. Start never autoconnects.
     *
     * A cancelled call never leaves a half-wired listener: the adapter is only
     * passed to the transport, and the transport is always either fully started
     * or never touched. If cancellation lands after the native call returned,
     * the client is marked started so a retry does not trip the engine's
     * "already started" error.
     */
    suspend fun start(paths: EnginePaths, bridge: DeviceBridge) {
        ensureOpen()
        val pathsJson = paths.toJsonString()
        val adapter = ListenerAdapter()
        startLock.withLock {
            ensureOpen()
            if (started.get()) return
            var completed = false
            try {
                withContext(dispatcher) {
                    transport.start(pathsJson, bridge, adapter)
                    completed = true
                }
            } catch (cancelled: CancellationException) {
                if (completed) started.set(true)
                throw cancelled
            } catch (error: Exception) {
                throw engineFailure("start the engine", error)
            }
            started.set(true)
        }
    }

    /**
     * Sends one request and returns the success payload. A domain failure is
     * thrown as [EngineException] with the engine's code; a malformed envelope
     * or JSON is an [EngineProtocolException].
     */
    suspend fun command(type: String, payload: JSONObject? = null): JSONObject {
        ensureOpen()
        require(type.isNotBlank()) { "an engine command needs a type" }
        val request = JSONObject().put("type", type)
        if (payload != null) request.put("payload", payload)
        val raw = try {
            withContext(dispatcher) { transport.command(request.toString()) }
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            throw engineFailure("run $type", error)
        }
        return EngineJson.decodeEnvelope(raw)
    }

    // ---- convenience commands ------------------------------------------

    /** The handshake result (version, protocol, OS, arch) as raw JSON. */
    suspend fun hello(): JSONObject = command("hello")

    /** Asks for a fresh status snapshot and mirrors it into [status]. */
    suspend fun refreshStatus(): EngineStatus {
        val status = EngineJson.status(command("status"))
        statusState.value = status
        return status
    }

    /** The server-list tree, optionally forcing a refresh. */
    suspend fun locations(refresh: Boolean = false): List<Country> =
        EngineJson.countries(command("locations", JSONObject().put("refresh", refresh)))

    /** Persists a location choice; it never connects by itself. */
    suspend fun selectLocation(countryCode: String, cityCode: String?, server: String) {
        val payload = JSONObject().put("country_code", countryCode).put("server", server)
        if (!cityCode.isNullOrEmpty()) payload.put("city_code", cityCode)
        command("select_location", payload)
    }

    /** Starts 24-way latency probes; results stream through [pings]. */
    suspend fun ping(hosts: List<String>) {
        command("ping", JSONObject().put("hosts", JSONArray(hosts)))
    }

    /** Connects, optionally proxy-only (which skips the VpnService bridge). */
    suspend fun connect(
        locationCode: String? = null,
        cityCode: String? = null,
        server: String? = null,
        proxyOnly: Boolean = false,
    ) {
        val payload = JSONObject().put("proxy_only", proxyOnly)
        if (!locationCode.isNullOrEmpty()) payload.put("location_code", locationCode)
        if (!cityCode.isNullOrEmpty()) payload.put("city_code", cityCode)
        if (!server.isNullOrEmpty()) payload.put("server", server)
        command("connect", payload)
    }

    suspend fun disconnect() {
        command("disconnect")
    }

    /** The runtime settings snapshot. */
    suspend fun settings(): Settings = EngineJson.settings(command("settings_get"))

    /** Changes one runtime setting, or resets it to the inherited value. */
    suspend fun setSetting(key: String, value: String, reset: Boolean = false) {
        command(
            "settings_set",
            JSONObject().put("key", key).put("value", value).put("reset", reset),
        )
    }

    suspend fun account(): Account = EngineJson.account(command("account"))

    suspend fun loginEmail(email: String): LoginState =
        EngineJson.loginState(command("login_email", JSONObject().put("email", email)))

    suspend fun loginPassword(password: String): LoginState =
        EngineJson.loginState(command("login_password", JSONObject().put("password", password)))

    /** Supplies the emailed code; an empty code re-checks the email link. */
    suspend fun login2fa(code: String): LoginState =
        EngineJson.loginState(command("login_2fa", JSONObject().put("code", code)))

    suspend fun logout() {
        command("logout")
    }

    /** The retained log ring, optionally filtered to a minimum level. */
    suspend fun logsSnapshot(level: String? = null): List<LogEntry> =
        EngineJson.logEntries(command("logs_snapshot", levelPayload(level)))

    /** Replaces the live log subscription; null keeps the engine default. */
    suspend fun logsSubscribe(level: String? = null) {
        command("logs_subscribe", levelPayload(level))
    }

    suspend fun logsUnsubscribe() {
        command("logs_unsubscribe")
    }

    suspend fun logsClear() {
        command("logs_clear")
    }

    /** Replays one entry captured before a crash into the engine's ring. */
    suspend fun logAppend(level: String, tag: String, message: String) {
        command(
            "log_append",
            JSONObject().put("level", level).put("tag", tag).put("msg", message),
        )
    }

    /** Runs the doctor with the Android facts the caller collected. */
    suspend fun doctor(facts: JSONObject = JSONObject()): DoctorReport =
        EngineJson.doctorReport(command("doctor", JSONObject().put("facts", facts)))

    /** Drops caches and logs; never touches tokens or the selection. */
    suspend fun resetState() {
        command("reset_state")
    }

    /** Final disconnect after the system revokes the VPN. */
    suspend fun revoke() {
        command("revoke")
    }

    /**
     * Closes the engine and cancels the client's scope. Idempotent; later
     * commands fail with [EngineErrorCodes.CLOSED]. Waits for an in-flight
     * start so a close can never be followed by a fresh native session.
     */
    suspend fun close() {
        if (closed.get()) return
        startLock.withLock {
            if (!closed.compareAndSet(false, true)) return
            var failure: Exception? = null
            try {
                withContext(NonCancellable + dispatcher) { transport.close() }
            } catch (error: Exception) {
                failure = error
            } finally {
                started.set(false)
                scope.cancel()
            }
            if (failure != null) throw engineFailure("close the engine", failure)
        }
    }

    // ---- listener -------------------------------------------------------

    /**
     * The only [Listener] passed to Go. Each callback parses and publishes; a
     * parse failure becomes a protocol notice and a rate-limited warning, never
     * an exception thrown back into Go.
     */
    private inner class ListenerAdapter : Listener {
        override fun onStatus(statusJSON: String) = guarded("status") {
            statusState.value = EngineJson.status(statusJSON)
        }

        override fun onLog(entryJSON: String) = guarded("log") {
            appendLog(EngineJson.logEntry(entryJSON))
        }

        override fun onPing(resultJSON: String) = guarded("ping") {
            pingEvents.tryEmit(EngineJson.pingResult(resultJSON))
        }

        override fun onNotice(noticeJSON: String) = guarded("notice") {
            noticeEvents.tryEmit(EngineJson.notice(noticeJSON))
        }
    }

    private inline fun guarded(kind: String, block: () -> Unit) {
        try {
            block()
        } catch (error: Throwable) {
            val detail = error.message ?: error::class.simpleName ?: "unknown error"
            val message = "could not parse an engine $kind event: $detail"
            noticeEvents.tryEmit(
                Notice(type = NOTICE_PROTOCOL, state = null, message = message),
            )
            warnOnce(message)
        }
    }

    private fun appendLog(entry: LogEntry) {
        logsState.update { current ->
            if (current.size < MAX_LOG_ENTRIES) {
                current + entry
            } else {
                current.subList(current.size - MAX_LOG_ENTRIES + 1, current.size) + entry
            }
        }
    }

    /** Logs a warning at most once per second; notices are never rate-limited. */
    private fun warnOnce(message: String) {
        val now = nowMillis()
        while (true) {
            val last = lastWarningAt.get()
            if (last != Long.MIN_VALUE && now - last < WARNING_INTERVAL_MS) return
            if (lastWarningAt.compareAndSet(last, now)) {
                warn(message)
                return
            }
        }
    }

    private fun levelPayload(level: String?): JSONObject? =
        if (level.isNullOrBlank()) null else JSONObject().put("level", level)

    private fun ensureOpen() {
        if (closed.get()) {
            throw EngineException(EngineErrorCodes.CLOSED, "the engine client is closed")
        }
    }

    private fun engineFailure(what: String, error: Throwable): EngineException {
        if (error is EngineException) return error
        val detail = error.message ?: error::class.simpleName ?: "unknown error"
        return EngineException(EngineErrorCodes.ENGINE, "could not $what: $detail", error)
    }

    private companion object {
        const val MAX_LOG_ENTRIES = 2000
        const val PING_BUFFER = 512
        const val NOTICE_BUFFER = 64
        const val WARNING_INTERVAL_MS = 1_000L
        const val NOTICE_PROTOCOL = "protocol_error"
    }
}
