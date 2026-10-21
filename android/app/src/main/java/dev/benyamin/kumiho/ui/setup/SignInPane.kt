package dev.benyamin.kumiho.ui.setup

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.components.KumihoErrorState
import dev.benyamin.kumiho.ui.theme.Dimens

/**
 * The Firefox Accounts pane: email, password, then either a code or the
 * email-link waiting state. It is UI only — no view model, no engine, no
 * Android permission API — so the Account screen (T15) can reuse it.
 *
 * Secrets never leave the composable: the password and the code live in
 * `remember` (never `rememberSaveable`, so they are not written to the saved
 * instance state) and are cleared synchronously the moment their submit is
 * pressed, before the caller sees them.
 */
@Composable
fun SignInPane(
    state: SignInPaneState,
    onSubmitEmail: (String) -> Unit,
    onSubmitPassword: (String) -> Unit,
    onSubmitCode: (String) -> Unit,
    onRecheck: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var email by rememberSaveable { mutableStateOf(state.email) }
    var password by remember { mutableStateOf("") }
    var code by remember { mutableStateOf("") }

    Column(
        modifier = modifier.fillMaxWidth(),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space4),
    ) {
        when (state.phase) {
            SignInPhase.EMAIL -> {
                Text(
                    text = stringResource(R.string.sign_in_intro),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                OutlinedTextField(
                    value = email,
                    onValueChange = { email = it },
                    label = { Text(stringResource(R.string.sign_in_email_label)) },
                    singleLine = true,
                    enabled = !state.busy,
                    keyboardOptions = KeyboardOptions(
                        keyboardType = KeyboardType.Email,
                        imeAction = ImeAction.Done,
                    ),
                    keyboardActions = KeyboardActions(onDone = { onSubmitEmail(email) }),
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(SetupTags.SIGN_IN_EMAIL),
                )
                Button(
                    onClick = { onSubmitEmail(email) },
                    enabled = !state.busy,
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(min = Dimens.ControlHeight)
                        .testTag(SetupTags.SIGN_IN_EMAIL_SUBMIT),
                ) {
                    Text(stringResource(R.string.sign_in_email_submit))
                }
            }

            SignInPhase.PASSWORD -> {
                Text(
                    text = stringResource(R.string.sign_in_password_title),
                    style = MaterialTheme.typography.titleMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                if (state.email.isNotBlank()) {
                    Text(
                        text = state.email,
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                OutlinedTextField(
                    value = password,
                    onValueChange = { password = it },
                    label = { Text(stringResource(R.string.sign_in_password_label)) },
                    singleLine = true,
                    enabled = !state.busy,
                    visualTransformation = PasswordVisualTransformation(),
                    keyboardOptions = KeyboardOptions(
                        keyboardType = KeyboardType.Password,
                        imeAction = ImeAction.Done,
                    ),
                    keyboardActions = KeyboardActions(
                        onDone = {
                            val value = password
                            password = ""
                            onSubmitPassword(value)
                        },
                    ),
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(SetupTags.SIGN_IN_PASSWORD),
                )
                Text(
                    text = stringResource(R.string.sign_in_password_note),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Button(
                    onClick = {
                        val value = password
                        password = ""
                        onSubmitPassword(value)
                    },
                    enabled = !state.busy,
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(min = Dimens.ControlHeight)
                        .testTag(SetupTags.SIGN_IN_PASSWORD_SUBMIT),
                ) {
                    Text(stringResource(R.string.sign_in_password_submit))
                }
            }

            SignInPhase.CODE -> {
                Text(
                    text = stringResource(R.string.sign_in_code_title),
                    style = MaterialTheme.typography.titleMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                if (state.email.isNotBlank()) {
                    Text(
                        text = state.email,
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                val reason = state.verificationReason
                if (!reason.isNullOrBlank()) {
                    Text(
                        text = stringResource(R.string.sign_in_reason, reason),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Text(
                    text = stringResource(R.string.sign_in_code_note),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                OutlinedTextField(
                    value = code,
                    onValueChange = { code = it },
                    label = { Text(stringResource(R.string.sign_in_code_label)) },
                    singleLine = true,
                    enabled = !state.busy,
                    keyboardOptions = KeyboardOptions(
                        keyboardType = KeyboardType.Number,
                        imeAction = ImeAction.Done,
                    ),
                    keyboardActions = KeyboardActions(
                        onDone = {
                            val value = code
                            code = ""
                            onSubmitCode(value)
                        },
                    ),
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(SetupTags.SIGN_IN_CODE),
                )
                Button(
                    onClick = {
                        val value = code
                        code = ""
                        onSubmitCode(value)
                    },
                    enabled = !state.busy,
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(min = Dimens.ControlHeight)
                        .testTag(SetupTags.SIGN_IN_CODE_SUBMIT),
                ) {
                    Text(stringResource(R.string.sign_in_code_submit))
                }
            }

            SignInPhase.EMAIL_LINK_WAITING -> {
                Text(
                    text = stringResource(R.string.sign_in_link_title),
                    style = MaterialTheme.typography.titleMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Text(
                    text = stringResource(
                        R.string.sign_in_link_text,
                        state.email.ifBlank { state.accountEmail.orEmpty() },
                    ),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(SetupTags.SIGN_IN_WAITING),
                    verticalArrangement = Arrangement.spacedBy(Dimens.Space2),
                ) {
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
                    ) {
                        CircularProgressIndicator(
                            color = MaterialTheme.colorScheme.primary,
                            strokeWidth = 2.dp,
                            modifier = Modifier.size(16.dp),
                        )
                        Text(
                            text = stringResource(R.string.sign_in_link_waiting),
                            style = MaterialTheme.typography.titleSmall,
                            color = MaterialTheme.colorScheme.onSurface,
                        )
                    }
                    Text(
                        text = stringResource(R.string.sign_in_link_polling),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Button(
                    onClick = onRecheck,
                    enabled = !state.busy,
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(min = Dimens.ControlHeight)
                        .testTag(SetupTags.SIGN_IN_RECHECK),
                ) {
                    Text(stringResource(R.string.sign_in_recheck))
                }
            }

            SignInPhase.AUTHENTICATED -> {
                Text(
                    text = stringResource(R.string.sign_in_signed_title),
                    style = MaterialTheme.typography.titleMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Text(
                    text = stringResource(
                        R.string.sign_in_signed_as,
                        state.accountEmail?.takeIf { it.isNotBlank() } ?: state.email,
                    ),
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurface,
                    modifier = Modifier.testTag(SetupTags.SIGN_IN_ACCOUNT),
                )
                Text(
                    text = stringResource(R.string.sign_in_signed_note),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }

        val errorText = state.error ?: state.message?.let { setupMessageText(it) }
        if (!errorText.isNullOrBlank()) {
            KumihoErrorState(
                message = errorText,
                title = stringResource(R.string.sign_in_error_title),
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(top = Dimens.Space1)
                    .testTag(SetupTags.SIGN_IN_ERROR),
            )
        }
    }
}

/** The copy for a locally generated message, resolved from `strings.xml`. */
@Composable
internal fun setupMessageText(message: SetupMessage): String = stringResource(
    when (message) {
        SetupMessage.EMAIL_REQUIRED -> R.string.sign_in_email_required
        SetupMessage.PASSWORD_REQUIRED -> R.string.sign_in_password_required
        SetupMessage.CODE_REQUIRED -> R.string.sign_in_code_required
        SetupMessage.UNEXPECTED_STEP -> R.string.sign_in_unexpected_step
    },
)
