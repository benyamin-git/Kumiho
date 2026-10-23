package dev.benyamin.kumiho.ui.setup

import dev.benyamin.kumiho.ui.account.BrowserSignIn

/**
 * The four setup steps, in the order the user walks them. [number] is the real
 * sequence number shown in the step indicator (the Ban 11 carve-out: this is a
 * genuine ordered flow), so it is never derived from the display order.
 */
enum class SetupStep(val number: Int) {
    CONSENT(1),
    SIGN_IN(2),
    PERMISSIONS(3),
    ALWAYS_ON(4),
    ;

    /** The step a back press lands on, or null at the first step. */
    val previous: SetupStep? get() = entries.getOrNull(ordinal - 1)
}

/**
 * The sign-in sub-state. [AUTHENTICATED] is the only phase that lets the wizard
 * move on; [EMAIL_LINK_WAITING] is the polling phase for the email-link
 * verification method.
 */
enum class SignInPhase {
    EMAIL,
    PASSWORD,
    CODE,
    EMAIL_LINK_WAITING,
    AUTHENTICATED,
}

/**
 * Copy the view model generates itself, so no user-facing sentence is built in
 * Kotlin: the UI resolves each code to a string resource.
 */
enum class SetupMessage {
    EMAIL_REQUIRED,
    PASSWORD_REQUIRED,
    CODE_REQUIRED,
    UNEXPECTED_STEP,
    NO_BROWSER,
}

/**
 * Everything the sign-in pane renders. It never holds a password or a code:
 * those live in the composable for one submit and are discarded.
 */
data class SignInPaneState(
    val phase: SignInPhase = SignInPhase.EMAIL,
    /** The email the user submitted, or the account email once known. */
    val email: String = "",
    /** The signed-in account, from [dev.benyamin.kumiho.engine.EngineStatus]. */
    val accountEmail: String? = null,
    /** The engine's `verification_method` (`email-2fa`, `email`, …). */
    val verificationMethod: String? = null,
    /** The engine's human-readable reason for the verification, when given. */
    val verificationReason: String? = null,
    /** A request is in flight; buttons and fields stay disabled. */
    val busy: Boolean = false,
    /** The engine's own error text, shown inline. */
    val error: String? = null,
    /** A locally generated message, resolved to a resource by the UI. */
    val message: SetupMessage? = null,
) {
    /** The engine's email-link verification method. */
    val isEmailLink: Boolean get() = verificationMethod == EMAIL_LINK_METHOD

    companion object {
        /** The wire value Firefox Accounts uses for the emailed link. */
        const val EMAIL_LINK_METHOD = "email"
    }
}

/** The two Android permission states the setup screen reports honestly. */
data class PermissionState(
    val vpnConsentGranted: Boolean = false,
    val notificationsGranted: Boolean = false,
    /** True on API 33+, where POST_NOTIFICATIONS is a runtime request. */
    val notificationsRuntimeRequired: Boolean = false,
)

/**
 * The setup screen's whole state. It is a pure value: the view model is the
 * only producer, and the composables only read it.
 */
data class SetupState(
    val step: SetupStep = SetupStep.CONSENT,
    val consentAccepted: Boolean = false,
    val signIn: SignInPaneState = SignInPaneState(),
    val permissions: PermissionState = PermissionState(),
    /** Android's Always-on VPN state, mirrored from the service. */
    val alwaysOn: Boolean = false,
    /** Android's lockdown state, mirrored from the service. */
    val lockdown: Boolean = false,
    /** A finish request is in flight. */
    val finishing: Boolean = false,
    /** A failure that keeps the user on the current step. */
    val error: String? = null,
    /** The engine-owned browser flow, while one is in flight. */
    val browser: BrowserSignIn? = null,
    /** A newly returned URL the screen should open once. */
    val browserLaunchURL: String? = null,
) {
    /** The account is signed in, so the sign-in step is satisfied. */
    val signedIn: Boolean get() = signIn.phase == SignInPhase.AUTHENTICATED

    /**
     * Whether the bottom action may advance. Consent needs the tick, sign-in
     * needs an authenticated account, and the last two steps never block: a
     * denied permission is surfaced in place and still lets the user finish.
     */
    val canContinue: Boolean
        get() = when (step) {
            SetupStep.CONSENT -> consentAccepted
            SetupStep.SIGN_IN -> signedIn
            SetupStep.PERMISSIONS, SetupStep.ALWAYS_ON -> true
        }

    /** "Connect now" is offered only when it can actually work. */
    val canConnectNow: Boolean get() = signedIn && permissions.vpnConsentGranted
}
