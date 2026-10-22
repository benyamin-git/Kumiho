package dev.benyamin.kumiho.ui.diagnostics

import androidx.activity.ComponentActivity
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import dev.benyamin.kumiho.diagnostics.AndroidFacts
import dev.benyamin.kumiho.engine.DoctorCheck
import dev.benyamin.kumiho.engine.DoctorReport
import dev.benyamin.kumiho.engine.DoctorStatus
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.vpn.SplitMode
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * The diagnostics report as drawn: the service/split cards, the visible check
 * chips with detail and remediation, copy/re-run and the reset confirmation.
 */
class DiagnosticsScreenTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private val calls = mutableListOf<String>()

    private fun show(
        state: DiagnosticsUiState = readyState(),
        serviceBound: Boolean = true,
        splitMode: SplitMode = SplitMode.EXCLUDE,
        splitAppCount: Int = 2,
    ) {
        rule.setContent {
            KumihoTheme(
                colors = PaletteMapping.fromJson(
                    dev.benyamin.kumiho.go.mobile.Mobile.palette(
                        ThemeId.LIGHT.id,
                        KumihoAccent.BLUE.id,
                    ),
                    ThemeId.LIGHT,
                    KumihoAccent.BLUE,
                ),
            ) {
                DiagnosticsScreen(
                    state = state,
                    serviceBound = serviceBound,
                    splitMode = splitMode,
                    splitAppCount = splitAppCount,
                    actions = actions(),
                )
            }
        }
        rule.waitForIdle()
    }

    @Test
    fun theReportRendersItsHeaderServiceSplitAndCheckSections() {
        show()

        rule.onNodeWithTag(DiagnosticsTags.SCREEN).assertIsDisplayed()
        rule.onNodeWithTag(DiagnosticsTags.HEADER).assertIsDisplayed()
        rule.onNodeWithTag(DiagnosticsTags.SERVICE_CARD).assertIsDisplayed()
        rule.onNodeWithText("1 failed · 0 warnings · 0 skipped").assertIsDisplayed()

        scrollTo(DiagnosticsTags.SPLIT_CARD)
        rule.onNodeWithTag(DiagnosticsTags.SPLIT_CARD).assertIsDisplayed()
        rule.onNodeWithText("Excluding 2 app(s)").assertIsDisplayed()

        scrollTo(DiagnosticsTags.CHECKS)
        rule.onNodeWithTag(DiagnosticsTags.CHECKS).assertIsDisplayed()
    }

    @Test
    fun eachCheckChipShowsItsStatusDetailAndRemediation() {
        show()

        for (check in DiagnosticsReducer.visibleChecks(REPORT)) {
            scrollTo(DiagnosticsTags.check(check.name))
            rule.onNodeWithTag(DiagnosticsTags.check(check.name)).assertIsDisplayed()
        }
        rule.onNodeWithText("Grant VPN permission").assertIsDisplayed()
        rule.onNodeWithText("Fix: Allow Kumiho to use the VPN.").assertIsDisplayed()
    }

    @Test
    fun desktopOnlyChecksAreNeverRendered() {
        show()

        scrollTo(DiagnosticsTags.CHECKS)
        rule.onNodeWithTag(DiagnosticsTags.check("platform")).assertDoesNotExist()
        rule.onNodeWithTag(DiagnosticsTags.check("nftables")).assertDoesNotExist()
    }

    @Test
    fun copyAllAndRerunCallTheirActions() {
        show()

        scrollTo(DiagnosticsTags.RERUN)
        rule.onNodeWithTag(DiagnosticsTags.RERUN).performClick()
        scrollTo(DiagnosticsTags.COPY_ALL)
        rule.onNodeWithTag(DiagnosticsTags.COPY_ALL).performClick()
        rule.waitForIdle()

        assertTrue(calls.contains("rerun"))
        assertTrue(calls.contains("copy"))
    }

    @Test
    fun theResetConfirmationAsksFirstAndThenConfirms() {
        show()

        scrollTo(DiagnosticsTags.RESET)
        rule.onNodeWithTag(DiagnosticsTags.RESET).performClick()
        rule.waitForIdle()
        assertTrue("the button only asks", calls.contains("request-reset"))
    }

    @Test
    fun theResetDialogConfirmsAndCancels() {
        show(state = readyState().copy(resetDialog = true))

        rule.onNodeWithTag(DiagnosticsTags.RESET_DIALOG).assertIsDisplayed()
        rule.onNodeWithTag(DiagnosticsTags.RESET_CANCEL).performClick()
        rule.onNodeWithTag(DiagnosticsTags.RESET_CONFIRM).performClick()
        rule.waitForIdle()

        assertTrue(calls.contains("dismiss-reset"))
        assertTrue(calls.contains("confirm-reset"))
    }

    @Test
    fun theSystemShortcutsArePresentAndWired() {
        show()

        for (tag in listOf(
            DiagnosticsTags.OPEN_VPN_SETTINGS,
            DiagnosticsTags.OPEN_NOTIFICATION_SETTINGS,
            DiagnosticsTags.OPEN_BATTERY_SETTINGS,
        )) {
            scrollTo(tag)
            rule.onNodeWithTag(tag).assertIsDisplayed().performClick()
        }
        rule.waitForIdle()

        assertTrue(calls.contains("vpn-settings"))
        assertTrue(calls.contains("notification-settings"))
        assertTrue(calls.contains("battery-settings"))
    }

    @Test
    fun loadingAndErrorSurfacesAreExplicit() {
        show(state = DiagnosticsUiState(loading = true))
        rule.onNodeWithTag(DiagnosticsTags.LOADING).assertIsDisplayed()
    }

    @Test
    fun aLoadErrorOffersARetry() {
        show(state = DiagnosticsUiState(loading = false, error = "the doctor is unavailable"))

        rule.onNodeWithTag(DiagnosticsTags.ERROR).assertIsDisplayed()
        rule.onNodeWithText("Retry").performClick()
        rule.waitForIdle()
        assertTrue(calls.contains("rerun"))
    }

    private fun scrollTo(tag: String) {
        rule.onNodeWithTag(DiagnosticsTags.LIST).performScrollToNode(hasTestTag(tag))
        rule.waitForIdle()
    }

    private fun actions() = DiagnosticsActions(
        onRerun = { calls += "rerun" },
        onCopyReport = { calls += "copy" },
        onDismissMessage = { calls += "dismiss" },
        onOpenVpnSettings = { calls += "vpn-settings" },
        onOpenNotificationSettings = { calls += "notification-settings" },
        onOpenBatterySettings = { calls += "battery-settings" },
        onRequestReset = { calls += "request-reset" },
        onDismissReset = { calls += "dismiss-reset" },
        onConfirmReset = { calls += "confirm-reset" },
    )

    private fun readyState() = DiagnosticsUiState(
        loading = false,
        report = REPORT,
        facts = FACTS,
    )

    private companion object {
        val REPORT = DoctorReport(
            version = "0.5.0",
            host = "android",
            os = "android",
            time = "2026-10-22T12:00:00Z",
            checks = listOf(
                DoctorCheck(
                    name = "vpn-permission",
                    status = DoctorStatus.OK,
                    detail = "Grant VPN permission",
                    remediation = null,
                ),
                DoctorCheck(
                    name = "dns",
                    status = DoctorStatus.FAIL,
                    detail = "Private DNS blocks the resolver",
                    remediation = "Allow Kumiho to use the VPN.",
                ),
                DoctorCheck(
                    name = "platform",
                    status = DoctorStatus.FAIL,
                    detail = "linux only",
                    remediation = null,
                ),
                DoctorCheck(
                    name = "nftables",
                    status = DoctorStatus.FAIL,
                    detail = "linux only",
                    remediation = null,
                ),
            ),
            failed = 3,
            warned = 0,
            skipped = 0,
        )

        val FACTS = AndroidFacts(
            device = "Google Pixel 8",
            sdk = 36,
            vpnPermission = true,
            alwaysOn = true,
            network = true,
            dnsServers = listOf("10.8.0.2"),
            privateDnsMode = AndroidFacts.PRIVATE_DNS_OFF,
            socksPortListening = true,
            serviceRunning = true,
            engineState = "connected",
        )
    }
}
