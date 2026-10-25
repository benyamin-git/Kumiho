package dev.benyamin.kumiho.ui.account

import dev.benyamin.kumiho.engine.Account
import dev.benyamin.kumiho.engine.AccountSummary
import dev.benyamin.kumiho.engine.Accounts
import dev.benyamin.kumiho.engine.LoginState
import dev.benyamin.kumiho.engine.Quota
import dev.benyamin.kumiho.test.MainDispatcherRule
import dev.benyamin.kumiho.ui.setup.SignInPhase
import java.time.Instant
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/** The account list, browser flow and rotation state machine on the JVM. */
@OptIn(ExperimentalCoroutinesApi::class)
class AccountAccountsTest {

    @get:Rule
    val mainRule = MainDispatcherRule(StandardTestDispatcher())

    private val gateway = FakeGateway()

    private fun viewModel(
        openExternalBrowser: (String) -> Boolean = { false },
        onSignedOut: () -> Unit = {},
    ): AccountViewModel = AccountViewModel(
        gateway = gateway,
        onSignedOut = onSignedOut,
        openExternalBrowser = openExternalBrowser,
        pollIntervalMillis = 3_000,
        browserPollIntervalMillis = 1_000,
        thresholdDebounceMillis = 600,
    )

    @Test
    fun `refresh loads accounts and active entitlement`() = runTest(mainRule.testDispatcher) {
        gateway.accountResult = { account() }
        gateway.accountsResult = { accounts() }

        val viewModel = viewModel()
        runCurrent()

        val state = viewModel.state.value
        assertTrue(state.signedIn)
        assertEquals("a", state.accounts?.active)
        assertEquals("a", state.selectedID)
        assertEquals(2, state.accounts?.accounts?.size)
        assertEquals(1, gateway.accountCalls)
        assertTrue(gateway.accountsCalls > 0)
    }

    @Test
    fun `browser start exposes URL and completion refreshes list`() =
        runTest(mainRule.testDispatcher) {
            gateway.accountResult = { account() }
            gateway.accountsResult = { accounts() }
            gateway.addStartResult = { _, _, _ ->
                LoginState("browser", null, null, null, null, method = "browser", url = URL, loginId = "flow-1")
            }
            val viewModel = viewModel()
            runCurrent()
            val before = gateway.accountsCalls

            viewModel.startBrowser()
            runCurrent()

            assertEquals("browser", gateway.addStartCalls.single().first)
            assertEquals("flow-1", viewModel.state.value.browser?.id)
            assertEquals(URL, viewModel.state.value.browser?.url)
            assertEquals(URL, viewModel.state.value.browserLaunchURL)
            assertTrue("the in-app browser opens on start", viewModel.state.value.browserWebViewOpen)
            assertFalse(viewModel.state.value.addDialog)

            gateway.statusResult = { LoginState("done", "user@example.com", null, null, null) }
            advanceTimeBy(1_000)
            runCurrent()

            assertNull(viewModel.state.value.browser)
            assertNull(viewModel.state.value.browserLaunchURL)
            assertTrue(gateway.accountsCalls > before)
            assertEquals(SignInPhase.AUTHENTICATED, viewModel.state.value.signIn.phase)
        }

    @Test
    fun `browser cancel clears waiting state`() = runTest(mainRule.testDispatcher) {
        gateway.accountResult = { account() }
        gateway.accountsResult = { accounts() }
        gateway.addStartResult = { _, _, _ ->
            LoginState("browser", null, null, null, null, method = "browser", url = URL, loginId = "flow-1")
        }
        val viewModel = viewModel()
        runCurrent()
        viewModel.startBrowser()
        runCurrent()

        viewModel.cancelBrowser()
        runCurrent()

        assertEquals(listOf("flow-1"), gateway.addCancelCalls)
        assertNull(viewModel.state.value.browser)
        assertNull(viewModel.state.value.browserLaunchURL)
    }

    @Test
    fun `use and remove reload the list`() = runTest(mainRule.testDispatcher) {
        gateway.accountResult = { account() }
        gateway.accountsResult = { accounts() }
        val viewModel = viewModel()
        runCurrent()

        viewModel.useAccount("b")
        runCurrent()
        assertEquals(listOf("b"), gateway.useCalls)
        assertTrue(gateway.accountsCalls >= 2)

        gateway.accountsResult = { accounts(active = "a") }
        viewModel.requestRemove(accounts().accounts[1])
        viewModel.confirmRemove()
        runCurrent()

        assertEquals(listOf("b"), gateway.removeCalls)
        assertEquals("a", viewModel.state.value.accounts?.active)
        assertNull(viewModel.state.value.removeTarget)
    }

    @Test
    fun `reauth uses the account id`() = runTest(mainRule.testDispatcher) {
        gateway.accountResult = { account() }
        gateway.accountsResult = { accounts() }
        gateway.addStartResult = { _, _, _ ->
            LoginState("browser", null, null, null, null, method = "browser", url = URL, loginId = "flow-2")
        }
        val viewModel = viewModel()
        runCurrent()

        viewModel.reauth(accounts().accounts[1])
        runCurrent()

        assertEquals(Triple("browser", "two@example.com", "b"), gateway.addStartCalls.single())
        assertEquals("flow-2", viewModel.state.value.browser?.id)
    }

    @Test
    fun `remove active copy names the next account`() {
        val view = accounts()
        assertEquals("two@example.com", AccountFormat.nextActive(view, view.accounts[0])?.email)
        assertNull(AccountFormat.nextActive(view, view.accounts[1]))
        assertNull(AccountFormat.nextActive(null, view.accounts[0]))
    }

    @Test
    fun `rotation threshold is clamped to 1-50`() = runTest(mainRule.testDispatcher) {
        gateway.accountResult = { account() }
        gateway.accountsResult = { accounts() }
        val viewModel = viewModel()
        runCurrent()

        viewModel.setThreshold(0)
        assertEquals(1, viewModel.state.value.accounts?.threshold)
        advanceTimeBy(600)
        runCurrent()
        assertEquals(Triple(false, 1, false), gateway.rotationCalls.last())

        viewModel.setThreshold(75)
        assertEquals(50, viewModel.state.value.accounts?.threshold)
        advanceTimeBy(600)
        runCurrent()
        assertEquals(Triple(false, 50, false), gateway.rotationCalls.last())

        viewModel.setRotation(true)
        runCurrent()
        assertEquals(true, gateway.rotationCalls.last().first)
        assertFalse(viewModel.state.value.rotationSaving)
    }

    @Test
    fun `browser error keeps previous accounts`() = runTest(mainRule.testDispatcher) {
        gateway.accountResult = { account() }
        gateway.accountsResult = { accounts() }
        gateway.addStartResult = { _, _, _ ->
            LoginState("browser", null, null, null, null, method = "browser", url = URL, loginId = "flow-1")
        }
        val viewModel = viewModel()
        runCurrent()
        viewModel.startBrowser()
        runCurrent()
        val before = viewModel.state.value.accounts

        gateway.statusResult = {
            LoginState("error", null, null, null, "browser sign-in timed out")
        }
        advanceTimeBy(1_000)
        runCurrent()

        assertEquals(before, viewModel.state.value.accounts)
        assertEquals("browser sign-in timed out", viewModel.state.value.browser?.error)
        assertNotNull(viewModel.state.value.browser)
    }

    @Test
    fun `browser resume adopts existing engine flow after viewmodel recreation`() =
        runTest(mainRule.testDispatcher) {
            gateway.accountResult = { account() }
            gateway.accountsResult = { accounts() }
            gateway.statusResult = { id ->
                if (id.isEmpty()) {
                    LoginState("browser", null, null, null, null, method = "browser", url = URL, loginId = "flow-9")
                } else {
                    error("unexpected poll")
                }
            }

            val viewModel = viewModel()
            runCurrent()

            assertEquals("flow-9", viewModel.state.value.browser?.id)
            assertEquals(URL, viewModel.state.value.browser?.url)
        }

    @Test
    fun `browserless launch shows password fallback banner`() = runTest(mainRule.testDispatcher) {
        gateway.accountResult = { account() }
        gateway.accountsResult = { accounts() }
        gateway.addStartResult = { _, _, _ ->
            LoginState("browser", null, null, null, null, method = "browser", url = URL, loginId = "flow-1")
        }
        val viewModel = viewModel()
        runCurrent()
        viewModel.startBrowser()
        runCurrent()

        viewModel.browserUnavailable()
        runCurrent()

        assertEquals(AccountMessage.NO_BROWSER, viewModel.state.value.message)
        assertNotNull("the flow must stay cancellable", viewModel.state.value.browser)
        assertNotNull(viewModel.state.value.browser?.id)
    }

    @Test
    fun `add password opens the pane and cancel clears the pending flow`() =
        runTest(mainRule.testDispatcher) {
            gateway.accountResult = { account() }
            gateway.accountsResult = { accounts() }
            val viewModel = viewModel()
            runCurrent()

            viewModel.startPassword()
            assertTrue(viewModel.state.value.addingAccount)
            assertEquals(SignInPhase.EMAIL, viewModel.state.value.signIn.phase)

            viewModel.cancelAddAccount()
            runCurrent()

            assertFalse(viewModel.state.value.addingAccount)
            assertEquals(SignInPhase.AUTHENTICATED, viewModel.state.value.signIn.phase)
            assertEquals(listOf(""), gateway.loginCancelCalls)
        }

    @Test
    fun `browser start failure clears the waiting card and reports it`() =
        runTest(mainRule.testDispatcher) {
            gateway.accountResult = { account() }
            gateway.accountsResult = { accounts() }
            gateway.addStartResult = { _, _, _ ->
                LoginState("error", null, null, null, "browser flow failed")
            }
            val viewModel = viewModel()
            runCurrent()

            viewModel.startBrowser()
            runCurrent()

            assertNull(viewModel.state.value.browser)
            assertEquals(AccountMessage.ACTION_FAILED, viewModel.state.value.message)
            assertEquals("browser flow failed", viewModel.state.value.error)
        }

    @Test
    fun `closing and reopening the webview keeps the browser flow`() =
        runTest(mainRule.testDispatcher) {
            gateway.accountResult = { account() }
            gateway.accountsResult = { accounts() }
            gateway.addStartResult = { _, _, _ ->
                LoginState("browser", null, null, null, null, method = "browser", url = URL, loginId = "flow-1")
            }
            val viewModel = viewModel()
            runCurrent()
            viewModel.startBrowser()
            runCurrent()

            viewModel.closeBrowserWebView()
            assertFalse(viewModel.state.value.browserWebViewOpen)
            assertNotNull("closing the browser must not cancel the flow", viewModel.state.value.browser)
            assertEquals(URL, viewModel.state.value.browserLaunchURL)

            viewModel.openBrowser()
            assertTrue(viewModel.state.value.browserWebViewOpen)
        }

    @Test
    fun `a redirect submit sends the full URL and id and settles on done`() =
        runTest(mainRule.testDispatcher) {
            gateway.accountResult = { account() }
            gateway.accountsResult = { accounts() }
            gateway.addStartResult = { _, _, _ ->
                LoginState("browser", null, null, null, null, method = "browser", url = URL, loginId = "flow-1")
            }
            val viewModel = viewModel()
            runCurrent()
            viewModel.startBrowser()
            runCurrent()
            val before = gateway.accountsCalls

            gateway.statusResult = { LoginState("done", "user@example.com", null, null, null) }
            viewModel.submitBrowserRedirect(FULL_REDIRECT)
            runCurrent()

            assertEquals(listOf(FULL_REDIRECT to "flow-1"), gateway.loginCompleteCalls)
            assertNull(viewModel.state.value.browser)
            assertFalse(viewModel.state.value.browserWebViewOpen)
            assertFalse(viewModel.state.value.browserPasteOpen)
            assertEquals("", viewModel.state.value.browserPasteValue)
            assertTrue("completion refreshes the list", gateway.accountsCalls > before)
        }

    @Test
    fun `a retryable redirect error closes the webview and prefills the paste field`() =
        runTest(mainRule.testDispatcher) {
            gateway.accountResult = { account() }
            gateway.accountsResult = { accounts() }
            gateway.addStartResult = { _, _, _ ->
                LoginState("browser", null, null, null, null, method = "browser", url = URL, loginId = "flow-1")
            }
            gateway.loginCompleteResult = { _, _ ->
                throw IllegalStateException("the OAuth state does not match")
            }
            val viewModel = viewModel()
            runCurrent()
            viewModel.startBrowser()
            runCurrent()

            viewModel.submitBrowserRedirect(FULL_REDIRECT)
            runCurrent()

            assertNotNull("a retryable error keeps the flow", viewModel.state.value.browser)
            assertFalse(viewModel.state.value.browserWebViewOpen)
            assertTrue(viewModel.state.value.browserPasteOpen)
            assertEquals(FULL_REDIRECT, viewModel.state.value.browserPasteValue)
            assertEquals(
                "the OAuth state does not match",
                viewModel.state.value.browserPasteError,
            )

            // The WebView will not emit the same redirect again; Continue retries
            // the stored URL once, and a nil completion settles through polling.
            gateway.loginCompleteResult = { _, _ -> }
            gateway.statusResult = { LoginState("done", "user@example.com", null, null, null) }
            viewModel.submitBrowserPaste()
            runCurrent()

            assertEquals(2, gateway.loginCompleteCalls.size)
            assertEquals(FULL_REDIRECT to "flow-1", gateway.loginCompleteCalls.last())
            assertNull(viewModel.state.value.browser)
        }

    @Test
    fun `a blank paste is rejected without an engine call`() =
        runTest(mainRule.testDispatcher) {
            gateway.accountResult = { account() }
            gateway.accountsResult = { accounts() }
            gateway.addStartResult = { _, _, _ ->
                LoginState("browser", null, null, null, null, method = "browser", url = URL, loginId = "flow-1")
            }
            val viewModel = viewModel()
            runCurrent()
            viewModel.startBrowser()
            runCurrent()
            viewModel.toggleBrowserPaste()
            viewModel.setBrowserPasteValue("   ")

            viewModel.submitBrowserPaste()
            runCurrent()

            assertTrue(gateway.loginCompleteCalls.isEmpty())
            assertEquals(BrowserPasteMessage.REQUIRED, viewModel.state.value.browserPasteMessage)
            assertTrue(viewModel.state.value.browserPasteOpen)
        }

    @Test
    fun `an unavailable webview opens the system fallback and reveals the paste field`() =
        runTest(mainRule.testDispatcher) {
            gateway.accountResult = { account() }
            gateway.accountsResult = { accounts() }
            gateway.addStartResult = { _, _, _ ->
                LoginState("browser", null, null, null, null, method = "browser", url = URL, loginId = "flow-1")
            }
            val opened = mutableListOf<String>()
            val viewModel = viewModel(openExternalBrowser = { url ->
                opened += url
                true
            })
            runCurrent()
            viewModel.startBrowser()
            runCurrent()

            viewModel.browserUnavailable()
            runCurrent()

            assertEquals(listOf(URL), opened)
            assertFalse(viewModel.state.value.browserWebViewOpen)
            assertTrue(viewModel.state.value.browserPasteOpen)
            assertTrue(viewModel.state.value.browserFallback)
            assertNotNull("the flow stays cancellable", viewModel.state.value.browser)
            assertNull("a working system browser is not an error", viewModel.state.value.message)
        }

    @Test
    fun `an unavailable system browser still reveals the paste field and reports it`() =
        runTest(mainRule.testDispatcher) {
            gateway.accountResult = { account() }
            gateway.accountsResult = { accounts() }
            gateway.addStartResult = { _, _, _ ->
                LoginState("browser", null, null, null, null, method = "browser", url = URL, loginId = "flow-1")
            }
            val viewModel = viewModel(openExternalBrowser = { false })
            runCurrent()
            viewModel.startBrowser()
            runCurrent()

            viewModel.browserUnavailable()
            runCurrent()

            assertEquals(AccountMessage.NO_BROWSER, viewModel.state.value.message)
            assertTrue(viewModel.state.value.browserPasteOpen)
            assertNotNull(viewModel.state.value.browser)
        }

    @Test
    fun `cancel clears the browser, webview and paste state`() =
        runTest(mainRule.testDispatcher) {
            gateway.accountResult = { account() }
            gateway.accountsResult = { accounts() }
            gateway.addStartResult = { _, _, _ ->
                LoginState("browser", null, null, null, null, method = "browser", url = URL, loginId = "flow-1")
            }
            val viewModel = viewModel()
            runCurrent()
            viewModel.startBrowser()
            runCurrent()
            viewModel.toggleBrowserPaste()
            viewModel.setBrowserPasteValue(FULL_REDIRECT)

            viewModel.cancelBrowser()
            runCurrent()

            assertEquals(listOf("flow-1"), gateway.addCancelCalls)
            assertNull(viewModel.state.value.browser)
            assertNull(viewModel.state.value.browserLaunchURL)
            assertFalse(viewModel.state.value.browserWebViewOpen)
            assertFalse(viewModel.state.value.browserPasteOpen)
            assertEquals("", viewModel.state.value.browserPasteValue)
            assertNull(viewModel.state.value.browserPasteError)
        }

    @Test
    fun `use password cancels the browser flow and opens the password pane`() =
        runTest(mainRule.testDispatcher) {
            gateway.accountResult = { account() }
            gateway.accountsResult = { accounts() }
            gateway.addStartResult = { _, _, _ ->
                LoginState("browser", null, null, null, null, method = "browser", url = URL, loginId = "flow-1")
            }
            val viewModel = viewModel()
            runCurrent()
            viewModel.startBrowser()
            runCurrent()

            viewModel.startPassword()
            runCurrent()

            assertNull(viewModel.state.value.browser)
            assertEquals(listOf("flow-1"), gateway.addCancelCalls)
            assertTrue(viewModel.state.value.addingAccount)
            assertEquals(SignInPhase.EMAIL, viewModel.state.value.signIn.phase)
        }

    @Test
    fun `sign out clears the browser cookies only after logout succeeds`() =
        runTest(mainRule.testDispatcher) {
            gateway.accountResult = { account() }
            gateway.accountsResult = { accounts() }
            var cleared = 0
            val viewModel = viewModel(onSignedOut = { cleared++ })
            runCurrent()

            gateway.logoutFailure = IllegalStateException("the engine is busy")
            viewModel.requestSignOut()
            viewModel.confirmSignOut()
            runCurrent()
            assertEquals("a failed logout must keep the browser session", 0, cleared)
            assertEquals(AccountMessage.SIGN_OUT_FAILED, viewModel.state.value.message)

            gateway.logoutFailure = null
            viewModel.requestSignOut()
            viewModel.confirmSignOut()
            runCurrent()
            assertEquals("a successful logout clears the browser session", 1, cleared)
        }

    @Test
    fun `rotation threshold waits for the last value in a burst`() =
        runTest(mainRule.testDispatcher) {
            gateway.accountResult = { account() }
            gateway.accountsResult = { accounts() }
            val viewModel = viewModel()
            runCurrent()

            viewModel.setThreshold(11)
            advanceTimeBy(300)
            viewModel.setThreshold(12)
            advanceTimeBy(300)
            runCurrent()
            assertTrue(gateway.rotationCalls.isEmpty())

            advanceTimeBy(300)
            runCurrent()
            assertEquals(Triple(false, 12, false), gateway.rotationCalls.single())
        }

    @Test
    fun `rename normalizes the label before the engine call`() =
        runTest(mainRule.testDispatcher) {
            gateway.accountResult = { account() }
            gateway.accountsResult = { accounts() }
            val viewModel = viewModel()
            runCurrent()

            viewModel.requestRename(accounts().accounts[0])
            viewModel.confirmRename("  Work  ")
            runCurrent()

            assertEquals(listOf("a" to "Work"), gateway.renameCalls)
            assertNull(viewModel.state.value.renameTarget)
        }

    private inner class FakeGateway : AccountGateway {
        var accountResult: suspend () -> Account = { account() }
        var accountsResult: suspend () -> Accounts = { accounts() }
        var addStartResult: suspend (String, String?, String?) -> LoginState = { _, _, _ ->
            LoginState("browser", null, null, null, null, method = "browser", url = URL, loginId = "flow-1")
        }
        var statusResult: suspend (String) -> LoginState = { error("sign-in flow not found") }
        var loginCompleteResult: suspend (String, String?) -> Unit = { _, _ -> }
        var logoutFailure: Exception? = null

        var accountCalls = 0
        var accountsCalls = 0
        val addStartCalls = mutableListOf<Triple<String, String?, String?>>()
        val addCancelCalls = mutableListOf<String>()
        val loginCompleteCalls = mutableListOf<Pair<String, String?>>()
        val useCalls = mutableListOf<String>()
        val removeCalls = mutableListOf<String>()
        val renameCalls = mutableListOf<Pair<String, String>>()
        val rotationCalls = mutableListOf<Triple<Boolean, Int, Boolean>>()
        val loginCancelCalls = mutableListOf<String>()

        override suspend fun account(): Account {
            accountCalls++
            return accountResult()
        }

        override suspend fun accountsList(): Accounts {
            accountsCalls++
            return accountsResult()
        }

        override suspend fun accountsAddStart(
            method: String,
            email: String?,
            accountId: String?,
        ): LoginState {
            addStartCalls += Triple(method, email, accountId)
            return addStartResult(method, email, accountId)
        }

        override suspend fun accountsAddStatus(id: String): LoginState = statusResult(id)

        override suspend fun accountsAddCancel(id: String) {
            addCancelCalls += id
        }

        override suspend fun loginComplete(redirectUrl: String, id: String?) {
            loginCompleteCalls += redirectUrl to id
            loginCompleteResult(redirectUrl, id)
        }

        override suspend fun useAccount(id: String) {
            useCalls += id
            accountsResult = { accounts(active = id) }
        }

        override suspend fun removeAccount(id: String) {
            removeCalls += id
            accountsResult = { accounts(active = "a").copy(accounts = accounts().accounts.filterNot { it.id == id }) }
        }

        override suspend fun renameAccount(id: String, label: String) {
            renameCalls += id to label
        }

        override suspend fun setRotation(enabled: Boolean, threshold: Int) {
            rotationCalls += Triple(enabled, threshold, false)
            accountsResult = { accounts().copy(rotation = enabled, threshold = threshold) }
        }

        override suspend fun loginEmail(email: String): LoginState =
            LoginState("password", email, null, null, null)

        override suspend fun loginPassword(password: String): LoginState =
            LoginState("done", "user@example.com", null, null, null)

        override suspend fun login2fa(code: String): LoginState =
            LoginState("done", "user@example.com", null, null, null)

        override suspend fun loginCancel(id: String) {
            loginCancelCalls += id
        }

        override suspend fun logout() {
            logoutFailure?.let { throw it }
        }
    }

    private companion object {
        const val URL = "https://vpn.mozilla.org/api/v2/vpn/login/android?port=49152"
        const val FULL_REDIRECT =
            "urn:ietf:wg:oauth:2.0:oob:oauth-redirect-webchannel?code=code-1&state=state-1"
        val TOKEN_EXPIRY: Long = Instant.parse("2026-11-21T12:00:00Z").epochSecond

        fun account() = Account(
            email = "user@example.com",
            uid = "uid-123",
            subscribed = true,
            maxBytes = 2L * 1024 * 1024 * 1024,
            limitedBandwidth = false,
            quotaRemaining = 1024L * 1024 * 1024,
            tokenExpires = TOKEN_EXPIRY,
            error = null,
        )

        fun accounts(active: String = "a") = Accounts(
            accounts = listOf(
                AccountSummary(
                    id = "a",
                    email = "one@example.com",
                    label = "Home",
                    method = "browser",
                    active = active == "a",
                    reauthRequired = false,
                    lastUsedAt = 1_761_086_400L,
                    quota = Quota(limit = 1_000L, remaining = 820L, reset = null),
                ),
                AccountSummary(
                    id = "b",
                    email = "two@example.com",
                    label = "Work",
                    method = "password",
                    active = active == "b",
                    reauthRequired = true,
                    lastUsedAt = 0L,
                    quota = null,
                ),
            ),
            active = active,
            rotation = false,
            threshold = 10,
            lastMethod = "browser",
            notice = null,
            error = null,
        )
    }
}
