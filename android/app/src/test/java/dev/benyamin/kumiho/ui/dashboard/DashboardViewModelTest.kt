package dev.benyamin.kumiho.ui.dashboard

import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.engine.Notice
import dev.benyamin.kumiho.engine.Quota
import dev.benyamin.kumiho.test.MainDispatcherRule
import dev.benyamin.kumiho.vpn.ConnectRequest
import dev.benyamin.kumiho.vpn.ServiceState
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * The dashboard state machine on the JVM: a fake gateway and fake permissions,
 * so the one-in-flight guard, the permission hand-off, the honest notice model,
 * the options sheet and the location choice to request mapping are all
 * deterministic without Android, Compose or a daemon.
 *
 * The service stays authoritative: every test drives state through
 * [FakeGateway.service] and asserts what the dashboard sent, never a local
 * guess about what the tunnel is doing.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class DashboardViewModelTest {

    @get:Rule
    val mainRule = MainDispatcherRule(StandardTestDispatcher())

    private val gateway = FakeGateway(initial = snapshot(state = STATE_IDLE))
    private val permissions = FakePermissions()

    private fun viewModel(timeoutMillis: Long = DEFAULT_TIMEOUT): DashboardViewModel =
        DashboardViewModel(
            gateway = gateway,
            permissions = permissions,
            inFlightTimeoutMillis = timeoutMillis,
        )

    // ---- start-up --------------------------------------------------------

    @Test
    fun `the initial state comes from the service and the tree loads once`() =
        runTest(mainRule.testDispatcher) {
            gateway.locationResult = { COUNTRIES }
            val viewModel = viewModel()
            runCurrent()

            assertEquals(DashboardPhase.OFF, viewModel.state.value.phase)
            assertEquals(LocationLoad.Ready(COUNTRIES), viewModel.state.value.locations)
            assertEquals(1, gateway.locationCalls)
        }

    @Test
    fun `a resume refreshes the status and re-reads the Android policy`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()

            viewModel.onResume()
            runCurrent()

            assertEquals(1, gateway.refreshCalls)
            assertEquals(1, gateway.policyRefreshes)
        }

    // ---- power -----------------------------------------------------------

    @Test
    fun `a connect sends one request and blocks a second tap`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()

            viewModel.onPower()
            runCurrent()

            assertEquals(
                listOf(ConnectRequest(locationCode = "rec")),
                gateway.requests,
            )
            assertTrue(viewModel.state.value.inFlight)
            assertTrue(viewModel.state.value.powerBusy)

            viewModel.onPower()
            runCurrent()
            assertEquals("a second tap must not submit twice", 1, gateway.requests.size)
        }

    @Test
    fun `the in-flight guard ends when the service leaves the command phase`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()
            viewModel.onPower()
            runCurrent()
            assertTrue(viewModel.state.value.inFlight)

            gateway.serviceState.value = snapshot(state = STATE_CONNECTING)
            runCurrent()

            assertEquals(DashboardPhase.CONNECTING, viewModel.state.value.phase)
            assertFalse(viewModel.state.value.inFlight)
            assertTrue("a connecting tunnel still shows a spinner", viewModel.state.value.powerBusy)
        }

    @Test
    fun `a lost intent cannot wedge the power control forever`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel(timeoutMillis = 1_000L)
            runCurrent()
            viewModel.onPower()
            runCurrent()
            assertTrue(viewModel.state.value.inFlight)

            advanceTimeBy(1_000L)
            runCurrent()

            assertFalse(viewModel.state.value.inFlight)
            assertEquals("the service is still the source of truth", DashboardPhase.OFF, viewModel.state.value.phase)
        }

    @Test
    fun `a disconnect sends one command and no connect`() =
        runTest(mainRule.testDispatcher) {
            gateway.serviceState.value = connected()
            val viewModel = viewModel()
            runCurrent()

            viewModel.onPower()
            runCurrent()

            assertEquals(1, gateway.disconnectCalls)
            assertTrue("a disconnect is never a connect", gateway.requests.isEmpty())

            viewModel.onPower()
            runCurrent()
            assertEquals("a second tap must not disconnect twice", 1, gateway.disconnectCalls)
        }

    @Test
    fun `a disconnect under lockdown asks first`() =
        runTest(mainRule.testDispatcher) {
            gateway.serviceState.value = connected(lockdown = true)
            val viewModel = viewModel()
            runCurrent()

            viewModel.onPower()
            runCurrent()
            assertTrue(viewModel.state.value.lockdownPrompt)
            assertEquals("the tunnel must stay up until confirmed", 0, gateway.disconnectCalls)

            viewModel.cancelLockdownPrompt()
            assertFalse(viewModel.state.value.lockdownPrompt)
            assertEquals(0, gateway.disconnectCalls)

            viewModel.onPower()
            viewModel.confirmLockdownDisconnect()
            runCurrent()
            assertFalse(viewModel.state.value.lockdownPrompt)
            assertEquals(1, gateway.disconnectCalls)
        }

    @Test
    fun `a failed connect is reported without a fake success`() =
        runTest(mainRule.testDispatcher) {
            gateway.connectError = IllegalStateException("the service is not bound")
            val viewModel = viewModel()
            runCurrent()

            viewModel.onPower()
            runCurrent()

            assertFalse(viewModel.state.value.inFlight)
            val notice = viewModel.state.value.notice
            assertEquals(DashboardNoticeTone.ERROR, notice?.tone)
            assertEquals(DashboardNoticeAction.RETRY_CONNECT, notice?.action)
            assertEquals(
                NoticeText.Local(DashboardMessage.CONNECT_FAILED, "the service is not bound"),
                notice?.text,
            )
        }

    @Test
    fun `a failed disconnect is reported as a disconnect failure`() =
        runTest(mainRule.testDispatcher) {
            gateway.serviceState.value = connected()
            gateway.disconnectError = IllegalStateException("the binder is gone")
            val viewModel = viewModel()
            runCurrent()

            viewModel.onPower()
            runCurrent()

            assertFalse(viewModel.state.value.inFlight)
            assertEquals(
                NoticeText.Local(DashboardMessage.DISCONNECT_FAILED, "the binder is gone"),
                viewModel.state.value.notice?.text,
            )
            assertEquals(
                "a lost session cannot be retried with a connect",
                DashboardNoticeAction.NONE,
                viewModel.state.value.notice?.action,
            )
        }

    // ---- permission ------------------------------------------------------

    @Test
    fun `no consent asks Android before it connects`() =
        runTest(mainRule.testDispatcher) {
            permissions.granted = false
            val viewModel = viewModel()
            runCurrent()
            val events = collectEvents(viewModel)
            runCurrent()

            viewModel.onPower()
            runCurrent()

            assertTrue("no request before consent", gateway.requests.isEmpty())
            assertEquals(listOf(DashboardEvent.RequestVpnPermission), events)

            // The user grants it in the system dialog; the connect then goes out.
            viewModel.onVpnConsentResult(true)
            runCurrent()
            assertEquals(listOf(ConnectRequest(locationCode = "rec")), gateway.requests)
        }

    @Test
    fun `a denied consent is a warning with another chance`() =
        runTest(mainRule.testDispatcher) {
            permissions.granted = false
            val viewModel = viewModel()
            runCurrent()
            val events = collectEvents(viewModel)
            runCurrent()

            viewModel.onVpnConsentResult(false)
            runCurrent()

            val notice = viewModel.state.value.notice
            assertEquals(DashboardNoticeTone.WARNING, notice?.tone)
            assertEquals(DashboardNoticeAction.GRANT_PERMISSION, notice?.action)
            assertEquals(
                NoticeText.Local(DashboardMessage.VPN_PERMISSION_DENIED),
                notice?.text,
            )
            assertFalse(
                "the power button never opens the sheet",
                viewModel.state.value.options.open,
            )
            assertTrue(gateway.requests.isEmpty())

            viewModel.retryNotice()
            runCurrent()
            assertEquals(
                "the one retry is the consent dialog again",
                listOf(DashboardEvent.RequestVpnPermission),
                events,
            )
        }

    @Test
    fun `a permission phase asks for consent from the power control`() =
        runTest(mainRule.testDispatcher) {
            gateway.serviceState.value = snapshot(state = STATE_IDLE, permissionNeeded = true)
            permissions.granted = false
            val viewModel = viewModel()
            runCurrent()
            val events = collectEvents(viewModel)
            runCurrent()

            assertEquals(DashboardPhase.PERMISSION_NEEDED, viewModel.state.value.phase)
            viewModel.onPower()
            runCurrent()

            assertEquals(listOf(DashboardEvent.RequestVpnPermission), events)
            assertTrue(gateway.requests.isEmpty())
        }

    @Test
    fun `a consent result while a session is up never reconnects`() =
        runTest(mainRule.testDispatcher) {
            permissions.granted = false
            gateway.serviceState.value = connected()
            val viewModel = viewModel()
            runCurrent()

            viewModel.onVpnConsentResult(true)
            runCurrent()

            assertTrue("a live session needs a disconnect first", gateway.requests.isEmpty())
        }

    @Test
    fun `signed out and unavailable phases explain instead of connecting`() =
        runTest(mainRule.testDispatcher) {
            gateway.serviceState.value = snapshot(state = STATE_IDLE, authenticated = false)
            val signedOut = viewModel()
            runCurrent()
            assertEquals(DashboardPhase.NOT_SIGNED_IN, signedOut.state.value.phase)
            signedOut.onPower()
            runCurrent()
            assertEquals(
                NoticeText.Local(DashboardMessage.SIGN_IN_REQUIRED),
                signedOut.state.value.notice?.text,
            )

            gateway.serviceState.value = snapshot(state = STATE_IDLE, binding = false)
            val unavailable = viewModel()
            runCurrent()
            assertEquals(DashboardPhase.SERVICE_UNAVAILABLE, unavailable.state.value.phase)
            unavailable.onPower()
            runCurrent()
            assertEquals(
                NoticeText.Local(DashboardMessage.SERVICE_UNAVAILABLE),
                unavailable.state.value.notice?.text,
            )
            assertTrue(gateway.requests.isEmpty())
        }

    // ---- notices ---------------------------------------------------------

    @Test
    fun `engine notices are shown in the engine's own words`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()

            gateway.emitNotice(Notice("fatal", STATE_IDLE, "edge handshake failed"))
            runCurrent()

            val notice = viewModel.state.value.notice
            assertEquals(NoticeText.Raw("edge handshake failed"), notice?.text)
            assertEquals(DashboardNoticeTone.ERROR, notice?.tone)
            assertEquals(DashboardNoticeAction.RETRY_CONNECT, notice?.action)

            viewModel.dismissNotice()
            assertNull(viewModel.state.value.notice)
        }

    @Test
    fun `a dismissed service error stays dismissed until a new one`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()

            gateway.serviceState.value = snapshot(state = STATE_ERROR, error = "the engine failed")
            runCurrent()
            assertEquals(NoticeText.Raw("the engine failed"), viewModel.state.value.notice?.text)

            viewModel.dismissNotice()
            assertNull(viewModel.state.value.notice)

            gateway.serviceState.value = snapshot(state = STATE_ERROR, error = "the engine failed")
                .copy(alwaysOn = true)
            runCurrent()
            assertNull("the same error must not resurrect a dismissed notice", viewModel.state.value.notice)

            gateway.serviceState.value = snapshot(state = STATE_ERROR, error = "the disk is full")
            runCurrent()
            assertEquals(NoticeText.Raw("the disk is full"), viewModel.state.value.notice?.text)
        }

    @Test
    fun `a failed refresh keeps the last status and offers one retry`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()
            val before = viewModel.state.value.status

            gateway.refreshError = IllegalStateException("the engine did not answer")
            viewModel.onResume()
            runCurrent()

            assertEquals(before, viewModel.state.value.status)
            val notice = viewModel.state.value.notice
            assertEquals(
                NoticeText.Local(DashboardMessage.STATUS_REFRESH_FAILED, "the engine did not answer"),
                notice?.text,
            )
            assertEquals(DashboardNoticeAction.RETRY_REFRESH, notice?.action)

            gateway.refreshError = null
            viewModel.retryNotice()
            runCurrent()

            assertEquals(2, gateway.refreshCalls)
            assertNull("a successful retry clears its own notice", viewModel.state.value.notice)
        }

    @Test
    fun `a retried connect goes out exactly once`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()
            gateway.serviceState.value = snapshot(state = STATE_ERROR, error = "the edge refused")
            runCurrent()
            assertEquals(DashboardNoticeAction.RETRY_CONNECT, viewModel.state.value.notice?.action)

            gateway.serviceState.value = snapshot(state = STATE_IDLE)
            runCurrent()
            viewModel.retryNotice()
            runCurrent()

            assertEquals(1, gateway.requests.size)
            assertNull(viewModel.state.value.notice)
        }

    @Test
    fun `Android VPN settings are a one-shot event with an honest failure`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()
            val events = collectEvents(viewModel)
            runCurrent()

            viewModel.openVpnSettings()
            runCurrent()
            assertEquals(listOf(DashboardEvent.OpenVpnSettings), events)

            viewModel.onSettingsUnavailable()
            assertEquals(
                NoticeText.Local(DashboardMessage.SETTINGS_UNAVAILABLE),
                viewModel.state.value.notice?.text,
            )
        }

    // ---- options sheet ---------------------------------------------------

    @Test
    fun `the sheet opens on Recommended and a picked city is the request`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()

            viewModel.openOptions()
            runCurrent()
            assertTrue(viewModel.state.value.options.open)
            assertEquals(LocationChoice.Recommended, viewModel.state.value.options.choice)
            assertTrue("the sheet re-reads Android policy", gateway.policyRefreshes > 0)

            viewModel.selectCity("us", "nyc")
            viewModel.setProxyOnly(true)
            viewModel.connectFromOptions()
            runCurrent()

            assertEquals(
                listOf(ConnectRequest(locationCode = "us", cityCode = "nyc", proxyOnly = true)),
                gateway.requests,
            )
            assertFalse("a submitted connect closes the sheet", viewModel.state.value.options.open)
        }

    @Test
    fun `a denied consent from the sheet closes it so the notice is visible`() =
        runTest(mainRule.testDispatcher) {
            permissions.granted = false
            val viewModel = viewModel()
            runCurrent()

            viewModel.openOptions()
            runCurrent()
            assertTrue(viewModel.state.value.options.open)

            viewModel.connectFromOptions()
            runCurrent()
            assertTrue("still waiting for consent", viewModel.state.value.options.open)
            assertTrue(gateway.requests.isEmpty())

            viewModel.onVpnConsentResult(false)
            runCurrent()

            assertFalse(
                "the denial must not hide behind the sheet's scrim",
                viewModel.state.value.options.open,
            )
            val notice = viewModel.state.value.notice
            assertEquals(
                NoticeText.Local(DashboardMessage.VPN_PERMISSION_DENIED),
                notice?.text,
            )
            assertEquals(DashboardNoticeAction.GRANT_PERMISSION, notice?.action)
            assertTrue(gateway.requests.isEmpty())
        }

    @Test
    fun `a saved selection sends no location fields`() =
        runTest(mainRule.testDispatcher) {
            gateway.serviceState.value = snapshot(state = STATE_IDLE, location = "us")
            val viewModel = viewModel()
            runCurrent()

            viewModel.openOptions()
            runCurrent()
            assertEquals(LocationChoice.Saved, viewModel.state.value.options.choice)

            viewModel.connectFromOptions()
            runCurrent()

            assertEquals(
                "Saved must let the engine keep its persisted selection",
                listOf(ConnectRequest()),
                gateway.requests,
            )
        }

    @Test
    fun `Saved is not offered without a persisted selection`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()
            viewModel.openOptions()
            runCurrent()

            viewModel.selectSaved()
            assertEquals(LocationChoice.Recommended, viewModel.state.value.options.choice)
        }

    @Test
    fun `the sheet refuses a connect while a session is active`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()
            viewModel.openOptions()
            runCurrent()

            gateway.serviceState.value = connected()
            runCurrent()

            assertEquals(SheetHint.DISCONNECT_FIRST, viewModel.state.value.sheetHint)
            assertFalse(viewModel.state.value.optionsConnectEnabled)
            viewModel.connectFromOptions()
            runCurrent()
            assertTrue("T12: a location change is a disconnect first", gateway.requests.isEmpty())
        }

    @Test
    fun `a country expands and collapses without a request`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()
            viewModel.openOptions()
            runCurrent()

            viewModel.toggleCountry("us")
            assertTrue("us" in viewModel.state.value.options.expanded)
            viewModel.toggleCountry("us")
            assertFalse("us" in viewModel.state.value.options.expanded)
            assertTrue(gateway.requests.isEmpty())
        }

    @Test
    fun `the proxy-only switch survives until the next connect`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()
            viewModel.openOptions()
            runCurrent()

            viewModel.setProxyOnly(true)
            assertTrue(viewModel.state.value.options.proxyOnly)
            assertEquals(
                ConnectRequest(locationCode = "rec", proxyOnly = true),
                viewModel.state.value.connectRequest(),
            )
            viewModel.setProxyOnly(false)
            assertFalse(viewModel.state.value.connectRequest().proxyOnly)
        }

    // ---- locations -------------------------------------------------------

    @Test
    fun `a failed tree load is honest and reloads on retry`() =
        runTest(mainRule.testDispatcher) {
            gateway.locationResult = { throw IllegalStateException("the tree did not load") }
            val viewModel = viewModel()
            runCurrent()

            assertEquals(
                LocationLoad.Failed("the tree did not load"),
                viewModel.state.value.locations,
            )
            val notice = viewModel.state.value.notice
            assertEquals(DashboardNoticeAction.RELOAD_LOCATIONS, notice?.action)
            assertEquals(
                NoticeText.Local(DashboardMessage.LOCATIONS_FAILED, "the tree did not load"),
                notice?.text,
            )

            gateway.locationResult = { COUNTRIES }
            viewModel.retryNotice()
            runCurrent()

            assertEquals(LocationLoad.Ready(COUNTRIES), viewModel.state.value.locations)
            assertNull(viewModel.state.value.notice)
        }

    @Test
    fun `a reload failure keeps a tree that already worked`() =
        runTest(mainRule.testDispatcher) {
            gateway.locationResult = { COUNTRIES }
            val viewModel = viewModel()
            runCurrent()

            gateway.locationResult = { throw IllegalStateException("no answer") }
            viewModel.loadLocations()
            runCurrent()

            assertEquals(
                "a failed refresh must not throw away a good tree",
                LocationLoad.Ready(COUNTRIES),
                viewModel.state.value.locations,
            )
            assertEquals(
                DashboardNoticeAction.RELOAD_LOCATIONS,
                viewModel.state.value.notice?.action,
            )
        }

    // ---- helpers ---------------------------------------------------------

    private fun TestScope.collectEvents(
        viewModel: DashboardViewModel,
    ): MutableList<DashboardEvent> {
        val events = mutableListOf<DashboardEvent>()
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) {
            viewModel.events.collect { events += it }
        }
        return events
    }

    private fun status(
        state: String,
        authenticated: Boolean,
        location: String?,
        server: String?,
        exitIp: String?,
    ) = EngineStatus(
        state = state,
        authenticated = authenticated,
        email = if (authenticated) EMAIL else null,
        since = 0L,
        tokenExpires = 0L,
        passExpires = 0L,
        killSwitch = true,
        autoconnect = true,
        location = location,
        server = server,
        proxyOnly = state == STATE_PROXY_ONLY,
        quota = Quota(limit = null, remaining = null, reset = null),
        dns = null,
        ipv6 = null,
        exitIp = exitIp,
        exitCountry = null,
        totals = null,
        rates = null,
    )

    private fun snapshot(
        state: String,
        authenticated: Boolean = true,
        binding: Boolean = true,
        error: String? = null,
        permissionNeeded: Boolean = false,
        location: String? = null,
        server: String? = null,
        exitIp: String? = null,
        lockdown: Boolean = false,
    ) = ServiceState(
        binding = binding,
        engineReady = binding,
        status = status(state, authenticated, location, server, exitIp),
        alwaysOn = false,
        lockdown = lockdown,
        error = error,
        permissionNeeded = permissionNeeded,
    )

    private fun connected(lockdown: Boolean = false) = snapshot(
        state = STATE_CONNECTED,
        location = "us",
        server = "edge.example.net:443",
        exitIp = "203.0.113.7",
        lockdown = lockdown,
    )

    private companion object {
        const val EMAIL = "user@example.com"
        const val STATE_IDLE = "IDLE"
        const val STATE_CONNECTED = "CONNECTED"
        const val STATE_CONNECTING = "CONNECTING"
        const val STATE_PROXY_ONLY = "PROXY_ONLY"
        const val STATE_ERROR = "FATAL"
        const val DEFAULT_TIMEOUT = 130_000L

        val COUNTRIES = listOf(
            Country(
                code = "us",
                name = "United States",
                cities = listOf(City(code = "nyc", name = "New York", servers = emptyList())),
            ),
            Country(
                code = "de",
                name = "Germany",
                cities = listOf(City(code = "ber", name = "Berlin", servers = emptyList())),
            ),
        )
    }
}

/** A fake gateway: the service mirror is the only authority, every call counted. */
private class FakeGateway(initial: ServiceState) : DashboardGateway {
    /** Tests drive the service through this, as the real coordinator does. */
    val serviceState = MutableStateFlow(initial)

    override val service: StateFlow<ServiceState> = serviceState

    private val noticeSource = MutableSharedFlow<Notice>(extraBufferCapacity = 8)

    override val notices: Flow<Notice> = noticeSource

    var locationResult: () -> List<Country> = { emptyList() }
    var locationCalls = 0
    var refreshCalls = 0
    var refreshError: Exception? = null
    var connectError: RuntimeException? = null
    var disconnectError: RuntimeException? = null
    val requests = mutableListOf<ConnectRequest>()
    var disconnectCalls = 0
    var policyRefreshes = 0

    suspend fun emitNotice(notice: Notice) {
        noticeSource.emit(notice)
    }

    override suspend fun refreshStatus() {
        refreshCalls++
        refreshError?.let { throw it }
    }

    override suspend fun locations(): List<Country> {
        locationCalls++
        return locationResult()
    }

    override fun connect(request: ConnectRequest) {
        connectError?.let { throw it }
        requests += request
    }

    override fun disconnect() {
        disconnectError?.let { throw it }
        disconnectCalls++
    }

    override fun refreshPolicy() {
        policyRefreshes++
    }
}

/** The VPN consent fact, set to whatever the test needs. */
private class FakePermissions(var granted: Boolean = true) : DashboardPermissions {
    override fun vpnConsentGranted(): Boolean = granted
}
