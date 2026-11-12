package dev.benyamin.kumiho.tv

import android.graphics.Bitmap
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.requiredSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import androidx.test.platform.app.InstrumentationRegistry
import dev.benyamin.kumiho.BuildConfig
import dev.benyamin.kumiho.data.AppLanguage
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
import dev.benyamin.kumiho.ui.account.AccountActions
import dev.benyamin.kumiho.ui.account.AccountSectionUi
import dev.benyamin.kumiho.ui.account.AccountUiState
import dev.benyamin.kumiho.ui.dashboard.DashboardActions
import dev.benyamin.kumiho.ui.dashboard.DashboardPhase
import dev.benyamin.kumiho.ui.dashboard.DashboardState
import dev.benyamin.kumiho.ui.dashboard.LocationLoad
import dev.benyamin.kumiho.ui.locations.LocationTree
import dev.benyamin.kumiho.ui.locations.LocationsActions
import dev.benyamin.kumiho.ui.locations.LocationsState
import dev.benyamin.kumiho.ui.settings.BatteryOptimization
import dev.benyamin.kumiho.ui.settings.SettingsActions
import dev.benyamin.kumiho.ui.settings.SettingsAppState
import dev.benyamin.kumiho.ui.settings.SettingsState
import dev.benyamin.kumiho.ui.setup.SetupActions
import dev.benyamin.kumiho.ui.setup.SetupState
import dev.benyamin.kumiho.ui.setup.SetupStep
import dev.benyamin.kumiho.ui.shell.Destination
import dev.benyamin.kumiho.ui.shell.ShellNavigation
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.ui.tv.TvAppShell
import dev.benyamin.kumiho.ui.tv.dashboard.TvDashboardScreen
import dev.benyamin.kumiho.ui.tv.locations.TvLocationsScreen
import dev.benyamin.kumiho.ui.tv.settings.TvSettingsScreen
import dev.benyamin.kumiho.ui.tv.setup.TvSetupScreen
import dev.benyamin.kumiho.ui.tv.theme.TvKumihoTheme
import dev.benyamin.kumiho.vpn.SplitMode
import java.io.File
import java.util.Locale
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Persian TV captures for the review gate and the Persian README. The phone
 * suite skips this class through [TvTestBase]. Pull the files with:
 *
 * `adb pull /sdcard/Android/data/dev.benyamin.kumiho.debug/files/language-screenshots-tv`
 */
class LanguageTvScreenshotsTest : TvTestBase() {

    private enum class Scene(val destination: Destination?) {
        HOME(Destination.Home),
        LOCATIONS(Destination.Locations),
        SETTINGS(Destination.Settings),
        SETUP(null),
    }

    private var scene by mutableStateOf(Scene.HOME)
    private val nav = ShellNavigation(Destination.Home)

    private val output: File
        get() = InstrumentationRegistry.getInstrumentation()
            .targetContext
            .getExternalFilesDir("language-screenshots-tv")
            ?: error("the app has no external files directory")

    @Test
    fun captureThePersianTvScreens() {
        rule.setContent { PersianTvFrame { CaptureFrame() } }
        output.mkdirs()

        for (entry in Scene.entries) {
            rule.runOnIdle {
                scene = entry
                entry.destination?.let(nav::selectTab)
            }
            rule.waitForIdle()
            write(entry.name.lowercase())
        }
    }

    @Composable
    private fun CaptureFrame() {
        TvKumihoTheme(
            colors = palette(ThemeId.LIGHT),
            language = AppLanguage.PERSIAN,
        ) {
            if (scene == Scene.SETUP) {
                Box(modifier = Modifier.requiredSize(960.dp, 540.dp)) {
                    TvSetupScreen(
                        state = SetupState(
                            step = SetupStep.CONSENT,
                            languagePreference = "system",
                            effectiveLanguage = AppLanguage.PERSIAN,
                        ),
                        actions = setupActions(),
                    )
                }
            } else {
                Box(modifier = Modifier.requiredSize(960.dp, 540.dp)) {
                    TvAppShell(
                        nav = nav,
                        home = {
                            TvDashboardScreen(
                                state = dashboardState(),
                                actions = dashboardActions(),
                            )
                        },
                        locations = {
                            TvLocationsScreen(
                                state = locationsState(),
                                actions = locationsActions(),
                            )
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
    }

    @Composable
    private fun PersianTvFrame(content: @Composable () -> Unit) {
        val base = LocalContext.current
        val baseConfiguration = LocalConfiguration.current
        val localized = remember(baseConfiguration) {
            val locale = Locale.forLanguageTag("fa")
            val configuration = android.content.res.Configuration(baseConfiguration).apply {
                setLocale(locale)
                setLayoutDirection(locale)
            }
            base.createConfigurationContext(configuration)
        }
        CompositionLocalProvider(
            LocalContext provides localized,
            LocalConfiguration provides localized.resources.configuration,
            LocalLayoutDirection provides LayoutDirection.Rtl,
        ) {
            androidx.compose.material3.Surface(modifier = Modifier.fillMaxSize()) {
                content()
            }
        }
    }

    private fun write(name: String) {
        val file = File(output, "fa-tv-$name.png")
        file.outputStream().use { stream ->
            rule.onRoot().captureToImage().asAndroidBitmap()
                .compress(Bitmap.CompressFormat.PNG, 100, stream)
        }
        assertTrue("a screenshot must not be empty: ${file.absolutePath}", file.length() > 0)
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

    private fun locationsState() = LocationsState(
        loading = false,
        loaded = true,
        countries = COUNTRIES,
        selection = LocationTree.currentSelection(COUNTRIES, "gb", "lon1.example.net:443"),
        latency = mapOf(
            LocationTree.addressKey("lon1.example.net:443") to 42.4,
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
        theme = ThemeId.LIGHT,
        accent = KumihoAccent.BLUE,
        accentPreviews = KumihoAccent.entries.associateWith { accent ->
            palette(ThemeId.LIGHT).primary
        },
        splitMode = SplitMode.EXCLUDE,
        splitAppCount = 3,
        bypassEnabled = true,
        bypassLoaded = true,
        battery = BatteryOptimization.OPTIMIZED,
        languagePreference = "system",
        effectiveLanguage = AppLanguage.PERSIAN,
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
        onThemeSelected = {},
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

    private companion object {
        const val GIB = 1024L * 1024L * 1024L
        const val MIB = 1024L * 1024L

        fun server(host: String, port: Int = 443) = Server(
            hostname = host,
            port = port,
            quarantined = false,
            protocols = listOf(Protocol("connect", host, port, "https")),
        )

        val REC = Country(
            code = "rec",
            name = "Recommended Location",
            cities = listOf(City("rec", "Recommended", listOf(server("auto.example.net")))),
        )
        val GB = Country(
            code = "gb",
            name = "United Kingdom",
            cities = listOf(
                City("lon", "London", listOf(server("lon1.example.net"))),
                City("man", "Manchester", listOf(server("man1.example.net"))),
            ),
        )
        val DE = Country(
            code = "de",
            name = "Germany",
            cities = listOf(City("fra", "Frankfurt", listOf(server("fra1.example.net")))),
        )
        val COUNTRIES = listOf(REC, GB, DE)
    }
}
