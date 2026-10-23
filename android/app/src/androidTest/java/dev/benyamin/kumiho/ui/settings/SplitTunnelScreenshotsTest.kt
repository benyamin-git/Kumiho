package dev.benyamin.kumiho.ui.settings

import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performScrollToIndex
import androidx.compose.ui.test.performScrollToNode
import androidx.test.platform.app.InstrumentationRegistry
import dev.benyamin.kumiho.data.InstalledApp
import dev.benyamin.kumiho.engine.Settings
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.vpn.SplitMode
import dev.benyamin.kumiho.vpn.SplitSettings
import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * Renders the split screen and the Settings shortcut in Light/Dark/OLED and
 * writes one PNG per (scene, theme) to the app's external files directory.
 * T13's `settings-screenshots` are untouched; these use `t14-*` names:
 * `adb pull /sdcard/Android/data/dev.benyamin.kumiho.debug/files/split-screenshots`
 */
class SplitTunnelScreenshotsTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private var theme by mutableStateOf(ThemeId.LIGHT)
    private var split by mutableStateOf(uiState())
    private var requiresReconnect by mutableStateOf(false)
    private var settingsPage by mutableStateOf(settingsState())
    private var settingsApp by mutableStateOf(appState(ThemeId.LIGHT, SplitMode.EXCLUDE, 2))

    private val output: File
        get() = InstrumentationRegistry.getInstrumentation()
            .targetContext
            .getExternalFilesDir("split-screenshots")
            ?: error("the app has no external files directory")

    @Test
    fun captureTheSplitScreensInEveryTheme() {
        rule.setContent {
            KumihoTheme(
                colors = PaletteMapping.fromJson(
                    dev.benyamin.kumiho.go.mobile.Mobile.palette(theme.id, KumihoAccent.BLUE.id),
                    theme,
                    KumihoAccent.BLUE,
                ),
            ) {
                Surface(
                    color = MaterialTheme.colorScheme.background,
                    modifier = Modifier.fillMaxSize(),
                ) {
                    SplitTunnelScreen(
                        state = split,
                        reconnectRequired = requiresReconnect,
                        actions = noOpActions(),
                        appIcon = { FakeAppIcon(it) },
                    )
                }
            }
        }
        output.mkdirs()

        // 1. Off: every app tunnels, the picker is disabled, Kumiho is outside.
        captureSplit(
            scene = "off",
            state = uiState(settings = SplitSettings(SplitMode.OFF)),
        )

        // 2. Exclude with a selection, Kumiho pinned as always outside, and a
        // live tunnel asking for a reconnect.
        captureSplit(
            scene = "exclude",
            state = uiState(
                settings = SplitSettings(
                    SplitMode.EXCLUDE,
                    setOf(CHROME.packageName, MAPS.packageName),
                ),
            ),
            reconnect = true,
        )

        // 3. Include with no apps: the honest all-apps-tunnel warning.
        captureSplit(
            scene = "include-empty",
            state = uiState(settings = SplitSettings(SplitMode.INCLUDE)),
        )

        // 4. Include with two apps: Kumiho omitted, both checks on.
        captureSplit(
            scene = "include",
            state = uiState(
                settings = SplitSettings(
                    SplitMode.INCLUDE,
                    setOf(CHROME.packageName, MAPS.packageName),
                ),
            ),
        )
    }

    @Test
    fun captureTheSplitRowInSettingsInEveryTheme() {
        rule.setContent {
            KumihoTheme(
                colors = PaletteMapping.fromJson(
                    dev.benyamin.kumiho.go.mobile.Mobile.palette(theme.id, KumihoAccent.BLUE.id),
                    theme,
                    KumihoAccent.BLUE,
                ),
            ) {
                Surface(
                    color = MaterialTheme.colorScheme.background,
                    modifier = Modifier.fillMaxSize(),
                ) {
                    SettingsScreen(
                        state = settingsPage,
                        appState = settingsApp,
                        actions = settingsActions(),
                    )
                }
            }
        }
        output.mkdirs()

        rule.onNodeWithTag(SettingsTags.LIST)
            .performScrollToNode(hasTestTag(SettingsTags.SPLIT_ROW))
        rule.waitForIdle()

        for (entry in ThemeId.entries) {
            rule.runOnIdle {
                theme = entry
                settingsApp = appState(entry, SplitMode.EXCLUDE, 2)
            }
            rule.waitForIdle()
            write(output, "t14-settings-split-${entry.id}")
        }
    }

    private fun captureSplit(
        scene: String,
        state: SplitTunnelUiState,
        reconnect: Boolean = false,
    ) {
        rule.runOnIdle {
            theme = ThemeId.LIGHT
            split = state
            requiresReconnect = reconnect
        }
        rule.waitForIdle()
        rule.onNodeWithTag(SplitTunnelTags.LIST).performScrollToIndex(0)
        rule.waitForIdle()

        for (entry in ThemeId.entries) {
            rule.runOnIdle { theme = entry }
            rule.waitForIdle()
            write(output, "t14-$scene-${entry.id}")
        }
    }

    private fun write(output: File, name: String) {
        val file = File(output, "$name.png")
        file.outputStream().use { stream ->
            rule.onRoot().captureToImage().asAndroidBitmap()
                .compress(Bitmap.CompressFormat.PNG, 100, stream)
        }
        assertTrue("a screenshot must not be empty: ${file.absolutePath}", file.length() > 0)
    }

    // ---- fixtures --------------------------------------------------------

    private fun uiState(
        settings: SplitSettings = SplitSettings(),
    ) = SplitTunnelUiState(
        settings = settings,
        loading = false,
        apps = APPS,
        appsLoading = false,
    )

    private fun settingsState() = SettingsState(
        loading = false,
        loaded = true,
        snapshot = SETTINGS,
        appVersion = "0.5.0-debug",
        engineVersion = "0.5.0",
    )

    private fun appState(
        theme: ThemeId,
        mode: SplitMode,
        count: Int,
    ) = SettingsAppState(
        theme = theme,
        accent = KumihoAccent.BLUE,
        accentPreviews = KumihoAccent.entries.associateWith { accent ->
            PaletteMapping.fromJson(
                dev.benyamin.kumiho.go.mobile.Mobile.palette(theme.id, accent.id),
                theme,
                accent,
            ).primary
        },
        alwaysOn = true,
        lockdown = false,
        notificationsAllowed = true,
        batteryUnrestricted = false,
        splitMode = mode,
        splitAppCount = count,
        bypassLoaded = true,
    )

    private fun noOpActions() = SplitTunnelActions(
        onModeSelected = {},
        onTogglePackage = {},
        onSearchChange = {},
        onClearSearch = {},
        onShowSystem = {},
        onRetryRepository = {},
        onRetryApps = {},
        onBack = {},
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

    private companion object {
        val SELF = InstalledApp(
            packageName = "dev.benyamin.kumiho.debug",
            label = "Kumiho",
            isSystem = false,
            launchable = true,
            isSelf = true,
        )
        val CHROME = InstalledApp(
            packageName = "com.android.chrome",
            label = "Chrome",
            isSystem = false,
            launchable = true,
        )
        val MAPS = InstalledApp(
            packageName = "com.example.maps",
            label = "Maps",
            isSystem = false,
            launchable = true,
        )
        val SETTINGS_APP = InstalledApp(
            packageName = "com.android.settings",
            label = "Settings",
            isSystem = true,
            launchable = false,
        )
        val APPS = listOf(SELF, CHROME, MAPS, SETTINGS_APP)

        val SETTINGS = Settings(
            socksPort = 1080,
            mtu = 1500,
            exitCheck = true,
            dohProvider = "automatic",
            dnsUpstream = "9.9.9.9",
            edgeAddress = "203.0.113.7",
            upstreamProxy = "http://proxy.example:3128",
            excludeCidrs = listOf("10.0.0.0/8"),
            allowLan = false,
            logLevel = "info",
            redactTargets = true,
            killSwitch = "on",
            autoconnect = "on",
            killSwitchEffective = true,
            autoconnectEffective = true,
            quotaPollMinutes = 30,
            overridden = emptyList(),
            tlsSplit = "first-byte",
            httpProxy = false,
            httpProxyPort = 8080,
            solveChallenges = true,
            keepaliveProfile = "standard",
        )
    }
}
