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
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToIndex
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.test.performTextReplacement
import androidx.test.platform.app.InstrumentationRegistry
import dev.benyamin.kumiho.engine.Settings
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * Renders the Settings screen in the three house themes and writes one PNG per
 * (scene, theme) pair to the app's external files directory, so the design
 * self-check looks at what the emulator actually drew.
 *
 * Scenes: the app section with an available update, the Android rows with
 * Advanced open and an overridden row, and the numeric editor holding an inline
 * validation error. This is an extra; it asserts no behaviour. Pull the files
 * with:
 * `adb pull /sdcard/Android/data/dev.benyamin.kumiho.debug/files/settings-screenshots`
 */
class SettingsScreenshotsTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private var theme by mutableStateOf(ThemeId.LIGHT)
    private var state by mutableStateOf(sceneState())
    private var app by mutableStateOf(appState(ThemeId.LIGHT))

    @Test
    fun captureTheSettingsScreensInEveryTheme() {
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
                    SettingsScreen(state = state, appState = app, actions = noOpActions())
                }
            }
        }
        val output = InstrumentationRegistry.getInstrumentation()
            .targetContext
            .getExternalFilesDir("settings-screenshots")
            ?: error("the app has no external files directory")
        output.mkdirs()

        // 1. The top of the list: theme, accent, versions, update, system state.
        rule.runOnIdle {
            state = sceneState()
            app = appState(theme)
        }
        rule.onNodeWithTag(SettingsTags.LIST).performScrollToIndex(0)
        rule.waitForIdle()
        capture(output, "app")

        // 2. The Android rows, the Advanced header and its overridden first row.
        rule.runOnIdle {
            state = sceneState().copy(
                advancedExpanded = true,
                snapshot = SETTINGS.copy(overridden = listOf("edge_address")),
            )
        }
        rule.onNodeWithTag(SettingsTags.LIST).performScrollToIndex(ANDROID_START_INDEX)
        rule.waitForIdle()
        capture(output, "advanced")

        // 3. The MTU editor, kept open by a client validation error.
        rule.runOnIdle { state = sceneState() }
        rule.onNodeWithTag(SettingsTags.LIST)
            .performScrollToNode(hasTestTag(SettingsTags.row("mtu")))
        rule.onNodeWithTag(SettingsTags.row("mtu")).performClick()
        rule.onNodeWithTag(SettingsTags.DIALOG_FIELD).performTextReplacement("abc")
        rule.onNodeWithTag(SettingsTags.DIALOG_APPLY).performClick()
        rule.waitForIdle()
        capture(output, "editor")
    }

    private fun capture(output: File, scene: String) {
        for (entry in ThemeId.entries) {
            rule.runOnIdle {
                theme = entry
                app = appState(entry)
            }
            rule.waitForIdle()
            val file = File(output, "t13-$scene-${entry.id}.png")
            file.outputStream().use { stream ->
                rule.onRoot().captureToImage().asAndroidBitmap()
                    .compress(Bitmap.CompressFormat.PNG, 100, stream)
            }
            assertTrue("a screenshot must not be empty: ${file.absolutePath}", file.length() > 0)
        }
    }

    private fun sceneState() = SettingsState(
        loading = false,
        loaded = true,
        snapshot = SETTINGS,
        update = UpdateState.Available(
            version = "0.6.0",
            url = "https://github.com/benyamin-git/Kumiho/releases/tag/v0.6.0",
        ),
        appVersion = "0.5.0-debug",
        engineVersion = "0.5.0",
    )

    private fun appState(theme: ThemeId) = SettingsAppState(
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
        lockdown = true,
        notificationsAllowed = false,
        batteryUnrestricted = false,
    )

    private fun noOpActions() = SettingsActions(
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
    )

    private companion object {
        /**
         * The kill-switch item's index: eight app items, the Split tunneling
         * row, the Connection header and its seven rows, the Android header and
         * its explanation precede it. Scrolling here frames the two Android
         * rows above the Advanced group.
         */
        const val ANDROID_START_INDEX = 19

        val SETTINGS = Settings(
            socksPort = 1080,
            mtu = 1500,
            exitCheck = true,
            dohProvider = "automatic",
            dnsUpstream = "9.9.9.9",
            edgeAddress = "203.0.113.7",
            upstreamProxy = "http://proxy.example:3128",
            excludeCidrs = listOf("10.0.0.0/8", "192.168.1.0/24"),
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
            httpProxy = true,
            httpProxyPort = 8080,
            solveChallenges = true,
            keepaliveProfile = "standard",
        )
    }
}
