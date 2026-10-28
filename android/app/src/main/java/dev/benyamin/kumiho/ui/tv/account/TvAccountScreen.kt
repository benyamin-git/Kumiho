package dev.benyamin.kumiho.ui.tv.account

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.OutlinedTextField
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Text
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.engine.AccountSummary
import dev.benyamin.kumiho.ui.account.AccountActions
import dev.benyamin.kumiho.ui.account.AccountFormat
import dev.benyamin.kumiho.ui.account.AccountMessage
import dev.benyamin.kumiho.ui.account.AccountTags
import dev.benyamin.kumiho.ui.account.AccountUiState
import dev.benyamin.kumiho.ui.account.BrowserWaitingCard
import dev.benyamin.kumiho.ui.account.MozillaAccountBrowserDialog
import dev.benyamin.kumiho.ui.account.accountNoticeText
import dev.benyamin.kumiho.ui.dashboard.DashboardFormat
import dev.benyamin.kumiho.ui.setup.SignInPane
import dev.benyamin.kumiho.ui.theme.KumihoShapes
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors
import dev.benyamin.kumiho.ui.tv.components.TvButton
import dev.benyamin.kumiho.ui.tv.components.TvDialog
import dev.benyamin.kumiho.ui.tv.components.TvFocusableSurface
import dev.benyamin.kumiho.ui.tv.components.TvPageHeader
import dev.benyamin.kumiho.ui.tv.components.TvValue
import dev.benyamin.kumiho.ui.tv.theme.TvDimens
import dev.benyamin.kumiho.ui.tv.theme.TvM3Typography
import java.time.ZoneId

/**
 * The TV account page: accounts on the left, the current account and its
 * device-wide actions on the right. The shared sign-in pane and browser card
 * keep full parity; they are rendered with the TV type scale.
 */
@Composable
fun TvAccountScreen(
    state: AccountUiState,
    actions: AccountActions,
    modifier: Modifier = Modifier,
) {
    var manageTarget by remember { mutableStateOf<AccountSummary?>(null) }
    val notice = accountNoticeText(state.message)
        ?: state.accounts?.error?.let { stringResource(R.string.account_accounts_failed) }
    val detail = state.error ?: state.accounts?.error ?: state.accounts?.notice

    Box(
        modifier = modifier
            .fillMaxSize()
            .testTag(AccountTags.SCREEN),
    ) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(TvDimens.Space2),
            verticalArrangement = Arrangement.spacedBy(TvDimens.Space2),
        ) {
            TvPageHeader(title = stringResource(R.string.account_signed_in_title))
            if (notice != null || detail != null) {
                AccountBanner(
                    notice = notice,
                    detail = detail,
                    onRetry = if (state.message in retryable) actions.onRetry else null,
                    onDismiss = actions.onDismissMessage,
                )
            }
            Row(
                modifier = Modifier.fillMaxSize(),
                horizontalArrangement = Arrangement.spacedBy(TvDimens.Space4),
            ) {
                Column(
                    modifier = Modifier
                        .weight(1f)
                        .fillMaxSize()
                        .verticalScroll(rememberScrollState()),
                    verticalArrangement = Arrangement.spacedBy(TvDimens.Space2),
                ) {
                    AccountsBlock(
                        state = state,
                        actions = actions,
                        onManage = { manageTarget = it },
                    )
                    if (state.addingAccount) {
                        Text(
                            text = stringResource(R.string.account_add_password_title),
                            style = MaterialTheme.typography.titleLarge,
                            color = MaterialTheme.colorScheme.onSurface,
                        )
                        PhoneAtTvScale {
                            SignInPane(
                                state = state.signIn,
                                onSubmitEmail = actions.onSubmitEmail,
                                onSubmitPassword = actions.onSubmitPassword,
                                onSubmitCode = actions.onSubmitCode,
                                onRecheck = actions.onRecheck,
                                onBrowser = null,
                            )
                        }
                        TvButton(
                            label = stringResource(R.string.account_add_cancel),
                            onClick = actions.onCancelAdd,
                            modifier = Modifier
                                .fillMaxWidth()
                                .testTag(AccountTags.ADD_PASSWORD_CANCEL),
                        )
                    } else if (state.showSignIn && state.browser == null) {
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
                        PhoneAtTvScale {
                            SignInPane(
                                state = state.signIn,
                                onSubmitEmail = actions.onSubmitEmail,
                                onSubmitPassword = actions.onSubmitPassword,
                                onSubmitCode = actions.onSubmitCode,
                                onRecheck = actions.onRecheck,
                                onBrowser = actions.onAddBrowser,
                                signedInNote = stringResource(R.string.account_signed_in_note),
                            )
                        }
                    }
                    if (state.loading && state.account == null && state.accounts == null) {
                        Text(
                            text = stringResource(R.string.account_loading),
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.testTag(AccountTags.LOADING),
                        )
                    }
                }
                Column(
                    modifier = Modifier
                        .weight(1f)
                        .fillMaxSize()
                        .verticalScroll(rememberScrollState()),
                    verticalArrangement = Arrangement.spacedBy(TvDimens.Space2),
                ) {
                    state.account?.let { account ->
                        if (state.signedIn) {
                            CurrentAccountCard(state = state)
                        }
                    }
                    if (state.accounts != null) {
                        RotationCard(state = state, actions = actions)
                    }
                    if (state.signedIn || state.hasAccounts) {
                        TvButton(
                            label = if (state.refreshing) {
                                stringResource(R.string.account_refreshing)
                            } else {
                                stringResource(R.string.account_refresh)
                            },
                            onClick = actions.onRefresh,
                            modifier = Modifier
                                .fillMaxWidth()
                                .testTag(AccountTags.REFRESH),
                            enabled = !state.refreshing && !state.signingOut,
                            primary = true,
                        )
                        TvButton(
                            label = if (state.signingOut) {
                                stringResource(R.string.account_signing_out)
                            } else {
                                stringResource(R.string.account_sign_out)
                            },
                            onClick = actions.onSignOut,
                            modifier = Modifier
                                .fillMaxWidth()
                                .testTag(AccountTags.SIGN_OUT),
                            enabled = !state.signingOut,
                        )
                    }
                }
            }
        }

        if (state.browser != null) {
            BrowserOverlay(state = state, actions = actions)
        }

        if (state.signOutDialog) {
            SignOutDialog(
                accountCount = state.accounts?.accounts?.size ?: 1,
                onConfirm = actions.onConfirmSignOut,
                onDismiss = actions.onDismissSignOut,
            )
        }
        if (state.addDialog) {
            AddAccountDialog(
                onBrowser = actions.onAddBrowser,
                onPassword = actions.onAddPassword,
                onDismiss = actions.onDismissAdd,
            )
        }
        state.removeTarget?.let { target ->
            RemoveAccountDialog(
                target = target,
                next = AccountFormat.nextActive(state.accounts, target),
                onConfirm = actions.onRemoveConfirm,
                onDismiss = actions.onDismissRemove,
            )
        }
        state.renameTarget?.let { target ->
            RenameAccountDialog(
                target = target,
                onConfirm = actions.onRenameConfirm,
                onDismiss = actions.onDismissRename,
            )
        }
        manageTarget?.let { target ->
            ManageAccountDialog(
                target = target,
                onUse = {
                    actions.onUse(target.id)
                    manageTarget = null
                },
                onReauth = {
                    actions.onReauth(target)
                    manageTarget = null
                },
                onRename = {
                    actions.onRenameRequest(target)
                    manageTarget = null
                },
                onRemove = {
                    actions.onRemoveRequest(target)
                    manageTarget = null
                },
                onDismiss = { manageTarget = null },
            )
        }

        val browser = state.browser
        val browserURL = state.browserLaunchURL ?: browser?.url
        if (state.browserWebViewOpen && browser != null && browserURL != null) {
            MozillaAccountBrowserDialog(
                loginId = browser.id,
                url = browserURL,
                onRedirect = actions.onBrowserRedirect,
                onClose = actions.onCloseBrowser,
                onUnavailable = actions.onBrowserUnavailable,
            )
        }
    }
}

@Composable
private fun AccountsBlock(
    state: AccountUiState,
    actions: AccountActions,
    onManage: (AccountSummary) -> Unit,
) {
    val colors = MaterialTheme.colorScheme
    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = stringResource(R.string.account_accounts_title),
                style = MaterialTheme.typography.titleLarge,
                color = colors.onSurface,
                modifier = Modifier.testTag(AccountTags.ACCOUNTS),
            )
            Text(
                text = stringResource(R.string.account_accounts_hint),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
        }
        TvButton(
            label = stringResource(R.string.account_add),
            onClick = actions.onAddAccount,
            modifier = Modifier.testTag(AccountTags.ADD_ACCOUNT),
        )
    }
    val accounts = state.accounts?.accounts.orEmpty()
    if (accounts.isEmpty()) {
        Text(
            text = stringResource(R.string.account_signed_out_text),
            style = MaterialTheme.typography.bodyMedium,
            color = colors.onSurfaceVariant,
        )
        return
    }
    accounts.forEach { account ->
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            TvFocusableSurface(
                onClick = { actions.onUse(account.id) },
                modifier = Modifier
                    .weight(1f)
                    .testTag(AccountTags.accountRow(account.id)),
            ) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
                ) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            text = account.label.ifBlank { account.email },
                            style = MaterialTheme.typography.titleSmall,
                            color = colors.onSurface,
                        )
                        Text(
                            text = account.email,
                            style = MaterialTheme.typography.bodySmall,
                            color = colors.onSurfaceVariant,
                        )
                        Text(
                            text = methodLabel(account.method) +
                                if (account.reauthRequired) {
                                    " · " + stringResource(R.string.account_reauth_required)
                                } else {
                                    ""
                                },
                            style = MaterialTheme.typography.bodySmall,
                            color = colors.onSurfaceVariant,
                        )
                        Text(
                            text = quotaLabel(account),
                            style = MaterialTheme.typography.bodySmall,
                            color = colors.onSurfaceVariant,
                            modifier = Modifier.testTag(AccountTags.ROW_QUOTA),
                        )
                    }
                    if (account.active) {
                        Text(
                            text = "●",
                            style = MaterialTheme.typography.titleSmall,
                            color = colors.primary,
                            modifier = Modifier.testTag(AccountTags.ACTIVE),
                        )
                    }
                }
            }
            TvButton(
                label = stringResource(R.string.account_actions),
                onClick = { onManage(account) },
                modifier = Modifier.testTag(AccountTags.OVERFLOW),
            )
        }
    }
}

@Composable
private fun CurrentAccountCard(state: AccountUiState) {
    val account = state.account ?: return
    val locale = LocalConfiguration.current.locales[0]
    val quota = AccountFormat.quota(account) ?: stringResource(R.string.account_quota_none)
    val expiry = AccountFormat.tokenExpiry(account, ZoneId.systemDefault(), locale)
        ?: stringResource(R.string.account_not_reported)
    val colors = MaterialTheme.colorScheme
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(colors.surface)
            .border(1.dp, colors.borderVariant, MaterialTheme.shapes.medium)
            .padding(TvDimens.Space2)
            .testTag(AccountTags.CARD),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
    ) {
        Text(
            text = stringResource(R.string.account_signed_in_title),
            style = MaterialTheme.typography.titleLarge,
            color = colors.onSurface,
        )
        Text(
            text = account.email.orEmpty(),
            style = MaterialTheme.typography.titleMedium,
            color = colors.onSurface,
            modifier = Modifier.testTag(AccountTags.EMAIL),
        )
        TvValue(label = stringResource(R.string.account_uid), value = account.uid ?: DashboardFormat.DASH)
        TvValue(
            label = stringResource(R.string.account_subscription),
            value = stringResource(
                if (account.subscribed) R.string.account_subscribed else R.string.account_not_subscribed,
            ),
        )
        TvValue(label = stringResource(R.string.account_quota), value = quota)
        TvValue(
            label = stringResource(R.string.account_bandwidth),
            value = stringResource(
                if (AccountFormat.bandwidthLimited(account)) {
                    R.string.account_bandwidth_limited
                } else {
                    R.string.account_bandwidth_unlimited
                },
            ),
        )
        TvValue(label = stringResource(R.string.account_token), value = expiry)
    }
}

@Composable
private fun RotationCard(state: AccountUiState, actions: AccountActions) {
    val accounts = state.accounts ?: return
    val colors = MaterialTheme.colorScheme
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(colors.surface)
            .border(1.dp, colors.borderVariant, MaterialTheme.shapes.medium)
            .padding(TvDimens.Space2),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
    ) {
        Text(
            text = stringResource(R.string.account_rotation_title),
            style = MaterialTheme.typography.titleMedium,
            color = colors.onSurface,
        )
        Text(
            text = stringResource(R.string.account_rotation_hint),
            style = MaterialTheme.typography.bodySmall,
            color = colors.onSurfaceVariant,
        )
        TvFocusableSurface(
            onClick = { actions.onRotation(!accounts.rotation) },
            modifier = Modifier
                .fillMaxWidth()
                .testTag(AccountTags.ROTATION),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = stringResource(R.string.account_rotation_title),
                    style = MaterialTheme.typography.bodyMedium,
                    color = colors.onSurface,
                )
                Text(
                    text = stringResource(if (accounts.rotation) R.string.dashboard_on else R.string.dashboard_off),
                    style = MaterialTheme.typography.labelLarge,
                    color = if (accounts.rotation) colors.primary else colors.onSurfaceVariant,
                )
            }
        }
        Text(
            text = stringResource(R.string.account_threshold_title),
            style = MaterialTheme.typography.titleSmall,
            color = colors.onSurface,
        )
        Text(
            text = stringResource(R.string.account_threshold_hint),
            style = MaterialTheme.typography.bodySmall,
            color = colors.onSurfaceVariant,
        )
        Row(
            horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            TvButton(
                label = stringResource(R.string.account_threshold_decrease_symbol),
                onClick = { actions.onThreshold((accounts.threshold - 5).coerceAtLeast(5)) },
                modifier = Modifier.testTag(AccountTags.THRESHOLD_DOWN),
                enabled = !state.rotationSaving,
            )
            Text(
                text = stringResource(R.string.account_threshold_value, accounts.threshold),
                style = MaterialTheme.typography.titleMedium,
                color = colors.onSurface,
                modifier = Modifier.testTag(AccountTags.THRESHOLD),
            )
            TvButton(
                label = stringResource(R.string.account_threshold_increase_symbol),
                onClick = { actions.onThreshold((accounts.threshold + 5).coerceAtMost(95)) },
                modifier = Modifier.testTag(AccountTags.THRESHOLD_UP),
                enabled = !state.rotationSaving,
            )
        }
    }
}

@Composable
private fun AccountBanner(
    notice: String?,
    detail: String?,
    onRetry: (() -> Unit)?,
    onDismiss: () -> Unit,
) {
    val colors = MaterialTheme.colorScheme
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(colors.surfaceVariant)
            .border(1.dp, colors.borderVariant, MaterialTheme.shapes.medium)
            .padding(TvDimens.Space2)
            .testTag(AccountTags.NOTICE),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
    ) {
        notice?.let {
            Text(text = it, style = MaterialTheme.typography.bodyMedium, color = colors.onSurface)
        }
        detail?.let {
            Text(text = it, style = MaterialTheme.typography.bodySmall, color = colors.onSurfaceVariant)
        }
        Row(horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1)) {
            if (onRetry != null) {
                TvButton(
                    label = stringResource(R.string.account_retry),
                    onClick = onRetry,
                    modifier = Modifier.testTag(AccountTags.ERROR),
                )
            }
            TvButton(
                label = stringResource(R.string.account_dismiss),
                onClick = onDismiss,
            )
        }
    }
}

@Composable
private fun BrowserOverlay(state: AccountUiState, actions: AccountActions) {
    val browser = state.browser ?: return
    BackHandler(onBack = actions.onCancelBrowser)
    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background),
    ) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(TvDimens.Space2)
                .verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(TvDimens.Space2),
        ) {
            Text(
                text = stringResource(R.string.account_browser_title),
                style = MaterialTheme.typography.headlineMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
            PhoneAtTvScale {
                BrowserWaitingCard(
                    browser = browser,
                    title = stringResource(R.string.account_browser_title),
                    hint = stringResource(R.string.account_browser_hint),
                    fallbackHint = stringResource(R.string.browser_waiting_fallback),
                    pasteOpen = state.browserPasteOpen,
                    pasteValue = state.browserPasteValue,
                    pasteError = state.browserPasteError,
                    pasteMessage = state.browserPasteMessage,
                    submitting = state.browserSubmitting,
                    fallback = state.browserFallback,
                    onOpen = actions.onOpenBrowser,
                    onTogglePaste = actions.onToggleBrowserPaste,
                    onPasteChange = actions.onBrowserPasteChange,
                    onPasteSubmit = actions.onBrowserPasteSubmit,
                    onUsePassword = actions.onUseBrowserPassword,
                    onCancel = actions.onCancelBrowser,
                )
            }
            TvButton(
                label = stringResource(R.string.account_dismiss),
                onClick = actions.onCancelBrowser,
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}

@Composable
private fun SignOutDialog(accountCount: Int, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    val focus = remember { FocusRequester() }
    TvDialog(
        title = stringResource(R.string.account_sign_out_title),
        onDismiss = onDismiss,
        initialFocus = focus,
        content = {
            Text(
                text = stringResource(R.string.account_sign_out_text),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
            if (accountCount > 1) {
                Text(
                    text = stringResource(R.string.account_sign_out_all_text),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        },
        actions = {
            TvButton(
                label = stringResource(R.string.account_sign_out_cancel),
                onClick = onDismiss,
                modifier = Modifier
                    .focusRequester(focus)
                    .testTag(AccountTags.DIALOG_CANCEL),
            )
            TvButton(
                label = stringResource(R.string.account_sign_out_confirm),
                onClick = onConfirm,
                modifier = Modifier.testTag(AccountTags.DIALOG_CONFIRM),
                primary = true,
            )
        },
    )
}

@Composable
private fun AddAccountDialog(onBrowser: () -> Unit, onPassword: () -> Unit, onDismiss: () -> Unit) {
    val focus = remember { FocusRequester() }
    TvDialog(
        title = stringResource(R.string.account_add_title),
        onDismiss = onDismiss,
        initialFocus = focus,
        content = {
            Text(
                text = stringResource(R.string.account_add_text),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
        },
        actions = {
            TvButton(
                label = stringResource(R.string.account_add_dismiss),
                onClick = onDismiss,
                modifier = Modifier.testTag(AccountTags.ADD_CANCEL),
            )
            TvButton(
                label = stringResource(R.string.account_add_password),
                onClick = onPassword,
                modifier = Modifier.testTag(AccountTags.ADD_PASSWORD),
            )
            TvButton(
                label = stringResource(R.string.account_add_browser),
                onClick = onBrowser,
                modifier = Modifier
                    .focusRequester(focus)
                    .testTag(AccountTags.ADD_BROWSER),
                primary = true,
            )
        },
    )
}

@Composable
private fun RemoveAccountDialog(
    target: AccountSummary,
    next: AccountSummary?,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
) {
    val focus = remember { FocusRequester() }
    TvDialog(
        title = stringResource(R.string.account_remove_title),
        onDismiss = onDismiss,
        initialFocus = focus,
        content = {
            Text(
                text = if (next != null) {
                    stringResource(R.string.account_remove_active_text, target.email, next.label)
                } else {
                    stringResource(R.string.account_remove_text, target.email)
                },
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
        },
        actions = {
            TvButton(
                label = stringResource(R.string.account_remove_cancel),
                onClick = onDismiss,
                modifier = Modifier
                    .focusRequester(focus)
                    .testTag(AccountTags.REMOVE_CANCEL),
            )
            TvButton(
                label = stringResource(R.string.account_remove_confirm),
                onClick = onConfirm,
                modifier = Modifier.testTag(AccountTags.REMOVE_CONFIRM),
            )
        },
    )
}

@Composable
private fun RenameAccountDialog(
    target: AccountSummary,
    onConfirm: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    var label by remember { mutableStateOf(target.label) }
    val focus = remember { FocusRequester() }
    TvDialog(
        title = stringResource(R.string.account_rename_title),
        onDismiss = onDismiss,
        initialFocus = focus,
        content = {
            PhoneAtTvScale {
                OutlinedTextField(
                    value = label,
                    onValueChange = { label = it },
                    label = { androidx.compose.material3.Text(stringResource(R.string.account_rename_label)) },
                    singleLine = true,
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(AccountTags.RENAME_INPUT),
                )
            }
        },
        actions = {
            TvButton(
                label = stringResource(R.string.account_rename_cancel),
                onClick = onDismiss,
                modifier = Modifier.testTag(AccountTags.RENAME_CANCEL),
            )
            TvButton(
                label = stringResource(R.string.account_rename_confirm),
                onClick = { onConfirm(label) },
                modifier = Modifier
                    .focusRequester(focus)
                    .testTag(AccountTags.RENAME_CONFIRM),
                primary = true,
            )
        },
    )
}

@Composable
private fun ManageAccountDialog(
    target: AccountSummary,
    onUse: () -> Unit,
    onReauth: () -> Unit,
    onRename: () -> Unit,
    onRemove: () -> Unit,
    onDismiss: () -> Unit,
) {
    val focus = remember { FocusRequester() }
    TvDialog(
        title = target.label.ifBlank { target.email },
        onDismiss = onDismiss,
        initialFocus = focus,
        content = {
            Column(verticalArrangement = Arrangement.spacedBy(TvDimens.Space1)) {
                TvButton(
                    label = stringResource(R.string.account_use),
                    onClick = onUse,
                    modifier = Modifier
                        .fillMaxWidth()
                        .focusRequester(focus),
                    primary = true,
                )
                if (target.reauthRequired) {
                    TvButton(
                        label = stringResource(R.string.account_reauth),
                        onClick = onReauth,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
                TvButton(
                    label = stringResource(R.string.account_rename),
                    onClick = onRename,
                    modifier = Modifier.fillMaxWidth(),
                )
                TvButton(
                    label = stringResource(R.string.account_remove),
                    onClick = onRemove,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        },
        actions = {
            TvButton(
                label = stringResource(R.string.account_dismiss),
                onClick = onDismiss,
            )
        },
    )
}

/** The shared phone panes at the TV type scale, with the house palette. */
@Composable
private fun PhoneAtTvScale(content: @Composable () -> Unit) {
    val house = LocalKumihoColors.current
    androidx.compose.material3.MaterialTheme(
        colorScheme = house.toColorScheme(),
        typography = TvM3Typography,
        shapes = KumihoShapes,
        content = content,
    )
}

@Composable
private fun methodLabel(method: String): String = stringResource(
    when (method.lowercase()) {
        "password" -> R.string.account_method_password
        "browser" -> R.string.account_method_browser
        else -> R.string.account_method_browser
    },
)

@Composable
private fun quotaLabel(account: AccountSummary): String {
    val percent = AccountFormat.quotaPercent(account.quota)
        ?: return stringResource(R.string.account_row_quota_unknown)
    return stringResource(R.string.account_row_quota_left, percent)
}

private val retryable = setOf(
    AccountMessage.TIMEOUT,
    AccountMessage.LOAD_FAILED,
    AccountMessage.REFRESH_FAILED,
    AccountMessage.ACCOUNTS_FAILED,
)
