package dev.benyamin.kumiho.ui.account

import dev.benyamin.kumiho.engine.Account
import dev.benyamin.kumiho.engine.AccountSummary
import dev.benyamin.kumiho.engine.Accounts
import dev.benyamin.kumiho.engine.Quota
import dev.benyamin.kumiho.ui.dashboard.DashboardFormat
import dev.benyamin.kumiho.ui.setup.SignInPaneState
import java.time.ZoneId
import java.util.Locale

/** Copy the account screen generates itself, resolved by the UI. */
enum class AccountMessage {
    TIMEOUT,
    LOAD_FAILED,
    REFRESH_FAILED,
    SIGN_OUT_FAILED,
    ACCOUNTS_FAILED,
    ACTION_FAILED,
    ROTATION_FAILED,
    NO_BROWSER,
}

/** One in-flight browser sign-in; the URL is opened once by the screen. */
data class BrowserSignIn(
    val id: String,
    val url: String?,
    val error: String? = null,
)

/**
 * The account screen state. The signed-in card is driven by the engine's local
 * token facts; the sign-in pane is reused from setup and is shown whenever the
 * account is not signed in.
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
    val rotationSaving: Boolean = false,
    val browserLaunchURL: String? = null,
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
 * Pure account presentation rules. Formatting reuses the dashboard's desktop
 * wording, so the Account card and the Home quota card cannot disagree.
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

    /** The quota sentence, or null when the engine reported no quota fields. */
    fun quota(account: Account?): String? = account?.let {
        DashboardFormat.quotaLabel(
            Quota(limit = it.maxBytes, remaining = it.quotaRemaining, reset = null),
        )
    }

    /** The token expiry date, or null for an absent timestamp. */
    fun tokenExpiry(
        account: Account?,
        zone: ZoneId = ZoneId.systemDefault(),
        locale: Locale = Locale.getDefault(),
    ): String? = DashboardFormat.dateLabel(account?.tokenExpires ?: 0L, zone, locale)

    /** The engine's bandwidth flag: true means limited. */
    fun bandwidthLimited(account: Account?): Boolean = account?.limitedBandwidth == true

    /** The remaining percentage, or null when the engine did not report it. */
    fun quotaPercent(quota: Quota?): Int? {
        val limit = quota?.limit ?: return null
        val remaining = quota.remaining ?: return null
        if (limit <= 0L) return null
        return ((remaining.coerceAtLeast(0L) * 100L) / limit).toInt().coerceIn(0, 100)
    }

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
