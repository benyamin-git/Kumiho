package dev.benyamin.kumiho.engine

/**
 * One row of the Account screen (`api.AccountSummary`).
 *
 * [quota] is null until a quota poll reports it; a reported zero remaining is
 * a real value. [lastUsedAt] is Unix seconds, or zero when never used.
 */
data class AccountSummary(
    val id: String,
    val email: String,
    val label: String,
    val method: String,
    val active: Boolean,
    val reauthRequired: Boolean,
    val lastUsedAt: Long,
    val quota: Quota?,
)

/**
 * The `accounts_list` snapshot (`api.AccountsView`): every stored account, the
 * active id, rotation settings, the onboarding last-method hint and the
 * engine's one-line notice or load error.
 */
data class Accounts(
    val accounts: List<AccountSummary>,
    val active: String?,
    val rotation: Boolean,
    val threshold: Int,
    val lastMethod: String?,
    val notice: String?,
    val error: String?,
)
