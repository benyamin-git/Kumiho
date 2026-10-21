package dev.benyamin.kumiho.ui.locations

import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.engine.PingResult
import dev.benyamin.kumiho.engine.Protocol
import dev.benyamin.kumiho.engine.Quota
import dev.benyamin.kumiho.engine.Server
import dev.benyamin.kumiho.test.MainDispatcherRule
import dev.benyamin.kumiho.vpn.ServiceState
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * The Locations state machine on the JVM: a fake gateway, virtual time and no
 * Android, Compose or daemon.
 *
 * The contract this file pins is deliberately narrow: loading and refreshing,
 * the live filter and expansion, `select_location` exactly once per choice and
 * never a connect, the current selection following the service's status push
 * instead of a local guess, and a speed test that only ever runs when asked.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class LocationsViewModelTest {

    @get:Rule
    val mainRule = MainDispatcherRule(StandardTestDispatcher())

    private val gateway = FakeGateway()

    private fun viewModel(): LocationsViewModel =
        LocationsViewModel(gateway = gateway, pingWindowMillis = WINDOW)

    // ---- loading ---------------------------------------------------------

    @Test
    fun `the first load puts REC first and shows the tree`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(US, REC, DE)

            val viewModel = viewModel()
            assertTrue("before the load resolves there is only a waiting state", viewModel.state.value.showLoading)
            runCurrent()

            val state = viewModel.state.value
            assertEquals(listOf("rec", "us", "de"), state.countries.map { it.code })
            assertEquals(1, gateway.locationCalls)
            assertEquals(listOf(false), gateway.refreshFlags)
            assertTrue(state.loaded)
            assertFalse(state.loading)
            assertFalse(state.showLoading)
            assertFalse(state.showError)
            assertFalse(state.showEmpty)
        }

    @Test
    fun `an initial load failure is an error state whose retry loads again`() =
        runTest(mainRule.testDispatcher) {
            gateway.locationsError = IllegalStateException("daemon offline")

            val viewModel = viewModel()
            runCurrent()

            assertTrue(viewModel.state.value.showError)
            assertEquals("daemon offline", viewModel.state.value.loadError)
            assertFalse(viewModel.state.value.showLoading)

            gateway.locationsError = null
            gateway.countries = listOf(REC, US)
            viewModel.retryLoad()
            runCurrent()

            val state = viewModel.state.value
            assertFalse(state.showError)
            assertNull(state.loadError)
            assertEquals(listOf("rec", "us"), state.countries.map { it.code })
            assertEquals(2, gateway.locationCalls)
        }

    @Test
    fun `a refresh failure keeps the tree and offers a retry that refreshes`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US, DE)
            val viewModel = viewModel()
            runCurrent()

            gateway.locationsError = IllegalStateException("list request failed")
            viewModel.refresh()
            runCurrent()

            val failed = viewModel.state.value
            assertEquals("a failed refresh must not blank the tree", 3, failed.countries.size)
            assertTrue(failed.hasTree)
            assertFalse(failed.refreshing)
            assertEquals(LocationsNoticeAction.RETRY_REFRESH, failed.notice?.action)
            assertEquals(
                LocationsNoticeText.Local(LocationsMessage.REFRESH_FAILED, "list request failed"),
                failed.notice?.text,
            )

            gateway.locationsError = null
            viewModel.retryNotice()
            runCurrent()

            val retried = viewModel.state.value
            assertNull("a successful refresh clears its own notice", retried.notice)
            assertEquals(3, retried.countries.size)
            assertEquals(listOf(false, true, true), gateway.refreshFlags)
        }

    @Test
    fun `a successful empty load is an honest empty state not an error`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = emptyList()

            val viewModel = viewModel()
            runCurrent()

            val state = viewModel.state.value
            assertTrue(state.showEmpty)
            assertFalse(state.showError)
            assertFalse(state.hasTree)
        }

    // ---- selection -------------------------------------------------------

    @Test
    fun `the current selection comes from the service status and marks its row`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US, DE)
            val viewModel = viewModel()
            runCurrent()

            assertNull("nothing is selected before the engine says so", viewModel.state.value.selection)

            gateway.serviceState.value = snapshot(state = STATE_CONNECTED, location = "us", server = EDGE)
            runCurrent()

            val state = viewModel.state.value
            val selection = state.selection
            requireNotNull(selection)
            assertEquals("United States", selection.countryName)
            assertEquals("New York", selection.cityName)
            assertEquals("edge.example.net", selection.serverName)
            assertFalse(selection.recommended)
            assertTrue(state.isSelected(US, NYC, EDGE_SERVER))
            assertFalse(state.isSelected(US, NYC, SHARE_SERVER))
            assertTrue(state.sessionActive)
        }

    @Test
    fun `no status at all means no selection`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US)
            val viewModel = viewModel()
            runCurrent()

            assertNull(viewModel.state.value.selection)
            assertFalse(viewModel.state.value.sessionActive)
        }

    @Test
    fun `saving sends the exact country city and address and never marks early`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US, DE)
            val viewModel = viewModel()
            runCurrent()

            viewModel.select(US, NYC, EDGE_SERVER)
            runCurrent()

            assertEquals(
                listOf(SelectionRequest(countryCode = "us", cityCode = "nyc", address = EDGE)),
                gateway.selections,
            )
            assertNull("the row must not move before the engine reports it", viewModel.state.value.selection)
            assertFalse(viewModel.state.value.isSelected(US, NYC, EDGE_SERVER))

            gateway.serviceState.value = snapshot(state = STATE_IDLE, location = "us", server = EDGE)
            runCurrent()

            assertTrue(viewModel.state.value.isSelected(US, NYC, EDGE_SERVER))
            assertNull(viewModel.state.value.savingServer)
            assertEquals("a save is never a connect", 1, gateway.selections.size)
            assertEquals(0, gateway.pingCalls.size)
        }

    @Test
    fun `a quarantined server cannot be selected`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US, DE)
            val viewModel = viewModel()
            runCurrent()

            viewModel.select(DE, BER, QUARANTINED_SERVER)
            runCurrent()

            assertTrue(gateway.selections.isEmpty())
            assertNull(viewModel.state.value.savingServer)
        }

    @Test
    fun `only one save runs at a time`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US)
            val viewModel = viewModel()
            runCurrent()

            val gate = CompletableDeferred<Unit>()
            gateway.selectGate = gate
            viewModel.select(US, NYC, EDGE_SERVER)
            runCurrent()
            assertEquals(LocationTree.addressKey(EDGE), viewModel.state.value.savingServer)

            viewModel.select(US, NYC, SHARE_SERVER)
            runCurrent()
            assertEquals("a second tap while saving is ignored", 1, gateway.selections.size)

            gate.complete(Unit)
            runCurrent()
            assertNull(viewModel.state.value.savingServer)
            assertEquals(1, gateway.selections.size)
        }

    @Test
    fun `a failed save offers a retry that repeats the same request`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US)
            gateway.selectError = IllegalStateException("write failed")
            val viewModel = viewModel()
            runCurrent()

            viewModel.select(US, NYC, EDGE_SERVER)
            runCurrent()

            val notice = viewModel.state.value.notice
            requireNotNull(notice)
            assertEquals(LocationsNoticeAction.RETRY_SELECT, notice.action)
            assertEquals(LocationsNoticeTone.ERROR, notice.tone)
            assertEquals(SelectionRequest("us", "nyc", EDGE), notice.request)
            assertNull(viewModel.state.value.savingServer)

            gateway.selectError = null
            viewModel.retryNotice()
            runCurrent()

            assertEquals(2, gateway.selections.size)
            assertEquals(notice.request, gateway.selections.last())
            assertNull(viewModel.state.value.notice)
        }

    @Test
    fun `a live session still allows a save and reports the hint`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US, DE)
            val viewModel = viewModel()
            runCurrent()

            gateway.serviceState.value = snapshot(state = STATE_CONNECTED, location = "us", server = EDGE)
            runCurrent()
            assertTrue(viewModel.state.value.sessionActive)

            viewModel.select(US, NYC, SHARE_SERVER)
            runCurrent()

            assertEquals(
                listOf(SelectionRequest("us", "nyc", "share.example.net:443")),
                gateway.selections,
            )
            assertEquals("the hint never blocks the choice", 1, gateway.selections.size)
        }

    // ---- filter and expansion -------------------------------------------

    @Test
    fun `expanding and collapsing a country and a city drives the visible rows`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US, DE)
            val viewModel = viewModel()
            runCurrent()

            viewModel.toggleCountry("US")
            runCurrent()
            assertEquals(
                listOf("country:rec", "country:us", "city:us/nyc", "city:us/sfo", "country:de"),
                viewModel.state.value.rows.map { it.key },
            )

            viewModel.toggleCity("us", "NYC")
            runCurrent()
            assertEquals(
                listOf(
                    "country:rec",
                    "country:us",
                    "city:us/nyc",
                    "server:us/nyc/edge.example.net:2499",
                    "server:us/nyc/share.example.net:443",
                    "city:us/sfo",
                    "country:de",
                ),
                viewModel.state.value.rows.map { it.key },
            )

            viewModel.toggleCity("us", "nyc")
            viewModel.toggleCountry("us")
            runCurrent()
            assertEquals(3, viewModel.state.value.rows.size)
        }

    @Test
    fun `a query narrows the tree and clearing it restores the expansion`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US, DE)
            val viewModel = viewModel()
            runCurrent()
            viewModel.toggleCountry("us")
            viewModel.toggleCity("us", "nyc")
            runCurrent()
            val before = viewModel.state.value.rows.map { it.key }

            viewModel.onQueryChange("germany")
            runCurrent()

            val filtered = viewModel.state.value
            assertTrue(filtered.searching)
            assertEquals("country:de", filtered.rows.first().key)
            assertTrue("matching branches open without a tap", filtered.rows.any { it is LocationRow.CityRow })
            assertEquals(
                "only the matching branch is probed",
                listOf("ber.example.net:443"),
                filtered.testTargets,
            )

            viewModel.clearQuery()
            runCurrent()

            assertEquals(before, viewModel.state.value.rows.map { it.key })
        }

    @Test
    fun `a query with no match is a no-match state and not an empty tree`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US)
            val viewModel = viewModel()
            runCurrent()

            viewModel.onQueryChange("zzz")
            runCurrent()

            val state = viewModel.state.value
            assertTrue(state.showNoMatch)
            assertFalse(state.showEmpty)
            assertTrue(state.rows.isEmpty())
            assertTrue(state.testTargets.isEmpty())
        }

    // ---- speed test ------------------------------------------------------

    @Test
    fun `ping targets are the visible servers deduped and skip quarantined ones`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US, DE)
            val viewModel = viewModel()
            runCurrent()
            viewModel.toggleCountry("us")
            viewModel.toggleCity("us", "nyc")
            viewModel.toggleCity("us", "sfo")
            runCurrent()

            assertEquals(
                listOf(EDGE, "share.example.net:443", "west.example.net:443"),
                viewModel.state.value.testTargets,
            )
            assertTrue(viewModel.state.value.canTest)

            viewModel.onQueryChange("germany")
            runCurrent()
            assertEquals(
                "the quarantined server is never probed",
                listOf("ber.example.net:443"),
                viewModel.state.value.testTargets,
            )
        }

    @Test
    fun `nothing pings until Test speed is tapped`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US, DE)
            val viewModel = viewModel()
            runCurrent()
            viewModel.toggleCountry("us")
            viewModel.toggleCity("us", "nyc")
            runCurrent()
            viewModel.onQueryChange("edge")
            runCurrent()
            viewModel.clearQuery()
            viewModel.select(US, NYC, EDGE_SERVER)
            viewModel.refresh()
            runCurrent()

            assertTrue(gateway.pingCalls.isEmpty())
            assertFalse(viewModel.state.value.testing)
            assertTrue(viewModel.state.value.latency.isEmpty())
        }

    @Test
    fun `Test speed sends one ping command for the visible targets`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US, DE)
            val viewModel = viewModel()
            runCurrent()
            viewModel.toggleCountry("us")
            viewModel.toggleCity("us", "nyc")
            runCurrent()

            viewModel.testSpeed()
            runCurrent()

            assertEquals(listOf(listOf(EDGE, "share.example.net:443")), gateway.pingCalls)
            assertTrue(viewModel.state.value.testing)
            assertFalse(viewModel.state.value.canTest)

            viewModel.testSpeed()
            runCurrent()
            assertEquals("a second tap while testing is ignored", 1, gateway.pingCalls.size)
        }

    @Test
    fun `a null round trip is a timeout and distinct from an untested host`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US, DE)
            val viewModel = viewModel()
            runCurrent()
            viewModel.toggleCountry("us")
            viewModel.toggleCity("us", "nyc")
            runCurrent()
            assertEquals(LatencyState.Untested, viewModel.state.value.latencyOf(EDGE_SERVER))

            viewModel.testSpeed()
            runCurrent()
            gateway.emitPing(EDGE, rttMs = null)
            runCurrent()

            assertEquals(LatencyState.Timeout, viewModel.state.value.latencyOf(EDGE_SERVER))
            assertEquals(
                "a host that has not answered is still untested",
                LatencyState.Untested,
                viewModel.state.value.latencyOf(SHARE_SERVER),
            )
        }

    @Test
    fun `testing ends when every target has answered`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US, DE)
            val viewModel = viewModel()
            runCurrent()
            viewModel.toggleCountry("us")
            viewModel.toggleCity("us", "nyc")
            runCurrent()
            viewModel.testSpeed()
            runCurrent()

            gateway.emitPing(EDGE, rttMs = 42.4)
            runCurrent()
            assertTrue("one host is still pending", viewModel.state.value.testing)

            gateway.emitPing(SHARE.uppercase(), rttMs = 9.0)
            runCurrent()

            assertFalse(viewModel.state.value.testing)
            assertEquals(LatencyState.Measured(42), viewModel.state.value.latencyOf(EDGE_SERVER))
            assertEquals(LatencyState.Measured(9), viewModel.state.value.latencyOf(SHARE_SERVER))
        }

    @Test
    fun `testing ends after the ping window even without answers`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US)
            val viewModel = viewModel()
            runCurrent()
            viewModel.toggleCountry("us")
            viewModel.toggleCity("us", "nyc")
            viewModel.toggleCity("us", "sfo")
            runCurrent()
            viewModel.testSpeed()
            runCurrent()
            assertTrue(viewModel.state.value.testing)

            advanceTimeBy(WINDOW + 1)
            runCurrent()

            assertFalse(viewModel.state.value.testing)
            assertTrue(
                "an unanswered host stays untested, not a timeout",
                viewModel.state.value.latency.isEmpty(),
            )
            assertTrue(viewModel.state.value.canTest)
        }

    @Test
    fun `a new test replaces the hosts it probed and keeps the others`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US, DE)
            val viewModel = viewModel()
            runCurrent()
            viewModel.toggleCountry("us")
            viewModel.toggleCity("us", "nyc")
            viewModel.toggleCity("us", "sfo")
            runCurrent()
            viewModel.testSpeed()
            runCurrent()
            gateway.emitPing(EDGE, rttMs = 42.0)
            gateway.emitPing(SHARE, rttMs = null)
            gateway.emitPing("west.example.net:443", rttMs = 88.0)
            runCurrent()
            assertFalse(viewModel.state.value.testing)

            // Narrow the view to one city and test again; only its hosts are probed.
            viewModel.toggleCity("us", "sfo")
            runCurrent()
            viewModel.testSpeed()
            runCurrent()
            assertEquals(listOf(listOf(EDGE, SHARE)), gateway.pingCalls.drop(1))
            assertEquals(2, gateway.pingCalls.size)
            gateway.emitPing(EDGE, rttMs = 7.0)
            gateway.emitPing(SHARE, rttMs = 12.0)
            runCurrent()

            val latency = viewModel.state.value.latency
            assertEquals(LatencyState.Measured(7), viewModel.state.value.latencyOf(EDGE_SERVER))
            assertEquals(LatencyState.Measured(12), viewModel.state.value.latencyOf(SHARE_SERVER))
            assertEquals(
                "a host outside this run keeps its last measurement",
                LatencyState.Measured(88),
                viewModel.state.value.latencyOf(WEST_SERVER),
            )
            assertEquals(3, latency.size)
        }

    @Test
    fun `a failed ping command stops testing and offers a retry`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US)
            val viewModel = viewModel()
            runCurrent()
            viewModel.toggleCountry("us")
            viewModel.toggleCity("us", "nyc")
            runCurrent()

            gateway.pingError = IllegalStateException("ping unavailable")
            viewModel.testSpeed()
            runCurrent()

            val notice = viewModel.state.value.notice
            requireNotNull(notice)
            assertEquals(LocationsNoticeAction.RETRY_TEST, notice.action)
            assertEquals(LocationsNoticeTone.WARNING, notice.tone)
            assertFalse(viewModel.state.value.testing)

            gateway.pingError = null
            viewModel.retryNotice()
            runCurrent()

            assertEquals(2, gateway.pingCalls.size)
            assertNull(viewModel.state.value.notice)
            assertTrue(viewModel.state.value.testing)

            advanceTimeBy(WINDOW + 1)
            runCurrent()
            assertFalse(viewModel.state.value.testing)
        }

    @Test
    fun `Test speed with nothing visible is a no-op`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US)
            val viewModel = viewModel()
            runCurrent()
            viewModel.onQueryChange("zzz")
            runCurrent()

            viewModel.testSpeed()
            runCurrent()

            assertTrue(gateway.pingCalls.isEmpty())
            assertFalse(viewModel.state.value.testing)
            assertFalse(viewModel.state.value.canTest)
        }

    // ---- notices and reload ---------------------------------------------

    @Test
    fun `a notice can be dismissed`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US)
            gateway.selectError = IllegalStateException("nope")
            val viewModel = viewModel()
            runCurrent()
            viewModel.select(US, NYC, EDGE_SERVER)
            runCurrent()
            assertNotNull(viewModel.state.value.notice)

            viewModel.dismissNotice()
            runCurrent()

            assertNull(viewModel.state.value.notice)
        }

    @Test
    fun `a reload re-resolves the selected city against the new tree`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(REC, US)
            gateway.serviceState.value = snapshot(state = STATE_IDLE, location = "us", server = EDGE)
            val viewModel = viewModel()
            runCurrent()
            assertEquals("New York", viewModel.state.value.selection?.cityName)

            gateway.countries = listOf(
                REC,
                US.copy(cities = listOf(NYC.copy(name = "New York Metro"), SFO)),
            )
            viewModel.refresh()
            runCurrent()

            assertEquals("New York Metro", viewModel.state.value.selection?.cityName)
            assertTrue(viewModel.state.value.isSelected(US, NYC, EDGE_SERVER))
        }

    @Test
    fun `a recommended selection is presented as recommended`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(US, REC)
            gateway.serviceState.value = snapshot(state = STATE_CONNECTED, location = "REC", server = null)
            val viewModel = viewModel()
            runCurrent()

            val selection = viewModel.state.value.selection
            requireNotNull(selection)
            assertTrue(selection.recommended)
            assertEquals("rec", selection.country?.code)
            assertEquals(listOf("rec", "us"), viewModel.state.value.countries.map { it.code })
        }

    private companion object {
        const val WINDOW = 5_000L
        const val STATE_IDLE = "IDLE"
        const val STATE_CONNECTED = "CONNECTED"
        const val EMAIL = "user@example.com"
        const val EDGE = "edge.example.net:2499"
        const val SHARE = "share.example.net:443"

        fun server(host: String, port: Int = 443, quarantined: Boolean = false) = Server(
            hostname = host,
            port = port,
            quarantined = quarantined,
            protocols = listOf(Protocol(name = "connect", host = host, port = port, scheme = "https")),
        )

        val EDGE_SERVER = server("edge.example.net", 2499)
        val SHARE_SERVER = server("share.example.net")
        val WEST_SERVER = server("west.example.net")
        val QUARANTINED_SERVER = server("quarantined.example.net", quarantined = true)

        val REC = Country(
            code = "rec",
            name = "Recommended Location",
            cities = listOf(
                City(code = "rec", name = "Recommended", servers = listOf(server("auto.example.net"))),
            ),
        )

        val NYC = City(
            code = "nyc",
            name = "New York",
            servers = listOf(EDGE_SERVER, SHARE_SERVER),
        )

        val SFO = City(
            code = "sfo",
            name = "San Francisco",
            servers = listOf(SHARE_SERVER, WEST_SERVER),
        )

        val BER = City(
            code = "ber",
            name = "Berlin",
            servers = listOf(server("ber.example.net"), QUARANTINED_SERVER),
        )

        val US = Country(code = "us", name = "United States", cities = listOf(NYC, SFO))

        val DE = Country(code = "de", name = "Germany", cities = listOf(BER))

        fun snapshot(state: String, location: String? = null, server: String? = null) = ServiceState(
            binding = true,
            engineReady = true,
            status = status(state = state, location = location, server = server),
        )

        fun status(state: String, location: String?, server: String?) = EngineStatus(
            state = state,
            authenticated = true,
            email = EMAIL,
            since = 0L,
            tokenExpires = 0L,
            passExpires = 0L,
            killSwitch = true,
            autoconnect = true,
            location = location,
            server = server,
            proxyOnly = false,
            quota = Quota(limit = null, remaining = null, reset = null),
            dns = null,
            ipv6 = null,
            exitIp = null,
            exitCountry = null,
            totals = null,
            rates = null,
        )
    }
}

/**
 * The fake gateway: one mutable service mirror, a recorded ping command, and a
 * shared results flow the test drives. It has no connect method at all, which
 * is the T12 contract expressed in types.
 */
private class FakeGateway : LocationsGateway {
    val serviceState = MutableStateFlow(ServiceState())

    override val service: StateFlow<ServiceState> = serviceState

    private val pingSource = MutableSharedFlow<PingResult>(extraBufferCapacity = 64)

    override val pings: Flow<PingResult> = pingSource

    var countries: List<Country> = emptyList()
    var locationsError: Exception? = null
    var selectError: Exception? = null
    var selectGate: CompletableDeferred<Unit>? = null
    var pingError: Exception? = null

    var locationCalls = 0
    val refreshFlags = mutableListOf<Boolean>()
    val selections = mutableListOf<SelectionRequest>()
    val pingCalls = mutableListOf<List<String>>()

    override suspend fun locations(refresh: Boolean): List<Country> {
        locationCalls++
        refreshFlags += refresh
        locationsError?.let { throw it }
        return countries
    }

    override suspend fun selectLocation(countryCode: String, cityCode: String?, server: String) {
        selections += SelectionRequest(countryCode = countryCode, cityCode = cityCode, address = server)
        selectGate?.await()
        selectError?.let { throw it }
    }

    override suspend fun ping(hosts: List<String>) {
        pingCalls += hosts
        pingError?.let { throw it }
    }

    suspend fun emitPing(host: String, rttMs: Double?) {
        pingSource.emit(PingResult(host = host, rttMs = rttMs))
    }
}
