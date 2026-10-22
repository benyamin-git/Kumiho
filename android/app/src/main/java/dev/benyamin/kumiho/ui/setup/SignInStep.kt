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
import dev.benyamin.kumiho.ui.theme.Dimens

/**
 * Step 2: the sign-in heading plus the shared [SignInPane]. The pane is reused
 * by the Account screen (T15), so the step owns only the heading and the step
 * tag.
 */
@Composable
fun SignInStep(
    state: SignInPaneState,
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
        SignInPane(
            state = state,
            onSubmitEmail = onSubmitEmail,
            onSubmitPassword = onSubmitPassword,
            onSubmitCode = onSubmitCode,
            onRecheck = onRecheck,
        )
    }
}
