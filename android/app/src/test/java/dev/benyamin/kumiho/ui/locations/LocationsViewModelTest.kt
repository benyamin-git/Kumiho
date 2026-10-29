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
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * The Locations state machine on the JVM: the flat list, filter, selection,
 * persistence, the all-servers ping run and the transient speed order.
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
    fun `the first load puts Recommended first in the flat rows`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(US, REC, DE)

            val viewModel = viewModel()
            assertTrue(viewModel.state.value.showLoading)
            runCurrent()

            val state = viewModel.state.value
            assertEquals(
                listOf("rec", "de", "us", "us", "us"),
                state.rows.map { it.country.code },
            )
            assertEquals(1, gateway.locationCalls)
            assertEquals(listOf(false), gateway.refreshFlags)
            assertTrue(state.loaded)
            assertFalse(state.loading)
            assertFalse(state.showLoading)
            assertFalse(state.showError)
            assertFalse(state.showEmpty)
        }

    @Test
    fun `an initial load failure is an error whose retry loads again`() =
        runTest(mainRule.testDispatcher) {
            gateway.locationsError = IllegalStateException("daemon offline")

            val viewModel = viewModel()
            runCurrent()

            assertTrue(viewModel.state.value.showError)
            assertEquals("daemon offline", viewModel.state.value.loadError)

            gateway.locationsError = null
            gateway.countries = listOf(REC, US)
            viewModel.retryLoad()
            runCurrent()

            assertFalse(viewModel.state.value.showError)
            assertNull(viewModel.state.value.loadError)
            assertEquals(listOf("rec", "us", "us", "us"), viewModel.state.value.rows.map { it.country.code })
            assertEquals(2, gateway.locationCalls)
        }

    @Test
    fun `a refresh failure keeps the rows and offers the existing retry`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(US, REC, DE)
            val viewModel = viewModel()
            runCurrent()

            gateway.locationsError = IllegalStateException("list request failed")
            viewModel.refresh()
            runCurrent()

            val failed = viewModel.state.value
            assertEquals(5, failed.rows.size)
            assertFalse(failed.refreshing)
            assertEquals(LocationsNoticeAction.RETRY_REFRESH, failed.notice?.action)
            assertEquals(
                LocationsNoticeText.Local(LocationsMessage.REFRESH_FAILED, "list request failed"),
                failed.notice?.text,
            )

            gateway.locationsError = null
            viewModel.retryNotice()
            runCurrent()
            assertEquals(3, gateway.locationCalls)
            assertNull(viewModel.state.value.notice)
        }

    @Test
    fun `a successful empty load is honest`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()

            assertTrue(viewModel.state.value.loaded)
            assertTrue(viewModel.state.value.showEmpty)
            assertFalse(viewModel.state.value.showError)
        }

    // ---- selection -------------------------------------------------------

    @Test
    fun `the current selection follows the service status and marks its row`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(US, REC, DE)
            val viewModel = viewModel()
            runCurrent()

            gateway.serviceState.value = snapshot("CONNECTED", "us", EDGE_ADDRESS)
            runCurrent()

            val state = viewModel.state.value
            assertEquals("United States", state.selection?.countryName)
            assertEquals("New York", state.selection?.cityName)
            assertEquals(EDGE_ADDRESS, state.selection?.serverAddress)
            assertEquals(listOf("connect", "quic"), state.selection?.protocolNames)
            assertTrue(state.isSelected(US, NYC, EDGE))
            assertFalse(state.isSelected(US, NYC, SHARE))
        }

    @Test
    fun `saving persists the exact country city and address and never marks early`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(US)
            val viewModel = viewModel()
            runCurrent()
            gateway.selectGate = CompletableDeferred()

            viewModel.select(US, NYC, SHARE)
            runCurrent()

            assertEquals(
                listOf(SelectionRequest("us", "nyc", SHARE_ADDRESS)),
                gateway.selections,
            )
            assertTrue(viewModel.state.value.isSaving(SHARE))
            assertFalse(viewModel.state.value.isSelected(US, NYC, SHARE))

            gateway.selectGate?.complete(Unit)
            runCurrent()
            assertFalse(viewModel.state.value.isSaving(SHARE))
            assertFalse(
                "the mark moves only when the service pushes a status",
                viewModel.state.value.isSelected(US, NYC, SHARE),
            )
        }

    @Test
    fun `only one save runs at a time and a failed save retries the same request`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(US)
            val viewModel = viewModel()
            runCurrent()
            gateway.selectGate = CompletableDeferred()

            viewModel.select(US, NYC, EDGE)
            runCurrent()
            viewModel.select(US, NYC, SHARE)
            assertEquals(1, gateway.selections.size)

            gateway.selectGate?.complete(Unit)
            runCurrent()
            gateway.selectError = IllegalStateException("save failed")
            viewModel.select(US, NYC, SHARE)
            runCurrent()

            val notice = viewModel.state.value.notice
            assertEquals(LocationsNoticeAction.RETRY_SELECT, notice?.action)
            assertEquals(SelectionRequest("us", "nyc", SHARE_ADDRESS), notice?.request)

            gateway.selectError = null
            viewModel.retryNotice()
            runCurrent()
            assertEquals(3, gateway.selections.size)
            assertEquals(SHARE_ADDRESS, gateway.selections.last().address)
        }

    @Test
    fun `quarantined and targetless servers cannot be selected`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(US)
            val viewModel = viewModel()
            runCurrent()

            viewModel.select(US, NYC, QUARANTINED)
            viewModel.select(US, NYC, server("", 443))
            viewModel.select(US, NYC, server("bad.example.net", 0))
            assertTrue(gateway.selections.isEmpty())
        }

    // ---- filter and visit ------------------------------------------------

    @Test
    fun `the query filters flat rows without changing the order`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(US, REC, DE)
            val viewModel = viewModel()
            runCurrent()

            viewModel.onQueryChange("  QUIC ")
            val filtered = viewModel.state.value.rows
            assertEquals(listOf("edge.example.net:2499"), filtered.map { LocationTree.addressOf(it.server) })

            viewModel.clearQuery()
            assertEquals(5, viewModel.state.value.rows.size)
        }

    @Test
    fun `re-entering the screen resets speed order but keeps the latency labels`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(US, REC, DE)
            val viewModel = viewModel()
            runCurrent()
            viewModel.pingAll()
            runCurrent()
            gateway.emitPing(EDGE_ADDRESS, 80.0)
            gateway.emitPing(SHARE_ADDRESS, 20.0)
            gateway.emitPing(WEST_ADDRESS, 10.0)
            gateway.emitPing(BER_ADDRESS, null)
            gateway.emitPing(AUTO_ADDRESS, 50.0)
            runCurrent()
            assertEquals(LocationSortMode.SPEED, viewModel.state.value.sortMode)
            val speedToken = viewModel.state.value.listResetToken

            viewModel.beginVisit()

            assertEquals(LocationSortMode.COUNTRY, viewModel.state.value.sortMode)
            assertEquals(null, viewModel.state.value.speedOrderLatency)
            assertTrue(viewModel.state.value.listResetToken > speedToken)
            assertEquals(80.0, viewModel.state.value.latency[LocationTree.addressKey(EDGE_ADDRESS)])
            assertEquals("de", viewModel.state.value.rows[1].country.code)
        }

    // ---- ping all --------------------------------------------------------

    @Test
    fun `ping all targets every usable server even when the query hides it`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(US, REC, DE)
            val viewModel = viewModel()
            runCurrent()
            viewModel.onQueryChange("edge")

            val targets = viewModel.state.value.allTargets
            assertEquals(
                listOf(AUTO_ADDRESS, EDGE_ADDRESS, SHARE_ADDRESS, WEST_ADDRESS, BER_ADDRESS).toSet(),
                targets.toSet(),
            )
            viewModel.pingAll()
            runCurrent()

            assertEquals(listOf(targets), gateway.pingCalls)
            assertEquals(targets.size, viewModel.state.value.pingAllCount)
            assertTrue(viewModel.state.value.pingingAll)
            assertFalse(viewModel.state.value.canPingAll)
        }

    @Test
    fun `results fill labels while the run is active and speed order applies only at completion`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(US, REC, DE)
            val viewModel = viewModel()
            runCurrent()

            viewModel.pingAll()
            runCurrent()
            val startToken = viewModel.state.value.listResetToken
            gateway.emitPing(EDGE_ADDRESS, 80.0)
            runCurrent()

            assertEquals(LocationSortMode.COUNTRY, viewModel.state.value.sortMode)
            assertEquals("de", viewModel.state.value.rows[1].country.code)
            assertEquals(80L, (viewModel.state.value.latencyOf(EDGE) as LatencyState.Measured).milliseconds)

            gateway.emitPing(SHARE_ADDRESS, 20.0)
            gateway.emitPing(WEST_ADDRESS, 10.0)
            gateway.emitPing(BER_ADDRESS, null)
            gateway.emitPing(AUTO_ADDRESS, 50.0)
            runCurrent()

            val state = viewModel.state.value
            assertFalse(state.pingingAll)
            assertEquals(LocationSortMode.SPEED, state.sortMode)
            assertTrue(state.listResetToken > startToken)
            assertEquals(
                listOf("auto.example.net", "west.example.net", "share.example.net", "edge.example.net", "ber.example.net"),
                state.rows.map { it.server.hostname },
            )
            assertEquals(
                mapOf(
                    LocationTree.addressKey(EDGE_ADDRESS) to 80.0,
                    LocationTree.addressKey(SHARE_ADDRESS) to 20.0,
                    LocationTree.addressKey(WEST_ADDRESS) to 10.0,
                    LocationTree.addressKey(BER_ADDRESS) to null,
                    LocationTree.addressKey(AUTO_ADDRESS) to 50.0,
                ),
                state.speedOrderLatency,
            )
        }

    @Test
    fun `a run with no results stays in country order and says so`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(US, REC, DE)
            val viewModel = viewModel()
            runCurrent()

            viewModel.pingAll()
            runCurrent()
            advanceTimeBy(WINDOW + 1)
            runCurrent()

            val state = viewModel.state.value
            assertFalse(state.pingingAll)
            assertEquals(LocationSortMode.COUNTRY, state.sortMode)
            assertTrue(state.latency.isEmpty())
            assertEquals(
                LocationsNoticeText.Local(LocationsMessage.PING_ALL_NO_RESULTS),
                state.notice?.text,
            )
            assertEquals(LocationsNoticeAction.RETRY_PING_ALL, state.notice?.action)
        }

    @Test
    fun `a timeout counts as a result but a failed run does not change the mode`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(US, REC, DE)
            val viewModel = viewModel()
            runCurrent()

            viewModel.pingAll()
            runCurrent()
            gateway.emitPing(EDGE_ADDRESS, null)
            gateway.emitPing(SHARE_ADDRESS, null)
            gateway.emitPing(WEST_ADDRESS, null)
            gateway.emitPing(BER_ADDRESS, null)
            gateway.emitPing(AUTO_ADDRESS, null)
            runCurrent()

            assertEquals(LocationSortMode.SPEED, viewModel.state.value.sortMode)

            gateway.countries = listOf(US, REC, DE)
            viewModel.beginVisit()
            gateway.pingError = IllegalStateException("ping failed")
            viewModel.pingAll()
            runCurrent()

            val failed = viewModel.state.value
            assertEquals(LocationSortMode.COUNTRY, failed.sortMode)
            assertEquals(LocationsNoticeAction.RETRY_PING_ALL, failed.notice?.action)
            assertFalse(failed.pingingAll)
        }

    @Test
    fun `a second run does not reorder until it completes`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(US, REC, DE)
            val viewModel = viewModel()
            runCurrent()
            viewModel.pingAll()
            runCurrent()
            gateway.emitPing(EDGE_ADDRESS, 80.0)
            gateway.emitPing(SHARE_ADDRESS, 20.0)
            gateway.emitPing(WEST_ADDRESS, 10.0)
            gateway.emitPing(BER_ADDRESS, null)
            gateway.emitPing(AUTO_ADDRESS, 50.0)
            runCurrent()
            val firstOrder = viewModel.state.value.rows.map { it.server.hostname }

            viewModel.pingAll()
            runCurrent()
            gateway.emitPing(EDGE_ADDRESS, 1.0)
            runCurrent()

            assertEquals(
                "the old speed order stays frozen while the new run is in flight",
                firstOrder,
                viewModel.state.value.rows.map { it.server.hostname },
            )

            gateway.emitPing(SHARE_ADDRESS, 1.0)
            gateway.emitPing(WEST_ADDRESS, 1.0)
            gateway.emitPing(BER_ADDRESS, 1.0)
            gateway.emitPing(AUTO_ADDRESS, 1.0)
            runCurrent()

            assertEquals(
                listOf("auto.example.net", "ber.example.net", "edge.example.net", "share.example.net", "west.example.net"),
                viewModel.state.value.rows.map { it.server.hostname },
            )
        }

    @Test
    fun `refresh clears latency and speed order and reloads`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(US, REC, DE)
            val viewModel = viewModel()
            runCurrent()
            viewModel.pingAll()
            runCurrent()
            gateway.emitPing(EDGE_ADDRESS, 80.0)
            gateway.emitPing(SHARE_ADDRESS, 20.0)
            gateway.emitPing(WEST_ADDRESS, 10.0)
            gateway.emitPing(BER_ADDRESS, null)
            gateway.emitPing(AUTO_ADDRESS, 50.0)
            runCurrent()
            assertEquals(LocationSortMode.SPEED, viewModel.state.value.sortMode)
            val speedToken = viewModel.state.value.listResetToken

            viewModel.refresh()
            runCurrent()

            val state = viewModel.state.value
            assertEquals(LocationSortMode.COUNTRY, state.sortMode)
            assertTrue(state.listResetToken > speedToken)
            assertTrue(state.latency.isEmpty())
            assertNull(state.speedOrderLatency)
            assertEquals(2, gateway.locationCalls)
            assertEquals(listOf(false, true), gateway.refreshFlags)
        }

    @Test
    fun `ping all is not offered with no usable targets`() =
        runTest(mainRule.testDispatcher) {
            gateway.countries = listOf(
                Country("us", "United States", listOf(City("nyc", "New York", listOf(QUARANTINED)))),
            )
            val viewModel = viewModel()
            runCurrent()

            assertTrue(viewModel.state.value.rows.isEmpty())
            assertTrue(viewModel.state.value.allTargets.isEmpty())
            assertFalse(viewModel.state.value.canPingAll)
        }

    private companion object {
        const val WINDOW = 5_000L
        const val EDGE_ADDRESS = "edge.example.net:2499"
        const val SHARE_ADDRESS = "share.example.net:443"
        const val WEST_ADDRESS = "west.example.net:443"
        const val BER_ADDRESS = "ber.example.net:443"
        const val AUTO_ADDRESS = "auto.example.net:443"

        fun server(
            host: String,
            port: Int = 443,
            quarantined: Boolean = false,
            protocols: List<String> = listOf("connect"),
        ) = Server(
            hostname = host,
            port = port,
            quarantined = quarantined,
            protocols = protocols.map { Protocol(name = it, host = host, port = port, scheme = "https") },
        )

        val EDGE = server("edge.example.net", 2499, protocols = listOf("connect", "quic"))
        val SHARE = server("share.example.net")
        val WEST = server("west.example.net")
        val BER = server("ber.example.net")
        val QUARANTINED = server("quarantined.example.net", quarantined = true)
        val AUTO = server("auto.example.net")

        val NYC = City("nyc", "New York", listOf(EDGE, SHARE))
        val SFO = City("sfo", "San Francisco", listOf(WEST))
        val US = Country("us", "United States", listOf(NYC, SFO))
        val DE = Country("de", "Germany", listOf(City("ber", "Berlin", listOf(BER))))
        val REC = Country(
            "rec",
            "Recommended Location",
            listOf(City("rec", "Recommended", listOf(AUTO))),
        )
        fun snapshot(state: String, location: String? = null, server: String? = null) = ServiceState(
            binding = true,
            engineReady = true,
            status = EngineStatus(
                state = state,
                authenticated = true,
                email = "user@example.com",
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
            ),
        )
    }
}

/**
 * The fake gateway: one mutable service mirror, recorded calls and a shared
 * result flow the test drives.
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
        selections += SelectionRequest(countryCode, cityCode, server)
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
