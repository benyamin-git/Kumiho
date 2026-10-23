package dev.benyamin.kumiho.ui.setup

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.account.BrowserSignIn
import dev.benyamin.kumiho.ui.components.KumihoCard
import dev.benyamin.kumiho.ui.theme.Dimens
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors

/**
 * Step 2: the sign-in heading plus the shared [SignInPane]. The pane is reused
 * by the Account screen, so the step owns only the heading, the browser wait
 * card and the step tag.
 */
@Composable
fun SignInStep(
    state: SignInPaneState,
    browser: BrowserSignIn?,
    onBrowser: () -> Unit,
    onOpenBrowser: () -> Unit,
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
                fallback = state.message,
                onOpen = onOpenBrowser,
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

@Composable
private fun BrowserWaitingCard(
    browser: BrowserSignIn,
    fallback: SetupMessage?,
    onOpen: () -> Unit,
    onCancel: () -> Unit,
) {
    val warning = LocalKumihoColors.current.warning
    KumihoCard(modifier = Modifier.fillMaxWidth().testTag(SetupTags.SIGN_IN_BROWSER_WAITING)) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            if (browser.error == null && fallback == null) {
                CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp)
            } else {
                Icon(
                    imageVector = Icons.Filled.Warning,
                    contentDescription = null,
                    tint = warning,
                    modifier = Modifier.size(20.dp),
                )
            }
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = stringResource(R.string.sign_in_browser_waiting_title),
                    style = MaterialTheme.typography.titleMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Text(
                    text = browser.error
                        ?: fallback?.let { setupMessageText(it) }
                        ?: stringResource(R.string.sign_in_browser_waiting_text),
                    style = MaterialTheme.typography.bodySmall,
                    color = if (browser.error == null && fallback == null) {
                        MaterialTheme.colorScheme.onSurfaceVariant
                    } else {
                        warning
                    },
                )
            }
        }
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            Button(
                onClick = onOpen,
                modifier = Modifier
                    .weight(1f)
                    .heightIn(min = Dimens.ControlHeight)
                    .testTag(SetupTags.SIGN_IN_BROWSER_OPEN),
            ) {
                Text(stringResource(R.string.sign_in_browser_open))
            }
            TextButton(
                onClick = onCancel,
                modifier = Modifier
                    .heightIn(min = Dimens.ControlHeight)
                    .testTag(SetupTags.SIGN_IN_BROWSER_CANCEL),
            ) {
                Text(stringResource(R.string.sign_in_browser_cancel))
            }
        }
    }
}
