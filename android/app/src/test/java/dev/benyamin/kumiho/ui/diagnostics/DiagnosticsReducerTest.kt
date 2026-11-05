package dev.benyamin.kumiho.ui.diagnostics

import dev.benyamin.kumiho.diagnostics.AndroidFacts
import dev.benyamin.kumiho.engine.ByteRates
import dev.benyamin.kumiho.engine.CachedPing
import dev.benyamin.kumiho.engine.DoctorCheck
import dev.benyamin.kumiho.engine.DoctorReport
import dev.benyamin.kumiho.engine.DoctorStatus
import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.engine.Quota
import dev.benyamin.kumiho.test.MainDispatcherRule
import java.time.Instant
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * The doctor report's pure filtering/text rules and the diagnostics state
 * machine. A report containing desktop-only plumbing proves the client
 * deny-list; the fake gateway proves the report is not polled.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class DiagnosticsReducerTest {

    @get:Rule
    val mainRule = MainDispatcherRule(StandardTestDispatcher())

    private val gateway = FakeGateway()

    private fun viewModel(): DiagnosticsViewModel = DiagnosticsViewModel(gateway)

    // ---- reducer ---------------------------------------------------------

    @Test
    fun `desktop-only checks are dropped and the useful order is kept`() {
        val report = report(
            check("platform", DoctorStatus.FAIL),
            check("vpn-permission", DoctorStatus.OK),
            check("nftables", DoctorStatus.FAIL),
            check("engine-state", DoctorStatus.WARN),
            check("socks-port", DoctorStatus.FAIL),
        )

        val visible = DiagnosticsReducer.visibleChecks(report)

        assertEquals(listOf("vpn-permission", "engine-state"), visible.map { it.name })
    }

    @Test
    fun `status counts and the summary describe only visible checks`() {
        val checks = listOf(
            check("vpn-permission", DoctorStatus.OK),
            check("engine-state", DoctorStatus.WARN),
            check("dns", DoctorStatus.FAIL),
            check("network", DoctorStatus.SKIP),
            check("network", DoctorStatus.SKIP),
        )

        assertEquals(
            mapOf(
                DoctorStatus.OK to 1,
                DoctorStatus.WARN to 1,
                DoctorStatus.FAIL to 1,
                DoctorStatus.SKIP to 2,
            ),
            DiagnosticsReducer.statusCounts(checks),
        )
        assertEquals("1 failed · 1 warnings · 2 skipped", DiagnosticsReducer.summary(report(), checks))
        assertEquals("", DiagnosticsReducer.summary(null, checks))
    }

    @Test
    fun `a check is looked up by its exact wire name`() {
        val report = report(check("clock-skew", DoctorStatus.WARN, detail = "4 s ahead"))

        assertEquals("4 s ahead", DiagnosticsReducer.check(report, "clock-skew")?.detail)
        assertNull(DiagnosticsReducer.check(report, "clock"))
    }

    @Test
    fun `report text includes facts and only visible checks`() {
        val facts = AndroidFacts(
            device = "Google Pixel 8",
            sdk = 36,
            vpnPermission = true,
            dnsServers = listOf("10.8.0.2"),
            engineState = "connected",
        )
        val report = report(
            check("vpn-permission", DoctorStatus.OK, detail = "granted"),
            check("platform", DoctorStatus.FAIL, detail = "linux only"),
            check("dns", DoctorStatus.WARN, detail = "private DNS", remediation = "turn it off"),
        )

        val text = DiagnosticsReducer.reportText(report, facts)

        assertTrue(text.contains("version: 0.5.0"))
        assertTrue(text.contains("Android facts"))
        assertTrue(text.contains("device: Google Pixel 8"))
        assertTrue(text.contains("dns_servers: 10.8.0.2"))
        assertTrue(text.contains("- [ok] vpn-permission: granted"))
        assertTrue(text.contains("- [warn] dns: private DNS — turn it off"))
        assertFalse("desktop plumbing must not be copied", text.contains("platform"))
        assertEquals("", DiagnosticsReducer.reportText(null, facts))
    }

    // ---- view model ------------------------------------------------------

    @Test
    fun `the first visit collects facts and runs the doctor exactly once`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()

            assertEquals(1, gateway.factsCalls)
            assertEquals(1, gateway.doctorCalls)
            val state = viewModel.state.value
            assertFalse(state.loading)
            assertTrue(state.hasReport)
            assertEquals(sampleFacts(), state.facts)
            assertEquals(listOf("vpn-permission", "engine-state"), state.checks.map { it.name })
        }

    @Test
    fun `re-run collects fresh facts and replaces the report`() = runTest(mainRule.testDispatcher) {
        val viewModel = viewModel()
        runCurrent()

        viewModel.rerun()
        runCurrent()

        assertEquals(2, gateway.factsCalls)
        assertEquals(2, gateway.doctorCalls)
        assertEquals("load-2", viewModel.state.value.report?.time)
    }

    @Test
    fun `a failed load reports it and a re-run recovers`() = runTest(mainRule.testDispatcher) {
        gateway.doctorError = IllegalStateException("the doctor is unavailable")
        val viewModel = viewModel()
        runCurrent()

        assertNull(viewModel.state.value.report)
        assertEquals(DiagnosticsMessage.LOAD_FAILED, viewModel.state.value.message)
        assertEquals("the doctor is unavailable", viewModel.state.value.error)

        gateway.doctorError = null
        viewModel.rerun()
        runCurrent()

        assertTrue(viewModel.state.value.hasReport)
        assertNull(viewModel.state.value.message)
    }

    @Test
    fun `reset asks first then clears local state and reloads`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()

            viewModel.requestReset()
            assertTrue(viewModel.state.value.resetDialog)
            assertEquals(0, gateway.resetCalls)

            viewModel.confirmReset()
            runCurrent()

            assertEquals(1, gateway.resetCalls)
            assertEquals(DiagnosticsMessage.RESET_DONE, viewModel.state.value.message)
            assertEquals(2, gateway.doctorCalls)
            assertFalse(viewModel.state.value.resetDialog)
        }

    @Test
    fun `a failed reset keeps the dialog result out and reports the error`() =
        runTest(mainRule.testDispatcher) {
            gateway.resetError = IllegalStateException("the cache is locked")
            val viewModel = viewModel()
            runCurrent()

            viewModel.requestReset()
            viewModel.dismissReset()
            assertFalse(viewModel.state.value.resetDialog)

            viewModel.requestReset()
            viewModel.confirmReset()
            runCurrent()

            assertEquals(DiagnosticsMessage.RESET_FAILED, viewModel.state.value.message)
            assertEquals("the cache is locked", viewModel.state.value.error)
            assertFalse(viewModel.state.value.resetting)
        }

    @Test
    fun `copy feedback is dismissible`() = runTest(mainRule.testDispatcher) {
        val viewModel = viewModel()
        runCurrent()

        viewModel.notifyCopied()
        assertEquals(DiagnosticsMessage.COPIED, viewModel.state.value.message)

        viewModel.dismissMessage()
        assertNull(viewModel.state.value.message)
        assertNull(viewModel.state.value.error)
    }

    // ---- charts ----------------------------------------------------------

    @Test
    fun `latency bars drop failed probes, sort and cap at twelve`() {
        val cache = buildMap {
            put("failed", CachedPing(null, Instant.EPOCH))
            put("nan", CachedPing(Double.NaN, Instant.EPOCH))
            put("negative", CachedPing(-1.0, Instant.EPOCH))
            (0 until 15).forEach { i ->
                put("h$i", CachedPing((15 - i).toDouble(), Instant.EPOCH))
            }
        }

        val bars = latencyBars(cache)

        assertEquals(12, bars.size)
        assertEquals("h14", bars.first().host)
        assertEquals(1.0, bars.first().rttMs, 0.0)
        assertEquals("h3", bars.last().host)
    }

    @Test
    fun `chart notes are honest for no session, no cache and a failure`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()
            assertEquals(DiagnosticsChartNote.NO_SESSION, viewModel.state.value.chartNote)

            gateway.statusFlow.value = status("CONNECTED")
            gateway.latency = emptyMap()
            viewModel.rerun()
            runCurrent()
            assertEquals(DiagnosticsChartNote.NO_CACHE, viewModel.state.value.chartNote)

            gateway.latencyError = IllegalStateException("cache locked")
            viewModel.rerun()
            runCurrent()
            assertEquals(
                DiagnosticsChartNote.LATENCY_UNAVAILABLE,
                viewModel.state.value.chartNote,
            )
            assertTrue(viewModel.state.value.latency.isEmpty())
        }

    @Test
    fun `cached latency becomes bars and clears the note`() =
        runTest(mainRule.testDispatcher) {
            gateway.statusFlow.value = status("CONNECTED")
            gateway.latency = mapOf(
                "a.example" to CachedPing(42.0, Instant.EPOCH),
                "b.example" to CachedPing(11.0, Instant.EPOCH),
            )

            val viewModel = viewModel()
            runCurrent()

            assertNull(viewModel.state.value.chartNote)
            assertEquals(listOf("b.example", "a.example"), viewModel.state.value.latency.map { it.host })
        }

    @Test
    fun `engine rates feed the live trace`() = runTest(mainRule.testDispatcher) {
        val viewModel = viewModel()
        runCurrent()

        gateway.statusFlow.value = status("CONNECTED")
            .copy(rates = ByteRates(up = 1.0, down = 2.0))
        runCurrent()
        assertEquals(1, viewModel.state.value.trace.size)
        assertEquals(2.0, viewModel.state.value.trace.single().down, 0.0)

        gateway.statusFlow.value = null
        runCurrent()
        assertTrue(viewModel.state.value.trace.isEmpty())
    }

    // ---- fake ------------------------------------------------------------

    private class FakeGateway : DiagnosticsGateway {
        var factsCalls = 0
        var doctorCalls = 0
        var resetCalls = 0
        var doctorError: Exception? = null
        var resetError: Exception? = null
        val statusFlow = MutableStateFlow<EngineStatus?>(null)
        var latency: Map<String, CachedPing> = emptyMap()
        var latencyError: Exception? = null

        override suspend fun status(): StateFlow<EngineStatus?> = statusFlow

        override suspend fun cachedLatency(): Map<String, CachedPing> {
            latencyError?.let { throw it }
            return latency
        }

        override suspend fun facts(): AndroidFacts {
            factsCalls++
            return sampleFacts()
        }

        override suspend fun doctor(facts: AndroidFacts): DoctorReport {
            doctorCalls++
            doctorError?.let { throw it }
            return report(
                check("vpn-permission", DoctorStatus.OK),
                check("platform", DoctorStatus.FAIL),
                check("engine-state", DoctorStatus.WARN),
                time = "load-$doctorCalls",
            )
        }

        override suspend fun resetState() {
            resetCalls++
            resetError?.let { throw it }
        }
    }

    private companion object {
        fun status(state: String) = EngineStatus(
            state = state,
            authenticated = true,
            email = "user@example.com",
            since = 0L,
            tokenExpires = 0L,
            passExpires = 0L,
            killSwitch = true,
            autoconnect = true,
            location = null,
            server = null,
            proxyOnly = false,
            quota = Quota(limit = null, remaining = null, reset = null),
            dns = null,
            ipv6 = null,
            exitIp = null,
            exitCountry = null,
            totals = null,
            rates = null,
        )

        fun sampleFacts() = AndroidFacts(
            device = "Google Pixel 8",
            sdk = 36,
            vpnPermission = true,
            dnsServers = listOf("10.8.0.2"),
            engineState = "connected",
        )

        fun check(
            name: String,
            status: DoctorStatus,
            detail: String = "detail",
            remediation: String? = null,
        ) = DoctorCheck(name = name, status = status, detail = detail, remediation = remediation)

        fun report(vararg checks: DoctorCheck, time: String = "now") = DoctorReport(
            version = "0.5.0",
            host = "android",
            os = "android",
            time = time,
            checks = checks.toList(),
            failed = checks.count { it.status == DoctorStatus.FAIL },
            warned = checks.count { it.status == DoctorStatus.WARN },
            skipped = checks.count { it.status == DoctorStatus.SKIP },
        )
    }
}
