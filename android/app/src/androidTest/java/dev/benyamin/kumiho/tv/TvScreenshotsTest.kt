package dev.benyamin.kumiho.tv

import android.graphics.Bitmap
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Surface
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.requestFocus
import androidx.test.platform.app.InstrumentationRegistry
import dev.benyamin.kumiho.BuildConfig
import dev.benyamin.kumiho.engine.Account
import dev.benyamin.kumiho.engine.AccountSummary
import dev.benyamin.kumiho.engine.Accounts
import dev.benyamin.kumiho.engine.ByteCounters
import dev.benyamin.kumiho.engine.ByteRates
import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.engine.Protocol
import dev.benyamin.kumiho.engine.Quota
import dev.benyamin.kumiho.engine.Server
import dev.benyamin.kumiho.engine.Settings
import dev.benyamin.kumiho.go.mobile.Mobile
import dev.benyamin.kumiho.ui.account.AccountActions
import dev.benyamin.kumiho.ui.account.AccountSectionUi
import dev.benyamin.kumiho.ui.account.AccountUiState
import dev.benyamin.kumiho.ui.dashboard.ConnectOptionsState
import dev.benyamin.kumiho.ui.dashboard.DashboardActions
import dev.benyamin.kumiho.ui.dashboard.DashboardPhase
import dev.benyamin.kumiho.ui.dashboard.DashboardState
import dev.benyamin.kumiho.ui.dashboard.LocationChoice
import dev.benyamin.kumiho.ui.dashboard.LocationLoad
import dev.benyamin.kumiho.ui.locations.LatencyState
import dev.benyamin.kumiho.ui.locations.LocationTree
import dev.benyamin.kumiho.ui.locations.LocationsActions
import dev.benyamin.kumiho.ui.locations.LocationsState
import dev.benyamin.kumiho.ui.settings.SettingsActions
import dev.benyamin.kumiho.ui.settings.SettingsAppState
import dev.benyamin.kumiho.ui.settings.BatteryOptimization
import dev.benyamin.kumiho.ui.settings.SettingsState
import dev.benyamin.kumiho.ui.shell.Destination
import dev.benyamin.kumiho.ui.shell.ShellNavigation
import dev.benyamin.kumiho.ui.setup.SetupActions
import dev.benyamin.kumiho.ui.setup.SetupState
import dev.benyamin.kumiho.ui.setup.SetupStep
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.ui.tv.TvAppShell
import dev.benyamin.kumiho.ui.tv.dashboard.TvDashboardScreen
import dev.benyamin.kumiho.ui.tv.locations.TvLocationsScreen
import dev.benyamin.kumiho.ui.tv.settings.TvSettingsScreen
import dev.benyamin.kumiho.ui.tv.setup.TvSetupScreen
import dev.benyamin.kumiho.ui.tv.theme.TvKumihoTheme
import dev.benyamin.kumiho.ui.tv.tvRailTag
import dev.benyamin.kumiho.vpn.SplitMode
import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Renders the TV screens in the three house themes and writes one PNG per
 * (screen, theme) pair to the app's external files directory. This is the
 * design self-check for overscan, focus and contrast; pull it with:
 * `adb pull /sdcard/Android/data/dev.benyamin.kumiho.debug/files/tv-screenshots`
 */
class TvScreenshotsTest : TvTestBase() {
    private var theme by mutableStateOf(ThemeId.LIGHT)

    @Test
    fun captureTheHomeSetupLocationsAndSettingsScreens() {
        rule.setContent { CaptureFrame() }
        val output = InstrumentationRegistry.getInstrumentation()
            .targetContext
            .getExternalFilesDir("tv-screenshots")
            ?: error("the app has no external files directory")
        output.mkdirs()

        capture(output, "home")
        show(Screen.Options)
        capture(output, "options")
        show(Screen.Setup)
        capture(output, "setup")
        show(Screen.Locations)
        capture(output, "locations")
        show(Screen.Settings)
        capture(output, "settings")
    }

    private var screen by mutableStateOf(Screen.Home)
    private val nav = ShellNavigation(Destination.Home)

    /** Selects the page and its rail tab; setup lives outside the shell. */
    private fun show(next: Screen) {
        rule.runOnIdle {
            screen = next
            next.destination?.let(nav::selectTab)
        }
        rule.waitForIdle()
        if (next.railFocus) {
            // A page that takes focus by itself (the locations list, the options
            // panel) keeps it; otherwise the remote is still on the rail item.
            rule.onNodeWithTag(tvRailTag(nav.state.value.tab)).requestFocus()
            rule.waitForIdle()
        }
    }

    @androidx.compose.runtime.Composable
    private fun CaptureFrame() {
        TvKumihoTheme(palette(theme)) {
            if (screen == Screen.Setup) {
                Surface(
                    color = androidx.tv.material3.MaterialTheme.colorScheme.background,
                    modifier = Modifier.fillMaxSize(),
                ) {
                    TvSetupScreen(state = SetupState(step = SetupStep.CONSENT), actions = setupActions())
                }
            } else {
                // The real shell, so the capture shows the rail, the top bar and
                // the overscan frame the app draws.
                TvAppShell(
                    nav = nav,
                    home = {
                        if (screen == Screen.Options) {
                            TvDashboardScreen(state = optionsState(), actions = dashboardActions())
                        } else {
                            TvDashboardScreen(state = dashboardState(), actions = dashboardActions())
                        }
                    },
                    locations = {
                        TvLocationsScreen(state = locationsState(), actions = locationsActions())
                    },
                    settings = {
                        TvSettingsScreen(
                            state = settingsState(),
                            appState = appState(),
                            actions = settingsActions(),
                            account = accountSection(),
                        )
                    },
                    logs = {},
                    diagnostics = {},
                )
            }
        }
    }

    private fun capture(output: File, name: String) {
        for (entry in ThemeId.entries) {
            rule.runOnIdle { theme = entry }
            rule.waitForIdle()
            val file = File(output, "t13-$name-${entry.id}.png")
            file.outputStream().use { stream ->
                rule.onRoot().captureToImage().asAndroidBitmap()
                    .compress(Bitmap.CompressFormat.PNG, 100, stream)
            }
            assertTrue("a screenshot must not be empty: ${file.absolutePath}", file.length() > 0)
        }
    }

    private fun dashboardState() = DashboardState(
        phase = DashboardPhase.CONNECTED,
        status = EngineStatus(
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
            totals = ByteCounters(up = 100L * MIB, down = 250L * MIB),
            rates = ByteRates(up = 1.2 * MIB, down = 3.4 * MIB),
        ),
        locations = LocationLoad.Ready(COUNTRIES),
    )

    private fun dashboardActions() = DashboardActions(
        onPower = {},
        onOpenOptions = {},
        onCloseOptions = {},
        onSelectRecommended = {},
        onSelectSaved = {},
        onToggleCountry = {},
        onSelectCity = { _, _ -> },
        onToggleProxyOnly = {},
        onConnectFromOptions = {},
        onReloadLocations = {},
        onOpenVpnSettings = {},
        onConfirmLockdownDisconnect = {},
        onCancelLockdown = {},
        onDismissNotice = {},
        onRetryNotice = {},
    )

    private fun setupActions() = SetupActions(
        onBack = {},
        onContinue = {},
        onFinish = {},
        onFinishAndConnect = {},
        onSetConsent = {},
        onStartBrowser = {},
        onOpenBrowser = {},
        onToggleBrowserPaste = {},
        onBrowserPasteChange = {},
        onSubmitBrowserPaste = {},
        onSubmitBrowserRedirect = {},
        onCancelBrowser = {},
        onCloseBrowserWebView = {},
        onBrowserUnavailable = {},
        onSubmitEmail = {},
        onSubmitPassword = {},
        onSubmitCode = {},
        onRecheckEmailLink = {},
        onRequestVpn = {},
        onRequestNotifications = {},
        onOpenNotificationSettings = {},
        onOpenVpnSettings = {},
    )

    private fun locationsState() = LocationsState(
        loading = false,
        loaded = true,
        countries = COUNTRIES,
        selection = LocationTree.currentSelection(COUNTRIES, "gb", "lon1.example.net:443"),
        latency = mapOf(
            LocationTree.addressKey("lon1.example.net:443") to 42.4,
            LocationTree.addressKey("fra1.example.net:443") to null,
            LocationTree.addressKey("auto.example.net:443") to 17.0,
        ),
    )

    private fun locationsActions() = LocationsActions(
        onQueryChange = {},
        onClearQuery = {},
        onSelect = { _, _, _ -> },
        onRefresh = {},
        onRetryLoad = {},
        onPingAll = {},
        onDismissNotice = {},
        onRetryNotice = {},
    )

    private val releaseVersion = BuildConfig.VERSION_NAME.removeSuffix("-debug")

    private fun settingsState() = SettingsState(
        loading = false,
        loaded = true,
        snapshot = Settings(
            socksPort = 1080,
            mtu = 1500,
            exitCheck = true,
            dohProvider = "cloudflare",
            dnsUpstream = null,
            edgeAddress = null,
            upstreamProxy = null,
            excludeCidrs = emptyList(),
            allowLan = false,
            logLevel = "info",
            redactTargets = true,
            killSwitch = "always-on",
            autoconnect = "on",
            killSwitchEffective = true,
            autoconnectEffective = true,
            quotaPollMinutes = 5,
            overridden = listOf("mtu"),
            tlsSplit = "off",
            httpProxy = false,
            httpProxyPort = 8080,
            solveChallenges = true,
            keepaliveProfile = "standard",
        ),
        appVersion = releaseVersion,
        engineVersion = "v$releaseVersion",
    )

    private fun appState() = SettingsAppState(
        theme = theme,
        accent = KumihoAccent.BLUE,
        accentPreviews = KumihoAccent.entries.associateWith { accent ->
            PaletteMapping.fromJson(Mobile.palette(theme.id, accent.id), theme, accent).primary
        },
        splitMode = SplitMode.EXCLUDE,
        splitAppCount = 3,
        bypassEnabled = true,
        bypassLoaded = true,
        battery = BatteryOptimization.OPTIMIZED,
    )

    private fun accountSection() = AccountSectionUi(
        state = AccountUiState(
            loading = false,
            account = Account(
                email = "user@example.com",
                uid = "9f3c2a7d",
                subscribed = true,
                maxBytes = 50L * GIB,
                limitedBandwidth = false,
                quotaRemaining = 12L * GIB,
                tokenExpires = 1_800_000_000L,
                error = null,
            ),
            accounts = Accounts(
                accounts = listOf(
                    AccountSummary(
                        id = "a",
                        email = "user@example.com",
                        label = "",
                        method = "browser",
                        active = true,
                        reauthRequired = false,
                        lastUsedAt = 1_761_086_400L,
                        quota = null,
                    ),
                ),
                active = "a",
                rotation = false,
                threshold = 10,
                lastMethod = "browser",
                notice = null,
                error = null,
            ),
        ),
        actions = AccountActions(),
    )

    private fun settingsActions() = SettingsActions(
        onToggleAdvanced = {},
        onToggle = { _, _ -> },
        onApply = { _, _ -> },
        onReset = {},
        onClearError = {},
        onRetryLoad = {},
        onRetryNotice = {},
        onDismissNotice = {},
        onThemeSelected = { theme = it },
        onAccentSelected = {},
        onCheckForUpdates = {},
        onOpenRelease = {},
        onOpenNotificationSettings = {},
        onOpenBatterySettings = {},
        onOpenVpnSettings = {},
        onDismissLaunchError = {},
        onToggleBypass = {},
        onOpenSplitTunneling = {},
    )

    private fun optionsState() = dashboardState().copy(
        options = ConnectOptionsState(
            open = true,
            choice = LocationChoice.Saved,
            expanded = setOf("gb"),
        ),
    )

    private enum class Screen(val destination: Destination?, val railFocus: Boolean = true) {
        Home(Destination.Home),
        Options(Destination.Home, railFocus = false),
        Setup(null, railFocus = false),
        Locations(Destination.Locations, railFocus = false),
        Settings(Destination.Settings),
    }

    private companion object {
        const val GIB = 1024L * 1024L * 1024L
        const val MIB = 1024 * 1024
        const val MIB_LONG = 1024L * 1024L

        fun server(host: String, port: Int = 443) = Server(
            hostname = host,
            port = port,
            quarantined = false,
            protocols = listOf(Protocol("connect", host, port, "https")),
        )

        val REC = Country(
            code = "rec",
            name = "Recommended Location",
            cities = listOf(City("rec", "Recommended", listOf(server("auto.example.net", 443)))),
        )
        val GB = Country(
            code = "gb",
            name = "United Kingdom",
            cities = listOf(
                City("lon", "London", listOf(server("lon1.example.net", 443))),
                City("man", "Manchester", listOf(server("man1.example.net", 443))),
            ),
        )
        val DE = Country(
            code = "de",
            name = "Germany",
            cities = listOf(City("fra", "Frankfurt", listOf(server("fra1.example.net", 443)))),
        )
        val COUNTRIES = listOf(REC, GB, DE)
    }
}
