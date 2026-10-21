package dev.benyamin.kumiho.ui.setup

import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.engine.LoginState
import dev.benyamin.kumiho.test.MainDispatcherRule
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * The setup state machine, on the JVM: a fake engine, fake permissions and
 * fake persistence effects. Nothing here touches Android or Go, so the consent
 * gate, the sign-in transitions, the 3 s email-link spacing, the honest
 * permission states and the finish/connect ordering are all deterministic.
 *
 * Secrets: the password and the code are asserted to be forwarded exactly once
 * and to never appear in the observable state.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class SetupViewModelTest {

    @get:Rule
    val mainRule = MainDispatcherRule(StandardTestDispatcher())

    private val engine = FakeEngine()
    private val permissions = FakePermissions()
    private val effects = mutableListOf<String>()

    private fun viewModel(): SetupViewModel = SetupViewModel(
        engine = engine,
        permissions = permissions,
        persistSetupComplete = { effects += "persist" },
        connectNow = { effects += "connect" },
    )

    private fun failingPersistViewModel(): SetupViewModel = SetupViewModel(
        engine = engine,
        permissions = permissions,
        persistSetupComplete = { throw IllegalStateException("the disk is full") },
        connectNow = { effects += "connect" },
    )

    // ---- consent --------------------------------------------------------

    @Test
    fun `consent must be ticked before Continue advances`() = runTest(mainRule.testDispatcher) {
        val viewModel = viewModel()
        assertEquals(SetupStep.CONSENT, viewModel.state.value.step)
        assertFalse(viewModel.state.value.canContinue)

        viewModel.next()
        runCurrent()
        assertEquals("an unticked consent must not advance", SetupStep.CONSENT, viewModel.state.value.step)

        viewModel.setConsentAccepted(true)
        assertTrue(viewModel.state.value.canContinue)
        viewModel.next()
        runCurrent()
        assertEquals(SetupStep.SIGN_IN, viewModel.state.value.step)
    }

    @Test
    fun `unticking consent blocks Continue again`() = runTest(mainRule.testDispatcher) {
        val viewModel = viewModel()
        viewModel.setConsentAccepted(true)
        viewModel.setConsentAccepted(false)
        assertFalse(viewModel.state.value.canContinue)
        viewModel.next()
        runCurrent()
        assertEquals(SetupStep.CONSENT, viewModel.state.value.step)
    }

    // ---- sign in --------------------------------------------------------

    @Test
    fun `sign in walks email password and code to a signed-in account`() =
        runTest(mainRule.testDispatcher) {
            engine.onPassword = { LoginState("2fa", "user@example.com", "email-otp", "the code", null) }
            engine.onCode = { LoginState("done", "user@example.com", null, null, null) }
            val viewModel = viewModel()
            reachSignIn(viewModel)

            viewModel.submitEmail("  user@example.com ")
            runCurrent()
            assertEquals(listOf("user@example.com"), engine.emailCalls)
            assertEquals(SignInPhase.PASSWORD, viewModel.state.value.signIn.phase)
            assertFalse(viewModel.state.value.canContinue)

            viewModel.submitPassword("hunter2")
            runCurrent()
            assertEquals(listOf("hunter2"), engine.passwordCalls)
            assertEquals(SignInPhase.CODE, viewModel.state.value.signIn.phase)
            assertEquals("the code", viewModel.state.value.signIn.verificationReason)

            viewModel.submitCode("123456")
            runCurrent()
            assertEquals(listOf("123456"), engine.codeCalls)
            assertEquals(SignInPhase.AUTHENTICATED, viewModel.state.value.signIn.phase)
            assertEquals("user@example.com", viewModel.state.value.signIn.accountEmail)
            assertTrue(viewModel.state.value.canContinue)
        }

    @Test
    fun `the password is forwarded once and never retained in the state`() =
        runTest(mainRule.testDispatcher) {
            val secret = "correct-horse-battery-staple"
            val viewModel = viewModel()
            reachSignIn(viewModel)
            viewModel.submitEmail("user@example.com")
            runCurrent()
            viewModel.submitPassword(secret)
            runCurrent()

            assertEquals(listOf(secret), engine.passwordCalls)
            assertFalse(
                "the password must never appear in the observably retained state",
                viewModel.state.value.toString().contains(secret),
            )
            assertFalse(viewModel.state.value.signIn.busy)
        }

    @Test
    fun `a code is forwarded once and never retained in the state`() =
        runTest(mainRule.testDispatcher) {
            engine.onPassword = { LoginState("2fa", "user@example.com", "email-otp", null, null) }
            val viewModel = viewModel()
            reachSignIn(viewModel)
            viewModel.submitEmail("user@example.com")
            runCurrent()
            viewModel.submitPassword("pw")
            runCurrent()
            viewModel.submitCode("987654")
            runCurrent()

            assertEquals(listOf("987654"), engine.codeCalls)
            assertFalse(viewModel.state.value.toString().contains("987654"))
        }

    @Test
    fun `an already signed-in account skips the form`() = runTest(mainRule.testDispatcher) {
        engine.status = status(authenticated = true, email = "account@example.com")
        val viewModel = viewModel()
        reachSignIn(viewModel)

        assertEquals(SignInPhase.AUTHENTICATED, viewModel.state.value.signIn.phase)
        assertEquals("account@example.com", viewModel.state.value.signIn.accountEmail)
        assertTrue(viewModel.state.value.canContinue)
        assertTrue(engine.emailCalls.isEmpty())
    }

    @Test
    fun `a captcha error is shown inline and does not advance`() =
        runTest(mainRule.testDispatcher) {
            val captcha = "captcha \"captcha\" (captcha cannot be solved automatically)"
            engine.onPassword = { LoginState("password", "user@example.com", null, null, captcha) }
            val viewModel = viewModel()
            reachSignIn(viewModel)
            viewModel.submitEmail("user@example.com")
            runCurrent()
            viewModel.submitPassword("pw")
            runCurrent()

            assertEquals(SignInPhase.PASSWORD, viewModel.state.value.signIn.phase)
            assertEquals(captcha, viewModel.state.value.signIn.error)
            assertFalse(viewModel.state.value.canContinue)
            viewModel.next()
            runCurrent()
            assertEquals(SetupStep.SIGN_IN, viewModel.state.value.step)
        }

    @Test
    fun `a thrown engine failure is surfaced inline and leaves the form usable`() =
        runTest(mainRule.testDispatcher) {
            engine.emailFailure = IllegalStateException("the engine is closed")
            val viewModel = viewModel()
            reachSignIn(viewModel)
            viewModel.submitEmail("user@example.com")
            runCurrent()

            assertEquals("the engine is closed", viewModel.state.value.signIn.error)
            assertFalse(viewModel.state.value.signIn.busy)
            assertEquals(SignInPhase.EMAIL, viewModel.state.value.signIn.phase)
        }

    @Test
    fun `a blank email and a blank password are rejected without an engine call`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            reachSignIn(viewModel)
            viewModel.submitEmail("   ")
            runCurrent()
            assertEquals(SetupMessage.EMAIL_REQUIRED, viewModel.state.value.signIn.message)
            assertTrue(engine.emailCalls.isEmpty())

            engine.onEmail = { LoginState("password", it, null, null, null) }
            viewModel.submitEmail("user@example.com")
            runCurrent()
            viewModel.submitPassword("")
            runCurrent()
            assertEquals(SetupMessage.PASSWORD_REQUIRED, viewModel.state.value.signIn.message)
            assertTrue(engine.passwordCalls.isEmpty())
        }

    // ---- email link ------------------------------------------------------

    private fun pendingLink(code: String): LoginState = LoginState(
        step = "2fa",
        email = "user@example.com",
        verificationMethod = null,
        verificationReason = null,
        error = "email link not confirmed yet; click the link in the email, then press Enter",
    )

    private fun TestScope.reachEmailLink(viewModel: SetupViewModel) {
        engine.onPassword = { LoginState("2fa", "user@example.com", "email", null, null) }
        engine.onCode = { pendingLink(it) }
        reachSignIn(viewModel)
        viewModel.submitEmail("user@example.com")
        runCurrent()
        viewModel.submitPassword("pw")
        runCurrent()
    }

    @Test
    fun `the email link waits and polls no sooner than three seconds`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            reachEmailLink(viewModel)
            assertEquals(SignInPhase.EMAIL_LINK_WAITING, viewModel.state.value.signIn.phase)
            assertNull("the waiting case must not look like a failure", viewModel.state.value.signIn.error)
            assertEquals(0, engine.codeCalls.size)

            advance(2_999)
            assertEquals("no poll before 3 s", 0, engine.codeCalls.size)

            advance(1)
            assertEquals("one poll at 3 s", 1, engine.codeCalls.size)
            assertEquals("", engine.codeCalls.single())

            advance(3_000)
            assertEquals(2, engine.codeCalls.size)

            // End the wait so the test scheduler has no pending poll left.
            viewModel.setScreenResumed(false)
            runCurrent()
        }

    @Test
    fun `pausing stops the poll and resuming restarts the spacing`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            reachEmailLink(viewModel)

            viewModel.setScreenResumed(false)
            advance(9_000)
            assertEquals("a paused screen must not poll", 0, engine.codeCalls.size)

            viewModel.setScreenResumed(true)
            advance(2_999)
            assertEquals(0, engine.codeCalls.size)
            advance(1)
            assertEquals(1, engine.codeCalls.size)

            viewModel.setScreenResumed(false)
            runCurrent()
        }

    @Test
    fun `a manual re-check polls immediately and restarts the spacing`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            reachEmailLink(viewModel)

            viewModel.recheckEmailLink()
            runCurrent()
            assertEquals("Re-check asks now", 1, engine.codeCalls.size)

            advance(2_999)
            assertEquals(1, engine.codeCalls.size)
            advance(1)
            assertEquals(2, engine.codeCalls.size)

            viewModel.setScreenResumed(false)
            runCurrent()
        }

    @Test
    fun `a confirmed email link signs in and stops polling`() = runTest(mainRule.testDispatcher) {
        val viewModel = viewModel()
        reachEmailLink(viewModel)
        engine.onCode = { LoginState("done", "user@example.com", null, null, null) }

        advance(3_000)
        assertEquals(SignInPhase.AUTHENTICATED, viewModel.state.value.signIn.phase)
        assertTrue(viewModel.state.value.canContinue)

        advance(9_000)
        assertEquals("polling must stop once signed in", 1, engine.codeCalls.size)
    }

    @Test
    fun `leaving the sign-in step stops the poll`() = runTest(mainRule.testDispatcher) {
        engine.onPassword = { LoginState("done", "user@example.com", null, null, null) }
        val viewModel = viewModel()
        reachEmailLink(viewModel)
        assertTrue(viewModel.back())
        advance(9_000)
        assertEquals(0, engine.codeCalls.size)
    }

    // ---- permissions ------------------------------------------------------

    @Test
    fun `a denied permission is reported honestly and still reaches finish`() =
        runTest(mainRule.testDispatcher) {
            engine.onPassword = { LoginState("done", "user@example.com", null, null, null) }
            val viewModel = viewModel()
            signIn(viewModel)

            viewModel.next()
            runCurrent()
            assertEquals(SetupStep.PERMISSIONS, viewModel.state.value.step)
            assertFalse(viewModel.state.value.permissions.vpnConsentGranted)
            assertFalse(viewModel.state.value.permissions.notificationsGranted)
            assertTrue("a denial never blocks Continue", viewModel.state.value.canContinue)

            viewModel.next()
            runCurrent()
            assertEquals(SetupStep.ALWAYS_ON, viewModel.state.value.step)
            assertFalse("Connect now needs the VPN permission", viewModel.state.value.canConnectNow)

            viewModel.finish(connectNow = true)
            runCurrent()
            assertEquals(listOf("persist"), effects)
            assertNull(viewModel.state.value.error)
        }

    @Test
    fun `permission results re-read the OS instead of trusting the callback`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            viewModel.onNotificationResult()
            runCurrent()
            assertFalse(viewModel.state.value.permissions.notificationsGranted)

            permissions.notifications = true
            permissions.vpn = true
            viewModel.onNotificationResult()
            runCurrent()
            assertTrue(viewModel.state.value.permissions.notificationsGranted)
            assertTrue(viewModel.state.value.permissions.vpnConsentGranted)
        }

    @Test
    fun `the Always-on step reads the OS policy and refreshes it`() =
        runTest(mainRule.testDispatcher) {
            permissions.alwaysOn = true
            permissions.lockdown = true
            val viewModel = viewModel()
            assertTrue("the constructor reads the policy once", viewModel.state.value.alwaysOn)
            assertTrue(viewModel.state.value.lockdown)

            permissions.alwaysOn = false
            permissions.lockdown = false
            val before = permissions.policyRefreshes
            viewModel.refreshPermissions()
            assertTrue("a refresh must re-read the OS policy", permissions.policyRefreshes > before)
            assertFalse(viewModel.state.value.alwaysOn)
            assertFalse(viewModel.state.value.lockdown)
        }

    // ---- finish -----------------------------------------------------------

    @Test
    fun `finish persists before it connects`() = runTest(mainRule.testDispatcher) {
        engine.onPassword = { LoginState("done", "user@example.com", null, null, null) }
        permissions.vpn = true
        val viewModel = viewModel()
        signIn(viewModel)
        walkToAlwaysOn(viewModel)
        assertTrue(viewModel.state.value.canConnectNow)

        viewModel.finish(connectNow = true)
        runCurrent()
        assertEquals(listOf("persist", "connect"), effects)
        assertFalse(viewModel.state.value.finishing)
    }

    @Test
    fun `connect now is skipped when the state does not allow it`() =
        runTest(mainRule.testDispatcher) {
            engine.onPassword = { LoginState("done", "user@example.com", null, null, null) }
            permissions.vpn = false
            val viewModel = viewModel()
            signIn(viewModel)
            walkToAlwaysOn(viewModel)

            viewModel.finish(connectNow = true)
            runCurrent()
            assertEquals(listOf("persist"), effects)
        }

    @Test
    fun `finish without connecting never connects`() = runTest(mainRule.testDispatcher) {
        engine.onPassword = { LoginState("done", "user@example.com", null, null, null) }
        permissions.vpn = true
        val viewModel = viewModel()
        signIn(viewModel)
        walkToAlwaysOn(viewModel)

        viewModel.finish(connectNow = false)
        runCurrent()
        assertEquals(listOf("persist"), effects)
    }

    @Test
    fun `a failed persist keeps the user on the last step`() = runTest(mainRule.testDispatcher) {
        engine.onPassword = { LoginState("done", "user@example.com", null, null, null) }
        permissions.vpn = true
        val viewModel = failingPersistViewModel()
        signIn(viewModel)
        walkToAlwaysOn(viewModel)

        viewModel.finish(connectNow = true)
        runCurrent()
        assertEquals(SetupStep.ALWAYS_ON, viewModel.state.value.step)
        assertEquals("the disk is full", viewModel.state.value.error)
        assertFalse(viewModel.state.value.finishing)
        assertTrue("a failed persist must not connect", effects.isEmpty())
    }

    @Test
    fun `finish is ignored before the last step`() = runTest(mainRule.testDispatcher) {
        val viewModel = viewModel()
        viewModel.finish(connectNow = false)
        runCurrent()
        assertTrue(effects.isEmpty())
    }

    // ---- back -------------------------------------------------------------

    @Test
    fun `back walks one step at a time and stops at consent`() = runTest(mainRule.testDispatcher) {
        engine.onPassword = { LoginState("done", "user@example.com", null, null, null) }
        val viewModel = viewModel()
        signIn(viewModel)
        walkToAlwaysOn(viewModel)

        assertTrue(viewModel.back())
        assertEquals(SetupStep.PERMISSIONS, viewModel.state.value.step)
        assertTrue(viewModel.back())
        assertEquals(SetupStep.SIGN_IN, viewModel.state.value.step)
        assertTrue(viewModel.back())
        assertEquals(SetupStep.CONSENT, viewModel.state.value.step)
        assertFalse("the UI exits at the first step", viewModel.back())
        assertEquals(SetupStep.CONSENT, viewModel.state.value.step)
    }

    // ---- helpers ----------------------------------------------------------

    private fun TestScope.reachSignIn(viewModel: SetupViewModel) {
        viewModel.setConsentAccepted(true)
        viewModel.next()
        runCurrent()
    }

    /** Email + password against the fake, reaching the signed-in pane. */
    private fun TestScope.signIn(viewModel: SetupViewModel) {
        reachSignIn(viewModel)
        viewModel.submitEmail("user@example.com")
        runCurrent()
        viewModel.submitPassword("pw")
        runCurrent()
    }

    private fun TestScope.walkToAlwaysOn(viewModel: SetupViewModel) {
        assertTrue(viewModel.state.value.signedIn)
        viewModel.next()
        runCurrent()
        assertEquals(SetupStep.PERMISSIONS, viewModel.state.value.step)
        viewModel.next()
        runCurrent()
        assertEquals(SetupStep.ALWAYS_ON, viewModel.state.value.step)
    }

    private fun TestScope.advance(millis: Long) {
        advanceTimeBy(millis)
        runCurrent()
    }
}

/** A scripted engine: every call is recorded, every reply is settable. */
private class FakeEngine : SetupEngine {
    val emailCalls = mutableListOf<String>()
    val passwordCalls = mutableListOf<String>()
    val codeCalls = mutableListOf<String>()

    var status: EngineStatus = status()
    var statusFailure: Exception? = null
    var emailFailure: Exception? = null
    var passwordFailure: Exception? = null
    var codeFailure: Exception? = null

    var onEmail: (String) -> LoginState = { LoginState("password", it, null, null, null) }
    var onPassword: (String) -> LoginState = { LoginState("done", "user@example.com", null, null, null) }
    var onCode: (String) -> LoginState = { LoginState("email", null, null, null, null) }

    override suspend fun loginEmail(email: String): LoginState {
        emailCalls += email
        emailFailure?.let { throw it }
        return onEmail(email)
    }

    override suspend fun loginPassword(password: String): LoginState {
        passwordCalls += password
        passwordFailure?.let { throw it }
        return onPassword(password)
    }

    override suspend fun login2fa(code: String): LoginState {
        codeCalls += code
        codeFailure?.let { throw it }
        return onCode(code)
    }

    override suspend fun refreshStatus(): EngineStatus {
        statusFailure?.let { throw it }
        return status
    }
}

/** A settable permissions source; every read is honest by construction. */
private class FakePermissions : SetupPermissions {
    var vpn: Boolean = false
    var notifications: Boolean = false
    var runtimeRequired: Boolean = true
    var alwaysOn: Boolean = false
    var lockdown: Boolean = false
    var policyRefreshes: Int = 0

    override fun vpnConsentGranted(): Boolean = vpn

    override fun notificationsGranted(): Boolean = notifications

    override fun notificationsRuntimePermissionRequired(): Boolean = runtimeRequired

    override fun alwaysOn(): Boolean = alwaysOn

    override fun lockdown(): Boolean = lockdown

    override fun refreshOsPolicy() {
        policyRefreshes++
    }
}

/** A signed-out status snapshot; every field is explicit. */
private fun status(
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
