package dev.benyamin.kumiho.ui.setup

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.account.BrowserPasteMessage
import dev.benyamin.kumiho.ui.account.BrowserSignIn
import dev.benyamin.kumiho.ui.account.BrowserWaitingCard
import dev.benyamin.kumiho.ui.theme.Dimens

/**
 * Step 2: the sign-in heading plus the shared [SignInPane]. The pane is reused
 * by the Account screen, so the step owns only the heading, the browser wait
 * card and the step tag.
 */
@Composable
fun SignInStep(
    state: SignInPaneState,
    browser: BrowserSignIn?,
    browserPasteOpen: Boolean,
    browserPasteValue: String,
    browserPasteError: String?,
    browserPasteMessage: BrowserPasteMessage?,
    browserSubmitting: Boolean,
    browserFallback: Boolean,
    onBrowser: () -> Unit,
    onOpenBrowser: () -> Unit,
    onToggleBrowserPaste: () -> Unit,
    onBrowserPasteChange: (String) -> Unit,
    onBrowserPasteSubmit: () -> Unit,
    onUsePassword: () -> Unit,
    onCancelBrowser: () -> Unit,
    onSubmitEmail: (String) -> Unit,
    onSubmitPassword: (String) -> Unit,
    onSubmitCode: (String) -> Unit,
    onRecheck: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .testTag(SetupTags.SIGN_IN_STEP),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space4),
    ) {
        Text(
            text = stringResource(R.string.setup_step_sign_in),
            style = MaterialTheme.typography.headlineMedium,
            color = MaterialTheme.colorScheme.onSurface,
        )
        if (browser != null) {
            BrowserWaitingCard(
                browser = browser,
                title = stringResource(R.string.sign_in_browser_waiting_title),
                hint = stringResource(R.string.sign_in_browser_waiting_text),
                fallbackHint = stringResource(R.string.browser_waiting_fallback),
                pasteOpen = browserPasteOpen,
                pasteValue = browserPasteValue,
                pasteError = browserPasteError,
                pasteMessage = browserPasteMessage,
                submitting = browserSubmitting,
                fallback = browserFallback,
                onOpen = onOpenBrowser,
                onTogglePaste = onToggleBrowserPaste,
                onPasteChange = onBrowserPasteChange,
                onPasteSubmit = onBrowserPasteSubmit,
                onUsePassword = onUsePassword,
                onCancel = onCancelBrowser,
            )
        } else {
            SignInPane(
                state = state,
                onSubmitEmail = onSubmitEmail,
                onSubmitPassword = onSubmitPassword,
                onSubmitCode = onSubmitCode,
                onRecheck = onRecheck,
                onBrowser = onBrowser,
            )
        }
    }
}
