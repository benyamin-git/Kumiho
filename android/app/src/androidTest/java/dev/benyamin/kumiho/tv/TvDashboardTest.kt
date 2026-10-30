package dev.benyamin.kumiho.tv

import androidx.activity.ComponentActivity
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertTextEquals
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import dev.benyamin.kumiho.engine.ByteCounters
import dev.benyamin.kumiho.engine.ByteRates
import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.engine.Quota
import dev.benyamin.kumiho.engine.Server
import dev.benyamin.kumiho.go.mobile.Mobile
import dev.benyamin.kumiho.ui.dashboard.ConnectOptionsState
import dev.benyamin.kumiho.ui.dashboard.DashboardActions
import dev.benyamin.kumiho.ui.dashboard.DashboardPhase
import dev.benyamin.kumiho.ui.dashboard.DashboardState
import dev.benyamin.kumiho.ui.dashboard.DashboardTags
import dev.benyamin.kumiho.ui.dashboard.LocationChoice
import dev.benyamin.kumiho.ui.dashboard.LocationLoad
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoColors
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.ui.tv.dashboard.TvDashboardScreen
import dev.benyamin.kumiho.ui.tv.theme.TvKumihoTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Rule
import org.junit.Test

/**
 * The TV dashboard renders the same facts as the phone dashboard, opens the
 * full-page options flow, and exposes the one-shot power control. TV only;
 * skipped on a phone emulator.
 */
class TvDashboardTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private fun palette(): KumihoColors = PaletteMapping.fromJson(
        Mobile.palette(ThemeId.DARK.id, KumihoAccent.BLUE.id),
        ThemeId.DARK,
        KumihoAccent.BLUE,
    )

    private fun status(): EngineStatus = EngineStatus(
        state = "CONNECTED",
        authenticated = true,
        email = "user@example.com",
        since = System.currentTimeMillis() / 1000 - 125,
        tokenExpires = 0,
        passExpires = 0,
        killSwitch = true,
        autoconnect = false,
        location = "gb",
        server = "lon1.example.net:443",
        proxyOnly = false,
        quota = Quota(limit = 50L * GIB, remaining = 12L * GIB, reset = 1_800_000_000L),
        dns = "10.8.0.1",
        ipv6 = "blackholed",
        exitIp = "203.0.113.7",
        exitCountry = "United Kingdom",
        totals = ByteCounters(up = 100L * MIB_LONG, down = 250L * MIB_LONG),
        rates = ByteRates(up = 1.2 * MIB, down = 3.4 * MIB),
    )

    private fun actions(
        open: () -> Unit,
        close: () -> Unit,
        proxy: (Boolean) -> Unit = {},
        city: (String, String) -> Unit = { _, _ -> },
        connect: () -> Unit = {},
        toggle: (String) -> Unit = {},
    ): DashboardActions = DashboardActions(
        onPower = {},
        onOpenOptions = open,
        onCloseOptions = close,
        onSelectRecommended = {},
        onSelectSaved = {},
        onToggleCountry = toggle,
        onSelectCity = city,
        onToggleProxyOnly = proxy,
        onConnectFromOptions = connect,
        onReloadLocations = {},
        onOpenVpnSettings = {},
        onConfirmLockdownDisconnect = {},
        onCancelLockdown = {},
        onDismissNotice = {},
        onRetryNotice = {},
    )

    @Test
    fun connectedDashboardShowsTheRealFacts() {
        assumeTrue(TvEnvironment.isTelevision(rule.activity))
        val state = DashboardState(
            phase = DashboardPhase.CONNECTED,
            status = status(),
            locations = LocationLoad.Ready(
                listOf(
                    Country("gb", "United Kingdom", listOf(City("lon", "London", emptyList()))),
                ),
            ),
        )
        rule.setContent {
            TvKumihoTheme(palette()) {
                TvDashboardScreen(state = state, actions = actions({}, {}))
            }
        }
        rule.onNodeWithTag(DashboardTags.POWER).assertIsDisplayed()
        rule.onNodeWithTag(DashboardTags.STATE_WORD).assertIsDisplayed()
        rule.onNodeWithTag(DashboardTags.CARD_LOCATION).assertExists()
        rule.onNodeWithTag(DashboardTags.CARD_EXIT).assertExists()
        rule.onNodeWithTag(DashboardTags.CARD_QUOTA).assertExists()
        rule.onNodeWithTag(DashboardTags.CARD_SESSION).assertExists()
    }

    @Test
    fun theOptionsPageIsAFullPageAndSelectionReachesTheConnectButton() {
        assumeTrue(TvEnvironment.isTelevision(rule.activity))
        var cityPicked = ""
        var connectClicks = 0
        rule.setContent {
            var open by remember { mutableStateOf(false) }
            var choice by remember { mutableStateOf<LocationChoice>(LocationChoice.Recommended) }
            var expanded by remember { mutableStateOf(emptySet<String>()) }
            val state = DashboardState(
                phase = DashboardPhase.OFF,
                status = status(),
                options = ConnectOptionsState(open = open, choice = null, expanded = setOf("gb")),
                locations = LocationLoad.Ready(
                    listOf(
                        Country("gb", "United Kingdom", listOf(City("lon", "London", emptyList()))),
                    ),
                ),
            ).copy(options = ConnectOptionsState(open = open, choice = choice, expanded = expanded))
            TvKumihoTheme(palette()) {
                TvDashboardScreen(
                    state = state,
                    actions = actions(
                        open = { open = true },
                        close = { open = false },
                        city = { country, city ->
                            cityPicked = "$country/$city"
                            choice = LocationChoice.Picked(country, city)
                        },
                        connect = { connectClicks++ },
                        toggle = { code ->
                            expanded = if (code in expanded) expanded - code else expanded + code
                        },
                    ),
                )
            }
        }

        rule.onNodeWithTag(DashboardTags.CONNECT_OPTIONS).performClick()
        rule.onNodeWithTag(DashboardTags.SHEET).assertIsDisplayed()
        rule.onNodeWithTag(DashboardTags.PICKER_RECOMMENDED).assertIsDisplayed()
        rule.onNodeWithTag(DashboardTags.SHEET_CONNECT).assertExists()

        rule.onNodeWithTag(dev.benyamin.kumiho.ui.dashboard.countryTag("gb")).performClick()
        rule.onNodeWithTag(
            dev.benyamin.kumiho.ui.dashboard.countryFlagTag("gb"),
            useUnmergedTree = true,
        ).assertExists()
        rule.onNodeWithTag(dev.benyamin.kumiho.ui.dashboard.cityTag("gb", "lon")).performClick()
        assertEquals("gb/lon", cityPicked)
        rule.onNodeWithTag(DashboardTags.SHEET_CONNECT).performClick()
        assertTrue(connectClicks >= 1)
    }

    @Test
    fun theOffDashboardShowsOneConnectControlAndNoFakeFacts() {
        assumeTrue(TvEnvironment.isTelevision(rule.activity))
        val state = DashboardState(phase = DashboardPhase.OFF)
        rule.setContent {
            TvKumihoTheme(palette()) {
                TvDashboardScreen(state = state, actions = actions({}, {}))
            }
        }
        rule.onNodeWithTag(DashboardTags.POWER).assertIsDisplayed()
        rule.onNodeWithTag(DashboardTags.STATE_WORD).assertIsDisplayed()
        rule.onNodeWithTag(DashboardTags.CARD_LOCATION).assertDoesNotExist()
        rule.onNodeWithTag(DashboardTags.CARD_EXIT).assertDoesNotExist()
        rule.onNodeWithTag(DashboardTags.CARD_QUOTA).assertDoesNotExist()
        rule.onNodeWithTag(DashboardTags.CARD_SESSION).assertDoesNotExist()
    }

    private companion object {
        const val GIB = 1024L * 1024L * 1024L
        const val MIB_LONG = 1024L * 1024L
        const val MIB = 1024.0 * 1024.0
    }
}
