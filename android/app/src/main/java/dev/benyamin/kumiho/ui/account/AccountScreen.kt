package dev.benyamin.kumiho.ui.account

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.LifecycleResumeEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.engine.Account
import dev.benyamin.kumiho.ui.components.KumihoCard
import dev.benyamin.kumiho.ui.components.KumihoLoadingState
import dev.benyamin.kumiho.ui.setup.SignInPane
import dev.benyamin.kumiho.ui.theme.Dimens
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors

/** Test tags for the Account screen. */
object AccountTags {
    const val SCREEN = "account_screen"
    const val CARD = "account_card"
    const val EMAIL = "account_email"
    const val UID = "account_uid"
    const val SUBSCRIPTION = "account_subscription"
    const val QUOTA = "account_quota"
    const val BANDWIDTH = "account_bandwidth"
    const val TOKEN = "account_token"
    const val REFRESH = "account_refresh"
    const val SIGN_OUT = "account_sign_out"
    const val LOADING = "account_loading"
    const val ERROR = "account_error"
    const val DIALOG = "account_sign_out_dialog"
    const val DIALOG_CONFIRM = "account_sign_out_confirm"
    const val DIALOG_CANCEL = "account_sign_out_cancel"
    const val SIGNED_OUT_TITLE = "account_signed_out_title"
}

/**
 * The Account entry point. It follows the Settings engine gate and owns one
 * rotation-safe view model per engine. Signing out stays on Account; signing in
 * reuses the setup pane and exits only to the signed-in card.
 */
@Composable
fun AccountRoute(
    container: AppContainer,
    modifier: Modifier = Modifier,
) {
    val coordinator = container.vpnServiceCoordinator
    val serviceState by coordinator.state.collectAsStateWithLifecycle()
    val engine by coordinator.engine.collectAsStateWithLifecycle()
    val client = engine
    if (client == null) {
        AccountWaitingSurface(error = serviceState.error, modifier = modifier)
        return
    }

    val gateway = remember(client) { EngineAccountGateway(client) }
    val key = remember(client) { "account-${System.identityHashCode(client)}" }
    val viewModel: AccountViewModel = viewModel(key = key) {
        AccountViewModel(gateway)
    }
    val state by viewModel.state.collectAsStateWithLifecycle()

    LifecycleResumeEffect(viewModel) {
        viewModel.setScreenResumed(true)
        onPauseOrDispose { viewModel.setScreenResumed(false) }
    }

    AccountScreen(
        state = state,
        actions = AccountActions(
            onRefresh = viewModel::refresh,
            onRetry = viewModel::retry,
            onDismissMessage = viewModel::dismissMessage,
            onSubmitEmail = viewModel::submitEmail,
            onSubmitPassword = viewModel::submitPassword,
            onSubmitCode = viewModel::submitCode,
            onRecheck = viewModel::recheckEmailLink,
            onSignOut = viewModel::requestSignOut,
            onDismissSignOut = viewModel::dismissSignOut,
            onConfirmSignOut = viewModel::confirmSignOut,
        ),
        modifier = modifier,
    )
}

/** The stateless Account screen. */
@Composable
fun AccountScreen(
    state: AccountUiState,
    actions: AccountActions,
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxSize().testTag(AccountTags.SCREEN)) {
        val notice = accountNoticeText(state.message)
        val detail = state.error
        if (notice != null || detail != null) {
            AccountBanner(
                title = notice.orEmpty(),
                detail = detail,
                onRetry = if (state.message == AccountMessage.TIMEOUT ||
                    state.message == AccountMessage.LOAD_FAILED ||
                    state.message == AccountMessage.REFRESH_FAILED
                ) {
                    actions.onRetry
                } else {
                    null
                },
                onDismiss = actions.onDismissMessage,
            )
        }

        when {
            state.loading && state.account == null -> KumihoLoadingState(
                label = stringResource(R.string.account_loading),
                modifier = Modifier.testTag(AccountTags.LOADING),
            )
            state.signedIn -> SignedInCard(state = state, actions = actions)
            else -> SignInSection(state = state, actions = actions)
        }
    }

    if (state.signOutDialog) {
        SignOutDialog(
            onConfirm = actions.onConfirmSignOut,
            onDismiss = actions.onDismissSignOut,
        )
    }
}

@Composable
private fun SignedInCard(state: AccountUiState, actions: AccountActions) {
    val account = state.account ?: return
    val loss = LocalKumihoColors.current.loss
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(Dimens.ScreenPadding),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space3),
    ) {
        item(key = "card") {
            AccountCard(account = account)
        }
        item(key = "actions") {
            Column(verticalArrangement = Arrangement.spacedBy(Dimens.Space2)) {
                Button(
                    onClick = actions.onRefresh,
                    enabled = !state.refreshing && !state.signingOut,
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(min = Dimens.ControlHeight)
                        .testTag(AccountTags.REFRESH),
                ) {
                    Text(
                        if (state.refreshing) {
                            stringResource(R.string.account_refreshing)
                        } else {
                            stringResource(R.string.account_refresh)
                        },
                    )
                }
                OutlinedButton(
                    onClick = actions.onSignOut,
                    enabled = !state.signingOut,
                    colors = ButtonDefaults.outlinedButtonColors(contentColor = loss),
                    border = BorderStroke(1.dp, loss.copy(alpha = 0.5f)),
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(min = Dimens.ControlHeight)
                        .testTag(AccountTags.SIGN_OUT),
                ) {
                    Text(
                        if (state.signingOut) {
                            stringResource(R.string.account_signing_out)
                        } else {
                            stringResource(R.string.account_sign_out)
                        },
                    )
                }
            }
        }
        item(key = "note") {
            Text(
                text = stringResource(R.string.account_sign_out_note),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun AccountCard(account: Account) {
    KumihoCard(modifier = Modifier.fillMaxWidth().testTag(AccountTags.CARD)) {
        Text(
            text = stringResource(R.string.account_signed_in_title),
            style = MaterialTheme.typography.titleLarge,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Text(
            text = account.email?.takeIf { it.isNotBlank() }
                ?: stringResource(R.string.account_not_reported),
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurface,
            modifier = Modifier
                .padding(top = Dimens.Space1)
                .testTag(AccountTags.EMAIL),
        )
        AccountRow(
            label = stringResource(R.string.account_uid),
            value = account.uid?.takeIf { it.isNotBlank() }
                ?: stringResource(R.string.account_not_reported),
            tag = AccountTags.UID,
        )
        AccountRow(
            label = stringResource(R.string.account_subscription),
            value = if (account.subscribed) {
                stringResource(R.string.account_subscribed)
            } else {
                stringResource(R.string.account_not_subscribed)
            },
            tag = AccountTags.SUBSCRIPTION,
        )
        AccountRow(
            label = stringResource(R.string.account_quota),
            value = AccountFormat.quota(account)
                ?: stringResource(R.string.account_quota_none),
            tag = AccountTags.QUOTA,
        )
        AccountRow(
            label = stringResource(R.string.account_bandwidth),
            value = if (AccountFormat.bandwidthLimited(account)) {
                stringResource(R.string.account_bandwidth_limited)
            } else {
                stringResource(R.string.account_bandwidth_unlimited)
            },
            tag = AccountTags.BANDWIDTH,
        )
        AccountRow(
            label = stringResource(R.string.account_token),
            value = AccountFormat.tokenExpiry(account)
                ?: stringResource(R.string.account_token_unknown),
            tag = AccountTags.TOKEN,
        )
    }
}

@Composable
private fun AccountRow(label: String, value: String, tag: String) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = Dimens.Space2)
            .testTag(tag),
        horizontalArrangement = Arrangement.spacedBy(Dimens.Space3),
    ) {
        Text(
            text = label,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.weight(1f),
        )
        Text(
            text = value,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurface,
            textAlign = TextAlign.End,
            modifier = Modifier.weight(1.6f),
        )
    }
}

@Composable
private fun SignInSection(state: AccountUiState, actions: AccountActions) {
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(Dimens.ScreenPadding),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space3),
    ) {
        item(key = "title") {
            Column(verticalArrangement = Arrangement.spacedBy(Dimens.Space1)) {
                Text(
                    text = stringResource(R.string.account_signed_out_title),
                    style = MaterialTheme.typography.titleLarge,
                    color = MaterialTheme.colorScheme.onSurface,
                    modifier = Modifier.testTag(AccountTags.SIGNED_OUT_TITLE),
                )
                Text(
                    text = stringResource(R.string.account_signed_out_text),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        item(key = "pane") {
            SignInPane(
                state = state.signIn,
                onSubmitEmail = actions.onSubmitEmail,
                onSubmitPassword = actions.onSubmitPassword,
                onSubmitCode = actions.onSubmitCode,
                onRecheck = actions.onRecheck,
                signedInNote = stringResource(R.string.account_signed_in_note),
            )
        }
    }
}

@Composable
private fun SignOutDialog(onConfirm: () -> Unit, onDismiss: () -> Unit) {
    val loss = LocalKumihoColors.current.loss
    AlertDialog(
        onDismissRequest = onDismiss,
        modifier = Modifier.testTag(AccountTags.DIALOG),
        title = { Text(stringResource(R.string.account_sign_out_title)) },
        text = { Text(stringResource(R.string.account_sign_out_text)) },
        confirmButton = {
            TextButton(
                onClick = onConfirm,
                colors = ButtonDefaults.textButtonColors(contentColor = loss),
                modifier = Modifier.testTag(AccountTags.DIALOG_CONFIRM),
            ) {
                Text(stringResource(R.string.account_sign_out_confirm))
            }
        },
        dismissButton = {
            TextButton(
                onClick = onDismiss,
                modifier = Modifier.testTag(AccountTags.DIALOG_CANCEL),
            ) {
                Text(stringResource(R.string.account_sign_out_cancel))
            }
        },
    )
}

@Composable
private fun AccountBanner(
    title: String,
    detail: String?,
    onRetry: (() -> Unit)?,
    onDismiss: () -> Unit,
) {
    val warning = LocalKumihoColors.current.warning
    Surface(
        modifier = Modifier
            .fillMaxWidth()
            .padding(
                start = Dimens.ScreenPadding,
                end = Dimens.ScreenPadding,
                top = Dimens.Space3,
            ),
        shape = MaterialTheme.shapes.medium,
        color = warning.copy(alpha = 0.12f),
        border = BorderStroke(1.dp, warning.copy(alpha = 0.5f)),
    ) {
        Row(
            modifier = Modifier.padding(Dimens.Space3),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            Icon(
                imageVector = Icons.Filled.Warning,
                contentDescription = null,
                tint = warning,
                modifier = Modifier.size(20.dp),
            )
            Column(modifier = Modifier.weight(1f)) {
                if (title.isNotBlank()) {
                    Text(
                        text = title,
                        style = MaterialTheme.typography.titleSmall,
                        color = warning,
                    )
                }
                if (!detail.isNullOrBlank()) {
                    Text(
                        text = detail,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                }
            }
            if (onRetry != null) {
                TextButton(onClick = onRetry) {
                    Text(stringResource(R.string.account_retry))
                }
            }
            TextButton(onClick = onDismiss) {
                Text(stringResource(R.string.account_dismiss))
            }
        }
    }
}

@Composable
private fun AccountWaitingSurface(error: String?, modifier: Modifier = Modifier) {
    Surface(modifier = modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            if (error == null) {
                KumihoLoadingState(
                    label = stringResource(R.string.account_loading),
                    modifier = Modifier.testTag(AccountTags.LOADING),
                )
            } else {
                Column(
                    modifier = Modifier.padding(Dimens.Space5),
                    horizontalAlignment = Alignment.CenterHorizontally,
                    verticalArrangement = Arrangement.spacedBy(Dimens.Space3),
                ) {
                    Icon(
                        imageVector = Icons.Filled.Warning,
                        contentDescription = null,
                        tint = MaterialTheme.colorScheme.onSurface,
                        modifier = Modifier.size(40.dp),
                    )
                    Text(
                        text = stringResource(R.string.account_engine_unavailable_title),
                        style = MaterialTheme.typography.titleLarge,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    Text(
                        text = stringResource(R.string.account_engine_unavailable),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        textAlign = TextAlign.Center,
                    )
                    Text(
                        text = error,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        textAlign = TextAlign.Center,
                    )
                }
            }
        }
    }
}

@Composable
private fun accountNoticeText(message: AccountMessage?): String? = when (message) {
    null -> null
    AccountMessage.TIMEOUT -> stringResource(R.string.account_timeout)
    AccountMessage.LOAD_FAILED -> stringResource(R.string.account_load_failed)
    AccountMessage.REFRESH_FAILED -> stringResource(R.string.account_refresh_failed)
    AccountMessage.SIGN_OUT_FAILED -> stringResource(R.string.account_sign_out_failed)
}

/** The callbacks the stateless Account screen needs. */
data class AccountActions(
    val onRefresh: () -> Unit,
    val onRetry: () -> Unit,
    val onDismissMessage: () -> Unit,
    val onSubmitEmail: (String) -> Unit,
    val onSubmitPassword: (String) -> Unit,
    val onSubmitCode: (String) -> Unit,
    val onRecheck: () -> Unit,
    val onSignOut: () -> Unit,
    val onDismissSignOut: () -> Unit,
    val onConfirmSignOut: () -> Unit,
)
