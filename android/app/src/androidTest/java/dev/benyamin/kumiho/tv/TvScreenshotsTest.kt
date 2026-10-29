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
import androidx.compose.ui.test.onRoot
import androidx.test.platform.app.InstrumentationRegistry
import dev.benyamin.kumiho.engine.ByteCounters
import dev.benyamin.kumiho.engine.ByteRates
import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.engine.Protocol
import dev.benyamin.kumiho.engine.Quota
import dev.benyamin.kumiho.engine.Server
import dev.benyamin.kumiho.engine.Settings
import dev.benyamin.kumiho.ui.dashboard.DashboardActions
import dev.benyamin.kumiho.ui.dashboard.DashboardPhase
import dev.benyamin.kumiho.ui.dashboard.DashboardState
import dev.benyamin.kumiho.ui.dashboard.LocationLoad
import dev.benyamin.kumiho.ui.locations.LatencyState
import dev.benyamin.kumiho.ui.locations.LocationTree
import dev.benyamin.kumiho.ui.locations.LocationsActions
import dev.benyamin.kumiho.ui.locations.LocationsState
import dev.benyamin.kumiho.ui.settings.SettingsActions
import dev.benyamin.kumiho.ui.settings.SettingsAppState
import dev.benyamin.kumiho.ui.settings.SettingsState
import dev.benyamin.kumiho.ui.settings.UpdateState
import dev.benyamin.kumiho.ui.setup.SetupActions
import dev.benyamin.kumiho.ui.setup.SetupState
import dev.benyamin.kumiho.ui.setup.SetupStep
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.ui.tv.dashboard.TvDashboardScreen
import dev.benyamin.kumiho.ui.tv.locations.TvLocationsScreen
import dev.benyamin.kumiho.ui.tv.settings.TvSettingsScreen
import dev.benyamin.kumiho.ui.tv.setup.TvSetupScreen
import dev.benyamin.kumiho.ui.tv.theme.TvKumihoTheme
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
        rule.runOnIdle { screen = Screen.Setup }
        capture(output, "setup")
        rule.runOnIdle { screen = Screen.Locations }
        capture(output, "locations")
        rule.runOnIdle { screen = Screen.Settings }
        capture(output, "settings")
    }

    private var screen by mutableStateOf(Screen.Home)

    @androidx.compose.runtime.Composable
    private fun CaptureFrame() {
        TvKumihoTheme(palette(theme)) {
            Surface(
                color = androidx.tv.material3.MaterialTheme.colorScheme.background,
                modifier = Modifier.fillMaxSize(),
            ) {
                when (screen) {
                    Screen.Home -> TvDashboardScreen(state = dashboardState(), actions = dashboardActions())
                    Screen.Setup -> TvSetupScreen(state = SetupState(step = SetupStep.CONSENT), actions = setupActions())
                    Screen.Locations -> TvLocationsScreen(state = locationsState(), actions = locationsActions())
                    Screen.Settings -> TvSettingsScreen(
                        state = settingsState(),
                        appState = SettingsAppState(
                            theme = theme,
                            splitMode = SplitMode.EXCLUDE,
                            splitAppCount = 3,
                            bypassEnabled = true,
                            bypassLoaded = true,
                        ),
                        actions = settingsActions(),
                    )
                }
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
        appVersion = "0.7.0",
        engineVersion = "0.7.0",
        update = UpdateState.Available("0.7.0", "https://example.invalid/release"),
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

    private enum class Screen { Home, Setup, Locations, Settings }

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
