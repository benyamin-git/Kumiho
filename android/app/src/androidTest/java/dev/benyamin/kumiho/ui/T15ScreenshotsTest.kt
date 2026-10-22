package dev.benyamin.kumiho.ui

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
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performScrollToNode
import androidx.test.platform.app.InstrumentationRegistry
import dev.benyamin.kumiho.diagnostics.AndroidFacts
import dev.benyamin.kumiho.engine.Account
import dev.benyamin.kumiho.engine.DoctorCheck
import dev.benyamin.kumiho.engine.DoctorReport
import dev.benyamin.kumiho.engine.DoctorStatus
import dev.benyamin.kumiho.engine.LogEntry
import dev.benyamin.kumiho.engine.LogLevel
import dev.benyamin.kumiho.ui.account.AccountActions
import dev.benyamin.kumiho.ui.account.AccountScreen
import dev.benyamin.kumiho.ui.account.AccountUiState
import dev.benyamin.kumiho.ui.diagnostics.DiagnosticsActions
import dev.benyamin.kumiho.ui.diagnostics.DiagnosticsScreen
import dev.benyamin.kumiho.ui.diagnostics.DiagnosticsTags
import dev.benyamin.kumiho.ui.diagnostics.DiagnosticsUiState
import dev.benyamin.kumiho.ui.logs.LogFilterLevel
import dev.benyamin.kumiho.ui.logs.LogsActions
import dev.benyamin.kumiho.ui.logs.LogsScreen
import dev.benyamin.kumiho.ui.logs.LogsUiState
import dev.benyamin.kumiho.ui.shell.MoreSheet
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.vpn.SplitMode
import java.io.File
import java.time.Instant
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * Renders the T15 screens in Light/Dark/OLED and writes one PNG per (scene,
 * theme) to the app's external files directory. Pull with:
 * `adb pull /sdcard/Android/data/dev.benyamin.kumiho.debug/files/t15-screenshots`
 */
class T15ScreenshotsTest {

    private enum class Scene {
        ACCOUNT_SIGNED_IN,
        ACCOUNT_SIGNED_OUT,
        LOGS,
        DIAGNOSTICS,
        MORE,
    }

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private var theme by mutableStateOf(ThemeId.LIGHT)
    private var scene by mutableStateOf(Scene.ACCOUNT_SIGNED_IN)

    private val output: File
        get() = InstrumentationRegistry.getInstrumentation()
            .targetContext
            .getExternalFilesDir("t15-screenshots")
            ?: error("the app has no external files directory")

    @Test
    fun captureTheT15ScreensInEveryTheme() {
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
                    when (scene) {
                        Scene.ACCOUNT_SIGNED_IN -> AccountScreen(
                            state = AccountUiState(loading = false, account = ACCOUNT),
                            actions = accountActions(),
                        )
                        Scene.ACCOUNT_SIGNED_OUT -> AccountScreen(
                            state = AccountUiState(loading = false, account = null),
                            actions = accountActions(),
                        )
                        Scene.LOGS -> LogsScreen(
                            state = LogsUiState(
                                loading = false,
                                entries = LOG_ENTRIES,
                                level = LogFilterLevel.WARN,
                            ),
                            actions = logsActions(),
                        )
                        Scene.DIAGNOSTICS -> DiagnosticsScreen(
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
                        Scene.MORE -> MoreSheet(
                            onDismiss = {},
                            onOpen = {},
                        )
                    }
                }
            }
        }
        output.mkdirs()

        for (entry in Scene.entries) {
            if (entry != Scene.MORE) {
                rule.runOnIdle {
                    theme = ThemeId.LIGHT
                    scene = entry
                }
                rule.waitForIdle()
                if (entry == Scene.DIAGNOSTICS) {
                    rule.onNodeWithTag(DiagnosticsTags.LIST)
                        .performScrollToNode(hasTestTag(DiagnosticsTags.CHECKS))
                    rule.waitForIdle()
                }
                for (themeEntry in ThemeId.entries) {
                    rule.runOnIdle { theme = themeEntry }
                    rule.waitForIdle()
                    write("t15-${entry.fileName()}-${themeEntry.id}")
                }
            } else {
                captureSheet()
            }
        }
    }

    /**
     * The sheet is a dialog window, so the Compose root capture would only see
     * the background. A full-device screenshot is the honest record.
     */
    private fun captureSheet() {
        rule.runOnIdle {
            theme = ThemeId.LIGHT
            scene = Scene.MORE
        }
        // The sheet animates in from the bottom; wait until it exists and then
        // advance the test clock past the enter animation, so the first
        // (light) capture is not an empty frame.
        rule.waitUntil(timeoutMillis = 5_000) {
            rule.onAllNodesWithText("Diagnostics").fetchSemanticsNodes().isNotEmpty()
        }
        rule.mainClock.advanceTimeBy(1_000)
        rule.waitForIdle()
        // The first full-device capture after the dialog window appears can
        // come back as an unrendered frame, so warm the pipeline up and take
        // the shots in an order that keeps light (the default) last.
        InstrumentationRegistry.getInstrumentation().uiAutomation
            .takeScreenshot()
            ?.recycle()
        for (themeEntry in listOf(ThemeId.DARK, ThemeId.OLED, ThemeId.LIGHT)) {
            rule.runOnIdle { theme = themeEntry }
            rule.waitForIdle()
            val bitmap = InstrumentationRegistry.getInstrumentation()
                .uiAutomation
                .takeScreenshot() ?: error("the device did not return a screenshot")
            val file = File(output, "t15-more-${themeEntry.id}.png")
            file.outputStream().use { stream ->
                bitmap.compress(Bitmap.CompressFormat.PNG, 100, stream)
            }
            assertTrue("a screenshot must not be empty: ${file.absolutePath}", file.length() > 0)
            bitmap.recycle()
        }
    }

    private fun write(name: String) {
        val file = File(output, "$name.png")
        file.outputStream().use { stream ->
            rule.onRoot().captureToImage().asAndroidBitmap()
                .compress(Bitmap.CompressFormat.PNG, 100, stream)
        }
        assertTrue("a screenshot must not be empty: ${file.absolutePath}", file.length() > 0)
    }

    private fun Scene.fileName(): String = when (this) {
        Scene.ACCOUNT_SIGNED_IN -> "account-signed-in"
        Scene.ACCOUNT_SIGNED_OUT -> "account-signed-out"
        Scene.LOGS -> "logs-warn"
        Scene.DIAGNOSTICS -> "diagnostics"
        Scene.MORE -> "more"
    }

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
            version = "0.5.0",
            host = "android",
            os = "android 16",
            time = "2026-10-22T12:00:00Z",
            checks = listOf(
                DoctorCheck(
                    name = "vpn-permission",
                    status = DoctorStatus.OK,
                    detail = "Kumiho may use the VPN.",
                    remediation = null,
                ),
                DoctorCheck(
                    name = "always-on",
                    status = DoctorStatus.OK,
                    detail = "Always-on VPN is on.",
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
                DoctorCheck(
                    name = "dns",
                    status = DoctorStatus.OK,
                    detail = "The tunnel resolver answers.",
                    remediation = null,
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
    }
}
