package dev.benyamin.kumiho.ui.account

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.components.KumihoCard
import dev.benyamin.kumiho.ui.theme.Dimens
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors

/** Stable test tags for the shared browser waiting card. */
object BrowserWaitingTags {
    const val CARD = "browser_waiting_card"
    const val URL = "browser_waiting_url"
    const val OPEN = "browser_waiting_open"
    const val PASTE_TOGGLE = "browser_waiting_paste_toggle"
    const val PASTE_FIELD = "browser_waiting_paste_field"
    const val PASTE_ERROR = "browser_waiting_paste_error"
    const val PASTE_SUBMIT = "browser_waiting_paste_submit"
    const val USE_PASSWORD = "browser_waiting_use_password"
    const val CANCEL = "browser_waiting_cancel"
}

/**
 * The browser sign-in waiting card shared by Account and Setup. It shows the
 * engine-owned flow, offers the in-app browser again, and reveals the pasted
 * redirect-URL fallback for devices or browsers that cannot intercept it.
 *
 * Every control is disabled while a submission is in flight; the paste field
 * takes focus as soon as it is revealed, and the engine's error stays inline so
 * the user can retry the same one-shot redirect manually.
 */
@Composable
fun BrowserWaitingCard(
    browser: BrowserSignIn,
    title: String,
    hint: String,
    fallbackHint: String,
    pasteOpen: Boolean,
    pasteValue: String,
    pasteError: String?,
    pasteMessage: BrowserPasteMessage?,
    submitting: Boolean,
    fallback: Boolean,
    onOpen: () -> Unit,
    onTogglePaste: () -> Unit,
    onPasteChange: (String) -> Unit,
    onPasteSubmit: () -> Unit,
    onUsePassword: () -> Unit,
    onCancel: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val warning = LocalKumihoColors.current.warning
    val failed = browser.error != null || fallback
    val focusRequester = remember { FocusRequester() }
    val keyboard = LocalSoftwareKeyboardController.current

    LaunchedEffect(pasteOpen) {
        if (pasteOpen) {
            focusRequester.requestFocus()
            keyboard?.show()
        }
    }

    KumihoCard(
        modifier = modifier
            .fillMaxWidth()
            .testTag(BrowserWaitingTags.CARD),
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            if (browser.error == null && !fallback) {
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
                    text = title,
                    style = MaterialTheme.typography.titleMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Text(
                    text = browser.error ?: if (fallback) fallbackHint else hint,
                    style = MaterialTheme.typography.bodySmall,
                    color = if (failed) warning else MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }

        browser.url?.takeIf { it.isNotBlank() }?.let { url ->
            Text(
                text = url,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier
                    .fillMaxWidth()
                    .testTag(BrowserWaitingTags.URL),
            )
        }

        Button(
            onClick = onOpen,
            enabled = !submitting,
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = Dimens.ControlHeight)
                .testTag(BrowserWaitingTags.OPEN),
        ) {
            Text(stringResource(R.string.browser_waiting_open))
        }

        TextButton(
            onClick = onTogglePaste,
            enabled = !submitting,
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = Dimens.TouchTarget)
                .testTag(BrowserWaitingTags.PASTE_TOGGLE),
        ) {
            Text(
                if (pasteOpen) {
                    stringResource(R.string.browser_waiting_paste_hide)
                } else {
                    stringResource(R.string.browser_waiting_paste_show)
                },
            )
        }

        if (pasteOpen) {
            val inlineError = pasteMessage?.let { pasteMessageText(it) } ?: pasteError
            OutlinedTextField(
                value = pasteValue,
                onValueChange = onPasteChange,
                label = { Text(stringResource(R.string.browser_waiting_paste_label)) },
                singleLine = false,
                minLines = 1,
                maxLines = 3,
                enabled = !submitting,
                isError = !inlineError.isNullOrBlank(),
                keyboardOptions = KeyboardOptions(
                    keyboardType = KeyboardType.Uri,
                    imeAction = ImeAction.Done,
                ),
                keyboardActions = KeyboardActions(onDone = { onPasteSubmit() }),
                modifier = Modifier
                    .fillMaxWidth()
                    .focusRequester(focusRequester)
                    .testTag(BrowserWaitingTags.PASTE_FIELD),
            )
            if (!inlineError.isNullOrBlank()) {
                Text(
                    text = inlineError,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(BrowserWaitingTags.PASTE_ERROR),
                )
            }
            Button(
                onClick = onPasteSubmit,
                enabled = !submitting,
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(min = Dimens.ControlHeight)
                    .testTag(BrowserWaitingTags.PASTE_SUBMIT),
            ) {
                Text(stringResource(R.string.browser_waiting_paste_continue))
            }
        }

        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            OutlinedButton(
                onClick = onUsePassword,
                enabled = !submitting,
                modifier = Modifier
                    .weight(1f)
                    .heightIn(min = Dimens.ControlHeight)
                    .testTag(BrowserWaitingTags.USE_PASSWORD),
            ) {
                Text(stringResource(R.string.browser_waiting_use_password))
            }
            TextButton(
                onClick = onCancel,
                enabled = !submitting,
                modifier = Modifier
                    .heightIn(min = Dimens.ControlHeight)
                    .testTag(BrowserWaitingTags.CANCEL),
            ) {
                Text(stringResource(R.string.browser_waiting_cancel))
            }
        }
    }
}

/** The local validation copy for the paste field, resolved by the UI. */
@Composable
internal fun pasteMessageText(message: BrowserPasteMessage): String = stringResource(
    when (message) {
        BrowserPasteMessage.REQUIRED -> R.string.browser_waiting_paste_required
    },
)
