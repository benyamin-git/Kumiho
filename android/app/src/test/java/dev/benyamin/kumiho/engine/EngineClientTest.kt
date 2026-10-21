package dev.benyamin.kumiho.engine

import dev.benyamin.kumiho.go.mobile.DeviceBridge
import dev.benyamin.kumiho.go.mobile.Listener
import dev.benyamin.kumiho.test.MainDispatcherRule
import java.io.File
import kotlin.coroutines.CoroutineContext
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * The client against a fake transport: no gomobile call, no native library and
 * no Android class. The fake captures the listener so the tests drive events
 * exactly as Go would.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class EngineClientTest {

    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    private class FakeTransport : EngineTransport {
        var started = false
        var startCalls = 0
        var closeCalls = 0
        var pathsJson: String? = null
        var bridge: DeviceBridge? = null
        var listener: Listener? = null
        var startFailure: Exception? = null
        var respond: (String) -> String = { """{"type":"result","payload":{"ok":true}}""" }
        val requests = mutableListOf<JSONObject>()

        override fun start(pathsJson: String, bridge: DeviceBridge, listener: Listener) {
            startCalls++
            startFailure?.let { throw it }
            started = true
            this.pathsJson = pathsJson
            this.bridge = bridge
            this.listener = listener
        }

        override fun command(requestJson: String): String {
            requests += JSONObject(requestJson)
            return respond(requestJson)
        }

        override fun close() {
            closeCalls++
            started = false
            listener = null
        }
    }

    private class FakeBridge : DeviceBridge {
        override fun acquireFD(mtu: Long): Long = -1L

        override fun configure(configJSON: String) = Unit

        override fun networkAvailable(): Boolean = false
    }

    private class CountingDispatcher(
        private val delegate: CoroutineDispatcher,
    ) : CoroutineDispatcher() {
        var dispatches = 0

        override fun dispatch(context: CoroutineContext, block: Runnable) {
            dispatches++
            delegate.dispatch(context, block)
        }
    }

    private fun paths(): EnginePaths = EnginePaths.forAppDirs(
        filesDir = File("/data/user/0/dev.benyamin.kumiho/files"),
        noBackupFilesDir = File("/data/user/0/dev.benyamin.kumiho/no_backup"),
        cacheDir = File("/data/user/0/dev.benyamin.kumiho/cache"),
    )

    private fun client(
        transport: EngineTransport,
        dispatcher: CoroutineDispatcher,
        now: () -> Long = System::currentTimeMillis,
        warn: (String) -> Unit = {},
    ) = EngineClient(transport, dispatcher, now, warn)

    private inline fun <reified T : Throwable> expect(block: () -> Unit): T {
        try {
            block()
        } catch (error: Throwable) {
            if (error is T) return error
            throw error
        }
        throw AssertionError("expected ${T::class.java.simpleName}")
    }

    // ---- start ----------------------------------------------------------

    @Test
    fun `start passes the five absolute paths and the bridge, then wires the listener`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val dispatcher = mainDispatcherRule.testDispatcher
            val transport = FakeTransport()
            val bridge = FakeBridge()
            val client = client(transport, dispatcher)

            client.start(paths(), bridge)
            client.start(paths(), bridge)

            assertEquals(1, transport.startCalls)
            assertTrue(transport.started)
            assertSame(bridge, transport.bridge)
            val json = JSONObject(requireNotNull(transport.pathsJson))
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

            requireNotNull(transport.listener).onStatus("""{"state":"connected","authenticated":true}""")
            assertEquals("connected", client.status.value?.state)
            assertEquals(true, client.status.value?.authenticated)
        }

    @Test
    fun `a start failure surfaces as an engine exception and can be retried`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val dispatcher = mainDispatcherRule.testDispatcher
            val transport = FakeTransport()
            transport.startFailure = IllegalStateException("already started")
            val client = client(transport, dispatcher)

            val failure = expect<EngineException> { client.start(paths(), FakeBridge()) }
            assertEquals(EngineErrorCodes.ENGINE, failure.code)
            assertTrue(requireNotNull(failure.message).contains("already started"))
            assertFalse(transport.started)
            assertNull(transport.listener)

            transport.startFailure = null
            client.start(paths(), FakeBridge())
            assertEquals(2, transport.startCalls)
            assertNotNull(transport.listener)
        }

    @Test
    fun `a cancelled start never wires a transport`() = runTest {
        val transport = FakeTransport()
        val io = StandardTestDispatcher(testScheduler)
        val client = client(transport, io)

        val job = launch(io) { client.start(paths(), FakeBridge()) }
        job.cancel()
        advanceUntilIdle()

        assertEquals(0, transport.startCalls)
        assertNull(transport.listener)

        // The same client can still start after the cancelled attempt.
        client.start(paths(), FakeBridge())
        assertEquals(1, transport.startCalls)
        assertNotNull(transport.listener)
    }

    // ---- events ---------------------------------------------------------

    @Test
    fun `listener events update status, logs, pings and notices`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val dispatcher = mainDispatcherRule.testDispatcher
            val transport = FakeTransport()
            val client = client(transport, dispatcher)
            val pings = mutableListOf<PingResult>()
            val notices = mutableListOf<Notice>()
            backgroundScope.launch(dispatcher) { client.pings.collect { pings += it } }
            backgroundScope.launch(dispatcher) { client.notices.collect { notices += it } }
            client.start(paths(), FakeBridge())
            val listener = requireNotNull(transport.listener)

            listener.onStatus(
                """
                {
                  "state": "connected",
                  "authenticated": true,
                  "quota": {"limit": 100, "remaining": 0},
                  "dns": "10.8.0.2"
                }
                """.trimIndent(),
            )
            listener.onLog(
                """{"ts":"2026-10-21T00:00:00Z","level":"warn","tag":"engine","msg":"hello"}""",
            )
            listener.onPing("""{"host":"edge.example:443","rtt_ms":42.5}""")
            listener.onNotice("""{"type":"signed_out","message":"signed out"}""")

            val status = requireNotNull(client.status.value)
            assertEquals("connected", status.state)
            assertTrue(status.authenticated)
            assertEquals(0L, status.quota?.remaining)
            assertEquals("10.8.0.2", status.dns)

            val entry = client.logs.value.single()
            assertEquals(LogLevel.WARN, entry.level)
            assertEquals("engine", entry.tag)
            assertEquals("hello", entry.message)

            assertEquals(1, pings.size)
            assertEquals("edge.example:443", pings.single().host)
            assertEquals(42.5, pings.single().rttMs)

            assertEquals(1, notices.size)
            assertEquals("signed_out", notices.single().type)
            assertEquals("signed out", notices.single().message)
        }

    @Test
    fun `logs are bounded to the newest 2000 entries`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val dispatcher = mainDispatcherRule.testDispatcher
            val transport = FakeTransport()
            val client = client(transport, dispatcher)
            client.start(paths(), FakeBridge())
            val listener = requireNotNull(transport.listener)

            repeat(2005) { index ->
                listener.onLog(
                    """{"ts":"2026-10-21T00:00:00Z","level":"info","tag":"t","msg":"line $index"}""",
                )
            }

            val logs = client.logs.value
            assertEquals(2000, logs.size)
            assertEquals("line 5", logs.first().message)
            assertEquals("line 2004", logs.last().message)
        }

    @Test
    fun `an unparseable listener event emits a notice and logs once per second`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val dispatcher = mainDispatcherRule.testDispatcher
            var now = 1_000L
            val warnings = mutableListOf<String>()
            val transport = FakeTransport()
            val client = client(transport, dispatcher, now = { now }, warn = { warnings += it })
            val notices = mutableListOf<Notice>()
            backgroundScope.launch(dispatcher) { client.notices.collect { notices += it } }
            client.start(paths(), FakeBridge())
            val listener = requireNotNull(transport.listener)

            listener.onStatus("not json")
            listener.onLog("also not json")

            assertEquals(2, notices.size)
            assertTrue(notices.all { it.type == "protocol_error" })
            assertTrue(notices.all { it.message!!.startsWith("could not parse an engine") })
            assertEquals(1, warnings.size)

            now = 1_500L
            listener.onPing("still not json")
            assertEquals(1, warnings.size)
            assertEquals(3, notices.size)

            now = 2_000L
            listener.onPing("still not json")
            assertEquals(2, warnings.size)
            assertEquals(4, notices.size)

            // Prior good state is untouched by a bad event.
            assertNull(client.status.value)
            assertTrue(client.logs.value.isEmpty())
        }

    // ---- commands and envelopes ----------------------------------------

    @Test
    fun `error envelopes surface their code and message`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val dispatcher = mainDispatcherRule.testDispatcher
            val transport = FakeTransport()
            transport.respond = {
                """{"error":{"code":"bad_request","message":"that setting is unsupported"}}"""
            }
            val client = client(transport, dispatcher)

            val failure = expect<EngineException> { client.setSetting("mtu", "x") }
            assertEquals("bad_request", failure.code)
            assertEquals("that setting is unsupported", failure.message)
        }

    @Test
    fun `malformed JSON and envelopes from the transport are protocol errors`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val dispatcher = mainDispatcherRule.testDispatcher
            val transport = FakeTransport()
            val client = client(transport, dispatcher)

            for (raw in listOf("", "not json", """{"payload":{}}""", """{"error":"oops"}""")) {
                transport.respond = { raw }
                val failure = expect<EngineProtocolException> { client.command("status") }
                assertEquals(EngineErrorCodes.PROTOCOL, failure.code)
                assertTrue(failure.message!!.isNotBlank())
            }
        }

    @Test
    fun `convenience commands shape their payloads`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val dispatcher = mainDispatcherRule.testDispatcher
            val transport = FakeTransport()
            val client = client(transport, dispatcher)

            client.ping(listOf("a.example:443", "b.example:443"))
            client.selectLocation("de", "ber", "de1")
            client.selectLocation("fr", null, "fr1")
            client.connect("de", "ber", "de1", proxyOnly = true)
            client.connect()
            client.setSetting("mtu", "1500", reset = true)
            client.logsSubscribe("warn")
            client.logAppend("info", "app", "hello from the app")
            client.doctor(JSONObject().put("sdk", 36))
            client.loginEmail("user@example.com")
            client.loginPassword("secret")
            client.login2fa("123456")
            client.logsSnapshot()

            val byType = transport.requests.groupBy { it.getString("type") }

            val ping = byType.getValue("ping").single().getJSONObject("payload")
            assertEquals(2, ping.getJSONArray("hosts").length())
            assertEquals("a.example:443", ping.getJSONArray("hosts").getString(0))

            val selections = byType.getValue("select_location")
            assertEquals("de", selections[0].getJSONObject("payload").getString("country_code"))
            assertEquals("ber", selections[0].getJSONObject("payload").getString("city_code"))
            assertEquals("de1", selections[0].getJSONObject("payload").getString("server"))
            assertFalse(selections[1].getJSONObject("payload").has("city_code"))

            val connects = byType.getValue("connect")
            val full = connects[0].getJSONObject("payload")
            assertTrue(full.getBoolean("proxy_only"))
            assertEquals("de", full.getString("location_code"))
            assertEquals("ber", full.getString("city_code"))
            assertEquals("de1", full.getString("server"))
            val bare = connects[1].getJSONObject("payload")
            assertFalse(bare.getBoolean("proxy_only"))
            assertFalse(bare.has("location_code"))
            assertFalse(bare.has("city_code"))
            assertFalse(bare.has("server"))

            val setting = byType.getValue("settings_set").single().getJSONObject("payload")
            assertEquals("mtu", setting.getString("key"))
            assertEquals("1500", setting.getString("value"))
            assertTrue(setting.getBoolean("reset"))

            val subscribe = byType.getValue("logs_subscribe").single().getJSONObject("payload")
            assertEquals("warn", subscribe.getString("level"))

            val append = byType.getValue("log_append").single().getJSONObject("payload")
            assertEquals("info", append.getString("level"))
            assertEquals("app", append.getString("tag"))
            assertEquals("hello from the app", append.getString("msg"))

            val doctor = byType.getValue("doctor").single().getJSONObject("payload")
            assertEquals(36, doctor.getJSONObject("facts").getInt("sdk"))

            assertEquals(
                "user@example.com",
                byType.getValue("login_email").single().getJSONObject("payload").getString("email"),
            )
            assertEquals(
                "secret",
                byType.getValue("login_password").single().getJSONObject("payload")
                    .getString("password"),
            )
            assertEquals(
                "123456",
                byType.getValue("login_2fa").single().getJSONObject("payload").getString("code"),
            )
            assertFalse(
                byType.getValue("logs_snapshot").single().has("payload"),
            )
        }

    @Test
    fun `settings, account, locations, status and doctor responses parse`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val dispatcher = mainDispatcherRule.testDispatcher
            val transport = FakeTransport()
            transport.respond = { raw ->
                when (JSONObject(raw).getString("type")) {
                    "settings_get" ->
                        """{"type":"settings","payload":{"mtu":1500,"log_level":"info"}}"""
                    "account" ->
                        """{"type":"account_info","payload":{"email":"a@b.example","subscribed":true}}"""
                    "locations" ->
                        """{"type":"locations","payload":{"countries":[{"code":"rec","name":"Recommended"}]}}"""
                    "doctor" ->
                        """
                        {"type":"doctor","payload":{
                          "version":"v0.5.0",
                          "checks":[{"name":"vpn-permission","status":"ok","detail":"granted"}],
                          "failed":0,"warned":0,"skipped":0}}
                        """.trimIndent()
                    "logs_snapshot" ->
                        """{"type":"log_snapshot","payload":{"entries":[{"ts":"2026-10-21T00:00:00Z","level":"info","tag":"engine","msg":"one"}]}}"""
                    "status" ->
                        """{"type":"status","payload":{"state":"connected","authenticated":true}}"""
                    else -> """{"type":"result","payload":{"ok":true}}"""
                }
            }
            val client = client(transport, dispatcher)

            val settings = client.settings()
            assertEquals(1500, settings.mtu)
            assertEquals("info", settings.logLevel)

            val account = client.account()
            assertEquals("a@b.example", account.email)
            assertTrue(account.subscribed)

            val countries = client.locations(refresh = true)
            assertEquals("rec", countries.single().code)

            val report = client.doctor()
            assertEquals("v0.5.0", report.version)
            assertEquals(DoctorStatus.OK, report.checks.single().status)

            val logs = client.logsSnapshot("info")
            assertEquals("one", logs.single().message)

            val status = client.refreshStatus()
            assertEquals("connected", status.state)
            assertEquals("connected", client.status.value?.state)

            val locationsRequest = transport.requests
                .single { it.getString("type") == "locations" }
                .getJSONObject("payload")
            assertTrue(locationsRequest.getBoolean("refresh"))
        }

    @Test
    fun `commands run on the injected dispatcher`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val delegate = StandardTestDispatcher(testScheduler)
            val counting = CountingDispatcher(delegate)
            val transport = FakeTransport()
            transport.respond = { """{"type":"status","payload":{"state":"idle"}}""" }
            val client = client(transport, counting)

            client.start(paths(), FakeBridge())
            client.refreshStatus()

            assertTrue("expected at least start and status dispatches", counting.dispatches >= 2)
        }

    @Test
    fun `close is idempotent and rejects later commands`() =
        runTest(mainDispatcherRule.testDispatcher) {
            val dispatcher = mainDispatcherRule.testDispatcher
            val transport = FakeTransport()
            val client = client(transport, dispatcher)
            client.start(paths(), FakeBridge())

            client.close()
            client.close()

            assertEquals(1, transport.closeCalls)
            assertFalse(transport.started)
            val failure = expect<EngineException> { client.refreshStatus() }
            assertEquals(EngineErrorCodes.CLOSED, failure.code)
        }
}
