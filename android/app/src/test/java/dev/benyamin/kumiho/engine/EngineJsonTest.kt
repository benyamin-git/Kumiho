package dev.benyamin.kumiho.engine

import java.io.File
import java.time.Instant
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The wire contract: paths, every model parser and the response envelopes.
 * All fixtures are string literals, so no native library or Android class is
 * loaded.
 */
class EngineJsonTest {

    private fun expectProtocolError(block: () -> Unit): EngineProtocolException =
        expect<EngineProtocolException>(block)

    private inline fun <reified T : Throwable> expect(block: () -> Unit): T {
        try {
            block()
        } catch (error: Throwable) {
            if (error is T) return error
            throw error
        }
        throw AssertionError("expected ${T::class.java.simpleName}")
    }

    // ---- paths ----------------------------------------------------------

    @Test
    fun `paths JSON has exactly five absolute app-private locations`() {
        val paths = EnginePaths.forAppDirs(
            filesDir = File("/data/user/0/dev.benyamin.kumiho/files"),
            noBackupFilesDir = File("/data/user/0/dev.benyamin.kumiho/no_backup"),
            cacheDir = File("/data/user/0/dev.benyamin.kumiho/cache"),
        )
        val json = paths.toJson()

        assertEquals(
            setOf("config", "state", "tokens", "runtime", "logs"),
            json.keys().asSequence().toSet(),
        )
        assertEquals(
            "/data/user/0/dev.benyamin.kumiho/files/config.toml",
            json.getString("config"),
        )
        assertEquals(
            "/data/user/0/dev.benyamin.kumiho/files/state.json",
            json.getString("state"),
        )
        assertEquals(
            "/data/user/0/dev.benyamin.kumiho/no_backup/tokens.json",
            json.getString("tokens"),
        )
        assertEquals(
            "/data/user/0/dev.benyamin.kumiho/cache/runtime",
            json.getString("runtime"),
        )
        assertEquals(
            "/data/user/0/dev.benyamin.kumiho/files/logs",
            json.getString("logs"),
        )
        for (key in json.keys().asSequence()) {
            assertTrue("$key is not absolute: ${json.getString(key)}", json.getString(key).startsWith("/"))
        }
        assertEquals(json.toString(), paths.toJsonString())
    }

    @Test
    fun `paths reject relative roots`() {
        val absolute = File("/data/user/0/dev.benyamin.kumiho/files")
        expect<IllegalArgumentException> {
            EnginePaths.forAppDirs(File("files"), absolute, absolute)
        }
        expect<IllegalArgumentException> {
            EnginePaths.forAppDirs(absolute, File("no_backup"), absolute)
        }
        expect<IllegalArgumentException> {
            EnginePaths.forAppDirs(absolute, absolute, File("cache"))
        }
    }

    @Test
    fun `explicit paths reject a relative component`() {
        val absolute = File("/data/user/0/dev.benyamin.kumiho/files/config.toml")
        expect<IllegalArgumentException> {
            EnginePaths.of(
                config = absolute,
                state = absolute,
                tokens = absolute,
                runtime = File("/data/user/0/dev.benyamin.kumiho/cache/runtime"),
                logs = File("logs"),
            )
        }
    }

    // ---- status ---------------------------------------------------------

    @Test
    fun `status parses every field`() {
        val status = EngineJson.status(
            """
            {
              "state": "connected",
              "authenticated": true,
              "email": "user@example.com",
              "since": 1761086400,
              "token_expires": 1761090000,
              "pass_expires": 1762090000,
              "kill_switch": true,
              "autoconnect": false,
              "location": "de",
              "server": "de1",
              "proxy_only": false,
              "quota": {"limit": 1000000000, "remaining": 250000000, "reset": 1762000000},
              "dns": "10.8.0.2",
              "ipv6": "blackholed",
              "exit_ip": "203.0.113.7",
              "exit_country": "DE",
              "totals": {"up": 1234, "down": 987654},
              "rates": {"up": 12.5, "down": 345.75}
            }
            """.trimIndent(),
        )

        assertEquals("connected", status.state)
        assertTrue(status.authenticated)
        assertEquals("user@example.com", status.email)
        assertEquals(1761086400L, status.since)
        assertEquals(1761090000L, status.tokenExpires)
        assertEquals(1762090000L, status.passExpires)
        assertTrue(status.killSwitch)
        assertFalse(status.autoconnect)
        assertEquals("de", status.location)
        assertEquals("de1", status.server)
        assertFalse(status.proxyOnly)
        assertEquals(Quota(1000000000L, 250000000L, 1762000000L), status.quota)
        assertEquals("10.8.0.2", status.dns)
        assertEquals("blackholed", status.ipv6)
        assertEquals("203.0.113.7", status.exitIp)
        assertEquals("DE", status.exitCountry)
        assertEquals(ByteCounters(1234L, 987654L), status.totals)
        assertEquals(ByteRates(12.5, 345.75), status.rates)
    }

    @Test
    fun `status tolerates missing fields and keeps zero values`() {
        val status = EngineJson.status("""{"state":"idle"}""")

        assertEquals("idle", status.state)
        assertFalse(status.authenticated)
        assertNull(status.email)
        assertEquals(0L, status.since)
        assertEquals(0L, status.tokenExpires)
        assertEquals(0L, status.passExpires)
        assertFalse(status.killSwitch)
        assertFalse(status.autoconnect)
        assertNull(status.location)
        assertNull(status.server)
        assertFalse(status.proxyOnly)
        assertNull(status.quota)
        assertNull(status.dns)
        assertNull(status.ipv6)
        assertNull(status.exitIp)
        assertNull(status.exitCountry)
        assertNull(status.totals)
        assertNull(status.rates)

        val zero = EngineJson.status(
            """
            {
              "state": "connected",
              "kill_switch": false,
              "totals": {"up": 0, "down": 0},
              "rates": {"up": 0.0, "down": 0.0}
            }
            """.trimIndent(),
        )
        assertEquals(ByteCounters(0L, 0L), zero.totals)
        assertEquals(ByteRates(0.0, 0.0), zero.rates)
    }

    @Test
    fun `a null quota stays distinct from a zero quota`() {
        assertNull(EngineJson.status("""{"state":"idle","quota":null}""").quota)

        val partial = EngineJson.status("""{"quota":{"limit":5,"remaining":null}}""")
        assertEquals(5L, partial.quota?.limit)
        assertNull(partial.quota?.remaining)
        assertNull(partial.quota?.reset)

        val zero = EngineJson.status("""{"quota":{"remaining":0}}""")
        assertEquals(0L, zero.quota?.remaining)
        assertNull(zero.quota?.limit)

        // An empty quota object is still a quota the engine reported.
        val empty = EngineJson.status("""{"quota":{}}""")
        assertEquals(Quota(null, null, null), empty.quota)
    }

    // ---- settings / account / login ------------------------------------

    @Test
    fun `settings parse the full view`() {
        val settings = EngineJson.settings(
            """
            {
              "socks_port": 1080,
              "mtu": 1500,
              "exit_check": true,
              "doh_provider": "cloudflare",
              "dns_upstream": "1.1.1.1:53",
              "edge_address": "edge.example:443",
              "upstream_proxy": "http://proxy.example:8080",
              "exclude_cidrs": ["10.0.0.0/8", "192.168.0.0/16"],
              "allow_lan": true,
              "log_level": "debug",
              "redact_targets": true,
              "kill_switch": "on",
              "autoconnect": "always",
              "kill_switch_effective": true,
              "autoconnect_effective": false,
              "quota_poll_minutes": 15,
              "overridden": ["mtu", "log_level"],
              "tls_split": "sni",
              "http_proxy": true,
              "http_proxy_port": 8080,
              "solve_challenges": true,
              "keepalive_profile": "aggressive"
            }
            """.trimIndent(),
        )

        assertEquals(1080, settings.socksPort)
        assertEquals(1500, settings.mtu)
        assertTrue(settings.exitCheck)
        assertEquals("cloudflare", settings.dohProvider)
        assertEquals("1.1.1.1:53", settings.dnsUpstream)
        assertEquals("edge.example:443", settings.edgeAddress)
        assertEquals("http://proxy.example:8080", settings.upstreamProxy)
        assertEquals(listOf("10.0.0.0/8", "192.168.0.0/16"), settings.excludeCidrs)
        assertTrue(settings.allowLan)
        assertEquals("debug", settings.logLevel)
        assertTrue(settings.redactTargets)
        assertEquals("on", settings.killSwitch)
        assertEquals("always", settings.autoconnect)
        assertTrue(settings.killSwitchEffective)
        assertFalse(settings.autoconnectEffective)
        assertEquals(15, settings.quotaPollMinutes)
        assertEquals(listOf("mtu", "log_level"), settings.overridden)
        assertEquals("sni", settings.tlsSplit)
        assertTrue(settings.httpProxy)
        assertEquals(8080, settings.httpProxyPort)
        assertTrue(settings.solveChallenges)
        assertEquals("aggressive", settings.keepaliveProfile)
    }

    @Test
    fun `settings tolerate an older engine view`() {
        val settings = EngineJson.settings("{}")

        assertEquals(0, settings.socksPort)
        assertEquals(0, settings.mtu)
        assertFalse(settings.exitCheck)
        assertEquals("", settings.dohProvider)
        assertNull(settings.dnsUpstream)
        assertNull(settings.edgeAddress)
        assertNull(settings.upstreamProxy)
        assertTrue(settings.excludeCidrs.isEmpty())
        assertFalse(settings.allowLan)
        assertEquals("", settings.logLevel)
        assertEquals("", settings.killSwitch)
        assertEquals("", settings.autoconnect)
        assertEquals(0, settings.quotaPollMinutes)
        assertTrue(settings.overridden.isEmpty())
        assertEquals("", settings.tlsSplit)
        assertEquals("", settings.keepaliveProfile)
    }

    @Test
    fun `login states parse every step`() {
        val email = EngineJson.loginState("""{"step":"email","email":"user@example.com"}""")
        assertEquals("email", email.step)
        assertEquals("user@example.com", email.email)
        assertNull(email.verificationMethod)
        assertNull(email.error)

        val password = EngineJson.loginState(
            """{"step":"password","verification_method":"email-otp","verification_reason":"new device"}""",
        )
        assertEquals("password", password.step)
        assertEquals("email-otp", password.verificationMethod)
        assertEquals("new device", password.verificationReason)

        val failed = EngineJson.loginState("""{"step":"done","error":"invalid password"}""")
        assertEquals("done", failed.step)
        assertEquals("invalid password", failed.error)
        assertNull(failed.email)
    }

    @Test
    fun `account keeps null quotas and a lookup error`() {
        val account = EngineJson.account(
            """
            {
              "email": "user@example.com",
              "uid": "abc123",
              "subscribed": true,
              "max_bytes": null,
              "limited_bandwidth": true,
              "quota_remaining": 0,
              "token_expires": 1761090000,
              "error": "guardian is unreachable"
            }
            """.trimIndent(),
        )

        assertEquals("user@example.com", account.email)
        assertEquals("abc123", account.uid)
        assertTrue(account.subscribed)
        assertNull(account.maxBytes)
        assertTrue(account.limitedBandwidth)
        assertEquals(0L, account.quotaRemaining)
        assertEquals(1761090000L, account.tokenExpires)
        assertEquals("guardian is unreachable", account.error)

        val minimal = EngineJson.account("{}")
        assertNull(minimal.email)
        assertNull(minimal.uid)
        assertFalse(minimal.subscribed)
        assertNull(minimal.maxBytes)
        assertFalse(minimal.limitedBandwidth)
        assertNull(minimal.quotaRemaining)
        assertEquals(0L, minimal.tokenExpires)
        assertNull(minimal.error)
    }

    // ---- events ---------------------------------------------------------

    @Test
    fun `log entries parse RFC3339 nanoseconds and unknown levels`() {
        val entry = EngineJson.logEntry(
            """{"ts":"2026-10-21T12:34:56.123456789Z","level":"warn","tag":"engine","msg":"hello"}""",
        )
        assertEquals(Instant.parse("2026-10-21T12:34:56.123456789Z"), entry.time)
        assertEquals(LogLevel.WARN, entry.level)
        assertEquals("engine", entry.tag)
        assertEquals("hello", entry.message)

        val empty = EngineJson.logEntry("{}")
        assertEquals(Instant.EPOCH, empty.time)
        assertEquals(LogLevel.INFO, empty.level)
        assertEquals("", empty.tag)
        assertEquals("", empty.message)

        val unknown = EngineJson.logEntry("""{"level":"verbose"}""")
        assertEquals(LogLevel.INFO, unknown.level)

        val offset = EngineJson.logEntry("""{"ts":"2026-10-21T14:34:56+02:00"}""")
        assertEquals(Instant.parse("2026-10-21T12:34:56Z"), offset.time)
    }

    @Test
    fun `ping results keep a failed probe null`() {
        val ok = EngineJson.pingResult("""{"host":"edge.example:443","rtt_ms":42.5}""")
        assertEquals("edge.example:443", ok.host)
        assertEquals(42.5, ok.rttMs)

        assertNull(EngineJson.pingResult("""{"host":"edge.example:443","rtt_ms":null}""").rttMs)
        assertNull(EngineJson.pingResult("""{"host":"edge.example:443"}""").rttMs)
        assertNull(EngineJson.pingResult("{}").rttMs)
        assertEquals("", EngineJson.pingResult("{}").host)
    }

    @Test
    fun `notices parse type, state and message`() {
        val fatal = EngineJson.notice("""{"type":"fatal","state":"fatal","message":"edge failed"}""")
        assertEquals(Notice("fatal", "fatal", "edge failed"), fatal)

        val signedOut = EngineJson.notice("""{"type":"signed_out"}""")
        assertEquals("signed_out", signedOut.type)
        assertNull(signedOut.state)
        assertNull(signedOut.message)
    }

    // ---- locations / doctor --------------------------------------------

    @Test
    fun `countries parse the location tree`() {
        val countries = EngineJson.countries(
            EngineJson.parseObject(
                """
                {
                  "countries": [
                    {"code": "rec", "name": "Recommended", "cities": []},
                    {
                      "code": "de",
                      "name": "Germany",
                      "cities": [
                        {
                          "code": "ber",
                          "name": "Berlin",
                          "servers": [
                            {
                              "hostname": "de1.example",
                              "port": 443,
                              "quarantined": false,
                              "protocols": [
                                {"name": "connect", "host": "de1.example", "port": 443, "scheme": "https"}
                              ]
                            }
                          ]
                        }
                      ]
                    }
                  ]
                }
                """.trimIndent(),
            ),
        )

        assertEquals(2, countries.size)
        assertEquals("rec", countries[0].code)
        assertTrue(countries[0].cities.isEmpty())
        assertEquals("Germany", countries[1].name)
        val city = countries[1].cities.single()
        assertEquals("ber", city.code)
        val server = city.servers.single()
        assertEquals("de1.example", server.hostname)
        assertEquals(443, server.port)
        assertFalse(server.quarantined)
        assertEquals(listOf(Protocol("connect", "de1.example", 443, "https")), server.protocols)
    }

    @Test
    fun `countries tolerate missing and null fields`() {
        assertTrue(EngineJson.countries(JSONObject()).isEmpty())
        assertTrue(EngineJson.countries(EngineJson.parseObject("""{"countries":null}""")).isEmpty())

        val partial = EngineJson.countries(
            EngineJson.parseObject("""{"countries":[{"code":"fr"},{"name":"no code"}]}"""),
        )
        assertEquals(2, partial.size)
        assertEquals("fr", partial[0].code)
        assertEquals("", partial[0].name)
        assertTrue(partial[0].cities.isEmpty())
        assertEquals("", partial[1].code)
    }

    @Test
    fun `doctor reports parse checks and counts`() {
        val report = EngineJson.doctorReport(
            """
            {
              "version": "v0.5.0",
              "host": "android",
              "os": "android/arm64",
              "time": "2026-10-21T12:34:56Z",
              "checks": [
                {"name": "vpn-permission", "status": "ok", "detail": "granted"},
                {"name": "always-on", "status": "warn", "detail": "off", "remediation": "enable it"},
                {"name": "future-check", "status": "maybe", "detail": "unknown"}
              ],
              "failed": 0,
              "warned": 1,
              "skipped": 0
            }
            """.trimIndent(),
        )

        assertEquals("v0.5.0", report.version)
        assertEquals("android", report.host)
        assertEquals("android/arm64", report.os)
        assertEquals("2026-10-21T12:34:56Z", report.time)
        assertEquals(3, report.checks.size)
        assertEquals(DoctorStatus.OK, report.checks[0].status)
        assertNull(report.checks[0].remediation)
        assertEquals(DoctorStatus.WARN, report.checks[1].status)
        assertEquals("enable it", report.checks[1].remediation)
        assertEquals(DoctorStatus.UNKNOWN, report.checks[2].status)
        assertEquals(0, report.failed)
        assertEquals(1, report.warned)
        assertEquals(0, report.skipped)

        val minimal = EngineJson.doctorReport("{}")
        assertEquals("", minimal.version)
        assertTrue(minimal.checks.isEmpty())
        assertEquals(0, minimal.failed)
    }

    // ---- envelopes ------------------------------------------------------

    @Test
    fun `a result envelope returns its payload object`() {
        val payload = EngineJson.decodeEnvelope(
            """{"type":"status","payload":{"state":"connected","authenticated":true}}""",
        )
        assertEquals("connected", payload.getString("state"))
        assertTrue(payload.getBoolean("authenticated"))

        assertEquals(
            0,
            EngineJson.decodeEnvelope("""{"type":"result"}""").length(),
        )
        assertEquals(
            "connected",
            EngineJson.decodeEnvelope(
                """{"error":null,"type":"status","payload":{"state":"connected"}}""",
            ).getString("state"),
        )
    }

    @Test
    fun `an error envelope carries the engine code and message`() {
        val failure = expect<EngineException> {
            EngineJson.decodeEnvelope(
                """{"error":{"code":"unsupported_setting","message":"mtu is read-only"}}""",
            )
        }
        assertEquals("unsupported_setting", failure.code)
        assertEquals("mtu is read-only", failure.message)
    }

    @Test
    fun `malformed envelopes are protocol errors`() {
        val malformed = listOf(
            "",
            "   ",
            "not json",
            """{"payload":{}}""",
            """{"type":""}""",
            """{"type":"result","payload":"text"}""",
            """{"type":"result","payload":[1,2]}""",
            """{"error":"oops"}""",
            """{"error":{"message":"no code"}}""",
            """{"error":{"code":"","message":"blank code"}}""",
        )
        for (raw in malformed) {
            val failure = expectProtocolError { EngineJson.decodeEnvelope(raw) }
            assertEquals(EngineErrorCodes.PROTOCOL, failure.code)
            assertTrue("blank message for: $raw", failure.message!!.isNotBlank())
        }
    }

    @Test
    fun `parsing invalid JSON text is a protocol error`() {
        assertTrue(
            expectProtocolError { EngineJson.status("not json") }.message!!.contains("status"),
        )
    }
}
