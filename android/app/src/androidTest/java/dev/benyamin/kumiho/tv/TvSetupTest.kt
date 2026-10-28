package dev.benyamin.kumiho.tv

import androidx.activity.ComponentActivity
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import dev.benyamin.kumiho.go.mobile.Mobile
import dev.benyamin.kumiho.ui.setup.PermissionState
import dev.benyamin.kumiho.ui.setup.SetupActions
import dev.benyamin.kumiho.ui.setup.SetupState
import dev.benyamin.kumiho.ui.setup.SetupStep
import dev.benyamin.kumiho.ui.setup.SetupTags
import dev.benyamin.kumiho.ui.setup.SignInPaneState
import dev.benyamin.kumiho.ui.setup.SignInPhase
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoColors
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.ui.tv.setup.TvSetupScreen
import dev.benyamin.kumiho.ui.tv.theme.TvKumihoTheme
import org.junit.Assert.assertEquals
import org.junit.Assume.assumeTrue
import org.junit.Rule
import org.junit.Test

/**
 * The TV setup chrome: the consent tick is required, the browser-first sign-in
 * controls are present, and the last step offers Finish and Connect now only
 * when they can work. TV only; skipped on a phone emulator.
 */
class TvSetupTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private fun palette(): KumihoColors = PaletteMapping.fromJson(
        Mobile.palette(ThemeId.LIGHT.id, KumihoAccent.BLUE.id),
        ThemeId.LIGHT,
        KumihoAccent.BLUE,
    )

    private fun show(initial: SetupState, onAction: (String) -> Unit = {}) {
        rule.setContent {
            var state by remember { mutableStateOf(initial) }
            val actions = SetupActions(
                onBack = { onAction("back") },
                onContinue = {
                    onAction("continue")
                    state = state.copy(step = SetupStep.SIGN_IN)
                },
                onFinish = { onAction("finish") },
                onFinishAndConnect = { onAction("connect") },
                onSetConsent = { accepted ->
                    onAction("consent")
                    state = state.copy(consentAccepted = accepted)
                },
                onStartBrowser = { onAction("browser") },
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
                onRequestVpn = { onAction("vpn") },
                onRequestNotifications = {},
                onOpenNotificationSettings = {},
                onOpenVpnSettings = {},
            )
            TvKumihoTheme(palette()) {
                TvSetupScreen(state = state, actions = actions)
            }
        }
    }

    @Test
    fun consentIsRequiredBeforeContinueEnables() {
        assumeTrue(TvEnvironment.isTelevision(rule.activity))
        show(SetupState())
        rule.onNodeWithTag(SetupTags.CONSENT_STEP).assertIsDisplayed()
        rule.onNodeWithTag(SetupTags.CONTINUE).assertIsNotEnabled()
        rule.onNodeWithTag(SetupTags.CONSENT_ACCEPT).performClick()
        rule.waitForIdle()
        rule.onNodeWithTag(SetupTags.CONTINUE).assertIsDisplayed()
        rule.onNodeWithTag(SetupTags.CONTINUE).performClick()
        rule.waitForIdle()
        rule.onNodeWithTag(SetupTags.SIGN_IN_STEP).assertIsDisplayed()
    }

    @Test
    fun browserFirstSignInIsVisibleAndReachable() {
        assumeTrue(TvEnvironment.isTelevision(rule.activity))
        var actions = 0
        show(SetupState(step = SetupStep.SIGN_IN)) { actions++ }
        rule.onNodeWithTag(SetupTags.SIGN_IN_BROWSER).assertIsDisplayed()
        rule.onNodeWithTag(SetupTags.SIGN_IN_EMAIL).assertIsDisplayed()
        rule.onNodeWithTag(SetupTags.SIGN_IN_BROWSER).performClick()
        assertEquals(1, actions)
    }

    @Test
    fun permissionsExposeTheVpnAction() {
        assumeTrue(TvEnvironment.isTelevision(rule.activity))
        var action = ""
        show(SetupState(step = SetupStep.PERMISSIONS)) { action = it }
        rule.onNodeWithTag(SetupTags.PERMISSIONS_STEP).assertIsDisplayed()
        rule.onNodeWithTag(SetupTags.PERMISSIONS_VPN_ACTION).performClick()
        assertEquals("vpn", action)
    }

    @Test
    fun connectNowNeedsSignInAndVpnConsent() {
        assumeTrue(TvEnvironment.isTelevision(rule.activity))
        show(SetupState(step = SetupStep.ALWAYS_ON))
        rule.onNodeWithTag(SetupTags.FINISH).assertIsDisplayed()
        rule.onNodeWithTag(SetupTags.CONNECT_NOW).assertDoesNotExist()
    }

    @Test
    fun connectNowAppearsWhenItCanWork() {
        assumeTrue(TvEnvironment.isTelevision(rule.activity))
        show(
            SetupState(
                step = SetupStep.ALWAYS_ON,
                permissions = PermissionState(vpnConsentGranted = true),
                signIn = SignInPaneState(phase = SignInPhase.AUTHENTICATED),
            ),
        )
        rule.onNodeWithTag(SetupTags.CONNECT_NOW).assertIsDisplayed()
        rule.onNodeWithTag(SetupTags.FINISH).assertIsDisplayed()
    }
}
