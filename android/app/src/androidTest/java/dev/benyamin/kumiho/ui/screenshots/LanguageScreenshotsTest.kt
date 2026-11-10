package dev.benyamin.kumiho.ui.screenshots

import android.graphics.Bitmap
import androidx.activity.compose.LocalActivityResultRegistryOwner
import androidx.activity.result.ActivityResultRegistryOwner
import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.unit.LayoutDirection
import androidx.test.platform.app.InstrumentationRegistry
import dev.benyamin.kumiho.data.AppLanguage
import dev.benyamin.kumiho.diagnostics.AndroidFacts
import dev.benyamin.kumiho.engine.Account
import dev.benyamin.kumiho.engine.ByteCounters
import dev.benyamin.kumiho.engine.ByteRates
import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.DoctorCheck
import dev.benyamin.kumiho.engine.DoctorReport
import dev.benyamin.kumiho.engine.DoctorStatus
import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.engine.LogEntry
import dev.benyamin.kumiho.engine.LogLevel
import dev.benyamin.kumiho.engine.Protocol
import dev.benyamin.kumiho.engine.Quota
import dev.benyamin.kumiho.engine.Server
import dev.benyamin.kumiho.engine.Settings
import dev.benyamin.kumiho.go.mobile.Mobile
import dev.benyamin.kumiho.ui.account.AccountActions
import dev.benyamin.kumiho.ui.account.AccountScreen
import dev.benyamin.kumiho.ui.account.AccountUiState
import dev.benyamin.kumiho.ui.dashboard.DashboardActions
import dev.benyamin.kumiho.ui.dashboard.DashboardPhase
import dev.benyamin.kumiho.ui.dashboard.DashboardScreen
import dev.benyamin.kumiho.ui.dashboard.DashboardState
import dev.benyamin.kumiho.ui.dashboard.LocationLoad
import dev.benyamin.kumiho.ui.diagnostics.DiagnosticsActions
import dev.benyamin.kumiho.ui.diagnostics.DiagnosticsScreen
import dev.benyamin.kumiho.ui.diagnostics.DiagnosticsUiState
import dev.benyamin.kumiho.ui.locations.LocationTree
import dev.benyamin.kumiho.ui.locations.LocationsActions
import dev.benyamin.kumiho.ui.locations.LocationsScreen
import dev.benyamin.kumiho.ui.locations.LocationsState
import dev.benyamin.kumiho.ui.logs.LogFilterLevel
import dev.benyamin.kumiho.ui.logs.LogsActions
import dev.benyamin.kumiho.ui.logs.LogsScreen
import dev.benyamin.kumiho.ui.logs.LogsUiState
import dev.benyamin.kumiho.ui.settings.BatteryOptimization
import dev.benyamin.kumiho.ui.settings.SettingsActions
import dev.benyamin.kumiho.ui.settings.SettingsAppState
import dev.benyamin.kumiho.ui.settings.SettingsScreen
import dev.benyamin.kumiho.ui.settings.SettingsState
import dev.benyamin.kumiho.ui.setup.SetupFlow
import dev.benyamin.kumiho.ui.setup.SetupViewModel
import dev.benyamin.kumiho.ui.shell.AppShell
import dev.benyamin.kumiho.ui.shell.Destination
import dev.benyamin.kumiho.ui.shell.MoreRoute
import dev.benyamin.kumiho.ui.shell.ShellNavState
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoColors
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.vpn.SplitMode
import java.io.File
import java.time.Instant
import java.util.Locale
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * Persian phone captures for the v0.12.0 review gate and the Persian README.
 * Every screen is rendered in the real shell with the real Go palette, a
 * localized Persian context and RTL. The files land in the app's external
 * files directory:
 *
 * `adb pull /sdcard/Android/data/dev.benyamin.kumiho.debug/files/language-screenshots-phone`
 */
class LanguageScreenshotsTest {

    private enum class Scene {
        SETUP,
        DASHBOARD,
        LOCATIONS,
        SETTINGS,
        ACCOUNT,
        LOGS,
        DIAGNOSTICS,
    }

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private var scene by androidx.compose.runtime.mutableStateOf(Scene.DASHBOARD)

    private val output: File
        get() = InstrumentationRegistry.getInstrumentation()
            .targetContext
            .getExternalFilesDir("language-screenshots-phone")
            ?: error("the app has no external files directory")

    @Test
    fun captureThePersianPhoneScreens() {
        val setup = SetupViewModel(
            engine = dev.benyamin.kumiho.ui.setup.FakeSetupEngine(),
            permissions = dev.benyamin.kumiho.ui.setup.FakeSetupPermissions(),
            persistSetupComplete = {},
            connectNow = {},
            systemLanguage = { "fa" },
            initialLanguagePreference = "system",
            initialEffectiveLanguage = AppLanguage.PERSIAN,
        )
        rule.setContent {
            PersianPhoneFrame {
                when (scene) {
                    Scene.SETUP -> SetupFlow(viewModel = setup)
                    Scene.DASHBOARD -> Shell(
                        state = ShellNavState(tab = Destination.Home),
                        content = {
                            DashboardScreen(state = dashboardState(), actions = dashboardActions())
                        },
                    )
                    Scene.LOCATIONS -> Shell(
                        state = ShellNavState(tab = Destination.Locations),
                        content = {
                            LocationsScreen(state = locationsState(), actions = locationsActions())
                        },
                    )
                    Scene.SETTINGS -> Shell(
                        state = ShellNavState(tab = Destination.Settings),
                        content = {
                            SettingsScreen(
                                state = settingsState(),
                                appState = appState(),
                                actions = settingsActions(),
                            )
                        },
                    )
                    Scene.ACCOUNT -> Shell(
                        state = ShellNavState(tab = Destination.Account),
                        content = {
                            AccountScreen(
                                state = AccountUiState(loading = false, account = ACCOUNT),
                                actions = accountActions(),
                            )
                        },
                    )
                    Scene.LOGS -> Shell(
                        state = ShellNavState(tab = Destination.More, moreRoute = MoreRoute.Logs),
                        content = {
                            LogsScreen(
                                state = LogsUiState(loading = false, entries = LOG_ENTRIES),
                                actions = logsActions(),
                            )
                        },
                    )
                    Scene.DIAGNOSTICS -> Shell(
                        state = ShellNavState(tab = Destination.More, moreRoute = MoreRoute.Diagnostics),
                        content = {
                            DiagnosticsScreen(
                                state = DiagnosticsUiState(
                                    loading = false,
                                    report = REPORT,
                                    facts = FACTS,
                                ),
                                serviceBound = true,
                                splitMode = SplitMode.EXCLUDE,
                                splitAppCount = 2,
                                actions = diagnosticsActions(),
                            )
                        },
                    )
                }
            }
        }
        output.mkdirs()
        rule.waitForIdle()

        for (entry in Scene.entries) {
            rule.runOnIdle { scene = entry }
            rule.waitForIdle()
            write(entry.name.lowercase())
        }
    }

    @Composable
    private fun Shell(state: ShellNavState, content: @Composable () -> Unit) {
        AppShell(
            state = state,
            onSelectTab = {},
            onOpenMoreRoute = {},
            onBack = { false },
            home = content,
            locations = content,
            account = content,
            settings = content,
            logs = content,
            diagnostics = content,
        )
    }

    private fun write(name: String) {
        val file = File(output, "fa-$name.png")
        file.outputStream().use { stream ->
            rule.onRoot().captureToImage().asAndroidBitmap()
                .compress(Bitmap.CompressFormat.PNG, 100, stream)
        }
        assertTrue("a screenshot must not be empty: ${file.absolutePath}", file.length() > 0)
    }

    @Composable
    private fun PersianPhoneFrame(content: @Composable () -> Unit) {
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
            LocalActivityResultRegistryOwner provides (base as ActivityResultRegistryOwner),
            LocalContext provides localized,
            LocalConfiguration provides localized.resources.configuration,
            LocalLayoutDirection provides LayoutDirection.Rtl,
        ) {
            KumihoTheme(colors = palette(), language = AppLanguage.PERSIAN) {
                androidx.compose.material3.Surface(
                    modifier = Modifier.fillMaxSize(),
                ) {
                    content()
                }
            }
        }
    }

    private fun palette(): KumihoColors = PaletteMapping.fromJson(
        Mobile.palette(ThemeId.LIGHT.id, KumihoAccent.BLUE.id),
        ThemeId.LIGHT,
        KumihoAccent.BLUE,
    )

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

    private fun settingsState() = SettingsState(
        loading = false,
        loaded = true,
        snapshot = SETTINGS,
        appVersion = "0.12.0",
        engineVersion = "v0.12.0",
    )

    private fun appState() = SettingsAppState(
        theme = ThemeId.LIGHT,
        accent = KumihoAccent.BLUE,
        accentPreviews = KumihoAccent.entries.associateWith { accent ->
            PaletteMapping.fromJson(
                Mobile.palette(ThemeId.LIGHT.id, accent.id),
                ThemeId.LIGHT,
                accent,
            ).primary
        },
        splitMode = SplitMode.EXCLUDE,
        splitAppCount = 3,
        bypassEnabled = true,
        bypassLoaded = true,
        battery = BatteryOptimization.OPTIMIZED,
        languagePreference = "system",
        effectiveLanguage = AppLanguage.PERSIAN,
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

    private fun accountActions() = AccountActions(
        onRefresh = {},
        onRetry = {},
        onDismissMessage = {},
        onSubmitEmail = {},
        onSubmitPassword = {},
        onSubmitCode = {},
        onRecheck = {},
        onSignOut = {},
        onDismissSignOut = {},
        onConfirmSignOut = {},
    )

    private fun logsActions() = LogsActions(
        onLevelSelected = {},
        onSearchChange = {},
        onClearSearch = {},
        onFollowChange = {},
        onClear = {},
        onCopyLine = {},
        onShare = {},
        onDismissMessage = {},
        onRetry = {},
    )

    private fun diagnosticsActions() = DiagnosticsActions(
        onRerun = {},
        onCopyReport = {},
        onDismissMessage = {},
        onOpenVpnSettings = {},
        onOpenNotificationSettings = {},
        onOpenBatterySettings = {},
        onRequestReset = {},
        onDismissReset = {},
        onConfirmReset = {},
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

        val ACCOUNT = Account(
            email = "user@example.com",
            uid = "9f3c2a7d",
            subscribed = true,
            maxBytes = 50L * 1024 * 1024 * 1024,
            limitedBandwidth = false,
            quotaRemaining = 12L * 1024 * 1024 * 1024,
            tokenExpires = Instant.parse("2026-11-21T12:00:00Z").epochSecond,
            error = null,
        )

        val LOG_ENTRIES = listOf(
            LogEntry(
                time = Instant.parse("2026-10-22T12:00:01Z"),
                level = LogLevel.WARN,
                tag = "net",
                message = "the edge answered with a challenge",
            ),
            LogEntry(
                time = Instant.parse("2026-10-22T12:00:09Z"),
                level = LogLevel.ERROR,
                tag = "auth",
                message = "the account token could not be refreshed; retrying",
            ),
            LogEntry(
                time = Instant.parse("2026-10-22T12:00:14Z"),
                level = LogLevel.WARN,
                tag = "dns",
                message = "private DNS blocked the tunnel resolver",
            ),
        )

        val REPORT = DoctorReport(
            version = "0.12.0",
            host = "android",
            os = "android 16",
            time = "2026-11-10T12:00:00Z",
            checks = listOf(
                DoctorCheck(
                    name = "vpn-permission",
                    status = DoctorStatus.OK,
                    detail = "Kumiho may use the VPN.",
                    remediation = null,
                ),
                DoctorCheck(
                    name = "notifications",
                    status = DoctorStatus.WARN,
                    detail = "Notifications are blocked.",
                    remediation = "Allow notifications for Kumiho.",
                ),
                DoctorCheck(
                    name = "battery-optimization",
                    status = DoctorStatus.FAIL,
                    detail = "Battery optimization may stop the tunnel.",
                    remediation = "Set Kumiho to unrestricted.",
                ),
            ),
            failed = 1,
            warned = 1,
            skipped = 0,
        )

        val FACTS = AndroidFacts(
            device = "Google Pixel 8",
            sdk = 36,
            vpnPermission = true,
            alwaysOn = true,
            notifications = false,
            batteryOptimized = true,
            network = true,
            dnsServers = listOf("10.8.0.2"),
            privateDnsMode = AndroidFacts.PRIVATE_DNS_OFF,
            socksPortListening = true,
            httpProxyPortListening = false,
            serviceRunning = true,
            engineState = "connected",
        )

        val SETTINGS = Settings(
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
        )
    }
}
