package dev.benyamin.kumiho.ui.locations

import androidx.activity.ComponentActivity
import androidx.compose.ui.test.assertHeightIsAtLeast
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.assertIsNotSelected
import androidx.compose.ui.test.assertIsSelected
import androidx.compose.ui.test.assertTextContains
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.test.performTextInput
import androidx.compose.ui.unit.dp
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.Protocol
import dev.benyamin.kumiho.engine.Server
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * The flat Locations screen as it is drawn: real Compose, the real light
 * palette, and only [LocationsState]/[LocationsActions] for input. No engine,
 * service or network is involved.
 */
class LocationsScreenTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private val calls = mutableListOf<String>()

    private fun show(state: LocationsState) {
        rule.setContent {
            KumihoTheme(
                colors = PaletteMapping.fromJson(
                    dev.benyamin.kumiho.go.mobile.Mobile.palette(ThemeId.LIGHT.id, KumihoAccent.BLUE.id),
                    ThemeId.LIGHT,
                    KumihoAccent.BLUE,
                ),
            ) {
                LocationsScreen(state = state, actions = actions())
            }
        }
        rule.waitForIdle()
    }

    private fun text(id: Int): String = rule.activity.getString(id)

    // ---- rows ------------------------------------------------------------

    @Test
    fun recommendedIsTheFirstFlatServerRow() {
        show(state())

        val auto = rowTag("rec", "rec", AUTO_ADDRESS)
        val edge = rowTag("us", "nyc", EDGE_ADDRESS)
        scrollTo(auto)
        val autoTop = rule.onNodeWithTag(auto).fetchSemanticsNode().boundsInRoot.top
        scrollTo(edge)
        val edgeTop = rule.onNodeWithTag(edge).fetchSemanticsNode().boundsInRoot.top
        assertTrue("the Recommended server must be the first row", autoTop < edgeTop)
        rule.onNodeWithTag(auto).assertTextContains(text(R.string.locations_recommended))
    }

    @Test
    fun theFirstLineIsCountryAndCityAndTheSecondIsHostPortProtocols() {
        show(state())

        val row = rule.onNodeWithTag(rowTag("us", "nyc", EDGE_ADDRESS))
        scrollTo(rowTag("us", "nyc", EDGE_ADDRESS))
        row.assertTextContains("United States · New York")
        row.assertTextContains("edge.example.net:2499 · connect, quic")
    }

    @Test
    fun thereIsNoCountryExpansionOrConnectedHint() {
        show(state(selection = selection()))

        rule.onNodeWithTag(LocationsTags.LIST).assertExists()
        assertNoTag("locations_country_rec")
        assertNoTag("locations_toggle_all")
        assertNoTag("locations_connected_hint")
        assertNoTag("locations_test_speed")
        rule.onNodeWithText("Expand all").assertDoesNotExist()
    }

    @Test
    fun quarantinedAndTargetlessRowsAreNotInTheList() {
        show(state())

        assertNoTag(rowTag("us", "nyc", QUARANTINED_ADDRESS))
        scrollTo(rowTag("us", "nyc", EDGE_ADDRESS))
        rule.onNodeWithTag(rowTag("us", "nyc", EDGE_ADDRESS)).assertExists()
    }

    @Test
    fun aServerTapSendsTheExactTuple() {
        show(state())

        scrollTo(rowTag("us", "nyc", EDGE_ADDRESS))
        rule.onNodeWithTag(rowTag("us", "nyc", EDGE_ADDRESS)).performClick()
        assertEquals(listOf("select-us-nyc-$EDGE_ADDRESS"), calls)
    }

    @Test
    fun theSelectedRowShowsTheSelectionAndTheSummaryShowsTheFullDetail() {
        show(state(selection = selection()))

        val row = rule.onNodeWithTag(rowTag("us", "nyc", EDGE_ADDRESS))
        scrollTo(rowTag("us", "nyc", EDGE_ADDRESS))
        row.assertIsSelected()
        rule.onNodeWithTag(rowTag("us", "nyc", SHARE_ADDRESS)).assertIsNotSelected()
        rule.onNodeWithText("United States · New York · $EDGE_ADDRESS · connect, quic").assertExists()
    }

    @Test
    fun aQuarantinedSelectionStillAppearsInTheSummary() {
        show(
            state(
                selection = LocationTree.currentSelection(COUNTRIES, "us", QUARANTINED_ADDRESS),
            ),
        )

        rule.onNodeWithText("United States · New York · $QUARANTINED_ADDRESS · connect").assertExists()
    }

    // ---- filter and controls --------------------------------------------

    @Test
    fun theSearchPlaceholderCoversCountryCityServerAndProtocol() {
        show(state())

        rule.onNodeWithTag(LocationsTags.SEARCH).assertExists()
        rule.onNodeWithTag(LocationsTags.SEARCH).performClick()
        rule.onNodeWithText(text(R.string.locations_search_placeholder)).assertExists()
        rule.onNodeWithTag(LocationsTags.SEARCH).performTextInput("quic")
        assertEquals(listOf("query-quic"), calls)
    }

    @Test
    fun clearingTheSearchIsTheOnlySearchAction() {
        show(state(query = "ber"))
        rule.onNodeWithTag(LocationsTags.SEARCH_CLEAR).performClick()
        assertEquals(listOf("clear-query"), calls)
    }

    @Test
    fun refreshAndPingAllAreTheOnlyToolbarActions() {
        show(state())

        rule.onNodeWithTag(LocationsTags.REFRESH).assertExists()
        rule.onNodeWithTag(LocationsTags.PING_ALL).assertExists()
        assertNoTag("locations_toggle_all")
        rule.onNodeWithTag(LocationsTags.PING_ALL).performClick()
        assertEquals(listOf("ping-all"), calls)
    }

    @Test
    fun pingAllIsDisabledAndCountsWhileRunning() {
        show(state(pingingAll = true, pingAllCount = 5))

        rule.onNodeWithTag(LocationsTags.PING_ALL).assertIsNotEnabled()
        rule.onNodeWithText(rule.activity.getString(R.string.locations_pinging_all, 5)).assertExists()
    }

    @Test
    fun speedOrderPutsTheFastestRowBeforeTheSlowOne() {
        val latency = mapOf(
            LocationTree.addressKey(EDGE_ADDRESS) to 80.0,
            LocationTree.addressKey(BER_ADDRESS) to 10.0,
        )
        show(
            state(
                latency = latency,
                sortMode = LocationSortMode.SPEED,
                speedOrderLatency = latency,
            ),
        )

        val ber = rowTag("de", "ber", BER_ADDRESS)
        val edge = rowTag("us", "nyc", EDGE_ADDRESS)
        scrollTo(ber)
        val berTop = rule.onNodeWithTag(ber).fetchSemanticsNode().boundsInRoot.top
        scrollTo(edge)
        val edgeTop = rule.onNodeWithTag(edge).fetchSemanticsNode().boundsInRoot.top
        assertTrue("the measured order must put Berlin before New York", berTop < edgeTop)
    }

    @Test
    fun rowTouchTargetsStayAtLeast48dp() {
        show(state())

        scrollTo(rowTag("us", "nyc", EDGE_ADDRESS))
        rule.onNodeWithTag(rowTag("us", "nyc", EDGE_ADDRESS)).assertHeightIsAtLeast(48.dp)
        rule.onNodeWithTag(LocationsTags.REFRESH).assertHeightIsAtLeast(48.dp)
        rule.onNodeWithTag(LocationsTags.PING_ALL).assertHeightIsAtLeast(48.dp)
    }

    // ---- honest states ---------------------------------------------------

    @Test
    fun loadingShowsNoList() {
        show(state(loading = true, loaded = false, countries = emptyList()))
        rule.onNodeWithTag(LocationsTags.LOADING).assertExists()
        assertNoTag(LocationsTags.LIST)
    }

    @Test
    fun aFirstLoadErrorOffersRetry() {
        show(state(loaded = false, countries = emptyList(), loadError = "daemon offline"))
        rule.onNodeWithTag(LocationsTags.ERROR).assertExists()
        rule.onNodeWithText("daemon offline").assertExists()
        rule.onNodeWithText(text(R.string.locations_retry)).performClick()
        assertEquals(listOf("retry-load"), calls)
    }

    @Test
    fun anEmptySuccessfulLoadOffersRefresh() {
        show(state(countries = emptyList(), loaded = true))
        rule.onNodeWithTag(LocationsTags.EMPTY).assertExists()
        rule.onNodeWithText(text(R.string.locations_refresh_action)).performClick()
        assertEquals(listOf("refresh"), calls)
    }

    @Test
    fun noMatchOffersClear() {
        show(state(query = "nowhere"))
        rule.onNodeWithTag(LocationsTags.NO_MATCH).assertExists()
        rule.onNodeWithText(rule.activity.getString(R.string.locations_no_match_message, "nowhere"))
            .assertExists()
        rule.onNodeWithTag(LocationsTags.NO_MATCH_CLEAR).performClick()
        assertEquals(listOf("clear-query"), calls)
    }

    @Test
    fun aNoticeKeepsTheRowsVisibleAndOffersTheRetryAndDismiss() {
        show(
            state(
                notice = LocationsNotice.refreshFailed("the engine did not answer"),
            ),
        )

        rule.onNodeWithTag(LocationsTags.NOTICE).assertExists()
        rule.onNodeWithText(text(R.string.locations_message_refresh_failed)).assertExists()
        rule.onNodeWithText("the engine did not answer").assertExists()
        rule.onNodeWithTag(rowTag("us", "nyc", EDGE_ADDRESS)).assertExists()
        rule.onNodeWithTag(LocationsTags.NOTICE_ACTION).performClick()
        rule.onNodeWithTag(LocationsTags.NOTICE_DISMISS).performClick()
        assertEquals(listOf("retry-notice", "dismiss-notice"), calls)
    }

    // ---- fixture ---------------------------------------------------------

    private fun rowTag(country: String, city: String, address: String) =
        serverRowTag(country, city, address)

    private fun assertNoTag(tag: String) {
        rule.onNodeWithTag(tag).assertDoesNotExist()
    }

    private fun scrollTo(tag: String) {
        rule.onNodeWithTag(LocationsTags.LIST).performScrollToNode(hasTestTag(tag))
        rule.waitForIdle()
    }

    private fun selection() = LocationTree.currentSelection(COUNTRIES, "us", EDGE_ADDRESS)

    private fun actions() = LocationsActions(
        onQueryChange = { calls += "query-$it" },
        onClearQuery = { calls += "clear-query" },
        onSelect = { country, city, server ->
            calls += "select-${country.code}-${city.code}-${LocationTree.connectTarget(server)}"
        },
        onRefresh = { calls += "refresh" },
        onRetryLoad = { calls += "retry-load" },
        onPingAll = { calls += "ping-all" },
        onDismissNotice = { calls += "dismiss-notice" },
        onRetryNotice = { calls += "retry-notice" },
    )

    private fun state(
        countries: List<Country> = COUNTRIES,
        loaded: Boolean = true,
        loading: Boolean = false,
        loadError: String? = null,
        query: String = "",
        selection: LocationSelection? = null,
        latency: Map<String, Double?> = emptyMap(),
        sortMode: LocationSortMode = LocationSortMode.COUNTRY,
        speedOrderLatency: Map<String, Double?>? = null,
        pingingAll: Boolean = false,
        pingAllCount: Int = 0,
        savingServer: String? = null,
        notice: LocationsNotice? = null,
    ) = LocationsState(
        loading = loading,
        refreshing = false,
        loaded = loaded,
        countries = countries,
        loadError = loadError,
        notice = notice,
        query = query,
        selection = selection,
        latency = latency,
        sortMode = sortMode,
        speedOrderLatency = speedOrderLatency,
        pingingAll = pingingAll,
        pingAllCount = pingAllCount,
        savingServer = savingServer,
    )

    private companion object {
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

        val EDGE_SERVER = server("edge.example.net", 2499, protocols = listOf("connect", "quic"))
        val SHARE_SERVER = server("share.example.net")
        val WEST_SERVER = server("west.example.net")
        val QUARANTINED_SERVER = server("quarantined.example.net", quarantined = true)
        val AUTO_SERVER = server("auto.example.net")
        val BER_SERVER = server("ber.example.net")

        const val EDGE_ADDRESS = "edge.example.net:2499"
        const val SHARE_ADDRESS = "share.example.net:443"
        const val WEST_ADDRESS = "west.example.net:443"
        const val QUARANTINED_ADDRESS = "quarantined.example.net:443"
        const val AUTO_ADDRESS = "auto.example.net:443"
        const val BER_ADDRESS = "ber.example.net:443"

        val REC = Country(
            code = "rec",
            name = "Recommended Location",
            cities = listOf(City("rec", "Recommended", listOf(AUTO_SERVER))),
        )

        val US = Country(
            code = "us",
            name = "United States",
            cities = listOf(
                City(
                    code = "nyc",
                    name = "New York",
                    servers = listOf(EDGE_SERVER, SHARE_SERVER, QUARANTINED_SERVER),
                ),
                City(code = "sfo", name = "San Francisco", servers = listOf(WEST_SERVER)),
            ),
        )

        val DE = Country(
            code = "de",
            name = "Germany",
            cities = listOf(City("ber", "Berlin", listOf(BER_SERVER))),
        )

        val COUNTRIES = listOf(US, REC, DE)
    }
}
