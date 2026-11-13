package dev.benyamin.kumiho.ui.account

import dev.benyamin.kumiho.engine.Account
import dev.benyamin.kumiho.engine.AccountSummary
import dev.benyamin.kumiho.engine.Accounts
import dev.benyamin.kumiho.ui.setup.SignInPaneState

/** Copy the account section generates itself, resolved by the UI. */
enum class AccountMessage {
    TIMEOUT,
    LOAD_FAILED,
    REFRESH_FAILED,
    SIGN_OUT_FAILED,
    ACCOUNTS_FAILED,
    ACTION_FAILED,
    NO_BROWSER,
}

/** One in-flight browser sign-in; the URL is loaded in the in-app browser. */
data class BrowserSignIn(
    val id: String,
    val url: String?,
    val error: String? = null,
    /** A client-generated failure, resolved to a resource by the UI. */
    val message: BrowserMessage? = null,
)

/** Client-generated browser failures; engine errors stay in [BrowserSignIn.error]. */
enum class BrowserMessage {
    START_FAILED,
    NO_SIGN_IN_ID,
    FAILED,
    UNEXPECTED_STEP,
}

/** The local validation copy for the redirect-URL fallback field. */
enum class BrowserPasteMessage {
    REQUIRED,
    FAILED,
}

/**
 * The account section state. The sign-in pane is reused from setup and is
 * shown whenever no account is stored.
 */
data class AccountUiState(
    val loading: Boolean = true,
    val account: Account? = null,
    val refreshing: Boolean = false,
    val signIn: SignInPaneState = SignInPaneState(),
    /** An online-lookup failure, shown inline while the local fields survive. */
    val error: String? = null,
    val message: AccountMessage? = null,
    val signOutDialog: Boolean = false,
    val signingOut: Boolean = false,
    val accounts: Accounts? = null,
    val selectedID: String? = null,
    val removeTarget: AccountSummary? = null,
    val renameTarget: AccountSummary? = null,
    val addDialog: Boolean = false,
    val browser: BrowserSignIn? = null,
    /** The engine URL the in-app browser loads. */
    val browserLaunchURL: String? = null,
    /** The full-screen in-app browser window is open. */
    val browserWebViewOpen: Boolean = false,
    /** The paste-the-redirect fallback is expanded in the waiting card. */
    val browserPasteOpen: Boolean = false,
    val browserPasteValue: String = "",
    /** A retryable submission error from the engine, shown inline. */
    val browserPasteError: String? = null,
    /** Locally generated paste validation copy. */
    val browserPasteMessage: BrowserPasteMessage? = null,
    /** A redirect submission is in flight; the card controls are disabled. */
    val browserSubmitting: Boolean = false,
    /** The WebView was unavailable, so the system browser is the fallback. */
    val browserFallback: Boolean = false,
    /** The shared sign-in pane is adding an account, not bootstrapping one. */
    val addingAccount: Boolean = false,
) {
    /** Token presence is the local, reliable signed-in signal (never the network). */
    val signedIn: Boolean get() = AccountFormat.isSignedIn(account)

    /** Show the shared sign-in pane only when the engine says there is no session. */
    val showSignIn: Boolean get() = !loading && !signedIn

    /** True while a sign-out confirmation or request is in flight. */
    val signingOutOrAsking: Boolean get() = signingOut || signOutDialog

    /** The list has at least one stored account (even if it needs re-auth). */
    val hasAccounts: Boolean get() = accounts?.accounts?.isNotEmpty() == true

    /** The radio selection; the engine's active id is the fallback. */
    val activeID: String? get() = selectedID ?: accounts?.active

    val activeAccount: AccountSummary?
        get() = accounts?.accounts?.firstOrNull { it.id == activeID }
}

/**
 * Pure account presentation rules shared by the phone and TV sections.
 */
object AccountFormat {
    /** The engine's local wording for a missing session (`Account` error). */
    const val NOT_SIGNED_IN = "not signed in"

    /** A token expiry in the future is the local signed-in signal. */
    fun isSignedIn(account: Account?): Boolean = (account?.tokenExpires ?: 0L) > 0L

    /** True when the engine's error only means "there is no session". */
    fun isNotSignedIn(account: Account?): Boolean =
        account?.error?.trim()?.equals(NOT_SIGNED_IN, ignoreCase = true) == true

    /** The online entitlement error, or null when there is nothing to report. */
    fun onlineError(account: Account?): String? = account?.error
        ?.trim()
        ?.takeIf { it.isNotEmpty() && !isNotSignedIn(account) }

    /** The account that becomes active after removing the active one. */
    fun nextActive(accounts: Accounts?, target: AccountSummary?): AccountSummary? {
        if (accounts == null || target == null || accounts.active != target.id) return null
        // The engine selects the most recently used remaining account; match
        // that rule so the confirmation does not name a different successor.
        return accounts.accounts
            .filter { it.id != target.id }
            .maxByOrNull { it.lastUsedAt }
    }

    /**
     * The client's label normalization: trim, drop control characters and cap
     * at 64 code points. An empty result falls back to the email; the engine
     * applies the same rule independently.
     */
    fun normalizeLabel(label: String, fallback: String): String {
        val cleaned = label
            .filterNot { Character.isISOControl(it) }
            .trim()
        if (cleaned.isEmpty()) return fallback
        if (cleaned.codePointCount(0, cleaned.length) <= LABEL_MAX) return cleaned
        return cleaned.substring(0, cleaned.offsetByCodePoints(0, LABEL_MAX))
    }

    private const val LABEL_MAX = 64
}
