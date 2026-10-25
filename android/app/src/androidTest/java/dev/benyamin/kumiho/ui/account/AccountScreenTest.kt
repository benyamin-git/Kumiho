package dev.benyamin.kumiho.ui.account

import androidx.activity.ComponentActivity
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.assertTextEquals
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.engine.Account
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
 * The Account screen as drawn: the signed-in card, the reused sign-in pane and
 * the sign-out confirmation. Callbacks are recorded; nothing reaches an engine.
 */
class AccountScreenTest {

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
                    ),
                )
            }
        }
        rule.waitForIdle()
    }

    @Test
    fun aSignedInAccountRendersEveryCardField() {
        show(AccountUiState(loading = false, account = account()))

        rule.onNodeWithTag(AccountTags.SCREEN).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.CARD).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.EMAIL).assertTextEquals("user@example.com")
        rule.onNodeWithTag(AccountTags.UID).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.SUBSCRIPTION).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.QUOTA).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.BANDWIDTH).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.TOKEN).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.REFRESH).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.SIGN_OUT).assertIsDisplayed()
    }

    @Test
    fun refreshAndSignOutInvokeTheirActions() {
        show(AccountUiState(loading = false, account = account()))

        rule.onNodeWithTag(AccountTags.REFRESH).performClick()
        rule.onNodeWithTag(AccountTags.SIGN_OUT).performClick()
        rule.waitForIdle()

        assertTrue(calls.contains("refresh"))
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
    fun aSignedOutAccountShowsTheSharedSigninPane() {
        show(AccountUiState(loading = false, account = null))

        rule.onNodeWithTag(AccountTags.SIGNED_OUT_TITLE).assertIsDisplayed()
        rule.onNodeWithTag(SetupTags.SIGN_IN_EMAIL).assertIsDisplayed()
        rule.onNodeWithTag(SetupTags.SIGN_IN_EMAIL_SUBMIT).assertIsDisplayed()
        rule.onNodeWithTag(AccountTags.CARD).assertDoesNotExist()
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
    fun aRefreshInFlightDisablesTheActions() {
        show(
            AccountUiState(
                loading = false,
                account = account(),
                refreshing = true,
                signingOut = true,
            ),
        )

        rule.onNodeWithTag(AccountTags.REFRESH).assertIsNotEnabled()
        rule.onNodeWithTag(AccountTags.SIGN_OUT).assertIsNotEnabled()
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
}
