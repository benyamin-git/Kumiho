package dev.benyamin.kumiho.engine

import java.time.Instant
import java.time.format.DateTimeParseException
import org.json.JSONArray
import org.json.JSONException
import org.json.JSONObject

/**
 * Every wire-shape parser for the mobile facade. Helpers are deliberately
 * tolerant: an older or newer engine may omit fields (`omitempty`) or send
 * `null`, and an unknown enum value must not take the screen down. Missing
 * scalars become zero/empty, missing collections become empty lists, and a
 * missing object stays null so "not reported" is distinct from "reported as
 * zero".
 */
object EngineJson {
    /** Parses one JSON object, mapping invalid text to a protocol error. */
    fun parseObject(raw: String): JSONObject = parseObject(raw, "JSON")

    /**
     * Decodes a response envelope and returns its payload object.
     *
     * A success envelope is `{"type":…,"payload":{…}}`. A domain failure is
     * `{"error":{"code":…,"message":…}}` with **no** `type`; it is thrown as an
     * [EngineException] carrying the engine's code. Anything else is an
     * [EngineProtocolException].
     */
    fun decodeEnvelope(raw: String): JSONObject {
        val envelope = parseObject(raw, "response")
        if (envelope.has("error") && !envelope.isNull("error")) {
            val error = envelope.optJSONObject("error")
                ?: throw EngineProtocolException("the engine sent a malformed error envelope")
            val code = string(error, "code")
            if (code.isNullOrBlank()) {
                throw EngineProtocolException("the engine sent an error without a code")
            }
            throw EngineException(code, stringOrEmpty(error, "message"))
        }
        val type = string(envelope, "type")
        if (type.isNullOrBlank()) {
            throw EngineProtocolException("the engine response has no type")
        }
        if (!envelope.has("payload") || envelope.isNull("payload")) {
            return JSONObject()
        }
        return envelope.optJSONObject("payload")
            ?: throw EngineProtocolException("the engine response payload is not an object")
    }

    // ---- models ---------------------------------------------------------

    fun status(json: String): EngineStatus = status(parseObject(json, "status"))

    fun status(fields: JSONObject): EngineStatus = EngineStatus(
        state = stringOrEmpty(fields, "state"),
        authenticated = boolean(fields, "authenticated"),
        email = string(fields, "email"),
        since = longOrNull(fields, "since") ?: 0L,
        tokenExpires = longOrNull(fields, "token_expires") ?: 0L,
        passExpires = longOrNull(fields, "pass_expires") ?: 0L,
        killSwitch = boolean(fields, "kill_switch"),
        autoconnect = boolean(fields, "autoconnect"),
        location = string(fields, "location"),
        server = string(fields, "server"),
        proxyOnly = boolean(fields, "proxy_only"),
        quota = quotaOrNull(fields),
        dns = string(fields, "dns"),
        ipv6 = string(fields, "ipv6"),
        exitIp = string(fields, "exit_ip"),
        exitCountry = string(fields, "exit_country"),
        totals = countersOrNull(fields),
        rates = ratesOrNull(fields),
    )

    fun settings(json: String): Settings = settings(parseObject(json, "settings"))

    fun settings(fields: JSONObject): Settings = Settings(
        socksPort = int(fields, "socks_port"),
        mtu = int(fields, "mtu"),
        exitCheck = boolean(fields, "exit_check"),
        dohProvider = stringOrEmpty(fields, "doh_provider"),
        dnsUpstream = string(fields, "dns_upstream"),
        edgeAddress = string(fields, "edge_address"),
        upstreamProxy = string(fields, "upstream_proxy"),
        excludeCidrs = stringList(fields, "exclude_cidrs"),
        allowLan = boolean(fields, "allow_lan"),
        logLevel = stringOrEmpty(fields, "log_level"),
        redactTargets = boolean(fields, "redact_targets"),
        killSwitch = stringOrEmpty(fields, "kill_switch"),
        autoconnect = stringOrEmpty(fields, "autoconnect"),
        killSwitchEffective = boolean(fields, "kill_switch_effective"),
        autoconnectEffective = boolean(fields, "autoconnect_effective"),
        quotaPollMinutes = int(fields, "quota_poll_minutes"),
        overridden = stringList(fields, "overridden"),
        tlsSplit = stringOrEmpty(fields, "tls_split"),
        httpProxy = boolean(fields, "http_proxy"),
        httpProxyPort = int(fields, "http_proxy_port"),
        solveChallenges = boolean(fields, "solve_challenges"),
        keepaliveProfile = stringOrEmpty(fields, "keepalive_profile"),
        accountRotation = boolean(fields, "account_rotation"),
        rotationThreshold = int(fields, "rotation_threshold"),
    )

    fun loginState(json: String): LoginState = loginState(parseObject(json, "login state"))

    fun loginState(fields: JSONObject): LoginState = LoginState(
        step = stringOrEmpty(fields, "step"),
        email = string(fields, "email"),
        verificationMethod = string(fields, "verification_method"),
        verificationReason = string(fields, "verification_reason"),
        error = string(fields, "error"),
        method = string(fields, "method"),
        url = string(fields, "url"),
        loginId = string(fields, "login_id"),
    )

    fun accounts(json: String): Accounts = accounts(parseObject(json, "accounts"))

    fun accounts(fields: JSONObject): Accounts = Accounts(
        accounts = objectsIn(arrayOrNull(fields, "accounts")).map(::accountSummary),
        active = string(fields, "active"),
        rotation = boolean(fields, "rotation"),
        threshold = int(fields, "rotation_threshold"),
        lastMethod = string(fields, "last_method"),
        notice = string(fields, "notice"),
        error = string(fields, "error"),
    )

    private fun accountSummary(fields: JSONObject): AccountSummary = AccountSummary(
        id = stringOrEmpty(fields, "id"),
        email = stringOrEmpty(fields, "email"),
        label = stringOrEmpty(fields, "label"),
        method = stringOrEmpty(fields, "method"),
        active = boolean(fields, "active"),
        reauthRequired = boolean(fields, "reauth_required"),
        lastUsedAt = longOrNull(fields, "last_used_at") ?: 0L,
        quota = quotaOrNull(fields),
    )

    fun account(json: String): Account = account(parseObject(json, "account"))

    fun account(fields: JSONObject): Account = Account(
        email = string(fields, "email"),
        uid = string(fields, "uid"),
        subscribed = boolean(fields, "subscribed"),
        maxBytes = longOrNull(fields, "max_bytes"),
        limitedBandwidth = boolean(fields, "limited_bandwidth"),
        quotaRemaining = longOrNull(fields, "quota_remaining"),
        tokenExpires = longOrNull(fields, "token_expires") ?: 0L,
        error = string(fields, "error"),
    )

    fun logEntry(json: String): LogEntry = logEntry(parseObject(json, "log entry"))

    fun logEntry(fields: JSONObject): LogEntry = LogEntry(
        time = instantOrNull(string(fields, "ts")) ?: Instant.EPOCH,
        level = LogLevel.fromWire(string(fields, "level")),
        tag = stringOrEmpty(fields, "tag"),
        message = stringOrEmpty(fields, "msg"),
    )

    fun logEntries(payload: JSONObject): List<LogEntry> =
        objectsIn(arrayOrNull(payload, "entries")).map(::logEntry)

    fun pingResult(json: String): PingResult = pingResult(parseObject(json, "ping result"))

    fun pingResult(fields: JSONObject): PingResult = PingResult(
        host = stringOrEmpty(fields, "host"),
        rttMs = doubleOrNull(fields, "rtt_ms"),
    )

    fun notice(json: String): Notice = notice(parseObject(json, "notice"))

    fun notice(fields: JSONObject): Notice = Notice(
        type = stringOrEmpty(fields, "type"),
        state = string(fields, "state"),
        message = string(fields, "message"),
    )

    fun doctorReport(json: String): DoctorReport = doctorReport(parseObject(json, "doctor report"))

    fun doctorReport(payload: JSONObject): DoctorReport = DoctorReport(
        version = stringOrEmpty(payload, "version"),
        host = stringOrEmpty(payload, "host"),
        os = stringOrEmpty(payload, "os"),
        time = stringOrEmpty(payload, "time"),
        checks = objectsIn(arrayOrNull(payload, "checks")).map(::doctorCheck),
        failed = int(payload, "failed"),
        warned = int(payload, "warned"),
        skipped = int(payload, "skipped"),
    )

    fun doctorCheck(fields: JSONObject): DoctorCheck = DoctorCheck(
        name = stringOrEmpty(fields, "name"),
        status = DoctorStatus.fromWire(string(fields, "status")),
        detail = stringOrEmpty(fields, "detail"),
        remediation = string(fields, "remediation"),
    )

    fun countries(payload: JSONObject): List<Country> =
        objectsIn(arrayOrNull(payload, "countries")).map(::country)

    fun country(fields: JSONObject): Country = Country(
        code = stringOrEmpty(fields, "code"),
        name = stringOrEmpty(fields, "name"),
        cities = objectsIn(arrayOrNull(fields, "cities")).map(::city),
    )

    private fun city(fields: JSONObject): City = City(
        code = stringOrEmpty(fields, "code"),
        name = stringOrEmpty(fields, "name"),
        servers = objectsIn(arrayOrNull(fields, "servers")).map(::server),
    )

    private fun server(fields: JSONObject): Server = Server(
        hostname = stringOrEmpty(fields, "hostname"),
        port = int(fields, "port"),
        quarantined = boolean(fields, "quarantined"),
        protocols = objectsIn(arrayOrNull(fields, "protocols")).map(::protocol),
    )

    private fun protocol(fields: JSONObject): Protocol = Protocol(
        name = stringOrEmpty(fields, "name"),
        host = stringOrEmpty(fields, "host"),
        port = int(fields, "port"),
        scheme = stringOrEmpty(fields, "scheme"),
    )

    private fun quotaOrNull(fields: JSONObject): Quota? {
        val quota = objectOrNull(fields, "quota") ?: return null
        return Quota(
            limit = longOrNull(quota, "limit"),
            remaining = longOrNull(quota, "remaining"),
            reset = longOrNull(quota, "reset"),
        )
    }

    private fun countersOrNull(fields: JSONObject): ByteCounters? {
        val totals = objectOrNull(fields, "totals") ?: return null
        return ByteCounters(
            up = longOrNull(totals, "up") ?: 0L,
            down = longOrNull(totals, "down") ?: 0L,
        )
    }

    private fun ratesOrNull(fields: JSONObject): ByteRates? {
        val rates = objectOrNull(fields, "rates") ?: return null
        return ByteRates(
            up = doubleOrNull(rates, "up") ?: 0.0,
            down = doubleOrNull(rates, "down") ?: 0.0,
        )
    }

    private fun instantOrNull(value: String?): Instant? {
        if (value.isNullOrBlank()) return null
        return try {
            Instant.parse(value)
        } catch (_: DateTimeParseException) {
            null
        }
    }

    // ---- tolerant field helpers ----------------------------------------

    private fun parseObject(raw: String, what: String): JSONObject {
        if (raw.isBlank()) throw EngineProtocolException("the engine sent an empty $what")
        return try {
            JSONObject(raw)
        } catch (error: JSONException) {
            throw EngineProtocolException(
                "the engine sent malformed $what JSON: ${error.message}",
                error,
            )
        }
    }

    private fun field(obj: JSONObject, key: String): Any? =
        if (!obj.has(key) || obj.isNull(key)) null else obj.opt(key)

    private fun string(obj: JSONObject, key: String): String? = when (val value = field(obj, key)) {
        is String -> value
        is Number, is Boolean -> value.toString()
        else -> null
    }

    private fun stringOrEmpty(obj: JSONObject, key: String): String = string(obj, key) ?: ""

    private fun boolean(obj: JSONObject, key: String, default: Boolean = false): Boolean =
        when (val value = field(obj, key)) {
            is Boolean -> value
            is Number -> value.toInt() != 0
            is String -> value.equals("true", ignoreCase = true)
            else -> default
        }

    private fun longOrNull(obj: JSONObject, key: String): Long? = when (val value = field(obj, key)) {
        is Number -> value.toLong()
        is String -> value.trim().toLongOrNull()
        else -> null
    }

    private fun int(obj: JSONObject, key: String): Int = (longOrNull(obj, key) ?: 0L).toInt()

    private fun doubleOrNull(obj: JSONObject, key: String): Double? =
        when (val value = field(obj, key)) {
            is Number -> value.toDouble()
            is String -> value.trim().toDoubleOrNull()
            else -> null
        }

    private fun objectOrNull(obj: JSONObject, key: String): JSONObject? =
        if (!obj.has(key) || obj.isNull(key)) null else obj.optJSONObject(key)

    private fun arrayOrNull(obj: JSONObject, key: String): JSONArray? =
        if (!obj.has(key) || obj.isNull(key)) null else obj.optJSONArray(key)

    private fun stringList(obj: JSONObject, key: String): List<String> {
        val array = arrayOrNull(obj, key) ?: return emptyList()
        return buildList {
            for (index in 0 until array.length()) {
                if (!array.isNull(index)) array.opt(index)?.let { add(it.toString()) }
            }
        }
    }

    private fun objectsIn(array: JSONArray?): List<JSONObject> {
        if (array == null) return emptyList()
        return buildList {
            for (index in 0 until array.length()) {
                array.optJSONObject(index)?.let { add(it) }
            }
        }
    }
}
