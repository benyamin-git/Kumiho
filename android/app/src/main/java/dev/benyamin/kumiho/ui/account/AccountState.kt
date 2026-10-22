package dev.benyamin.kumiho.ui.account

import dev.benyamin.kumiho.engine.Account
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
}

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
) {
    /** Token presence is the local, reliable signed-in signal (never the network). */
    val signedIn: Boolean get() = AccountFormat.isSignedIn(account)

    /** Show the shared sign-in pane only when the engine says there is no session. */
    val showSignIn: Boolean get() = !loading && !signedIn

    /** True while a sign-out confirmation or request is in flight. */
    val signingOutOrAsking: Boolean get() = signingOut || signOutDialog
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
}
