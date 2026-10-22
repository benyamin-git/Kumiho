package dev.benyamin.kumiho.ui.setup

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dev.benyamin.kumiho.engine.LoginState
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

/**
 * The four-step setup state machine: consent, Firefox Accounts sign-in, the two
 * Android permissions, then the Always-on guidance.
 *
 * It is a pure state machine over [SetupEngine] and [SetupPermissions]: no
 * Android API, no gomobile, no Compose. [persistSetupComplete] writes the
 * `setup_complete` preference and [connectNow] asks the service to connect, so
 * the view model never touches DataStore or the VpnService itself.
 *
 * Secrets: a password or a code is handed to the engine by the caller and
 * never enters [state]; it is not stored, not saved across process death, and
 * not logged. A successful sign-in is remembered only as an email address.
 */
class SetupViewModel(
    private val engine: SetupEngine,
    private val permissions: SetupPermissions,
    private val persistSetupComplete: suspend () -> Unit,
    private val connectNow: () -> Unit,
    private val pollIntervalMillis: Long = DEFAULT_POLL_INTERVAL_MS,
) : ViewModel() {

    private val mutableState = MutableStateFlow(
        SetupState(
            permissions = currentPermissions(),
            alwaysOn = permissions.alwaysOn(),
            lockdown = permissions.lockdown(),
        ),
    )

    /** The whole screen state; the UI only reads it. */
    val state: StateFlow<SetupState> = mutableState.asStateFlow()

    /**
     * The screen is resumed until told otherwise. Polling only runs while it
     * is, so a backgrounded app never keeps asking Firefox Accounts for a link
     * confirmation.
     */
    private var resumed = true

    private var pollJob: Job? = null

    // ---- consent --------------------------------------------------------

    /** Records the required consent tick; Continue stays disabled until true. */
    fun setConsentAccepted(accepted: Boolean) {
        mutableState.update { it.copy(consentAccepted = accepted) }
    }

    // ---- navigation -----------------------------------------------------

    /**
     * Advances one step, and only when the current step allows it. There is no
     * silent skip: an unticked consent or an unfinished sign-in keeps the user
     * where they are.
     */
    fun next() {
        val current = mutableState.value
        if (!current.canContinue) return
        when (current.step) {
            SetupStep.CONSENT -> enterSignIn()
            SetupStep.SIGN_IN -> advanceTo(SetupStep.PERMISSIONS)
            SetupStep.PERMISSIONS -> {
                refreshPermissions()
                advanceTo(SetupStep.ALWAYS_ON)
            }
            // The last step is left through finish(), never through next().
            SetupStep.ALWAYS_ON -> Unit
        }
    }

    /** Goes back one step. False at the first step, where the UI exits. */
    fun back(): Boolean {
        val previous = mutableState.value.step.previous ?: return false
        stopPolling()
        mutableState.update { it.copy(step = previous, error = null) }
        if (previous == SetupStep.SIGN_IN) refreshSignInStatus()
        return true
    }

    private fun advanceTo(step: SetupStep) {
        if (step != SetupStep.SIGN_IN) stopPolling()
        mutableState.update { it.copy(step = step, error = null) }
    }

    // ---- sign in --------------------------------------------------------

    /** Starts the sign-in step: it opens with the engine's real auth state. */
    private fun enterSignIn() {
        mutableState.update { it.copy(step = SetupStep.SIGN_IN, error = null) }
        refreshSignInStatus()
    }

    /**
     * Reads the engine status once. An already-authenticated account skips the
     * form and shows the account instead; a failure is surfaced inline without
     * blocking the fields.
     */
    private fun refreshSignInStatus() {
        viewModelScope.launch {
            try {
                val status = engine.refreshStatus()
                if (status.authenticated) markAuthenticated(status.email)
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                mutableState.update {
                    it.copy(signIn = it.signIn.copy(error = messageOf(error)))
                }
            }
        }
    }

    /** Sends the email to the engine; the engine decides the next step. */
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
        viewModelScope.launch { runLogin { engine.loginEmail(trimmed) } }
    }

    /**
     * Sends the password once. The value is captured by this request only: it
     * is never written to [state] and never logged.
     */
    fun submitPassword(password: String) {
        if (password.isEmpty()) {
            setSignInMessage(SetupMessage.PASSWORD_REQUIRED)
            return
        }
        if (!signInIdle()) return
        mutableState.update {
            it.copy(signIn = it.signIn.copy(busy = true, error = null, message = null))
        }
        viewModelScope.launch { runLogin { engine.loginPassword(password) } }
    }

    /** Sends the emailed code once; the code is never stored either. */
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
        viewModelScope.launch { runLogin { engine.login2fa(trimmed) } }
    }

    /**
     * The email-link "Re-check": asks immediately, then resumes the 3 s
     * spacing, so a manual tap never turns into a burst of requests.
     */
    fun recheckEmailLink() {
        val current = mutableState.value
        if (current.step != SetupStep.SIGN_IN) return
        if (current.signIn.phase != SignInPhase.EMAIL_LINK_WAITING) return
        if (current.signIn.busy) return
        restartPolling(immediate = true)
    }

    private fun signInIdle(): Boolean {
        val current = mutableState.value
        return current.step == SetupStep.SIGN_IN && !current.signIn.busy
    }

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

    /**
     * Maps one engine [LoginState] onto the pane. The mapping is generic: the
     * engine's step names drive it, so a new verification method needs no
     * client change. The email-link method polls; every other method asks for
     * a code.
     */
    private fun applyLogin(login: LoginState) {
        when (login.step) {
            STEP_EMAIL -> {
                stopPolling()
                mutableState.update {
                    it.copy(
                        signIn = it.signIn.copy(
                            phase = SignInPhase.EMAIL,
                            email = login.email ?: it.signIn.email,
                            busy = false,
                            error = login.error,
                            message = null,
                        ),
                    )
                }
            }
            STEP_PASSWORD -> {
                stopPolling()
                mutableState.update {
                    it.copy(
                        signIn = it.signIn.copy(
                            phase = SignInPhase.PASSWORD,
                            email = login.email ?: it.signIn.email,
                            busy = false,
                            error = login.error,
                            message = null,
                        ),
                    )
                }
            }
            STEP_2FA -> {
                val current = mutableState.value.signIn
                val method = login.verificationMethod ?: current.verificationMethod
                val link = method == SignInPaneState.EMAIL_LINK_METHOD
                // The engine reports "not confirmed yet" as the error of an
                // empty-code check. That is the normal waiting case, not a
                // failure, so it must not paint the pane as broken.
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
                    // Entering the waiting phase starts the poll; an ongoing
                    // poll (or a manual Re-check) keeps its own spacing.
                    restartPolling(immediate = false)
                }
            }
            STEP_DONE -> {
                markAuthenticated(login.email)
                if (!login.error.isNullOrBlank()) {
                    mutableState.update { it.copy(signIn = it.signIn.copy(error = login.error)) }
                }
                refreshAccountEmail()
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

    private fun markAuthenticated(email: String?) {
        stopPolling()
        mutableState.update {
            it.copy(
                signIn = it.signIn.copy(
                    phase = SignInPhase.AUTHENTICATED,
                    accountEmail = email ?: it.signIn.accountEmail
                    ?: it.signIn.email.takeIf { value -> value.isNotBlank() },
                    busy = false,
                    error = null,
                    message = null,
                ),
            )
        }
    }

    /** Confirms the account email after a successful login; best effort. */
    private fun refreshAccountEmail() {
        viewModelScope.launch {
            try {
                val status = engine.refreshStatus()
                val email = status.email
                if (status.authenticated && !email.isNullOrBlank()) {
                    mutableState.update { it.copy(signIn = it.signIn.copy(accountEmail = email)) }
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (_: Exception) {
                // The login reply already told us the address; a status read
                // failure must not undo a successful sign-in.
            }
        }
    }

    private fun setSignInMessage(message: SetupMessage) {
        mutableState.update {
            it.copy(signIn = it.signIn.copy(message = message, error = null))
        }
    }

    // ---- email-link polling ---------------------------------------------

    /** Tells the view model whether the screen is in the resumed state. */
    fun setScreenResumed(resumed: Boolean) {
        if (this.resumed == resumed) return
        this.resumed = resumed
        if (resumed) {
            if (shouldPoll()) restartPolling(immediate = false)
        } else {
            stopPolling()
        }
    }

    private fun shouldPoll(): Boolean {
        val current = mutableState.value
        return resumed &&
            current.step == SetupStep.SIGN_IN &&
            current.signIn.phase == SignInPhase.EMAIL_LINK_WAITING
    }

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
            applyLogin(engine.login2fa(""))
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            mutableState.update {
                it.copy(signIn = it.signIn.copy(busy = false, error = messageOf(error)))
            }
        }
    }

    // ---- Android permissions ---------------------------------------------

    /** Re-reads every permission and the OS policy; never assumes a grant. */
    fun refreshPermissions() {
        val next = currentPermissions()
        mutableState.update {
            it.copy(
                permissions = next,
                alwaysOn = permissions.alwaysOn(),
                lockdown = permissions.lockdown(),
            )
        }
    }

    /** The user returned from the system VPN consent dialog. */
    fun onVpnConsentResult() {
        refreshPermissions()
    }

    /** The user returned from the notification permission request. */
    fun onNotificationResult() {
        refreshPermissions()
    }

    private fun currentPermissions(): PermissionState {
        permissions.refreshOsPolicy()
        return PermissionState(
            vpnConsentGranted = permissions.vpnConsentGranted(),
            notificationsGranted = permissions.notificationsGranted(),
            notificationsRuntimeRequired = permissions.notificationsRuntimePermissionRequired(),
        )
    }

    // ---- finish ----------------------------------------------------------

    /**
     * Persists `setup_complete` and optionally connects. The preference is
     * written first, so a connect that fails still leaves a complete setup; a
     * write failure keeps the user on the step with an inline error.
     * [connectNow] is honoured only when the state actually allows it.
     */
    fun finish(connectNow: Boolean) {
        val current = mutableState.value
        if (current.step != SetupStep.ALWAYS_ON || current.finishing) return
        val eligible = current.canConnectNow
        mutableState.update { it.copy(finishing = true, error = null) }
        viewModelScope.launch {
            try {
                persistSetupComplete()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                mutableState.update { it.copy(finishing = false, error = messageOf(error)) }
                return@launch
            }
            mutableState.update { it.copy(finishing = false) }
            if (connectNow && eligible) this@SetupViewModel.connectNow()
        }
    }

    private fun messageOf(error: Throwable): String =
        error.message?.takeIf { it.isNotBlank() }
            ?: error::class.simpleName
            ?: error.javaClass.name
}

/**
 * The engine's wording for an unconfirmed email link (`Login2FA` with an empty
 * code). It is the one engine reply that means "keep waiting" rather than
 * "something failed", so the pane shows the waiting state instead of an error.
 */
private fun isPendingLinkMessage(message: String?): Boolean =
    !message.isNullOrBlank() && message.contains(PENDING_LINK_SENTINEL, ignoreCase = true)

private const val PENDING_LINK_SENTINEL = "not confirmed yet"

private const val STEP_EMAIL = "email"
private const val STEP_PASSWORD = "password"
private const val STEP_2FA = "2fa"
private const val STEP_DONE = "done"

/** The plan's polling floor: never more than one check every 3 s. */
private const val DEFAULT_POLL_INTERVAL_MS = 3_000L
