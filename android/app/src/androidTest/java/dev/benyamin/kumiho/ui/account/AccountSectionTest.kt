package dev.benyamin.kumiho.ui.account

import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.mutableStateOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.engine.Account
import dev.benyamin.kumiho.engine.AccountSummary
import dev.benyamin.kumiho.engine.Accounts
import dev.benyamin.kumiho.ui.setup.SetupTags
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import java.time.Instant
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * The Account section as drawn inside Settings: the compact status line, the
 * account list, the reused sign-in pane and the sign-out confirmation.
 * Callbacks are recorded; nothing reaches an engine.
 */
class AccountSectionTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private val calls = mutableListOf<String>()

    private fun show(state: AccountUiState) {
        showMutable(state)
    }

    private fun showMutable(initial: AccountUiState): MutableState<AccountUiState> {
        val state = mutableStateOf(initial)
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
                Column(modifier = Modifier.verticalScroll(rememberScrollState())) {
                    AccountSection(state = state.value, actions = actions())
                    AccountSectionDialogs(state = state.value, actions = actions())
                }
            }
        }
        rule.waitForIdle()
        return state
    }

    private fun actions() = AccountActions(
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
        onOpenBrowser = { calls += "open-browser" },
        onCloseBrowser = { calls += "close-browser" },
        onBrowserRedirect = { calls += "redirect-$it" },
        onToggleBrowserPaste = { calls += "toggle-paste" },
        onBrowserPasteChange = { calls += "paste-$it" },
        onBrowserPasteSubmit = { calls += "paste-submit" },
        onUseBrowserPassword = { calls += "use-password" },
        onCancelBrowser = { calls += "cancel-browser" },
        onBrowserUnavailable = { calls += "browser-unavailable" },
        onCancelAdd = { calls += "cancel-add" },
    )

    @Test
    fun theSectionShowsTheAccountListAndSignOut() {
        show(AccountUiState(loading = false, account = account(), accounts = accounts()))

        rule.onNodeWithTag(AccountTags.SECTION).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.ACCOUNTS).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.accountRow("a")).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.accountRow("b")).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.SIGN_OUT).assertExists()
        // No entitlement or quota card survives in the section.
        rule.onNodeWithTag(AccountTags.NOTICE).assertDoesNotExist()
    }

    @Test
    fun signOutInvokesItsAction() {
        show(AccountUiState(loading = false, account = account(), accounts = accounts()))

        rule.onNodeWithTag(AccountTags.SIGN_OUT).performScrollTo().performClick()
        rule.waitForIdle()

        assertTrue(calls.contains("sign-out"))
    }

    @Test
    fun theLegacyRemovalNoticeUsesAnInformationalBanner() {
        val notice = "Old browser sign-in removed. Add your Firefox account again."
        show(
            AccountUiState(
                loading = false,
                account = null,
                accounts = Accounts(
                    accounts = emptyList(),
                    active = null,
                    rotation = false,
                    threshold = 10,
                    lastMethod = null,
                    notice = notice,
                    error = null,
                ),
            ),
        )

        rule.onNodeWithTag(AccountTags.NOTICE).assertIsDisplayed()
        rule.onNodeWithText(notice).assertIsDisplayed()
        rule.onNodeWithContentDescription(rule.activity.getString(R.string.account_notice_information))
            .assertIsDisplayed()
    }

    @Test
    fun aSignedOutSectionShowsTheSharedSigninPane() {
        show(AccountUiState(loading = false, account = null))

        rule.onNodeWithTag(AccountTags.SIGNED_OUT_TITLE).assertIsDisplayed()
        rule.onNodeWithTag(SetupTags.SIGN_IN_EMAIL).assertIsDisplayed()
        rule.onNodeWithTag(SetupTags.SIGN_IN_EMAIL_SUBMIT).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.ACCOUNTS).assertDoesNotExist()
    }

    @Test
    fun theLoadingSectionShowsACompactLoadingRow() {
        show(AccountUiState(loading = true))

        rule.onNodeWithTag(AccountTags.LOADING).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.SIGNED_OUT_TITLE).assertDoesNotExist()
    }

    @Test
    fun theAddDialogOpensFromTheAddButton() {
        show(AccountUiState(loading = false, account = account(), accounts = accounts()))

        rule.onNodeWithTag(AccountTags.ADD_ACCOUNT).performClick()
        rule.waitForIdle()

        assertTrue(calls.contains("add-account"))
    }

    @Test
    fun theAddDialogOffersBrowserAndPassword() {
        show(
            AccountUiState(
                loading = false,
                account = account(),
                accounts = accounts(),
                addDialog = true,
            ),
        )

        rule.onNodeWithTag(AccountTags.ADD_DIALOG).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.ADD_CANCEL).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.ADD_PASSWORD).assertIsDisplayed().performClick()
        rule.waitForIdle()

        assertTrue(calls.contains("add-password"))
    }

    @Test
    fun theSignOutDialogConfirmsAndCancels() {
        show(
            AccountUiState(
                loading = false,
                account = account(),
                signOutDialog = true,
            ),
        )

        rule.onNodeWithTag(AccountTags.DIALOG).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.DIALOG_CANCEL).performClick()
        rule.waitForIdle()
        assertTrue(calls.contains("dismiss-sign-out"))
    }

    @Test
    fun confirmingTheSignOutDialogInvokesTheEnginePath() {
        show(
            AccountUiState(
                loading = false,
                account = account(),
                signOutDialog = true,
            ),
        )

        rule.onNodeWithTag(AccountTags.DIALOG_CONFIRM).performClick()
        rule.waitForIdle()

        assertTrue(calls.contains("confirm-sign-out"))
    }

    @Test
    fun signingOutDisablesTheSignOutButton() {
        show(
            AccountUiState(
                loading = false,
                account = account(),
                accounts = accounts(),
                signingOut = true,
            ),
        )

        rule.onNodeWithTag(AccountTags.SIGN_OUT).performScrollTo().assertIsNotEnabled()
    }

    @Test
    fun thePasswordPaneRendersAboveTheList() {
        val accounts = Accounts(
            accounts = (1..4).map { index ->
                AccountSummary(
                    id = "account-$index",
                    email = "user$index@example.com",
                    label = "Account $index",
                    method = "password",
                    active = index == 1,
                    reauthRequired = false,
                    lastUsedAt = 0,
                    quota = null,
                )
            },
            active = "account-1",
            rotation = false,
            threshold = 10,
            lastMethod = "password",
            notice = null,
            error = null,
        )
        show(
            AccountUiState(
                loading = false,
                account = account(),
                accounts = accounts,
                addingAccount = true,
            ),
        )

        rule.onNodeWithTag(SetupTags.SIGN_IN_EMAIL).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.ADD_PASSWORD_CANCEL).assertIsDisplayed()
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
                quota = null,
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
