package dev.benyamin.kumiho.ui.account

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dev.benyamin.kumiho.engine.Account
import dev.benyamin.kumiho.engine.AccountSummary
import dev.benyamin.kumiho.engine.Accounts
import dev.benyamin.kumiho.engine.EngineClient
import dev.benyamin.kumiho.engine.LoginState
import dev.benyamin.kumiho.ui.setup.SetupMessage
import dev.benyamin.kumiho.ui.setup.SignInPaneState
import dev.benyamin.kumiho.ui.setup.SignInPhase
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.TimeoutCancellationException
import kotlinx.coroutines.async
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.supervisorScope
import kotlinx.coroutines.withTimeout

/** The account calls the screen makes; JVM tests use an in-memory fake. */
interface AccountGateway {
    suspend fun account(): Account

    suspend fun accountsList(): Accounts

    suspend fun loginEmail(email: String): LoginState

    suspend fun loginPassword(password: String): LoginState

    suspend fun login2fa(code: String): LoginState

    suspend fun loginCancel(id: String)

    suspend fun logout()

    suspend fun accountsAddStart(
        method: String,
        email: String? = null,
        accountId: String? = null,
    ): LoginState

    suspend fun accountsAddStatus(id: String): LoginState

    suspend fun accountsAddCancel(id: String)

    suspend fun loginComplete(redirectUrl: String, id: String? = null)

    suspend fun useAccount(id: String)

    suspend fun removeAccount(id: String)

    suspend fun renameAccount(id: String, label: String)

    suspend fun setRotation(enabled: Boolean, threshold: Int)
}

/** The production gateway over the already-bound engine client. */
class EngineAccountGateway(private val engine: EngineClient) : AccountGateway {
    override suspend fun account(): Account = engine.account()

    override suspend fun accountsList(): Accounts = engine.accountsList()

    override suspend fun loginEmail(email: String): LoginState = engine.loginEmail(email)

    override suspend fun loginPassword(password: String): LoginState = engine.loginPassword(password)

    override suspend fun login2fa(code: String): LoginState = engine.login2fa(code)

    override suspend fun loginCancel(id: String) = engine.loginCancel(id)

    override suspend fun logout() = engine.logout()

    override suspend fun accountsAddStart(
        method: String,
        email: String?,
        accountId: String?,
    ): LoginState = engine.accountsAddStart(method, email, accountId)

    override suspend fun accountsAddStatus(id: String): LoginState = engine.accountsAddStatus(id)

    override suspend fun accountsAddCancel(id: String) = engine.accountsAddCancel(id)

    override suspend fun loginComplete(redirectUrl: String, id: String?) =
        engine.loginComplete(redirectUrl, id)

    override suspend fun useAccount(id: String) = engine.accountsUse(id)

    override suspend fun removeAccount(id: String) = engine.accountsRemove(id)

    override suspend fun renameAccount(id: String, label: String) =
        engine.accountsUpdate(id, label)

    override suspend fun setRotation(enabled: Boolean, threshold: Int) =
        engine.accountsSetRotation(enabled, threshold)
}

/**
 * The Account state machine: the signed-in card, the stored account list, the
 * browser/password sign-in panes, rotation and the account actions.
 *
 * It is a plain JVM state machine over [AccountGateway]; passwords and
 * verification codes live only in the composable for one submit and never
 * enter [state]. The browser redirect URL is retained only for retry prefill
 * and is cleared on success or cancel. Account-list and
 * entitlement loads run independently, so a failed entitlement lookup never
 * hides the list and a failed list read never hides the active card.
 */
class AccountViewModel(
    private val gateway: AccountGateway,
    private val onSignedOut: () -> Unit = {},
    private val openExternalBrowser: (String) -> Boolean = { false },
    private val pollIntervalMillis: Long = DEFAULT_POLL_INTERVAL_MS,
    private val browserPollIntervalMillis: Long = DEFAULT_BROWSER_POLL_INTERVAL_MS,
    private val thresholdDebounceMillis: Long = DEFAULT_THRESHOLD_DEBOUNCE_MS,
) : ViewModel() {
    private val mutableState = MutableStateFlow(AccountUiState())

    /** The whole screen state; the UI only reads it. */
    val state: StateFlow<AccountUiState> = mutableState.asStateFlow()

    private var loadJob: Job? = null
    private var pollJob: Job? = null
    private var browserJob: Job? = null
    private var actionJob: Job? = null
    private var thresholdJob: Job? = null
    private var resumed = true
    private var browserAdoptionAttempted = false

    init {
        loadAll()
        adoptBrowser()
    }

    // ---- account ---------------------------------------------------------

    /** A user-requested refresh; the last local fields stay visible on failure. */
    fun refresh() {
        loadAll(refresh = true)
    }

    fun retry() {
        loadAll(refresh = false)
    }

    fun dismissMessage() {
        mutableState.update { it.copy(message = null, error = null) }
    }

    /**
     * Fetches the entitlement and the account list concurrently. Either one may
     * fail without discarding the other's last good value.
     */
    private fun loadAll(refresh: Boolean = false) {
        if (loadJob?.isActive == true) return
        val hasAccount = mutableState.value.account != null
        val hasAccounts = mutableState.value.accounts != null
        mutableState.update {
            it.copy(
                loading = !hasAccount && !hasAccounts,
                refreshing = refresh && (hasAccount || hasAccounts),
                error = null,
                message = null,
            )
        }
        loadJob = viewModelScope.launch {
            val (entitlement, list) = supervisorScope {
                val account = async { loadEntitlement() }
                val accounts = async { loadAccounts() }
                account.await() to accounts.await()
            }

            var message: AccountMessage? = null
            var detail: String? = null

            when (entitlement) {
                is LoadResult.Ok -> applyAccount(entitlement.value)
                LoadResult.Timeout -> message = AccountMessage.TIMEOUT
                is LoadResult.Failed -> {
                    message = if (hasAccount) {
                        AccountMessage.REFRESH_FAILED
                    } else {
                        AccountMessage.LOAD_FAILED
                    }
                    detail = messageOf(entitlement.error)
                }
            }

            when (list) {
                is LoadResult.Ok -> mutableState.update {
                    it.copy(
                        accounts = list.value,
                        selectedID = list.value.active,
                    )
                }
                is LoadResult.Failed -> {
                    message = if (hasAccounts) {
                        AccountMessage.REFRESH_FAILED
                    } else {
                        AccountMessage.LOAD_FAILED
                    }
                    detail = detail ?: messageOf(list.error)
                }
                LoadResult.Timeout -> {
                    message = AccountMessage.REFRESH_FAILED
                }
            }

            mutableState.update {
                it.copy(
                    loading = false,
                    refreshing = false,
                    message = message ?: it.message,
                    error = detail ?: it.error,
                )
            }
        }
    }

    private suspend fun loadEntitlement(): LoadResult<Account> = try {
        LoadResult.Ok(withTimeout(ACCOUNT_TIMEOUT_MS) { gateway.account() })
    } catch (_: TimeoutCancellationException) {
        LoadResult.Timeout
    } catch (cancelled: CancellationException) {
        throw cancelled
    } catch (error: Exception) {
        LoadResult.Failed(error)
    }

    private suspend fun loadAccounts(): LoadResult<Accounts> = try {
        LoadResult.Ok(withTimeout(ACCOUNT_LIST_TIMEOUT_MS) { gateway.accountsList() })
    } catch (timeout: TimeoutCancellationException) {
        LoadResult.Failed(IllegalStateException("the account list timed out", timeout))
    } catch (cancelled: CancellationException) {
        throw cancelled
    } catch (error: Exception) {
        LoadResult.Failed(error)
    }

    private fun applyAccount(account: Account) {
        val signedIn = AccountFormat.isSignedIn(account)
        mutableState.update { current ->
            current.copy(
                account = account,
                loading = false,
                refreshing = false,
                error = AccountFormat.onlineError(account),
                signIn = if (current.addingAccount || current.browser != null) {
                    current.signIn
                } else if (signedIn) {
                    current.signIn.copy(
                        phase = SignInPhase.AUTHENTICATED,
                        accountEmail = account.email ?: current.signIn.accountEmail,
                        busy = false,
                    )
                } else {
                    current.signIn.copy(
                        phase = SignInPhase.EMAIL,
                        busy = false,
                    )
                },
            )
        }
    }

    // ---- sign in ---------------------------------------------------------

    fun submitEmail(email: String) {
        val trimmed = email.trim()
        if (trimmed.isEmpty()) {
            setSignInMessage(SetupMessage.EMAIL_REQUIRED)
            return
        }
        if (!signInIdle()) return
        mutableState.update {
            it.copy(
                signIn = it.signIn.copy(
                    email = trimmed,
                    phase = SignInPhase.EMAIL,
                    busy = true,
                    error = null,
                    message = null,
                ),
            )
        }
        viewModelScope.launch { runLogin { gateway.loginEmail(trimmed) } }
    }

    /** The password is captured by this request only; it is never stored. */
    fun submitPassword(password: String) {
        if (password.isEmpty()) {
            setSignInMessage(SetupMessage.PASSWORD_REQUIRED)
            return
        }
        if (!signInIdle()) return
        mutableState.update {
            it.copy(signIn = it.signIn.copy(busy = true, error = null, message = null))
        }
        viewModelScope.launch { runLogin { gateway.loginPassword(password) } }
    }

    /** The code is captured by this request only; it is never stored. */
    fun submitCode(code: String) {
        val trimmed = code.trim()
        if (trimmed.isEmpty()) {
            setSignInMessage(SetupMessage.CODE_REQUIRED)
            return
        }
        if (!signInIdle()) return
        mutableState.update {
            it.copy(signIn = it.signIn.copy(busy = true, error = null, message = null))
        }
        viewModelScope.launch { runLogin { gateway.login2fa(trimmed) } }
    }

    fun recheckEmailLink() {
        val current = mutableState.value
        if (current.signIn.phase != SignInPhase.EMAIL_LINK_WAITING) return
        if (current.signIn.busy) return
        restartEmailPolling(immediate = true)
    }

    private fun signInIdle(): Boolean = !mutableState.value.signIn.busy

    private suspend fun runLogin(call: suspend () -> LoginState) {
        try {
            applyLogin(call())
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            mutableState.update {
                it.copy(signIn = it.signIn.copy(busy = false, error = messageOf(error)))
            }
        }
    }

    private fun applyLogin(login: LoginState) {
        when (login.step) {
            STEP_EMAIL -> {
                stopEmailPolling()
                updateSignIn(
                    phase = SignInPhase.EMAIL,
                    email = login.email,
                    busy = false,
                    error = login.error,
                )
            }
            STEP_PASSWORD -> {
                stopEmailPolling()
                updateSignIn(
                    phase = SignInPhase.PASSWORD,
                    email = login.email,
                    busy = false,
                    error = login.error,
                )
            }
            STEP_2FA -> {
                val current = mutableState.value.signIn
                val method = login.verificationMethod ?: current.verificationMethod
                val link = method == SignInPaneState.EMAIL_LINK_METHOD
                val pending = link && isPendingLinkMessage(login.error)
                val wasWaiting = current.phase == SignInPhase.EMAIL_LINK_WAITING
                mutableState.update {
                    it.copy(
                        signIn = it.signIn.copy(
                            phase = if (link) SignInPhase.EMAIL_LINK_WAITING else SignInPhase.CODE,
                            email = login.email ?: it.signIn.email,
                            verificationMethod = method,
                            verificationReason = login.verificationReason
                                ?: it.signIn.verificationReason,
                            busy = false,
                            error = if (pending) null else login.error,
                            message = null,
                        ),
                    )
                }
                if (!link) {
                    stopEmailPolling()
                } else if (!wasWaiting) {
                    restartEmailPolling(immediate = false)
                }
            }
            STEP_DONE -> {
                mutableState.update {
                    it.copy(
                        addingAccount = false,
                        signIn = it.signIn.copy(
                            phase = SignInPhase.AUTHENTICATED,
                            accountEmail = login.email ?: it.signIn.accountEmail
                            ?: it.signIn.email.takeIf { value -> value.isNotBlank() },
                            busy = false,
                            error = login.error,
                            message = null,
                        ),
                    )
                }
                stopEmailPolling()
                loadAll(refresh = true)
            }
            else -> {
                mutableState.update {
                    it.copy(
                        signIn = it.signIn.copy(
                            busy = false,
                            error = login.error,
                            message = SetupMessage.UNEXPECTED_STEP,
                        ),
                    )
                }
            }
        }
    }

    private fun updateSignIn(
        phase: SignInPhase,
        email: String?,
        busy: Boolean,
        error: String?,
    ) {
        mutableState.update {
            it.copy(
                signIn = it.signIn.copy(
                    phase = phase,
                    email = email ?: it.signIn.email,
                    busy = busy,
                    error = error,
                    message = null,
                ),
            )
        }
    }

    private fun setSignInMessage(message: SetupMessage) {
        mutableState.update {
            it.copy(signIn = it.signIn.copy(message = message, error = null))
        }
    }

    // ---- account add flows ----------------------------------------------

    fun openAddDialog() {
        if (mutableState.value.browser != null) return
        mutableState.update { it.copy(addDialog = true, message = null, error = null) }
    }

    fun dismissAddDialog() {
        mutableState.update { it.copy(addDialog = false) }
    }

    /** Starts adding an account in the browser (also used for re-auth). */
    fun startBrowser(email: String? = null, accountId: String? = null) {
        val current = mutableState.value
        if (current.browser != null || current.signIn.busy) return
        mutableState.update {
            it.copy(
                addDialog = false,
                addingAccount = false,
                browser = BrowserSignIn("", null),
                browserLaunchURL = null,
                browserWebViewOpen = false,
                browserPasteOpen = false,
                browserPasteValue = "",
                browserPasteError = null,
                browserPasteMessage = null,
                browserSubmitting = false,
                browserFallback = false,
                message = null,
                error = null,
            )
        }
        viewModelScope.launch {
            try {
                val login = withTimeout(BROWSER_TIMEOUT_MS) {
                    gateway.accountsAddStart("browser", email, accountId)
                }
                when {
                    login.step == STEP_BROWSER && !login.loginId.isNullOrBlank() -> {
                        val browser = BrowserSignIn(login.loginId, login.url, login.error)
                        mutableState.update {
                            it.copy(
                                browser = browser,
                                browserLaunchURL = login.url ?: it.browserLaunchURL,
                                browserWebViewOpen = login.url != null && login.error == null,
                                browserPasteOpen = false,
                                browserPasteValue = "",
                                browserPasteError = null,
                                browserPasteMessage = null,
                                browserSubmitting = false,
                                browserFallback = false,
                            )
                        }
                        if (browser.error == null) restartBrowserPolling(immediate = false)
                    }
                    login.step == STEP_DONE -> {
                        clearBrowser()
                        loadAll(refresh = true)
                    }
                    else -> {
                        clearBrowser()
                        mutableState.update {
                            it.copy(
                                message = AccountMessage.ACTION_FAILED,
                                error = login.error ?: "browser sign-in did not start",
                            )
                        }
                    }
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                clearBrowser()
                mutableState.update {
                    it.copy(
                        message = AccountMessage.ACTION_FAILED,
                        error = messageOf(error),
                    )
                }
            }
        }
    }

    fun reauth(account: AccountSummary) {
        startBrowser(email = account.email, accountId = account.id)
    }

    /**
     * Opens the in-app browser for the active flow. The route renders the
     * full-screen dialog only while this flag and a URL are both present.
     */
    fun openBrowser() {
        val current = mutableState.value
        if (current.browser == null) return
        val url = (current.browserLaunchURL ?: current.browser.url)?.takeIf { it.isNotBlank() }
            ?: return
        mutableState.update {
            it.copy(
                browserLaunchURL = url,
                browserWebViewOpen = true,
                browserFallback = false,
                browserPasteMessage = null,
            )
        }
    }

    /** Closes the browser window but leaves the engine-owned flow active. */
    fun closeBrowserWebView() {
        if (!mutableState.value.browserWebViewOpen) return
        mutableState.update { it.copy(browserWebViewOpen = false) }
    }

    /**
     * The in-app browser could not be created. Close it, hand the same URL to
     * the system browser, and reveal the paste field because that path cannot
     * intercept the redirect.
     */
    fun browserUnavailable() {
        val current = mutableState.value
        val browser = current.browser ?: return
        val url = current.browserLaunchURL ?: browser.url
        val opened = url != null && openExternalBrowser(url)
        mutableState.update {
            it.copy(
                browserWebViewOpen = false,
                browserPasteOpen = true,
                browserPasteError = null,
                browserPasteMessage = null,
                browserSubmitting = false,
                browserFallback = true,
                message = if (opened) null else AccountMessage.NO_BROWSER,
            )
        }
    }

    /** Expands or collapses the paste fallback in the waiting card. */
    fun toggleBrowserPaste() {
        mutableState.update {
            it.copy(
                browserPasteOpen = !it.browserPasteOpen,
                browserPasteMessage = null,
            )
        }
    }

    /** Edits the redirect URL; a keystroke clears the previous inline error. */
    fun setBrowserPasteValue(value: String) {
        mutableState.update {
            it.copy(
                browserPasteValue = value,
                browserPasteError = null,
                browserPasteMessage = null,
            )
        }
    }

    /** Submits the pasted redirect URL after local validation. */
    fun submitBrowserPaste() {
        val url = mutableState.value.browserPasteValue.trim()
        if (url.isEmpty()) {
            mutableState.update {
                it.copy(
                    browserPasteOpen = true,
                    browserPasteError = null,
                    browserPasteMessage = BrowserPasteMessage.REQUIRED,
                )
            }
            return
        }
        submitBrowserRedirect(url)
    }

    /**
     * Completes the browser flow with the full redirect URL. A nil reply is a
     * terminal state: the WebView closes and the existing status poll settles
     * the account. A retryable error closes the WebView too, but keeps the flow
     * and prefills the paste field with the URL, because the one-shot WebView
     * will not emit the same redirect twice.
     */
    fun submitBrowserRedirect(redirectUrl: String) {
        val url = redirectUrl.trim()
        if (url.isEmpty()) {
            mutableState.update {
                it.copy(
                    browserPasteOpen = true,
                    browserPasteError = null,
                    browserPasteMessage = BrowserPasteMessage.REQUIRED,
                )
            }
            return
        }
        val current = mutableState.value
        val browser = current.browser ?: return
        if (current.browserSubmitting || browser.id.isBlank()) return
        val id = browser.id
        mutableState.update {
            it.copy(
                browserSubmitting = true,
                browserPasteError = null,
                browserPasteMessage = null,
            )
        }
        viewModelScope.launch {
            try {
                gateway.loginComplete(url, id)
                if (mutableState.value.browser?.id != id) return@launch
                mutableState.update {
                    it.copy(
                        browser = it.browser?.copy(error = null),
                        browserWebViewOpen = false,
                        browserPasteOpen = false,
                        browserPasteValue = "",
                        browserPasteError = null,
                        browserPasteMessage = null,
                        browserSubmitting = false,
                        browserFallback = false,
                    )
                }
                // Read the terminal state once through the existing poll
                // instead of inventing a second status path.
                restartBrowserPolling(immediate = true)
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                if (mutableState.value.browser?.id != id) return@launch
                mutableState.update {
                    it.copy(
                        browserWebViewOpen = false,
                        browserPasteOpen = true,
                        browserPasteValue = url,
                        browserPasteError = messageOf(error),
                        browserPasteMessage = null,
                        browserSubmitting = false,
                    )
                }
            }
        }
    }

    fun cancelBrowser() {
        val browser = mutableState.value.browser ?: return
        stopBrowserPolling()
        clearBrowser()
        mutableState.update { it.copy(message = null, error = null) }
        if (browser.id.isBlank()) return
        viewModelScope.launch {
            try {
                gateway.accountsAddCancel(browser.id)
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (_: Exception) {
                // The local flow is gone either way; the engine will discard a
                // late callback or report it on the next account refresh.
            }
        }
    }

    /** Opens the shared pane for an additional password sign-in. */
    fun startPassword() {
        val current = mutableState.value
        if (current.browser != null) cancelBrowser()
        stopEmailPolling()
        mutableState.update {
            it.copy(
                addDialog = false,
                addingAccount = true,
                message = null,
                error = null,
                signIn = SignInPaneState(phase = SignInPhase.EMAIL),
            )
        }
    }

    fun cancelAddAccount() {
        if (!mutableState.value.addingAccount) return
        stopEmailPolling()
        mutableState.update {
            it.copy(
                addingAccount = false,
                signIn = it.signIn.copy(
                    phase = if (it.signedIn) SignInPhase.AUTHENTICATED else SignInPhase.EMAIL,
                    busy = false,
                    error = null,
                    message = null,
                ),
            )
        }
        viewModelScope.launch {
            try {
                gateway.loginCancel("")
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (_: Exception) {
                // Clearing the local pane is enough; a pending engine state is
                // replaced by the next sign-in start.
            }
        }
    }

    // ---- browser flow ----------------------------------------------------

    /**
     * Empty-id status is the engine's adoption hook: after an Activity
     * recreation it returns the in-flight browser flow and its URL.
     */
    private fun adoptBrowser() {
        if (browserAdoptionAttempted) return
        browserAdoptionAttempted = true
        viewModelScope.launch {
            try {
                val login = withTimeout(BROWSER_TIMEOUT_MS) { gateway.accountsAddStatus("") }
                if (mutableState.value.browser == null) applyBrowser(login)
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (_: Exception) {
                // No flow is the normal first-run outcome.
            }
        }
    }

    private fun shouldPollBrowser(): Boolean {
        val browser = mutableState.value.browser
        return resumed && browser != null && browser.error == null
    }

    private fun restartBrowserPolling(immediate: Boolean) {
        stopBrowserPolling()
        if (!shouldPollBrowser()) return
        browserJob = viewModelScope.launch {
            if (immediate) pollBrowser()
            while (isActive && shouldPollBrowser()) {
                delay(browserPollIntervalMillis)
                if (!shouldPollBrowser()) return@launch
                pollBrowser()
            }
        }
    }

    private fun stopBrowserPolling() {
        browserJob?.cancel()
        browserJob = null
    }

    private suspend fun pollBrowser() {
        val id = mutableState.value.browser?.id.orEmpty()
        try {
            val login = withTimeout(BROWSER_TIMEOUT_MS) { gateway.accountsAddStatus(id) }
            applyBrowser(login)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            mutableState.update {
                it.copy(
                    browser = it.browser?.copy(error = messageOf(error)),
                    message = AccountMessage.ACTION_FAILED,
                    error = messageOf(error),
                )
            }
        }
    }

    private fun applyBrowser(login: LoginState) {
        when (login.step) {
            STEP_BROWSER -> {
                val current = mutableState.value.browser
                val id = login.loginId ?: current?.id
                if (id.isNullOrBlank()) {
                    mutableState.update {
                        it.copy(browser = it.browser?.copy(error = "the engine did not return a sign-in id"))
                    }
                    return
                }
                val url = login.url ?: current?.url
                val changed = current == null || current.id != id || current.url != url
                mutableState.update {
                    it.copy(
                        browser = BrowserSignIn(id, url, login.error),
                        browserLaunchURL = if (changed && url != null) url else it.browserLaunchURL,
                    )
                }
            }
            STEP_DONE -> {
                clearBrowser()
                loadAll(refresh = true)
            }
            STEP_CANCELLED -> clearBrowser()
            STEP_ERROR -> mutableState.update {
                it.copy(
                    browser = it.browser?.copy(
                        error = login.error ?: "browser sign-in failed",
                    ),
                    browserWebViewOpen = false,
                    browserSubmitting = false,
                    message = AccountMessage.ACTION_FAILED,
                    error = login.error,
                )
            }
            else -> mutableState.update {
                it.copy(
                    browser = it.browser?.copy(
                        error = login.error ?: "unexpected sign-in step ${login.step}",
                    ),
                    browserWebViewOpen = false,
                    browserSubmitting = false,
                    message = AccountMessage.ACTION_FAILED,
                    error = login.error,
                )
            }
        }
    }

    private fun clearBrowser() {
        stopBrowserPolling()
        mutableState.update {
            it.copy(
                browser = null,
                browserLaunchURL = null,
                browserWebViewOpen = false,
                browserPasteOpen = false,
                browserPasteValue = "",
                browserPasteError = null,
                browserPasteMessage = null,
                browserSubmitting = false,
                browserFallback = false,
            )
        }
    }

    // ---- account actions -------------------------------------------------

    fun useAccount(id: String) {
        if (actionJob?.isActive == true) return
        mutableState.update {
            it.copy(selectedID = id, message = null, error = null)
        }
        actionJob = viewModelScope.launch {
            try {
                gateway.useAccount(id)
                loadAll(refresh = true)
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                mutableState.update {
                    it.copy(
                        selectedID = it.accounts?.active,
                        message = AccountMessage.ACTION_FAILED,
                        error = messageOf(error),
                    )
                }
            }
        }
    }

    fun requestRemove(account: AccountSummary) {
        if (actionJob?.isActive == true) return
        mutableState.update { it.copy(removeTarget = account, message = null, error = null) }
    }

    fun dismissRemove() {
        mutableState.update { it.copy(removeTarget = null) }
    }

    fun confirmRemove() {
        if (actionJob?.isActive == true) return
        val target = mutableState.value.removeTarget ?: return
        mutableState.update { it.copy(removeTarget = null) }
        actionJob = viewModelScope.launch {
            try {
                gateway.removeAccount(target.id)
                loadAll(refresh = true)
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                mutableState.update {
                    it.copy(
                        message = AccountMessage.ACTION_FAILED,
                        error = messageOf(error),
                    )
                }
            }
        }
    }

    fun requestRename(account: AccountSummary) {
        if (actionJob?.isActive == true) return
        mutableState.update { it.copy(renameTarget = account, message = null, error = null) }
    }

    fun dismissRename() {
        mutableState.update { it.copy(renameTarget = null) }
    }

    fun confirmRename(label: String) {
        if (actionJob?.isActive == true) return
        val target = mutableState.value.renameTarget ?: return
        val normalized = AccountFormat.normalizeLabel(label, target.email)
        mutableState.update { it.copy(renameTarget = null) }
        actionJob = viewModelScope.launch {
            try {
                gateway.renameAccount(target.id, normalized)
                reloadAccountsAfterWrite()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                mutableState.update {
                    it.copy(
                        message = AccountMessage.ACTION_FAILED,
                        error = messageOf(error),
                    )
                }
            }
        }
    }

    // ---- rotation --------------------------------------------------------

    fun setRotation(enabled: Boolean) {
        val current = mutableState.value.accounts ?: return
        if (mutableState.value.rotationSaving) return
        val threshold = current.threshold.coerceIn(MIN_THRESHOLD, MAX_THRESHOLD)
        thresholdJob?.cancel()
        mutableState.update {
            it.copy(
                accounts = it.accounts?.copy(rotation = enabled),
                rotationSaving = true,
                message = null,
                error = null,
            )
        }
        viewModelScope.launch {
            try {
                gateway.setRotation(enabled, threshold)
                reloadAccountsAfterWrite()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                mutableState.update {
                    it.copy(
                        rotationSaving = false,
                        message = AccountMessage.ROTATION_FAILED,
                        error = messageOf(error),
                    )
                }
                reloadAccountsAfterWrite()
            }
        }
    }

    /**
     * The UI calls this for every keystroke; only the last value in a burst is
     * sent. The value is clamped before it reaches the engine.
     */
    fun setThreshold(value: Int) {
        val current = mutableState.value.accounts ?: return
        val clamped = value.coerceIn(MIN_THRESHOLD, MAX_THRESHOLD)
        mutableState.update {
            it.copy(accounts = it.accounts?.copy(threshold = clamped))
        }
        if (current.threshold == clamped) return
        thresholdJob?.cancel()
        thresholdJob = viewModelScope.launch {
            delay(thresholdDebounceMillis)
            val enabled = mutableState.value.accounts?.rotation ?: false
            mutableState.update { it.copy(rotationSaving = true, message = null, error = null) }
            try {
                gateway.setRotation(enabled, clamped)
                reloadAccountsAfterWrite()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                mutableState.update {
                    it.copy(
                        rotationSaving = false,
                        message = AccountMessage.ROTATION_FAILED,
                        error = messageOf(error),
                    )
                }
                reloadAccountsAfterWrite()
            }
        }
    }

    private suspend fun reloadAccountsAfterWrite() {
        try {
            val accounts = withTimeout(ACCOUNT_LIST_TIMEOUT_MS) { gateway.accountsList() }
            mutableState.update {
                it.copy(
                    accounts = accounts,
                    selectedID = accounts.active,
                    rotationSaving = false,
                )
            }
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            mutableState.update {
                it.copy(
                    rotationSaving = false,
                    message = AccountMessage.REFRESH_FAILED,
                    error = messageOf(error),
                )
            }
        }
    }

    // ---- signing out -----------------------------------------------------

    /** Opens the confirmation; no engine call happens until it is confirmed. */
    fun requestSignOut() {
        if (!mutableState.value.signedIn && !mutableState.value.hasAccounts) return
        mutableState.update { it.copy(signOutDialog = true) }
    }

    fun dismissSignOut() {
        mutableState.update { it.copy(signOutDialog = false) }
    }

    /**
     * Calls `logout` and stays on Account. The confirmation copy is the only
     * warning; tokens are cleared by the engine, never by Kotlin.
     */
    fun confirmSignOut() {
        val current = mutableState.value
        if (current.signingOut || (!current.signedIn && !current.hasAccounts)) return
        stopEmailPolling()
        clearBrowser()
        mutableState.update {
            it.copy(signingOut = true, signOutDialog = false, message = null, error = null)
        }
        viewModelScope.launch {
            try {
                gateway.logout()
                mutableState.update {
                    it.copy(
                        account = null,
                        accounts = null,
                        selectedID = null,
                        signingOut = false,
                        addingAccount = false,
                        signIn = SignInPaneState(phase = SignInPhase.EMAIL),
                    )
                }
                // The engine session is gone locally; clearing Mozilla's
                // WebView cookies must never block or fail that sign-out.
                try {
                    onSignedOut()
                } catch (_: RuntimeException) {
                    // No usable WebView or cookie jar on this device.
                }
                loadAll()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                mutableState.update {
                    it.copy(
                        signingOut = false,
                        message = AccountMessage.SIGN_OUT_FAILED,
                        error = messageOf(error),
                    )
                }
            }
        }
    }

    // ---- email-link polling ---------------------------------------------

    /** Polling runs only while the screen is resumed. */
    fun setScreenResumed(resumed: Boolean) {
        if (this.resumed == resumed) return
        this.resumed = resumed
        if (resumed) {
            if (mutableState.value.browser != null) {
                restartBrowserPolling(immediate = true)
            } else {
                adoptBrowser()
            }
            if (shouldPollEmail()) restartEmailPolling(immediate = false)
        } else {
            stopEmailPolling()
            stopBrowserPolling()
        }
    }

    private fun shouldPollEmail(): Boolean =
        resumed && mutableState.value.signIn.phase == SignInPhase.EMAIL_LINK_WAITING

    private fun restartEmailPolling(immediate: Boolean) {
        stopEmailPolling()
        if (!shouldPollEmail()) return
        if (immediate) {
            mutableState.update { it.copy(signIn = it.signIn.copy(busy = true)) }
        }
        pollJob = viewModelScope.launch {
            if (immediate) pollEmailLink()
            while (isActive && shouldPollEmail()) {
                delay(pollIntervalMillis)
                if (!shouldPollEmail()) return@launch
                pollEmailLink()
            }
        }
    }

    private fun stopEmailPolling() {
        pollJob?.cancel()
        pollJob = null
    }

    private suspend fun pollEmailLink() {
        mutableState.update { it.copy(signIn = it.signIn.copy(busy = true)) }
        try {
            applyLogin(gateway.login2fa(""))
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            mutableState.update {
                it.copy(signIn = it.signIn.copy(busy = false, error = messageOf(error)))
            }
        }
    }

    private fun messageOf(error: Throwable): String =
        error.message?.takeIf { it.isNotBlank() }
            ?: error::class.simpleName
            ?: error.javaClass.name

    private sealed interface LoadResult<out T> {
        data class Ok<T>(val value: T) : LoadResult<T>
        data object Timeout : LoadResult<Nothing>
        data class Failed(val error: Exception) : LoadResult<Nothing>
    }

    private companion object {
        const val ACCOUNT_TIMEOUT_MS = 35_000L
        const val ACCOUNT_LIST_TIMEOUT_MS = 10_000L
        const val BROWSER_TIMEOUT_MS = 10_000L
        const val DEFAULT_POLL_INTERVAL_MS = 3_000L
        const val DEFAULT_BROWSER_POLL_INTERVAL_MS = 1_000L
        const val DEFAULT_THRESHOLD_DEBOUNCE_MS = 600L
        const val MIN_THRESHOLD = 1
        const val MAX_THRESHOLD = 50
        const val STEP_EMAIL = "email"
        const val STEP_PASSWORD = "password"
        const val STEP_2FA = "2fa"
        const val STEP_BROWSER = "browser"
        const val STEP_DONE = "done"
        const val STEP_CANCELLED = "cancelled"
        const val STEP_ERROR = "error"
    }
}

private fun isPendingLinkMessage(message: String?): Boolean =
    !message.isNullOrBlank() && message.contains(PENDING_LINK_SENTINEL, ignoreCase = true)

private const val PENDING_LINK_SENTINEL = "not confirmed yet"
