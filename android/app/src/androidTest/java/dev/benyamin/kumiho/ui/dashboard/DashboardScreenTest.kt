package dev.benyamin.kumiho.ui.dashboard

import androidx.activity.ComponentActivity
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.assertTextContains
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.engine.ByteCounters
import dev.benyamin.kumiho.engine.ByteRates
import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.engine.Quota
import dev.benyamin.kumiho.engine.Server
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoColors
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.vpn.ServiceState
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test

/**
 * The dashboard as the user sees it: the real Compose hierarchy in the real
 * palette, with fake state. The power control, the four data cards, the notice
 * row, the permission phase, the options sheet and the lockdown explanation are
 * asserted through the stable dashboard tags, never through a sentence that
 * copy could change.
 *
 * Only the state and the callbacks are fake; there is no engine, no service and
 * no gomobile call in this test.
 */
class DashboardScreenTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private val calls = mutableListOf<String>()

    private fun show(state: DashboardState) {
        rule.setContent {
            KumihoTheme(colors = palette(ThemeId.LIGHT)) {
                DashboardScreen(state = state, actions = actions())
            }
        }
        rule.waitForIdle()
    }

    // ---- the connected dashboard ----------------------------------------

    @Test
    fun theConnectedDashboardShowsEveryRealCard() {
        show(connectedState())

        rule.onNodeWithTag(DashboardTags.SCREEN).assertExists()
        rule.onNodeWithTag(DashboardTags.POWER).assertExists()
        rule.onNodeWithTag(DashboardTags.STATE_WORD).assertExists()
        rule.onNodeWithText(text(R.string.dashboard_state_connected)).assertExists()

        rule.onNodeWithTag(DashboardTags.CARD_LOCATION).assertExists()
        rule.onNodeWithTag(DashboardTags.CARD_EXIT).assertExists()
        rule.onNodeWithTag(DashboardTags.CARD_QUOTA).assertExists()
        rule.onNodeWithTag(DashboardTags.CARD_SESSION).assertExists()
        rule.onNodeWithTag(DashboardTags.DETAILS).assertExists()

        // The cards show the engine's numbers in the desktop's wording.
        rule.onNodeWithText("United States · New York").assertExists()
        rule.onNodeWithText("edge.example.net").assertExists()
        rule.onNodeWithText("Netherlands").assertExists()
        rule.onNodeWithText("203.0.113.7").assertExists()
        rule.onNodeWithText("12.3 GiB remaining of 50.0 GiB").assertExists()
        rule.onNodeWithText("up 12.3 MiB / down 45.6 MiB").assertExists()
        rule.onNodeWithText("up 1.2 MiB/s / down 3.4 MiB/s").assertExists()
    }

    @Test
    fun thePowerControlIsTheDisconnectActionWhenConnected() {
        show(connectedState())

        rule.onNodeWithTag(DashboardTags.POWER).performClick()

        assertEquals(listOf("power"), calls)
    }

    @Test
    fun aDisconnectedDashboardKeepsOnlyTheCardsItCanFill() {
        show(
            DashboardState.fromService(snapshot(state = STATE_IDLE))
                .copy(locations = LocationLoad.Ready(COUNTRIES)),
        )

        rule.onNodeWithTag(DashboardTags.CARD_LOCATION).assertExists()
        rule.onNodeWithTag(DashboardTags.CARD_EXIT).assertDoesNotExist()
        rule.onNodeWithTag(DashboardTags.CARD_QUOTA).assertDoesNotExist()
        rule.onNodeWithTag(DashboardTags.CARD_SESSION).assertDoesNotExist()
        rule.onNodeWithText(text(R.string.dashboard_location_recommended)).assertExists()
    }

    @Test
    fun aLoadingDashboardShowsNoFakeCards() {
        show(DashboardState(phase = DashboardPhase.LOADING))

        rule.onNodeWithText(text(R.string.dashboard_state_loading)).assertExists()
        rule.onNodeWithTag(DashboardTags.CARD_LOCATION).assertDoesNotExist()
        rule.onNodeWithTag(DashboardTags.CARD_EXIT).assertDoesNotExist()
        rule.onNodeWithTag(DashboardTags.CARD_QUOTA).assertDoesNotExist()
        rule.onNodeWithTag(DashboardTags.CARD_SESSION).assertDoesNotExist()
    }

    @Test
    fun aProxyOnlySessionIsAnHonestBadgeNotATunnel() {
        show(
            DashboardState.fromService(
                snapshot(state = STATE_PROXY_ONLY).copy(status = status(state = STATE_PROXY_ONLY)),
            ).copy(locations = LocationLoad.Ready(COUNTRIES)),
        )

        rule.onNodeWithText(text(R.string.dashboard_state_proxy_only)).assertExists()
        rule.onNodeWithText(text(R.string.dashboard_proxy_only_chip)).assertExists()
    }

    // ---- notice ----------------------------------------------------------

    @Test
    fun aNoticeOffersExactlyItsOwnRetryAndADismiss() {
        show(
            connectedState().copy(
                notice = DashboardNotice.statusRefreshFailed("the engine did not answer"),
            ),
        )

        rule.onNodeWithTag(DashboardTags.NOTICE).assertExists()
        rule.onNodeWithText(text(R.string.dashboard_message_refresh_failed)).assertExists()
        rule.onNodeWithText("the engine did not answer").assertExists()

        rule.onNodeWithTag(DashboardTags.NOTICE_ACTION).performClick()
        rule.onNodeWithTag(DashboardTags.NOTICE_DISMISS).performClick()

        assertEquals(listOf("retry", "dismiss"), calls)
    }

    @Test
    fun aNoticeWithoutAnActionHasNoRetryButton() {
        show(
            connectedState().copy(notice = DashboardNotice.signInRequired()),
        )

        rule.onNodeWithTag(DashboardTags.NOTICE).assertExists()
        rule.onNodeWithTag(DashboardTags.NOTICE_ACTION).assertDoesNotExist()
        rule.onNodeWithTag(DashboardTags.NOTICE_DISMISS).assertExists()
    }

    // ---- permission ------------------------------------------------------

    @Test
    fun thePermissionPhaseAsksThePowerControlForConsent() {
        show(
            DashboardState.fromService(
                snapshot(state = STATE_IDLE, permissionNeeded = true),
            ),
        )

        rule.onNodeWithText(text(R.string.dashboard_state_permission_needed)).assertExists()
        rule.onNodeWithTag(DashboardTags.POWER).performClick()
        assertEquals(listOf("power"), calls)
    }

    @Test
    fun aDeniedPermissionNoticeOffersTheConsentDialogAgain() {
        show(connectedState().copy(notice = DashboardNotice.permissionDenied()))

        rule.onNodeWithTag(DashboardTags.NOTICE_ACTION).performClick()
        assertEquals(listOf("retry"), calls)
    }

    // ---- options sheet ---------------------------------------------------

    @Test
    fun theSheetExplainsWhyConnectIsOffAndOpensAndroidSettings() {
        show(
            connectedState().copy(
                options = ConnectOptionsState(open = true, choice = LocationChoice.Saved),
                locations = LocationLoad.Ready(COUNTRIES),
            ),
        )

        rule.onNodeWithTag(DashboardTags.SHEET).assertExists()
        rule.onNodeWithTag(DashboardTags.SHEET_CONTENT).assertExists()
        rule.onNodeWithTag(DashboardTags.SHEET_CONNECT).assertIsNotEnabled()
        rule.onNodeWithTag(DashboardTags.SHEET_HINT)
            .performScrollTo()
        rule.onNodeWithText(text(R.string.dashboard_options_disconnect_first)).assertExists()

        rule.onNodeWithTag(DashboardTags.SHEET_LOCKDOWN).performScrollTo().performClick()
        assertEquals(listOf("vpn-settings"), calls)
    }

    @Test
    fun theCompactPickerSelectsASavedOrPickedLocation() {
        show(
            DashboardState.fromService(snapshot(state = STATE_IDLE, location = "us")).copy(
                options = ConnectOptionsState(
                    open = true,
                    choice = LocationChoice.Saved,
                    expanded = setOf("us"),
                ),
                locations = LocationLoad.Ready(COUNTRIES),
            ),
        )

        rule.onNodeWithTag(DashboardTags.PICKER_SAVED).performScrollTo()
        rule.onNodeWithTag(DashboardTags.PICKER_RECOMMENDED).performScrollTo().performClick()
        rule.onNodeWithTag(countryTag("us")).performScrollTo()
            .assertTextContains("United States")
        rule.onNodeWithText("🇺🇸").assertDoesNotExist()
        rule.onNodeWithTag(countryTag("us")).performScrollTo().performClick()
        rule.onNodeWithTag(cityTag("us", "nyc")).performScrollTo().performClick()
        rule.onNodeWithTag(DashboardTags.SHEET_PROXY_ONLY).performScrollTo().performClick()

        assertEquals(
            listOf("recommended", "toggle-country-us", "city-us-nyc", "proxy-only"),
            calls,
        )
    }

    @Test
    fun anEmptyLocationTreeSaysSoInsteadOfShowingNothing() {
        show(
            DashboardState.fromService(snapshot(state = STATE_IDLE)).copy(
                options = ConnectOptionsState(open = true, choice = LocationChoice.Recommended),
                locations = LocationLoad.Ready(emptyList()),
            ),
        )

        rule.onNodeWithTag(DashboardTags.PICKER_EMPTY).performScrollTo().assertExists()
    }

    // ---- lockdown --------------------------------------------------------

    @Test
    fun theLockdownDialogKeepsTheTunnelUnlessConfirmed() {
        show(connectedState().copy(lockdownPrompt = true))

        rule.onNodeWithTag(DashboardTags.LOCKDOWN_DIALOG).assertExists()
        rule.onNodeWithTag(DashboardTags.LOCKDOWN_CANCEL).performClick()

        assertEquals(listOf("cancel-lockdown"), calls)
    }

    @Test
    fun theLockdownDialogCanConfirmTheDisconnect() {
        show(connectedState().copy(lockdownPrompt = true))

        rule.onNodeWithTag(DashboardTags.LOCKDOWN_CONFIRM).performClick()

        assertEquals(listOf("confirm-lockdown"), calls)
    }

    @Test
    fun theLockdownDialogExplainsTheOsRestartAndOpensVpnSettings() {
        show(connectedState().copy(lockdownPrompt = true))

        rule.onNodeWithText(text(R.string.dashboard_lockdown_text)).assertExists()
        rule.onNodeWithTag(DashboardTags.LOCKDOWN_VPN_SETTINGS).performClick()

        assertEquals(listOf("vpn-settings"), calls)
    }

    @Test
    fun alwaysOnSaysTheOsMayRestartTheTunnel() {
        show(connectedState().copy(alwaysOn = true))

        rule.onNodeWithText(text(R.string.dashboard_details_always_on_restart)).assertExists()
    }

    @Test
    fun theSheetKeepsTheProxyOnlyWarningCopy() {
        show(
            DashboardState.fromService(snapshot(state = STATE_IDLE)).copy(
                options = ConnectOptionsState(open = true, choice = LocationChoice.Recommended),
                locations = LocationLoad.Ready(COUNTRIES),
            ),
        )

        rule.onNodeWithText(text(R.string.dashboard_options_proxy_only)).performScrollTo()
            .assertExists()
        rule.onNodeWithText(text(R.string.dashboard_options_proxy_only_text)).performScrollTo()
            .assertExists()
    }

    // ---- helpers ---------------------------------------------------------

    private fun actions() = DashboardActions(
        onPower = { calls += "power" },
        onOpenOptions = { calls += "open-options" },
        onCloseOptions = { calls += "close-options" },
        onSelectRecommended = { calls += "recommended" },
        onSelectSaved = { calls += "saved" },
        onToggleCountry = { calls += "toggle-country-$it" },
        onSelectCity = { country, city -> calls += "city-$country-$city" },
        onToggleProxyOnly = { calls += "proxy-only" },
        onConnectFromOptions = { calls += "connect" },
        onReloadLocations = { calls += "reload" },
        onOpenVpnSettings = { calls += "vpn-settings" },
        onConfirmLockdownDisconnect = { calls += "confirm-lockdown" },
        onCancelLockdown = { calls += "cancel-lockdown" },
        onDismissNotice = { calls += "dismiss" },
        onRetryNotice = { calls += "retry" },
    )

    private fun text(id: Int): String = rule.activity.getString(id)

    private fun connectedState(): DashboardState = DashboardState.fromService(
        ServiceState(
            binding = true,
            engineReady = true,
            status = status(
                state = STATE_CONNECTED,
                location = "us",
                server = "edge.example.net:443",
                exitIp = "203.0.113.7",
            ),
        ),
    ).copy(locations = LocationLoad.Ready(COUNTRIES))

    private fun snapshot(
        state: String,
        permissionNeeded: Boolean = false,
        location: String? = null,
    ) = ServiceState(
        binding = true,
        engineReady = true,
        status = status(state = state, location = location),
        permissionNeeded = permissionNeeded,
    )

    private fun status(
        state: String,
        location: String? = null,
        server: String? = null,
        exitIp: String? = null,
    ) = EngineStatus(
        state = state,
        authenticated = true,
        email = "user@example.com",
        since = System.currentTimeMillis() / 1000 - 3_600,
        tokenExpires = 0L,
        passExpires = 0L,
        killSwitch = true,
        autoconnect = true,
        location = location,
        server = server,
        proxyOnly = state == STATE_PROXY_ONLY,
        quota = if (state == STATE_CONNECTED) {
            Quota(limit = 50L * GIB, remaining = 13_207_024_436L, reset = 1_792_000_000L)
        } else {
            null
        },
        dns = "10.8.0.2",
        ipv6 = "blackholed",
        exitIp = exitIp,
        exitCountry = if (exitIp == null) null else "Netherlands",
        totals = if (state == STATE_CONNECTED) {
            ByteCounters(up = 12_897_485L, down = 47_817_523L)
        } else {
            null
        },
        rates = if (state == STATE_CONNECTED) {
            ByteRates(up = 1.2 * MIB, down = 3.4 * MIB)
        } else {
            null
        },
    )

    private fun palette(theme: ThemeId): KumihoColors = PaletteMapping.fromJson(
        dev.benyamin.kumiho.go.mobile.Mobile.palette(theme.id, KumihoAccent.BLUE.id),
        theme,
        KumihoAccent.BLUE,
    )

    private companion object {
        const val STATE_IDLE = "IDLE"
        const val STATE_CONNECTED = "CONNECTED"
        const val STATE_PROXY_ONLY = "PROXY_ONLY"

        const val GIB = 1024L * 1024L * 1024L
        const val MIB = 1024.0 * 1024.0

        val SERVER = Server(
            hostname = "edge.example.net",
            port = 443,
            quarantined = false,
            protocols = emptyList(),
        )

        val COUNTRIES = listOf(
            Country(
                code = "us",
                name = "United States",
                cities = listOf(
                    City(
                        code = "nyc",
                        name = "New York",
                        servers = listOf(SERVER),
                    ),
                ),
            ),
            Country(
                code = "de",
                name = "Germany",
                cities = listOf(City(code = "ber", name = "Berlin", servers = emptyList())),
            ),
        )
    }
}
