package dev.benyamin.kumiho.ui.setup

import androidx.activity.ComponentActivity
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import androidx.compose.ui.test.performTextReplacement
import androidx.compose.ui.semantics.getOrNull
import dev.benyamin.kumiho.engine.Accounts
import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.engine.LoginState
import dev.benyamin.kumiho.ui.account.BrowserSignIn
import dev.benyamin.kumiho.ui.account.MozillaAccountWebViewTags
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoColors
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test

/**
 * The four-step setup as the user walks it: a real Compose hierarchy over the
 * fakes, so the required consent tick, the cleared password field, the
 * email-link wait with its Re-check action, the honest permission denial and
 * the finish/connect ordering are all asserted without a daemon.
 *
 * The palette is the real Go one for the emulator AAR; only the engine and the
 * Android permission facts are fakes.
 */
class SetupFlowTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private val engine = FakeSetupEngine()
    private val permissions = FakeSetupPermissions()
    private var persisted = 0
    private var connected = 0

    private fun show(): SetupViewModel {
        val viewModel = SetupViewModel(
            engine = engine,
            permissions = permissions,
            persistSetupComplete = { persisted++ },
            connectNow = { connected++ },
        )
        rule.setContent {
            KumihoTheme(colors = palette(ThemeId.LIGHT)) {
                SetupFlow(viewModel = viewModel)
            }
        }
        return viewModel
    }

    @Test
    fun allFourStepsWithADeniedPermissionStillFinish() {
        engine.onPassword = { password ->
            if (password == SECRET) {
                LoginState("password", EMAIL, null, null, "Invalid password")
            } else {
                LoginState("done", EMAIL, null, null, null)
            }
        }
        permissions.vpn = false
        permissions.notifications = false
        permissions.notificationsRuntimeRequired = true
        val viewModel = show()

        // Step 1: consent is required.
        rule.onNodeWithTag(SetupTags.CONSENT_STEP).assertExists()
        rule.onNodeWithTag(SetupTags.CONTINUE).assertIsNotEnabled()
        rule.onNodeWithTag(SetupTags.CONSENT_ACCEPT).performClick()
        rule.waitUntil(TIMEOUT) { viewModel.state.value.canContinue }
        rule.onNodeWithTag(SetupTags.CONTINUE).assertIsEnabled()
        rule.onNodeWithTag(SetupTags.CONTINUE).performClick()
        rule.waitUntil(TIMEOUT) { viewModel.state.value.step == SetupStep.SIGN_IN }

        // Step 2: email, then a password the engine rejects. The field is
        // empty the moment the submit is pressed, so the user's secret never
        // survives even a failed attempt.
        rule.onNodeWithTag(SetupTags.SIGN_IN_EMAIL).performTextInput(EMAIL)
        rule.onNodeWithTag(SetupTags.SIGN_IN_EMAIL_SUBMIT).performClick()
        rule.waitUntil(TIMEOUT) { viewModel.state.value.signIn.phase == SignInPhase.PASSWORD }
        rule.onNodeWithTag(SetupTags.SIGN_IN_PASSWORD).performTextInput(SECRET)
        rule.onNodeWithTag(SetupTags.SIGN_IN_PASSWORD_SUBMIT).performClick()
        rule.waitUntil(TIMEOUT) { viewModel.state.value.signIn.error != null }
        assertEquals("the password field must be cleared on submit", "", passwordText())
        assertEquals(SECRET, engine.passwordCalls.single())

        // A good password signs in; the wizard then lets the user continue.
        rule.onNodeWithTag(SetupTags.SIGN_IN_PASSWORD).performTextInput("hunter2")
        rule.onNodeWithTag(SetupTags.SIGN_IN_PASSWORD_SUBMIT).performClick()
        rule.waitUntil(TIMEOUT) { viewModel.state.value.signedIn }
        rule.waitForIdle()
        rule.onNodeWithTag(SetupTags.SIGN_IN_ACCOUNT).assertExists()
        rule.onNodeWithTag(SetupTags.CONTINUE).assertIsEnabled()
        rule.onNodeWithTag(SetupTags.CONTINUE).performClick()
        rule.waitUntil(TIMEOUT) { viewModel.state.value.step == SetupStep.PERMISSIONS }

        // Step 3: denied is reported honestly, and Continue is not blocked.
        rule.onNodeWithTag(SetupTags.PERMISSIONS_STEP).assertExists()
        rule.onNodeWithTag(SetupTags.PERMISSIONS_VPN_STATUS).assertExists()
        rule.onNodeWithTag(SetupTags.PERMISSIONS_NOTIFICATIONS_STATUS).assertExists()
        rule.onNodeWithTag(SetupTags.CONTINUE).assertIsEnabled()
        rule.onNodeWithTag(SetupTags.CONTINUE).performClick()
        rule.waitUntil(TIMEOUT) { viewModel.state.value.step == SetupStep.ALWAYS_ON }

        // Step 4: no VPN consent, so no Connect now; Finish still completes.
        rule.onNodeWithTag(SetupTags.ALWAYS_ON_STEP).assertExists()
        rule.onNodeWithTag(SetupTags.CONNECT_NOW).assertDoesNotExist()
        rule.onNodeWithTag(SetupTags.FINISH).performClick()
        rule.waitUntil(TIMEOUT) { persisted == 1 }
        assertEquals("a denied permission must not connect", 0, connected)
    }

    @Test
    fun browserWaitingCardPasteAndPasswordFallbackAreVisible() {
        var toggles = 0
        var submits = 0
        var password = 0
        rule.setContent {
            KumihoTheme(colors = palette(ThemeId.LIGHT)) {
                SignInStep(
                    state = SignInPaneState(),
                    browser = BrowserSignIn(
                        id = "setup-1",
                        url = "https://vpn.mozilla.org/api/v2/vpn/login/android?port=49152",
                    ),
                    browserPasteOpen = true,
                    browserPasteValue = "urn:ietf:wg:oauth:2.0:oob:oauth-redirect-webchannel?code=c&state=s",
                    browserPasteError = null,
                    browserPasteMessage = null,
                    browserSubmitting = false,
                    browserFallback = false,
                    onBrowser = {},
                    onOpenBrowser = {},
                    onToggleBrowserPaste = { toggles++ },
                    onBrowserPasteChange = {},
                    onBrowserPasteSubmit = { submits++ },
                    onUsePassword = { password++ },
                    onCancelBrowser = {},
                    onSubmitEmail = {},
                    onSubmitPassword = {},
                    onSubmitCode = {},
                    onRecheck = {},
                )
            }
        }
        rule.waitForIdle()

        rule.onNodeWithTag(SetupTags.SIGN_IN_BROWSER_WAITING).assertIsDisplayed()
        rule.onNodeWithTag(SetupTags.SIGN_IN_BROWSER_PASTE_FIELD).assertIsDisplayed()
        rule.onNodeWithTag(SetupTags.SIGN_IN_BROWSER_PASTE_TOGGLE).performClick()
        rule.onNodeWithTag(SetupTags.SIGN_IN_BROWSER_PASTE_SUBMIT).performClick()
        rule.onNodeWithTag(SetupTags.SIGN_IN_BROWSER_USE_PASSWORD).performClick()
        rule.waitForIdle()

        assertEquals(1, toggles)
        assertEquals(1, submits)
        assertEquals(1, password)
    }

    @Test
    fun firstRunBrowserWebViewComposesFromTheFlowAndCloses() {
        engine.loginStartResult = { _, _ ->
            LoginState(
                step = "browser",
                email = null,
                verificationMethod = null,
                verificationReason = null,
                error = null,
                method = "browser",
                url = "https://evil.example/signin",
                loginId = "setup-webview-1",
            )
        }
        val viewModel = show()
        walkToSignIn(viewModel)

        rule.onNodeWithTag(SetupTags.SIGN_IN_BROWSER).performClick()
        rule.waitUntil(TIMEOUT) { viewModel.state.value.browserWebViewOpen }
        rule.waitForIdle()

        rule.onNodeWithTag(MozillaAccountWebViewTags.SCREEN).assertIsDisplayed()
        rule.onNodeWithTag(MozillaAccountWebViewTags.CLOSE).assertIsDisplayed()
        rule.onNodeWithTag(MozillaAccountWebViewTags.CLOSE).performClick()
        rule.waitUntil(TIMEOUT) { !viewModel.state.value.browserWebViewOpen }

        assertEquals(
            "closing the browser must keep the engine-owned flow",
            "setup-webview-1",
            viewModel.state.value.browser?.id,
        )
        rule.onNodeWithTag(MozillaAccountWebViewTags.SCREEN).assertDoesNotExist()
    }

    @Test
    fun theEmailLinkWaitsAndRecheckAsksAgain() {
        engine.onPassword = {
            LoginState("2fa", EMAIL, "email", null, null)
        }
        engine.onCode = { pendingLink() }
        val viewModel = show()
        walkToSignIn(viewModel)

        rule.onNodeWithTag(SetupTags.SIGN_IN_EMAIL).performTextInput(EMAIL)
        rule.onNodeWithTag(SetupTags.SIGN_IN_EMAIL_SUBMIT).performClick()
        rule.waitUntil(TIMEOUT) { viewModel.state.value.signIn.phase == SignInPhase.PASSWORD }
        rule.onNodeWithTag(SetupTags.SIGN_IN_PASSWORD).performTextInput("hunter2")
        rule.onNodeWithTag(SetupTags.SIGN_IN_PASSWORD_SUBMIT).performClick()
        rule.waitUntil(TIMEOUT) {
            viewModel.state.value.signIn.phase == SignInPhase.EMAIL_LINK_WAITING
        }

        rule.onNodeWithTag(SetupTags.SIGN_IN_WAITING).assertExists()
        rule.onNodeWithTag(SetupTags.SIGN_IN_ERROR).assertDoesNotExist()
        rule.onNodeWithTag(SetupTags.CONTINUE).assertIsNotEnabled()

        // Re-check asks immediately with an empty code.
        rule.onNodeWithTag(SetupTags.SIGN_IN_RECHECK).performClick()
        rule.waitUntil(TIMEOUT) { engine.codeCalls.isNotEmpty() }
        assertEquals("", engine.codeCalls.single())

        // The next Re-check finds the confirmation.
        engine.onCode = { LoginState("done", EMAIL, null, null, null) }
        rule.onNodeWithTag(SetupTags.SIGN_IN_RECHECK).performClick()
        rule.waitUntil(TIMEOUT) { viewModel.state.value.signedIn }
        rule.waitForIdle()
        rule.onNodeWithTag(SetupTags.SIGN_IN_ACCOUNT).assertExists()
        rule.onNodeWithTag(SetupTags.CONTINUE).assertIsEnabled()
    }

    @Test
    fun connectNowIsOfferedOnlyWithVpnConsentAndFinishesInOrder() {
        engine.onPassword = { LoginState("done", EMAIL, null, null, null) }
        permissions.vpn = true
        val viewModel = show()
        walkToSignIn(viewModel)
        signInWithPassword(viewModel)

        rule.onNodeWithTag(SetupTags.CONTINUE).performClick()
        rule.waitUntil(TIMEOUT) { viewModel.state.value.step == SetupStep.PERMISSIONS }
        rule.onNodeWithTag(SetupTags.CONTINUE).performClick()
        rule.waitUntil(TIMEOUT) { viewModel.state.value.step == SetupStep.ALWAYS_ON }

        rule.onNodeWithTag(SetupTags.CONNECT_NOW).assertExists()
        rule.onNodeWithTag(SetupTags.CONNECT_NOW).performClick()
        rule.waitUntil(TIMEOUT) { connected == 1 }
        assertEquals("the preference is written before the connect", 1, persisted)
    }

    // ---- helpers ----------------------------------------------------------

    private fun walkToSignIn(viewModel: SetupViewModel) {
        rule.onNodeWithTag(SetupTags.CONSENT_ACCEPT).performClick()
        rule.waitUntil(TIMEOUT) { viewModel.state.value.canContinue }
        rule.onNodeWithTag(SetupTags.CONTINUE).performClick()
        rule.waitUntil(TIMEOUT) { viewModel.state.value.step == SetupStep.SIGN_IN }
    }

    private fun signInWithPassword(viewModel: SetupViewModel) {
        rule.onNodeWithTag(SetupTags.SIGN_IN_EMAIL).performTextInput(EMAIL)
        rule.onNodeWithTag(SetupTags.SIGN_IN_EMAIL_SUBMIT).performClick()
        rule.waitUntil(TIMEOUT) { viewModel.state.value.signIn.phase == SignInPhase.PASSWORD }
        rule.onNodeWithTag(SetupTags.SIGN_IN_PASSWORD).performTextReplacement("hunter2")
        rule.onNodeWithTag(SetupTags.SIGN_IN_PASSWORD_SUBMIT).performClick()
        rule.waitUntil(TIMEOUT) { viewModel.state.value.signedIn }
        rule.waitForIdle()
    }

    private fun passwordText(): String =
        rule.onNodeWithTag(SetupTags.SIGN_IN_PASSWORD)
            .fetchSemanticsNode()
            .config
            .getOrNull(SemanticsProperties.EditableText)
            ?.text
            .orEmpty()

    private fun palette(theme: ThemeId): KumihoColors =
        PaletteMapping.fromJson(
            dev.benyamin.kumiho.go.mobile.Mobile.palette(theme.id, KumihoAccent.BLUE.id),
            theme,
            KumihoAccent.BLUE,
        )

    private fun pendingLink(): LoginState = LoginState(
        step = "2fa",
        email = EMAIL,
        verificationMethod = null,
        verificationReason = null,
        error = "email link not confirmed yet; click the link in the email, then press Enter",
    )

    private companion object {
        const val EMAIL = "user@example.com"
        const val SECRET = "correct-horse-battery-staple"
        const val TIMEOUT = 10_000L
    }
}

/** A scripted engine: every call is recorded, every reply is settable. */
internal class FakeSetupEngine : SetupEngine {
    val emailCalls = mutableListOf<String>()
    val passwordCalls = mutableListOf<String>()
    val codeCalls = mutableListOf<String>()

    var onEmail: (String) -> LoginState = { LoginState("password", it, null, null, null) }
    var onPassword: (String) -> LoginState = { LoginState("done", "user@example.com", null, null, null) }
    var onCode: (String) -> LoginState = { pending() }
    var loginStartResult: suspend (String, String?) -> LoginState = { _, _ ->
        error("browser sign-in is not used in this test")
    }

    override suspend fun loginEmail(email: String): LoginState {
        emailCalls += email
        return onEmail(email)
    }

    override suspend fun loginPassword(password: String): LoginState {
        passwordCalls += password
        return onPassword(password)
    }

    override suspend fun login2fa(code: String): LoginState {
        codeCalls += code
        return onCode(code)
    }

    override suspend fun refreshStatus(): EngineStatus = signedOutStatus()

    override suspend fun loginStart(method: String, email: String?): LoginState =
        loginStartResult(method, email)

    override suspend fun loginStatus(id: String): LoginState =
        error("no browser flow")

    override suspend fun loginComplete(redirectUrl: String, id: String?) = Unit

    override suspend fun loginCancel(id: String) = Unit

    override suspend fun accountsList(): Accounts =
        Accounts(emptyList(), null, false, 10, null, null, null)

    private fun pending(): LoginState = LoginState(
        step = "2fa",
        email = "user@example.com",
        verificationMethod = "email",
        verificationReason = null,
        error = "email link not confirmed yet; click the link in the email, then press Enter",
    )
}

/** Honest permission facts, driven by the test. */
internal class FakeSetupPermissions : SetupPermissions {
    var vpn = false
    var notifications = false
    var notificationsRuntimeRequired = false
    var alwaysOn = false
    var lockdown = false
    var policyRefreshes = 0

    override fun vpnConsentGranted(): Boolean = vpn
    override fun notificationsGranted(): Boolean = notifications
    override fun notificationsRuntimePermissionRequired(): Boolean = notificationsRuntimeRequired
    override fun alwaysOn(): Boolean = alwaysOn
    override fun lockdown(): Boolean = lockdown
    override fun refreshOsPolicy() {
        policyRefreshes++
    }
}

/** A signed-out engine status for the status read that opens sign-in. */
internal fun signedOutStatus(
    authenticated: Boolean = false,
    email: String? = null,
): EngineStatus = EngineStatus(
    state = if (authenticated) "IDLE" else "UNAUTHENTICATED",
    authenticated = authenticated,
    email = email,
    since = 0,
    tokenExpires = 0,
    passExpires = 0,
    killSwitch = false,
    autoconnect = false,
    location = null,
    server = null,
    proxyOnly = false,
    quota = null,
    dns = null,
    ipv6 = null,
    exitIp = null,
    exitCountry = null,
    totals = null,
    rates = null,
)
