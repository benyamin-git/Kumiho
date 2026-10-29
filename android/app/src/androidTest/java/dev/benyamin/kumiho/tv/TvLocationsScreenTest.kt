package dev.benyamin.kumiho.tv

import androidx.compose.ui.test.assertHeightIsAtLeast
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsFocused
import androidx.compose.ui.test.assertIsSelected
import androidx.compose.ui.test.assertTextContains
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.Protocol
import dev.benyamin.kumiho.engine.Server
import dev.benyamin.kumiho.ui.locations.LocationTree
import dev.benyamin.kumiho.ui.locations.LocationsActions
import dev.benyamin.kumiho.ui.locations.LocationsState
import dev.benyamin.kumiho.ui.locations.LocationsTags
import dev.benyamin.kumiho.ui.locations.serverRowTag
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.ui.tv.locations.TvLocationsScreen
import dev.benyamin.kumiho.ui.tv.theme.TvKumihoTheme
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * The TV Locations surface as it is drawn: one flat focusable list, real
 * flags, the selection summary and the search/no-match controls. TV only.
 */
class TvLocationsScreenTest : TvTestBase() {

    private val calls = mutableListOf<String>()

    private fun show(
        state: LocationsState = state(),
        actions: LocationsActions = actions(),
    ) {
        rule.setContent {
            TvKumihoTheme(palette(ThemeId.LIGHT)) {
                TvLocationsScreen(state = state, actions = actions)
            }
        }
        rule.waitForIdle()
    }

    private fun text(id: Int): String = rule.activity.getString(id)

    @Test
    fun theFirstRowIsFocusedAndTheListIsOneFlatFocusedSurface() {
        show()

        val first = rule.onNodeWithTag(serverRowTag("rec", "rec", AUTO_ADDRESS))
        first.assertIsFocused()
        first.assertHeightIsAtLeast(48.dp)
        first.assertTextContains(text(R.string.locations_recommended))

        rule.onNodeWithTag(serverRowTag("us", "nyc", EDGE_ADDRESS)).assertExists()
        rule.onNodeWithTag(LocationsTags.LIST).assertIsDisplayed()
        rule.onNodeWithTag("locations_country_rec").assertDoesNotExist()
        rule.onNodeWithTag("locations_toggle_all").assertDoesNotExist()
        rule.onNodeWithTag("locations_connected_hint").assertDoesNotExist()
        rule.onNodeWithTag("locations_test_speed").assertDoesNotExist()
    }

    @Test
    fun aRowHasTheTwoLineTextAndATapSelectsTheExactServer() {
        show()

        val row = rule.onNodeWithTag(serverRowTag("us", "nyc", EDGE_ADDRESS))
        row.assertTextContains("United States · New York")
        row.assertTextContains("edge.example.net:2499 · connect, quic")

        row.performClick()
        assertEquals(listOf("select-us-nyc-$EDGE_ADDRESS"), calls)
    }

    @Test
    fun theSelectedRowAndSummaryShowTheSelection() {
        show(
            state(
                selection = LocationTree.currentSelection(
                    COUNTRIES,
                    "us",
                    EDGE_ADDRESS,
                ),
            ),
        )

        rule.onNodeWithTag(serverRowTag("us", "nyc", EDGE_ADDRESS)).assertIsSelected()
        rule.onNodeWithTag(LocationsTags.SUMMARY).assertIsDisplayed()
        rule
            .onNodeWithText("United States · New York · $EDGE_ADDRESS · connect, quic")
            .assertExists()
    }

    @Test
    fun searchFiltersTheFlatRows() {
        show(state(query = "ber"))

        rule.onNodeWithTag(serverRowTag("de", "ber", BER_ADDRESS)).assertExists()
        rule.onNodeWithTag(serverRowTag("rec", "rec", AUTO_ADDRESS)).assertDoesNotExist()
        rule.onNodeWithTag(serverRowTag("us", "nyc", EDGE_ADDRESS)).assertDoesNotExist()
    }

    @Test
    fun noMatchNamesTheScope() {
        show(state(query = "zzz"))

        rule.onNodeWithTag(LocationsTags.NO_MATCH).assertIsDisplayed()
        rule
            .onNodeWithText(text(R.string.locations_no_match_message).format("zzz"))
            .assertExists()
    }

    @Test
    fun pingAllIsTheOnlyActionAndUsesEveryTarget() {
        show()

        rule.onNodeWithTag(LocationsTags.PING_ALL).assertIsEnabled().performClick()
        assertEquals(listOf("ping-all"), calls)
        rule.onNodeWithTag("locations_test_speed").assertDoesNotExist()
        rule.onNodeWithTag("locations_toggle_all").assertDoesNotExist()
    }

    private fun actions() = LocationsActions(
        onQueryChange = { calls += "query-$it" },
        onClearQuery = { calls += "clear" },
        onSelect = { country, city, server ->
            calls += "select-${country.code}-${city.code}-${LocationTree.connectTarget(server)}"
        },
        onRefresh = { calls += "refresh" },
        onRetryLoad = { calls += "retry-load" },
        onPingAll = { calls += "ping-all" },
        onDismissNotice = { calls += "dismiss" },
        onRetryNotice = { calls += "retry-notice" },
    )

    private fun state(
        query: String = "",
        selection: dev.benyamin.kumiho.ui.locations.LocationSelection? = null,
    ) = LocationsState(
        loading = false,
        loaded = true,
        countries = COUNTRIES,
        query = query,
        selection = selection,
    )

    private companion object {
        const val EDGE_ADDRESS = "edge.example.net:2499"
        const val BER_ADDRESS = "ber.example.net:443"
        const val AUTO_ADDRESS = "auto.example.net:443"

        fun server(
            host: String,
            port: Int,
            protocols: List<String>,
        ) = Server(
            hostname = host,
            port = port,
            quarantined = false,
            protocols = protocols.map { Protocol(name = it, host = host, port = port, scheme = "https") },
        )

        val EDGE = server("edge.example.net", 2499, listOf("connect", "quic"))
        val BER = server("ber.example.net", 443, listOf("connect"))
        val AUTO = server("auto.example.net", 443, listOf("connect"))

        val COUNTRIES = listOf(
            Country("us", "United States", listOf(City("nyc", "New York", listOf(EDGE)))),
            Country("de", "Germany", listOf(City("ber", "Berlin", listOf(BER)))),
            Country("rec", "Recommended", listOf(City("rec", "Recommended", listOf(AUTO)))),
        )
    }
}
