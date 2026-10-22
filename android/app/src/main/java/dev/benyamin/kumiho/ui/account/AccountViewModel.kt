package dev.benyamin.kumiho.ui.account

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dev.benyamin.kumiho.engine.Account
import dev.benyamin.kumiho.engine.EngineClient
import dev.benyamin.kumiho.engine.LoginState
import dev.benyamin.kumiho.ui.setup.SetupMessage
import dev.benyamin.kumiho.ui.setup.SignInPaneState
import dev.benyamin.kumiho.ui.setup.SignInPhase
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.TimeoutCancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeout

/** The account calls the screen makes; JVM tests use an in-memory fake. */
interface AccountGateway {
    suspend fun account(): Account

    suspend fun loginEmail(email: String): LoginState

    suspend fun loginPassword(password: String): LoginState

    suspend fun login2fa(code: String): LoginState

    suspend fun logout()
}

/** The production gateway over the already-bound engine client. */
class EngineAccountGateway(private val engine: EngineClient) : AccountGateway {
    override suspend fun account(): Account = engine.account()

    override suspend fun loginEmail(email: String): LoginState = engine.loginEmail(email)

    override suspend fun loginPassword(password: String): LoginState = engine.loginPassword(password)

    override suspend fun login2fa(code: String): LoginState = engine.login2fa(code)

    override suspend fun logout() = engine.logout()
}

/**
 * The Account state machine: the signed-in card, the 35 s account refresh, and
 * the shared sign-in pane reused from setup.
 *
 * It is a plain JVM state machine over [AccountGateway]; secrets live only in
 * the composable for one submit and never enter [state]. A refresh timeout
 * keeps the last local account fields and reports the timeout inline, which is
 * exactly the desktop behavior.
 */
class AccountViewModel(
    private val gateway: AccountGateway,
    private val pollIntervalMillis: Long = DEFAULT_POLL_INTERVAL_MS,
) : ViewModel() {
    private val mutableState = MutableStateFlow(AccountUiState())

    /** The whole screen state; the UI only reads it. */
    val state: StateFlow<AccountUiState> = mutableState.asStateFlow()

    private var loadJob: Job? = null
    private var pollJob: Job? = null
    private var resumed = true

    init {
        loadAccount()
    }

    // ---- account ---------------------------------------------------------

    /** A user-requested refresh; the last local fields stay visible on failure. */
    fun refresh() {
        loadAccount(refresh = true)
    }

    fun retry() {
        loadAccount(refresh = false)
    }

    fun dismissMessage() {
        mutableState.update { it.copy(message = null, error = null) }
    }

    private fun loadAccount(refresh: Boolean = false) {
        if (loadJob?.isActive == true) return
        val hasAccount = mutableState.value.account != null
        mutableState.update {
            it.copy(
                loading = !hasAccount,
                refreshing = refresh && hasAccount,
                error = null,
                message = null,
            )
        }
        loadJob = viewModelScope.launch {
            try {
                val account = withTimeout(ACCOUNT_TIMEOUT_MS) { gateway.account() }
                applyAccount(account)
            } catch (_: TimeoutCancellationException) {
                // The local fields stay exactly as they were.
                mutableState.update {
                    it.copy(
                        loading = false,
                        refreshing = false,
                        message = AccountMessage.TIMEOUT,
                    )
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                mutableState.update {
                    it.copy(
                        loading = false,
                        refreshing = false,
                        message = if (hasAccount) {
                            AccountMessage.REFRESH_FAILED
                        } else {
                            AccountMessage.LOAD_FAILED
                        },
                        error = messageOf(error),
                    )
                }
            }
        }
    }

    private fun applyAccount(account: Account) {
        val signedIn = AccountFormat.isSignedIn(account)
        mutableState.update { current ->
            current.copy(
                account = account,
                loading = false,
                refreshing = false,
                error = AccountFormat.onlineError(account),
                signIn = if (signedIn) {
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
        restartPolling(immediate = true)
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
                stopPolling()
                updateSignIn(
                    phase = SignInPhase.EMAIL,
                    email = login.email,
                    busy = false,
                    error = login.error,
                )
            }
            STEP_PASSWORD -> {
                stopPolling()
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
                    stopPolling()
                } else if (!wasWaiting) {
                    restartPolling(immediate = false)
                }
            }
            STEP_DONE -> {
                mutableState.update {
                    it.copy(
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
                stopPolling()
                loadAccount()
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

    // ---- signing out -----------------------------------------------------

    /** Opens the confirmation; no engine call happens until it is confirmed. */
    fun requestSignOut() {
        if (!mutableState.value.signedIn) return
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
        if (current.signingOut || !current.signedIn) return
        mutableState.update {
            it.copy(signingOut = true, signOutDialog = false, message = null, error = null)
        }
        viewModelScope.launch {
            try {
                gateway.logout()
                mutableState.update {
                    it.copy(
                        account = null,
                        signingOut = false,
                        signIn = SignInPaneState(phase = SignInPhase.EMAIL),
                    )
                }
                loadAccount()
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
            if (shouldPoll()) restartPolling(immediate = false)
        } else {
            stopPolling()
        }
    }

    private fun shouldPoll(): Boolean =
        resumed && mutableState.value.signIn.phase == SignInPhase.EMAIL_LINK_WAITING

    private fun restartPolling(immediate: Boolean) {
        stopPolling()
        if (!shouldPoll()) return
        if (immediate) {
            mutableState.update { it.copy(signIn = it.signIn.copy(busy = true)) }
        }
        pollJob = viewModelScope.launch {
            if (immediate) pollEmailLink()
            while (isActive && shouldPoll()) {
                delay(pollIntervalMillis)
                if (!shouldPoll()) return@launch
                pollEmailLink()
            }
        }
    }

    private fun stopPolling() {
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

    private companion object {
        const val ACCOUNT_TIMEOUT_MS = 35_000L
        const val DEFAULT_POLL_INTERVAL_MS = 3_000L
        const val STEP_EMAIL = "email"
        const val STEP_PASSWORD = "password"
        const val STEP_2FA = "2fa"
        const val STEP_DONE = "done"
    }
}

private fun isPendingLinkMessage(message: String?): Boolean =
    !message.isNullOrBlank() && message.contains(PENDING_LINK_SENTINEL, ignoreCase = true)

private const val PENDING_LINK_SENTINEL = "not confirmed yet"
