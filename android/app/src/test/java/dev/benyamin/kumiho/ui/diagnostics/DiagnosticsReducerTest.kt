package dev.benyamin.kumiho.ui.diagnostics

import dev.benyamin.kumiho.diagnostics.AndroidFacts
import dev.benyamin.kumiho.engine.DoctorCheck
import dev.benyamin.kumiho.engine.DoctorReport
import dev.benyamin.kumiho.engine.DoctorStatus
import dev.benyamin.kumiho.test.MainDispatcherRule
import kotlinx.coroutines.ExperimentalCoroutinesApi
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

    // ---- fake ------------------------------------------------------------

    private class FakeGateway : DiagnosticsGateway {
        var factsCalls = 0
        var doctorCalls = 0
        var resetCalls = 0
        var doctorError: Exception? = null
        var resetError: Exception? = null

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
