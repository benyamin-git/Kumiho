package dev.benyamin.kumiho.ui.account

import dev.benyamin.kumiho.engine.Account
import dev.benyamin.kumiho.engine.LoginState
import dev.benyamin.kumiho.test.MainDispatcherRule
import dev.benyamin.kumiho.ui.setup.SignInPhase
import java.time.Instant
import java.time.ZoneId
import java.util.Locale
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
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
 * The account state machine and its pure formatting rules. The gateway is an
 * in-memory fake, so routing, the 35 s timeout, the local signed-in signal and
 * the sign-in reuse are deterministic without Android or an engine.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class AccountStateTest {

    @get:Rule
    val mainRule = MainDispatcherRule(StandardTestDispatcher())

    private val gateway = FakeGateway()

    private fun viewModel(): AccountViewModel = AccountViewModel(gateway)

    // ---- formatting ------------------------------------------------------

    @Test
    fun `the token expiry is the local signed-in signal`() {
        val signedIn = account(tokenExpires = TOKEN_EXPIRY)
        val signedOut = account(tokenExpires = 0L, error = "not signed in")

        assertTrue(AccountFormat.isSignedIn(signedIn))
        assertFalse(AccountFormat.isSignedIn(signedOut))
        assertFalse(AccountFormat.isSignedIn(null))
        assertTrue(AccountFormat.isNotSignedIn(signedOut))
        assertFalse(AccountFormat.isNotSignedIn(signedIn))
    }

    @Test
    fun `the not-signed-in engine error is not an online failure`() {
        assertEquals(null, AccountFormat.onlineError(account(error = "not signed in")))
        assertEquals(null, AccountFormat.onlineError(account(error = "  ")))
        assertEquals("guardian refused", AccountFormat.onlineError(account(error = "guardian refused")))
    }

    @Test
    fun `quota and expiry render only what the engine reported`() {
        val missing = account(maxBytes = null, quotaRemaining = null, tokenExpires = 0L)

        assertNull(AccountFormat.quota(missing))
        assertNull(AccountFormat.tokenExpiry(missing))
        assertEquals(
            "1.0 GiB remaining of 2.0 GiB",
            AccountFormat.quota(account(maxBytes = 2L * 1024 * 1024 * 1024, quotaRemaining = 1024L * 1024 * 1024)),
        )
        assertEquals(
            "21 Nov 2026",
            AccountFormat.tokenExpiry(
                account(tokenExpires = TOKEN_EXPIRY),
                ZoneId.of("UTC"),
                Locale.UK,
            ),
        )
        assertTrue(AccountFormat.bandwidthLimited(account(limitedBandwidth = true)))
        assertFalse(AccountFormat.bandwidthLimited(missing))
    }

    // ---- initial load ----------------------------------------------------

    @Test
    fun `a signed-out account shows the shared sign-in pane`() = runTest(mainRule.testDispatcher) {
        gateway.accountResult = { account(tokenExpires = 0L, error = "not signed in") }

        val viewModel = viewModel()
        runCurrent()

        val state = viewModel.state.value
        assertFalse(state.loading)
        assertFalse(state.signedIn)
        assertTrue(state.showSignIn)
        assertNull("'not signed in' must not render as a failure", state.error)
        assertEquals(SignInPhase.EMAIL, state.signIn.phase)
    }

    @Test
    fun `a signed-in account sets the authenticated pane and the email`() =
        runTest(mainRule.testDispatcher) {
            gateway.accountResult = { account(email = "user@example.com") }

            val viewModel = viewModel()
            runCurrent()

            val state = viewModel.state.value
            assertTrue(state.signedIn)
            assertFalse(state.showSignIn)
            assertNull(state.error)
            assertEquals(SignInPhase.AUTHENTICATED, state.signIn.phase)
            assertEquals("user@example.com", state.signIn.accountEmail)
        }

    @Test
    fun `an online lookup error keeps the signed-in card`() = runTest(mainRule.testDispatcher) {
        gateway.accountResult = { account(error = "guardian timed out") }

        val viewModel = viewModel()
        runCurrent()

        val state = viewModel.state.value
        assertTrue(state.signedIn)
        assertEquals("guardian timed out", state.error)
        assertNull(state.message)
    }

    @Test
    fun `a failed first load reports the failure and stays signed out`() =
        runTest(mainRule.testDispatcher) {
            gateway.accountResult = { error("the engine is gone") }

            val viewModel = viewModel()
            runCurrent()

            val state = viewModel.state.value
            assertFalse(state.loading)
            assertFalse(state.signedIn)
            assertEquals(AccountMessage.LOAD_FAILED, state.message)
            assertEquals("the engine is gone", state.error)
        }

    // ---- the 35 s timeout ------------------------------------------------

    @Test
    fun `a timeout keeps the last local fields and reports the timeout`() =
        runTest(mainRule.testDispatcher) {
            val hang = CompletableDeferred<Unit>()
            var calls = 0
            gateway.accountResult = {
                calls++
                if (calls == 1) account() else {
                    hang.await()
                    account(email = "changed@example.com")
                }
            }
            val viewModel = viewModel()
            runCurrent()
            val before = viewModel.state.value.account

            viewModel.refresh()
            runCurrent()
            advanceTimeBy(35_001)
            runCurrent()

            val state = viewModel.state.value
            assertEquals(AccountMessage.TIMEOUT, state.message)
            assertFalse(state.loading)
            assertFalse(state.refreshing)
            assertEquals("the last local fields must survive a timeout", before, state.account)
            assertTrue(state.signedIn)
        }

    // ---- sign in ---------------------------------------------------------

    @Test
    fun `sign in walks email password and code and ends on the card`() =
        runTest(mainRule.testDispatcher) {
            gateway.accountResult = {
                account(
                    email = if (gateway.signedIn) "user@example.com" else null,
                    tokenExpires = if (gateway.signedIn) TOKEN_EXPIRY else 0L,
                    error = if (gateway.signedIn) null else "not signed in",
                )
            }
            gateway.onEmail = { LoginState("password", it, null, null, null) }
            gateway.onPassword = {
                LoginState("2fa", "user@example.com", "email-otp", "the code", null)
            }
            gateway.onCode = {
                gateway.signedIn = true
                LoginState("done", "user@example.com", null, null, null)
            }
            val viewModel = viewModel()
            runCurrent()

            viewModel.submitEmail("  user@example.com ")
            runCurrent()
            assertEquals(listOf("user@example.com"), gateway.emailCalls)
            assertEquals(SignInPhase.PASSWORD, viewModel.state.value.signIn.phase)

            viewModel.submitPassword("hunter2")
            runCurrent()
            assertEquals(listOf("hunter2"), gateway.passwordCalls)
            assertEquals(SignInPhase.CODE, viewModel.state.value.signIn.phase)
            assertEquals("email-otp", viewModel.state.value.signIn.verificationMethod)

            viewModel.submitCode("123456")
            runCurrent()
            assertEquals(listOf("123456"), gateway.codeCalls)
            val state = viewModel.state.value
            assertTrue(state.signedIn)
            assertEquals(SignInPhase.AUTHENTICATED, state.signIn.phase)
            assertEquals("user@example.com", state.signIn.accountEmail)
        }

    @Test
    fun `an empty email is rejected locally and never reaches the engine`() =
        runTest(mainRule.testDispatcher) {
            val viewModel = viewModel()
            runCurrent()

            viewModel.submitEmail("   ")
            runCurrent()

            assertTrue(gateway.emailCalls.isEmpty())
            assertEquals(
                dev.benyamin.kumiho.ui.setup.SetupMessage.EMAIL_REQUIRED,
                viewModel.state.value.signIn.message,
            )
        }

    @Test
    fun `a login engine error is shown inline and the pane stays usable`() =
        runTest(mainRule.testDispatcher) {
            gateway.onEmail = { LoginState("email", it, null, null, "unknown account") }
            val viewModel = viewModel()
            runCurrent()

            viewModel.submitEmail("user@example.com")
            runCurrent()

            val state = viewModel.state.value
            assertEquals("unknown account", state.signIn.error)
            assertFalse(state.signIn.busy)
            assertEquals(SignInPhase.EMAIL, state.signIn.phase)
        }

    // ---- signing out -----------------------------------------------------

    @Test
    fun `sign out asks first then logs out and shows the pane`() =
        runTest(mainRule.testDispatcher) {
            gateway.accountResult = {
                account(
                    email = if (gateway.signedIn) "user@example.com" else null,
                    tokenExpires = if (gateway.signedIn) TOKEN_EXPIRY else 0L,
                    error = if (gateway.signedIn) null else "not signed in",
                )
            }
            gateway.signedIn = true
            val viewModel = viewModel()
            runCurrent()
            assertTrue(viewModel.state.value.signedIn)

            viewModel.requestSignOut()
            assertTrue(viewModel.state.value.signOutDialog)
            assertEquals("no engine call before the confirmation", 0, gateway.logoutCalls)

            viewModel.confirmSignOut()
            runCurrent()

            assertEquals(1, gateway.logoutCalls)
            val state = viewModel.state.value
            assertFalse(state.signOutDialog)
            assertFalse(state.signingOut)
            assertFalse(state.signedIn)
            assertTrue(state.showSignIn)
            assertEquals(SignInPhase.EMAIL, state.signIn.phase)
        }

    @Test
    fun `dismissing the sign-out dialog keeps the session`() = runTest(mainRule.testDispatcher) {
        gateway.accountResult = { account() }
        val viewModel = viewModel()
        runCurrent()

        viewModel.requestSignOut()
        viewModel.dismissSignOut()
        runCurrent()

        assertFalse(viewModel.state.value.signOutDialog)
        assertEquals(0, gateway.logoutCalls)
        assertTrue(viewModel.state.value.signedIn)
    }

    @Test
    fun `a failed logout reports it and stays signed in`() = runTest(mainRule.testDispatcher) {
        gateway.accountResult = { account() }
        gateway.logoutError = IllegalStateException("the token is stuck")
        val viewModel = viewModel()
        runCurrent()

        viewModel.requestSignOut()
        viewModel.confirmSignOut()
        runCurrent()

        val state = viewModel.state.value
        assertEquals(AccountMessage.SIGN_OUT_FAILED, state.message)
        assertEquals("the token is stuck", state.error)
        assertFalse(state.signingOut)
        assertTrue(state.signedIn)
    }

    // ---- fake ------------------------------------------------------------

    private class FakeGateway : AccountGateway {
        var accountResult: suspend () -> Account = { error("no account result configured") }
        var onEmail: suspend (String) -> LoginState = { LoginState("password", it, null, null, null) }
        var onPassword: suspend (String) -> LoginState = { LoginState("2fa", null, null, null, null) }
        var onCode: suspend (String) -> LoginState = { LoginState("done", null, null, null, null) }
        var logoutError: Exception? = null
        var logoutCalls = 0
        var signedIn = false
        val emailCalls = mutableListOf<String>()
        val passwordCalls = mutableListOf<String>()
        val codeCalls = mutableListOf<String>()

        override suspend fun account(): Account = accountResult()

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

        override suspend fun logout() {
            logoutCalls++
            logoutError?.let { throw it }
            signedIn = false
        }
    }

    private companion object {
        val TOKEN_EXPIRY: Long = Instant.parse("2026-11-21T12:00:00Z").epochSecond

        fun account(
            email: String? = "user@example.com",
            uid: String? = "uid-123",
            subscribed: Boolean = true,
            maxBytes: Long? = 2L * 1024 * 1024 * 1024,
            limitedBandwidth: Boolean = false,
            quotaRemaining: Long? = 1024L * 1024 * 1024,
            tokenExpires: Long = TOKEN_EXPIRY,
            error: String? = null,
        ) = Account(
            email = email,
            uid = uid,
            subscribed = subscribed,
            maxBytes = maxBytes,
            limitedBandwidth = limitedBandwidth,
            quotaRemaining = quotaRemaining,
            tokenExpires = tokenExpires,
            error = error,
        )
    }
}
