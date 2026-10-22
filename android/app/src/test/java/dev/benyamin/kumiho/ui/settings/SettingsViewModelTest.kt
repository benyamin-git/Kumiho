package dev.benyamin.kumiho.ui.settings

import dev.benyamin.kumiho.engine.Settings
import dev.benyamin.kumiho.test.MainDispatcherRule
import kotlinx.coroutines.CompletableDeferred
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
 * The Settings state machine on the JVM: a fake gateway and update checker,
 * virtual time, no Android or Compose.
 *
 * It pins the whole write protocol: one change at a time, client validation
 * before any engine call, the fresh `settings_get` after a successful set, the
 * old snapshot on failure, the inline engine error, the refresh notice with its
 * retry, and that the two Android rows can never reach `settings_set`.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class SettingsViewModelTest {

    @get:Rule
    val mainRule = MainDispatcherRule(StandardTestDispatcher())

    private val gateway = FakeGateway()
    private val checker = FakeChecker()

    private fun viewModel() = SettingsViewModel(
        gateway = gateway,
        updateChecker = checker,
        appVersion = "0.5.0-debug",
        engineVersion = "0.5.0",
    )

    // ---- load ------------------------------------------------------------

    @Test
    fun `the first load exposes the snapshot and both versions`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            assertTrue(viewModel.state.value.showLoading)

            runCurrent()

            val state = viewModel.state.value
            assertTrue(state.loaded)
            assertFalse(state.loading)
            assertFalse(state.showLoading)
            assertFalse(state.showError)
            assertEquals(gateway.snapshot, state.snapshot)
            assertEquals("0.5.0-debug", state.appVersion)
            assertEquals("0.5.0", state.engineVersion)
            assertEquals("lightsail", state.snapshot?.edgeAddress)
            assertFalse("Advanced starts collapsed", state.advancedExpanded)
            assertNull(state.applyingKey)
        }

    @Test
    fun `an initial failure is an error whose retry loads`() =
        runTest(mainRule.testDispatcher) {
            gateway.settingsError = IllegalStateException("daemon offline")
            val viewModel = viewModel()
            runCurrent()

            assertTrue(viewModel.state.value.showError)
            assertEquals("daemon offline", viewModel.state.value.loadError)

            gateway.settingsError = null
            viewModel.retryLoad()
            runCurrent()

            assertFalse(viewModel.state.value.showError)
            assertNull(viewModel.state.value.loadError)
            assertTrue(viewModel.state.value.loaded)
            assertEquals(2, gateway.settingsCalls)
        }

    // ---- writes ----------------------------------------------------------

    @Test
    fun `a toggle sends on or off and only a fresh snapshot makes it visible`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()

            gateway.snapshot = gateway.snapshot.copy(exitCheck = false)
            viewModel.toggle("exit_check", checked = false)
            runCurrent()

            assertEquals(listOf(Triple("exit_check", "off", false)), gateway.changes)
            assertEquals("settings_set pushes status, never settings", 2, gateway.settingsCalls)
            assertEquals("off", viewModel.state.value.valueOf("exit_check"))
            assertNull(viewModel.state.value.applyingKey)
        }

    @Test
    fun `a boolean on is serialized as exactly on`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()

            viewModel.toggle("http_proxy", checked = true)
            runCurrent()

            assertEquals(listOf(Triple("http_proxy", "on", false)), gateway.changes)
        }

    @Test
    fun `one change at a time locks every row until it settles`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()

            gateway.setGate = CompletableDeferred()
            viewModel.toggle("exit_check", checked = false)
            viewModel.apply("mtu", "1400")
            viewModel.reset("log_level")
            runCurrent()

            assertEquals("only the first change is sent", 1, gateway.changes.size)
            assertEquals("exit_check", viewModel.state.value.applyingKey)
            assertTrue(viewModel.state.value.applying)

            gateway.setGate?.complete(Unit)
            runCurrent()

            assertNull(viewModel.state.value.applyingKey)
            assertFalse(viewModel.state.value.applying)
        }

    @Test
    fun `read-only Android rows never reach settings_set`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()

            viewModel.toggle("kill_switch", checked = true)
            viewModel.apply("autoconnect", "on")
            viewModel.reset("kill_switch")
            runCurrent()

            assertTrue(gateway.changes.isEmpty())
            assertNull(viewModel.state.value.rowError)
            assertEquals(1, gateway.settingsCalls)
        }

    // ---- client validation ----------------------------------------------

    @Test
    fun `a non-number is rejected before the engine is called`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()

            viewModel.apply("mtu", "abc")
            runCurrent()

            val error = viewModel.state.value.rowError
            assertEquals("mtu", error?.key)
            assertEquals(
                SettingInput.Invalid(SettingError.NOT_A_NUMBER),
                error?.validation,
            )
            assertTrue(gateway.changes.isEmpty())
        }

    @Test
    fun `an out-of-range port is rejected with its range`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()

            viewModel.apply("socks_port", "65536")
            runCurrent()

            assertEquals(
                SettingInput.Invalid(
                    SettingError.OUT_OF_RANGE,
                    range = SettingRange(1, 65535),
                ),
                viewModel.state.value.rowError?.validation,
            )
            assertTrue(gateway.changes.isEmpty())
        }

    @Test
    fun `an unknown enum value is rejected`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()

            viewModel.apply("doh_provider", "adguard")
            runCurrent()

            assertEquals(
                SettingInput.Invalid(SettingError.NOT_AN_OPTION),
                viewModel.state.value.rowError?.validation,
            )
            assertTrue(gateway.changes.isEmpty())
        }

    @Test
    fun `an IPv6 exclusion is rejected with the IPv6 reason`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()

            viewModel.apply("exclude_cidrs", "fd00::/8")
            runCurrent()

            assertEquals(
                SettingInput.Invalid(SettingError.IPV6_NOT_SUPPORTED),
                viewModel.state.value.rowError?.validation,
            )
            assertTrue(gateway.changes.isEmpty())
        }

    @Test
    fun `a valid CIDR list is normalized before it is sent`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()

            viewModel.apply("exclude_cidrs", " 10.0.0.0/8 , 192.168.1.5 ")
            runCurrent()

            assertEquals(
                listOf(Triple("exclude_cidrs", "10.0.0.0/8, 192.168.1.5/32", false)),
                gateway.changes,
            )
        }

    @Test
    fun `an edit clears only its own inline error`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()

            viewModel.apply("mtu", "abc")
            runCurrent()
            viewModel.clearError("socks_port")
            assertEquals("mtu", viewModel.state.value.rowError?.key)

            viewModel.clearError("mtu")
            assertNull(viewModel.state.value.rowError)
        }

    // ---- failures --------------------------------------------------------

    @Test
    fun `an engine rejection keeps the old snapshot and shows its words inline`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()
            val before = viewModel.state.value.snapshot

            gateway.setError = IllegalStateException("mtu: out of range: 10")
            viewModel.apply("mtu", "1400")
            runCurrent()

            val state = viewModel.state.value
            assertEquals("a failed change never guesses a new value", before, state.snapshot)
            assertNull(state.applyingKey)
            assertEquals("mtu", state.rowError?.key)
            assertEquals("mtu: out of range: 10", state.rowError?.text)
            assertEquals("no refetch after a failed set", 1, gateway.settingsCalls)
        }

    @Test
    fun `a refresh failure after a save is a notice with a retry`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()
            val before = viewModel.state.value.snapshot
            gateway.settingsErrorOnCall = 2

            viewModel.toggle("exit_check", checked = false)
            runCurrent()

            val state = viewModel.state.value
            assertEquals(before, state.snapshot)
            assertNull(state.applyingKey)
            assertEquals(SettingsMessage.REFRESH_AFTER_SAVE_FAILED, state.notice?.message)
            assertEquals("the engine did not answer", state.notice?.detail)
            assertTrue(state.notice?.retry == true)
            assertNull(state.rowError)
        }

    @Test
    fun `a failed refresh keeps the snapshot and its retry reloads`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()
            gateway.settingsErrorOnCall = 2

            viewModel.refresh()
            runCurrent()

            val failed = viewModel.state.value
            assertTrue(failed.hasSnapshot)
            assertEquals(SettingsMessage.REFRESH_FAILED, failed.notice?.message)
            assertEquals("the engine did not answer", failed.notice?.detail)

            gateway.settingsErrorOnCall = null
            viewModel.retryNotice()
            runCurrent()

            assertNull(viewModel.state.value.notice)
            assertEquals(3, gateway.settingsCalls)
        }

    @Test
    fun `dismissing a notice keeps the last snapshot`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()
            gateway.settingsErrorOnCall = 2
            viewModel.refresh()
            runCurrent()

            viewModel.dismissNotice()

            assertNull(viewModel.state.value.notice)
            assertTrue(viewModel.state.value.hasSnapshot)
        }

    // ---- groups and updates ---------------------------------------------

    @Test
    fun `Advanced toggles open and closed`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()
            assertFalse(viewModel.state.value.advancedExpanded)

            viewModel.toggleAdvanced()
            assertTrue(viewModel.state.value.advancedExpanded)

            viewModel.toggleAdvanced()
            assertFalse(viewModel.state.value.advancedExpanded)
        }

    @Test
    fun `loading never starts an update check`() =
        runTest(mainRule.testDispatcher) {
            viewModel()
            runCurrent()
            assertEquals(0, checker.calls)
        }

    @Test
    fun `a manual check reports a newer release and one check runs at a time`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()
            checker.gate = CompletableDeferred()
            checker.result = UpdateResult.Available("0.6.0", "https://example.test/release")

            viewModel.checkForUpdates()
            runCurrent()
            assertTrue(viewModel.state.value.update is UpdateState.Checking)

            viewModel.checkForUpdates()
            runCurrent()
            assertEquals("a second tap while checking is ignored", 1, checker.calls)

            checker.gate?.complete(Unit)
            runCurrent()

            assertEquals(
                UpdateState.Available("0.6.0", "https://example.test/release"),
                viewModel.state.value.update,
            )
            assertEquals("0.5.0-debug", checker.lastVersion)
        }

    @Test
    fun `a failed check is a visible failure that can be retried`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()
            checker.result = UpdateResult.Failed("no route to host")

            viewModel.checkForUpdates()
            runCurrent()

            assertEquals(UpdateState.Failed("no route to host"), viewModel.state.value.update)

            checker.result = UpdateResult.UpToDate
            viewModel.checkForUpdates()
            runCurrent()

            assertEquals(UpdateState.UpToDate, viewModel.state.value.update)
            assertEquals(2, checker.calls)
        }

    // ---- fakes -----------------------------------------------------------

    private class FakeGateway : SettingsGateway {
        var snapshot = sample()
        var settingsError: Exception? = null
        var settingsErrorOnCall: Int? = null
        var setError: Exception? = null
        var setGate: CompletableDeferred<Unit>? = null
        var settingsCalls = 0
        val changes = mutableListOf<Triple<String, String, Boolean>>()

        override suspend fun settings(): Settings {
            settingsCalls += 1
            if (settingsCalls == settingsErrorOnCall) {
                throw IllegalStateException("the engine did not answer")
            }
            settingsError?.let { throw it }
            return snapshot
        }

        override suspend fun setSetting(key: String, value: String, reset: Boolean) {
            changes += Triple(key, value, reset)
            setGate?.await()
            setError?.let { throw it }
        }
    }

    private class FakeChecker : UpdateChecker {
        var result: UpdateResult = UpdateResult.UpToDate
        var gate: CompletableDeferred<Unit>? = null
        var calls = 0
        var lastVersion: String? = null

        override suspend fun check(currentVersion: String): UpdateResult {
            calls += 1
            lastVersion = currentVersion
            gate?.await()
            return result
        }
    }

    private companion object {
        fun sample() = Settings(
            socksPort = 1080,
            mtu = 1500,
            exitCheck = true,
            dohProvider = "automatic",
            dnsUpstream = null,
            edgeAddress = "lightsail",
            upstreamProxy = null,
            excludeCidrs = emptyList(),
            allowLan = false,
            logLevel = "info",
            redactTargets = true,
            killSwitch = "last",
            autoconnect = "last",
            killSwitchEffective = true,
            autoconnectEffective = false,
            quotaPollMinutes = 30,
            overridden = emptyList(),
            tlsSplit = "off",
            httpProxy = false,
            httpProxyPort = 8080,
            solveChallenges = true,
            keepaliveProfile = "standard",
        )
    }
}
