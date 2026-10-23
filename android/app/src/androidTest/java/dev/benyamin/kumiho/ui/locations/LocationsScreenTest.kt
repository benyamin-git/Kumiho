package dev.benyamin.kumiho.ui.locations

import androidx.activity.ComponentActivity
import androidx.compose.ui.test.assertHasNoClickAction
import androidx.compose.ui.test.assertHeightIsAtLeast
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.assertIsNotSelected
import androidx.compose.ui.test.assertIsSelected
import androidx.compose.ui.test.assertTextContains
import androidx.compose.ui.test.assertTextEquals
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onFirst
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
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
 * The Locations screen as it is drawn: the real Compose tree in the real light
 * palette, driven only by [LocationsState] and recording callbacks.
 *
 * Every assertion goes through a stable `locations_*` tag or a resource string,
 * so copy changes cannot break a test and the test cannot depend on a phrase it
 * invented. Nothing here reaches the engine, the service or the network.
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

    // ---- the tree --------------------------------------------------------

    @Test
    fun theTreeIsRECFirstAndNamesTheRecommendedCountry() {
        show(state(expandedCountries = setOf("rec", "us")))

        val rec = rule.onNodeWithTag(countryRowTag("rec")).fetchSemanticsNode().boundsInRoot
        val us = rule.onNodeWithTag(countryRowTag("us")).fetchSemanticsNode().boundsInRoot
        assertTrue("Recommended must be the first country row", rec.top < us.top)
        rule.onNodeWithTag(countryRowTag("rec"))
            .assertTextContains(text(R.string.locations_recommended))
    }

    @Test
    fun aServerRowShowsItsHostProtocolsPortAndLatency() {
        show(
            state(
                latency = mapOf(
                    LocationTree.addressKey(EDGE_ADDRESS) to 42.4,
                    LocationTree.addressKey(WEST_ADDRESS) to null,
                    LocationTree.addressKey(LONG_ADDRESS) to 1_234.0,
                ),
            ),
        )

        rule.onNodeWithText(EDGE_SERVER.hostname).assertExists()
        rule.onNodeWithText("New York · connect, quic · port 2499").assertExists()
        // The country row carries the same number, so scope both by tag.
        rule.onNodeWithTag(serverRowTag("us", "nyc", EDGE_ADDRESS))
            .assertTextContains(rule.activity.getString(R.string.locations_latency, 42L))
        rule.onNodeWithTag(countryRowTag("us"))
            .assertTextContains(rule.activity.getString(R.string.locations_latency, 42L))

        // Never tested is an em dash, deliberately different from a timeout.
        rule.onAllNodesWithText(text(R.string.locations_latency_none)).onFirst().assertExists()

        scrollTo(serverRowTag("us", "sfo", WEST_ADDRESS))
        rule.onNodeWithText(WEST_SERVER.hostname).assertExists()
        rule.onNodeWithText(text(R.string.locations_latency_timeout)).assertExists()

        scrollTo(serverRowTag("us", "nyc", LONG_ADDRESS))
        rule.onNodeWithText(LONG_SERVER.hostname).assertExists()
        rule.onNodeWithText(rule.activity.getString(R.string.locations_latency, 1_234L)).assertExists()
    }

    @Test
    fun aServerRowNamesItsCity() {
        show(state())

        scrollTo(serverRowTag("us", "nyc", SHARE_ADDRESS))
        rule.onNodeWithTag(serverRowTag("us", "nyc", SHARE_ADDRESS))
            .assertTextContains("New York · connect · port 443")
    }

    @Test
    fun theCurrentSelectionIsMarkedForEveryoneNotOnlyInColour() {
        show(
            state(
                selection = LocationTree.currentSelection(COUNTRIES, "us", EDGE_ADDRESS),
            ),
        )

        val selected = rule.onNodeWithTag(serverRowTag("us", "nyc", EDGE_ADDRESS))
        selected.assertIsSelected()
        rule.onNodeWithTag(serverRowTag("us", "nyc", SHARE_ADDRESS)).assertIsNotSelected()
        rule.onNodeWithContentDescription(
            text(R.string.locations_selected),
            useUnmergedTree = true,
        ).assertExists()
    }

    @Test
    fun theSelectionSummaryNamesThePlaceAndServer() {
        show(state(selection = LocationTree.currentSelection(COUNTRIES, "us", EDGE_ADDRESS)))

        rule.onNodeWithTag(LocationsTags.SUMMARY).assertExists()
        // The visual line carries the flag; semantics keep the plain summary.
        rule.onNodeWithContentDescription("United States · New York · edge.example.net").assertExists()
        rule.onNodeWithText("United States · New York · edge.example.net").assertDoesNotExist()
    }

    @Test
    fun countryFlagsAreDecorativeAndRecommendedHasNone() {
        show(state())

        rule.onNodeWithTag(countryRowTag("de")).assertTextContains("Germany")
        rule.onNodeWithText("🇩🇪").assertDoesNotExist()
        rule.onNodeWithTag(countryRowTag("rec"))
            .assertTextContains(text(R.string.locations_recommended))
        rule.onNodeWithText("🇷🇪").assertDoesNotExist()
    }

    @Test
    fun aCountryRowShowsItsServerCount() {
        show(state())

        rule.onNodeWithTag(countryRowTag("us")).assertTextContains(
            rule.activity.resources.getQuantityString(R.plurals.locations_country_servers, 5, 5),
        )
        rule.onNodeWithTag(countryRowTag("rec")).assertTextContains(
            rule.activity.resources.getQuantityString(R.plurals.locations_country_servers, 1, 1),
        )
        scrollTo(countryRowTag("de"))
        rule.onNodeWithTag(countryRowTag("de")).assertTextContains(
            rule.activity.resources.getQuantityString(R.plurals.locations_country_servers, 1, 1),
        )
    }

    @Test
    fun anExpandedOrCollapsedCountryRowShowsItsBestLatency() {
        show(
            state(
                latency = mapOf(
                    LocationTree.addressKey(EDGE_ADDRESS) to 10.0,
                    LocationTree.addressKey(SHARE_ADDRESS) to 5.0,
                    LocationTree.addressKey(WEST_ADDRESS) to null,
                    LocationTree.addressKey(BER_ADDRESS) to 61.0,
                ),
            ),
        )

        rule.onNodeWithTag(countryRowTag("us"))
            .assertTextContains(rule.activity.getString(R.string.locations_latency, 5L))
        scrollTo(countryRowTag("de"))
        rule.onNodeWithTag(countryRowTag("de"))
            .assertTextContains(rule.activity.getString(R.string.locations_latency, 61L))
    }

    @Test
    fun withNoSelectionTheSummaryIsHonest() {
        show(state())

        rule.onNodeWithText(text(R.string.locations_selection_none)).assertExists()
    }

    @Test
    fun aQuarantinedServerIsVisibleButCannotBeSelected() {
        show(state())

        scrollTo(serverRowTag("us", "nyc", QUARANTINED_ADDRESS))
        rule.onNodeWithText(text(R.string.locations_quarantined), substring = true).assertExists()
        rule.onNodeWithTag(serverRowTag("us", "nyc", QUARANTINED_ADDRESS))
            .assertExists()
            .assertHasNoClickAction()
    }

    // ---- search and expansion -------------------------------------------

    @Test
    fun theSearchFieldHasAVisibleLabelAndAClearAction() {
        show(state(query = "ber"))

        rule.onNodeWithTag(LocationsTags.SEARCH).assertExists()
        rule.onNodeWithText(text(R.string.locations_search_label)).assertExists()
        rule.onNodeWithTag(LocationsTags.SEARCH_CLEAR).performClick()

        assertEquals(listOf("clear-query"), calls)
    }

    @Test
    fun tappingACountryRequestsItsToggle() {
        show(state(expandedCountries = emptySet()))

        rule.onNodeWithTag(countryRowTag("US")).performClick()

        assertEquals(listOf("toggle-country-us"), calls)
    }

    // ---- selection and the connected hint --------------------------------

    @Test
    fun tappingAServerRequestsThatExactSelection() {
        show(state())

        rule.onNodeWithTag(serverRowTag("us", "nyc", EDGE_ADDRESS)).performClick()

        assertEquals(listOf("select-us-nyc-$EDGE_ADDRESS"), calls)
    }

    @Test
    fun aLiveSessionShowsTheHintAndStillAllowsASelection() {
        show(
            state(
                sessionActive = true,
                selection = LocationTree.currentSelection(COUNTRIES, "us", EDGE_ADDRESS),
            ),
        )

        rule.onNodeWithTag(LocationsTags.CONNECTED_HINT).assertExists()
        rule.onNodeWithText(text(R.string.locations_connected_hint)).assertExists()
        rule.onNodeWithTag(serverRowTag("us", "nyc", SHARE_ADDRESS)).performClick()

        assertEquals(listOf("select-us-nyc-$SHARE_ADDRESS"), calls)
    }

    // ---- honest content states -------------------------------------------

    @Test
    fun theLoadingStateShowsNoTree() {
        show(state(loaded = false, loading = true, countries = emptyList()))

        rule.onNodeWithTag(LocationsTags.LOADING).assertExists()
        rule.onNodeWithTag(LocationsTags.LIST).assertDoesNotExist()
    }

    @Test
    fun theInitialErrorOffersARetry() {
        show(state(loaded = false, loading = false, countries = emptyList(), loadError = "daemon offline"))

        rule.onNodeWithTag(LocationsTags.ERROR).assertExists()
        rule.onNodeWithText("daemon offline").assertExists()
        rule.onNodeWithText(text(R.string.locations_load_failed_title)).assertExists()
        rule.onNodeWithText(text(R.string.locations_retry)).performClick()

        assertEquals(listOf("retry-load"), calls)
    }

    @Test
    fun anEmptyTreeOffersARefreshInsteadOfNothing() {
        show(state(loaded = true, loading = false, countries = emptyList()))

        rule.onNodeWithTag(LocationsTags.EMPTY).assertExists()
        rule.onNodeWithText(text(R.string.locations_empty_message)).assertExists()
        rule.onNodeWithText(text(R.string.locations_refresh_action)).performClick()

        assertEquals(listOf("refresh"), calls)
    }

    @Test
    fun aQueryThatMatchesNothingSaysSoAndOffersAClear() {
        show(state(query = "atlantis"))

        rule.onNodeWithTag(LocationsTags.NO_MATCH).assertExists()
        rule.onNodeWithText(rule.activity.getString(R.string.locations_no_match_message, "atlantis"))
            .assertExists()
        rule.onNodeWithTag(LocationsTags.NO_MATCH_CLEAR).performClick()

        assertEquals(listOf("clear-query"), calls)
    }

    @Test
    fun aFailedRefreshKeepsTheTreeAndOffersOneRetry() {
        show(
            state(
                notice = LocationsNotice.refreshFailed("the engine did not answer"),
            ),
        )

        rule.onNodeWithTag(LocationsTags.NOTICE).assertExists()
        rule.onNodeWithText(text(R.string.locations_message_refresh_failed)).assertExists()
        rule.onNodeWithText("the engine did not answer").assertExists()
        rule.onNodeWithTag(serverRowTag("us", "nyc", EDGE_ADDRESS)).assertExists()

        rule.onNodeWithTag(LocationsTags.NOTICE_ACTION).performClick()
        rule.onNodeWithTag(LocationsTags.NOTICE_DISMISS).performClick()

        assertEquals(listOf("retry-notice", "dismiss-notice"), calls)
    }

    // ---- ping all and expand all ----------------------------------------

    @Test
    fun PingAllTestsTheWholeTreeEvenWhenTheQueryShowsNothing() {
        show(state(query = "atlantis"))

        rule.onNodeWithTag(LocationsTags.PING_ALL).assertIsEnabled().performClick()

        assertEquals(listOf("ping-all"), calls)
    }

    @Test
    fun PingAllIsDisabledAndNamedWhileItRuns() {
        show(state(pingingAll = true, pingAllCount = 6))

        rule.onNodeWithTag(LocationsTags.PING_ALL).assertIsNotEnabled()
        rule.onNodeWithText(rule.activity.getString(R.string.locations_pinging_all, 6)).assertExists()
    }

    @Test
    fun theToolbarIsOneRowWithRefreshPingAllAndTheToggle() {
        show(state())

        rule.onNodeWithTag(LocationsTags.REFRESH).assertExists()
        rule.onNodeWithTag(LocationsTags.PING_ALL).assertExists()
        rule.onNodeWithTag(LocationsTags.TOGGLE_ALL).assertExists()
        rule.onNodeWithTag("locations_test_speed").assertDoesNotExist()
        rule.onNodeWithTag("locations_targets_helper").assertDoesNotExist()

        val top = rule.onNodeWithTag(LocationsTags.REFRESH).fetchSemanticsNode().boundsInRoot.top
        val pingTop = rule.onNodeWithTag(LocationsTags.PING_ALL).fetchSemanticsNode().boundsInRoot.top
        val toggleTop = rule.onNodeWithTag(LocationsTags.TOGGLE_ALL).fetchSemanticsNode().boundsInRoot.top
        assertEquals(top, pingTop, 2f)
        assertEquals(top, toggleTop, 2f)
    }

    @Test
    fun theToggleSaysExpandWhenNothingIsOpenAndRequestsIt() {
        show(state(expandedCountries = emptySet()))

        rule.onNodeWithText(text(R.string.locations_expand_all)).assertExists()
        rule.onNodeWithTag(LocationsTags.TOGGLE_ALL).assertIsEnabled().performClick()

        assertEquals(listOf("toggle-all"), calls)
    }

    @Test
    fun theToggleSaysCollapseWhenAnythingIsOpenAndRequestsIt() {
        show(state(expandedCountries = setOf("us")))

        rule.onNodeWithText(text(R.string.locations_collapse_all)).assertExists()
        rule.onNodeWithTag(LocationsTags.TOGGLE_ALL).assertIsEnabled().performClick()

        assertEquals(listOf("toggle-all"), calls)
    }

    @Test
    fun theToggleIsDisabledWhileSearching() {
        show(state(query = "ber"))

        rule.onNodeWithTag(LocationsTags.TOGGLE_ALL).assertIsNotEnabled()
    }

    // ---- touch targets ---------------------------------------------------

    @Test
    fun everyPrimaryControlMeetsTheTouchTargetFloor() {
        show(state(notice = LocationsNotice.pingAllFailed("no probes")))

        rule.onNodeWithTag(LocationsTags.REFRESH).assertHeightIsAtLeast(48.dp)
        rule.onNodeWithTag(LocationsTags.PING_ALL).assertHeightIsAtLeast(48.dp)
        rule.onNodeWithTag(LocationsTags.TOGGLE_ALL).assertHeightIsAtLeast(48.dp)
        rule.onNodeWithTag(LocationsTags.NOTICE_ACTION).assertHeightIsAtLeast(48.dp)
        rule.onNodeWithTag(LocationsTags.NOTICE_DISMISS).assertHeightIsAtLeast(48.dp)
        scrollTo(serverRowTag("us", "nyc", EDGE_ADDRESS))
        rule.onNodeWithTag(serverRowTag("us", "nyc", EDGE_ADDRESS)).assertHeightIsAtLeast(48.dp)
    }

    /** Scrolls the lazy tree until the row with [tag] is composed. */
    private fun scrollTo(tag: String) {
        rule.onNodeWithTag(LocationsTags.LIST).performScrollToNode(hasTestTag(tag))
        rule.waitForIdle()
    }

    // ---- fixture ---------------------------------------------------------

    private fun actions() = LocationsActions(
        onQueryChange = { calls += "query-$it" },
        onClearQuery = { calls += "clear-query" },
        onToggleCountry = { calls += "toggle-country-$it" },
        onSelect = { country, city, server ->
            calls += "select-${country.code}-${city.code}-${LocationTree.addressOf(server)}"
        },
        onRefresh = { calls += "refresh" },
        onRetryLoad = { calls += "retry-load" },
        onPingAll = { calls += "ping-all" },
        onToggleAll = { calls += "toggle-all" },
        onDismissNotice = { calls += "dismiss-notice" },
        onRetryNotice = { calls += "retry-notice" },
    )

    private fun state(
        countries: List<Country> = COUNTRIES,
        loaded: Boolean = true,
        loading: Boolean = false,
        loadError: String? = null,
        query: String = "",
        expandedCountries: Set<String> = setOf("us"),
        selection: LocationSelection? = null,
        latency: Map<String, Double?> = emptyMap(),
        pingingAll: Boolean = false,
        pingAllCount: Int = 0,
        savingServer: String? = null,
        sessionActive: Boolean = false,
        notice: LocationsNotice? = null,
    ) = LocationsState(
        loading = loading,
        refreshing = false,
        loaded = loaded,
        countries = countries,
        loadError = loadError,
        notice = notice,
        query = query,
        expandedCountries = expandedCountries,
        selection = selection,
        latency = latency,
        pingingAll = pingingAll,
        pingAllCount = pingAllCount,
        savingServer = savingServer,
        sessionActive = sessionActive,
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
        val LONG_SERVER = server("a-very-long-edge-hostname-that-needs-truncation.example.net")
        val WEST_SERVER = server("west.example.net")
        val QUARANTINED_SERVER = server("quarantined.example.net", quarantined = true)
        val AUTO_SERVER = server("auto.example.net")
        val BER_SERVER = server("ber.example.net")

        const val EDGE_ADDRESS = "edge.example.net:2499"
        const val SHARE_ADDRESS = "share.example.net:443"
        const val LONG_ADDRESS = "a-very-long-edge-hostname-that-needs-truncation.example.net:443"
        const val WEST_ADDRESS = "west.example.net:443"
        const val QUARANTINED_ADDRESS = "quarantined.example.net:443"
        const val BER_ADDRESS = "ber.example.net:443"

        val REC = Country(
            code = "rec",
            name = "Recommended Location",
            cities = listOf(City(code = "rec", name = "Recommended", servers = listOf(AUTO_SERVER))),
        )

        val US = Country(
            code = "us",
            name = "United States",
            cities = listOf(
                City(
                    code = "nyc",
                    name = "New York",
                    servers = listOf(EDGE_SERVER, SHARE_SERVER, LONG_SERVER, QUARANTINED_SERVER),
                ),
                City(code = "sfo", name = "San Francisco", servers = listOf(WEST_SERVER)),
            ),
        )

        val DE = Country(
            code = "de",
            name = "Germany",
            cities = listOf(City(code = "ber", name = "Berlin", servers = listOf(BER_SERVER))),
        )

        /** The tree the view model would hand over: REC first. */
        val COUNTRIES = LocationTree.ordered(listOf(US, REC, DE))
    }
}
