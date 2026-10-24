package dev.benyamin.kumiho.ui.account

import androidx.activity.ComponentActivity
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsSelected
import androidx.compose.ui.test.assertTextEquals
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import dev.benyamin.kumiho.engine.Account
import dev.benyamin.kumiho.engine.AccountSummary
import dev.benyamin.kumiho.engine.Accounts
import dev.benyamin.kumiho.engine.Quota
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import java.time.Instant
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/** The account list, add dialog, rotation card and browser waiting card as drawn. */
class AccountAccountsScreenTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private val calls = mutableListOf<String>()

    private fun show(state: AccountUiState) {
        rule.setContent {
            KumihoTheme(
                colors = PaletteMapping.fromJson(
                    dev.benyamin.kumiho.go.mobile.Mobile.palette(
                        ThemeId.LIGHT.id,
                        KumihoAccent.BLUE.id,
                    ),
                    ThemeId.LIGHT,
                    KumihoAccent.BLUE,
                ),
            ) {
                AccountScreen(
                    state = state,
                    actions = AccountActions(
                        onRefresh = { calls += "refresh" },
                        onRetry = { calls += "retry" },
                        onDismissMessage = { calls += "dismiss" },
                        onSubmitEmail = { calls += "email" },
                        onSubmitPassword = { calls += "password" },
                        onSubmitCode = { calls += "code" },
                        onRecheck = { calls += "recheck" },
                        onSignOut = { calls += "sign-out" },
                        onDismissSignOut = { calls += "dismiss-sign-out" },
                        onConfirmSignOut = { calls += "confirm-sign-out" },
                        onAddAccount = { calls += "add-account" },
                        onDismissAdd = { calls += "dismiss-add" },
                        onAddBrowser = { calls += "add-browser" },
                        onAddPassword = { calls += "add-password" },
                        onUse = { calls += "use-$it" },
                        onReauth = { calls += "reauth-${it.id}" },
                        onRemoveRequest = { calls += "remove-${it.id}" },
                        onRemoveConfirm = { calls += "remove-confirm" },
                        onDismissRemove = { calls += "dismiss-remove" },
                        onRenameRequest = { calls += "rename-${it.id}" },
                        onRenameConfirm = { calls += "rename-confirm" },
                        onDismissRename = { calls += "dismiss-rename" },
                        onRotation = { calls += "rotation-$it" },
                        onThreshold = { calls += "threshold-$it" },
                        onOpenBrowser = { calls += "open-browser" },
                        onCancelBrowser = { calls += "cancel-browser" },
                        onBrowserUnavailable = { calls += "browser-unavailable" },
                        onCancelAdd = { calls += "cancel-add" },
                    ),
                )
            }
        }
        rule.waitForIdle()
    }

    @Test
    fun accountListShowsEveryAccountAndActiveMarker() {
        show(AccountUiState(loading = false, account = account(), accounts = accounts()))

        rule.onNodeWithTag(AccountTags.ACCOUNTS).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.accountRow("a")).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.accountRow("b")).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.active("a")).assertIsSelected()
    }

    @Test
    fun accountRowShowsMethodQuotaAndReauthBadge() {
        show(AccountUiState(loading = false, account = account(), accounts = accounts()))

        rule.onNodeWithTag(AccountTags.method("a")).assertTextEquals("Browser")
        rule.onNodeWithTag(AccountTags.quota("a")).assertTextEquals("82% left")
        rule.onNodeWithTag(AccountTags.quota("b")).assertTextEquals("Quota unknown")
        rule.onNodeWithTag(AccountTags.reauth("b")).assertIsDisplayed()
    }

    @Test
    fun addDialogOffersBrowserAndPassword() {
        show(
            AccountUiState(
                loading = false,
                account = account(),
                accounts = accounts(),
                addDialog = true,
            ),
        )

        rule.onNodeWithTag(AccountTags.ADD_BROWSER).assertIsDisplayed().performClick()
        rule.waitForIdle()
        assertTrue(calls.contains("add-browser"))
        rule.onNodeWithTag(AccountTags.ADD_PASSWORD).assertIsDisplayed()
    }

    @Test
    fun rotationSwitchAndThresholdRow() {
        show(
            AccountUiState(
                loading = false,
                account = account(),
                accounts = accounts().copy(rotation = true, threshold = 20),
            ),
        )

        rule.onNodeWithTag(AccountTags.ROTATION).assertIsDisplayed().performClick()
        rule.onNodeWithTag(AccountTags.THRESHOLD).assertIsDisplayed()
        rule.waitForIdle()
        assertTrue(calls.contains("rotation-false"))
    }

    @Test
    fun browserWaitingCardShowsOpenAndCancel() {
        show(
            AccountUiState(
                loading = false,
                account = account(),
                accounts = accounts(),
                browser = BrowserSignIn(
                    "flow-1",
                    "https://vpn.mozilla.org/api/v2/vpn/login/android?port=49152",
                ),
            ),
        )

        rule.onNodeWithTag(AccountTags.BROWSER_WAITING).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.BROWSER_OPEN).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.BROWSER_CANCEL).performClick()
        rule.waitForIdle()
        assertTrue(calls.contains("cancel-browser"))
    }

    @Test
    fun reauthActionStartsBrowserFlow() {
        show(AccountUiState(loading = false, account = account(), accounts = accounts()))

        rule.onNodeWithTag(AccountTags.reauth("b")).performClick()
        rule.waitForIdle()

        assertTrue(calls.contains("reauth-b"))
    }

    private fun account() = Account(
        email = "user@example.com",
        uid = "uid-123",
        subscribed = true,
        maxBytes = 2L * 1024 * 1024 * 1024,
        limitedBandwidth = false,
        quotaRemaining = 1024L * 1024 * 1024,
        tokenExpires = Instant.parse("2026-11-21T12:00:00Z").epochSecond,
        error = null,
    )

    private fun accounts() = Accounts(
        accounts = listOf(
            AccountSummary(
                id = "a",
                email = "one@example.com",
                label = "Home",
                method = "browser",
                active = true,
                reauthRequired = false,
                lastUsedAt = 1_761_086_400L,
                quota = Quota(limit = 1_000L, remaining = 820L, reset = null),
            ),
            AccountSummary(
                id = "b",
                email = "two@example.com",
                label = "Work",
                method = "password",
                active = false,
                reauthRequired = true,
                lastUsedAt = 0L,
                quota = null,
            ),
        ),
        active = "a",
        rotation = false,
        threshold = 10,
        lastMethod = "browser",
        notice = null,
        error = null,
    )
}
