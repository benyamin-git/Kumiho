package dev.benyamin.kumiho.ui.tv.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
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
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
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
import dev.benyamin.kumiho.ui.setup.SignInPane
import dev.benyamin.kumiho.ui.theme.KumihoShapes
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors
import dev.benyamin.kumiho.ui.tv.components.TvButton
import dev.benyamin.kumiho.ui.tv.components.TvDialog
import dev.benyamin.kumiho.ui.tv.components.TvFocusableSurface
import dev.benyamin.kumiho.ui.tv.theme.TvDimens
import dev.benyamin.kumiho.ui.tv.theme.TvM3Typography

/**
 * The Account group inside TV Settings: the same account list, sign-in and
 * sign-out the phone section offers, at the ten-foot type scale with remote
 * focus. Dialogs ([TvAccountDialogs]) are rendered by the settings screen root
 * so the scrolling list never clips them.
 */
@Composable
fun TvAccountSection(
    state: AccountUiState,
    actions: AccountActions,
    modifier: Modifier = Modifier,
) {
    var manageTarget by remember { mutableStateOf<AccountSummary?>(null) }
    val notice = accountNoticeText(state.message)
        ?: state.accounts?.error?.let { stringResource(R.string.account_accounts_failed) }
    val detail = state.error ?: state.accounts?.error ?: state.accounts?.notice
    val colors = MaterialTheme.colorScheme

    Column(
        modifier = modifier
            .fillMaxWidth()
            .testTag(AccountTags.SECTION),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space2),
    ) {
        if (notice != null || detail != null) {
            TvAccountNotice(
                notice = notice,
                detail = detail,
                onRetry = if (state.message in retryable) actions.onRetry else null,
                onDismiss = actions.onDismissMessage,
            )
        } else if (state.loading && state.account == null && state.accounts == null) {
            Text(
                text = stringResource(R.string.account_loading),
                style = MaterialTheme.typography.bodyMedium,
                color = colors.onSurfaceVariant,
                modifier = Modifier.testTag(AccountTags.LOADING),
            )
        }

        state.browser?.let { browser ->
            TvSectionCard {
                Text(
                    text = stringResource(R.string.account_browser_title),
                    style = MaterialTheme.typography.titleMedium,
                    color = colors.onSurface,
                    modifier = Modifier.testTag(AccountTags.BROWSER_WAITING),
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
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(AccountTags.BROWSER_CANCEL),
                )
            }
        }

        if (state.addingAccount) {
            TvSectionCard {
                Text(
                    text = stringResource(R.string.account_add_password_title),
                    style = MaterialTheme.typography.titleMedium,
                    color = colors.onSurface,
                )
                PhoneAtTvScale {
                    SignInPane(
                        state = state.signIn,
                        onSubmitEmail = actions.onSubmitEmail,
                        onSubmitPassword = actions.onSubmitPassword,
                        onSubmitCode = actions.onSubmitCode,
                        onRecheck = actions.onRecheck,
                        onBrowser = null,
                        tvFocus = true,
                    )
                }
                TvButton(
                    label = stringResource(R.string.account_add_cancel),
                    onClick = actions.onCancelAdd,
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(AccountTags.ADD_PASSWORD_CANCEL),
                )
            }
        } else if (!state.signedIn && !state.hasAccounts && state.browser == null &&
            !state.loading
        ) {
            TvSectionCard {
                Text(
                    text = stringResource(R.string.account_signed_out_title),
                    style = MaterialTheme.typography.titleMedium,
                    color = colors.onSurface,
                    modifier = Modifier.testTag(AccountTags.SIGNED_OUT_TITLE),
                )
                Text(
                    text = stringResource(R.string.account_signed_out_text),
                    style = MaterialTheme.typography.bodyMedium,
                    color = colors.onSurfaceVariant,
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
                        tvFocus = true,
                    )
                }
            }
        }

        if (state.hasAccounts) {
            TvSectionCard {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.SpaceBetween,
                ) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            text = stringResource(R.string.account_accounts_title),
                            style = MaterialTheme.typography.titleMedium,
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
                state.accounts?.accounts.orEmpty().forEach { account ->
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
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
                                    val label = account.label.trim()
                                    val sameAsEmail = label.equals(account.email, ignoreCase = true)
                                    if (label.isNotEmpty() && !sameAsEmail) {
                                        Text(
                                            text = label,
                                            style = MaterialTheme.typography.titleSmall,
                                            color = colors.onSurface,
                                        )
                                    }
                                    Text(
                                        text = account.email,
                                        style = if (label.isEmpty() || sameAsEmail) {
                                            MaterialTheme.typography.titleSmall
                                        } else {
                                            MaterialTheme.typography.bodySmall
                                        },
                                        color = if (label.isEmpty() || sameAsEmail) {
                                            colors.onSurface
                                        } else {
                                            colors.onSurfaceVariant
                                        },
                                        maxLines = 1,
                                        overflow = TextOverflow.Ellipsis,
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
                                        modifier = Modifier.testTag(AccountTags.method(account.id)),
                                    )
                                }
                                if (account.active) {
                                    Text(
                                        text = "●",
                                        style = MaterialTheme.typography.titleSmall,
                                        color = colors.primary,
                                        modifier = Modifier.testTag(AccountTags.active(account.id)),
                                    )
                                }
                            }
                        }
                        TvButton(
                            label = stringResource(R.string.tv_account_manage),
                            onClick = { manageTarget = account },
                            modifier = Modifier.testTag(AccountTags.overflow(account.id)),
                        )
                    }
                }
            }
        }

        if (state.signedIn || state.hasAccounts) {
            Column(verticalArrangement = Arrangement.spacedBy(TvDimens.Space1)) {
                TvButton(
                    label = if (state.signingOut) {
                        stringResource(R.string.account_signing_out)
                    } else if ((state.accounts?.accounts?.size ?: 0) > 1) {
                        stringResource(R.string.account_sign_out_all)
                    } else {
                        stringResource(R.string.account_sign_out)
                    },
                    onClick = actions.onSignOut,
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(AccountTags.SIGN_OUT),
                    enabled = !state.signingOut,
                )
                Text(
                    text = stringResource(R.string.account_sign_out_note),
                    style = MaterialTheme.typography.bodySmall,
                    color = colors.onSurfaceVariant,
                )
            }
        }
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
}

/** Every Account dialog at the TV scale, rendered by the settings screen root. */
@Composable
fun TvAccountDialogs(state: AccountUiState, actions: AccountActions) {
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

@Composable
private fun TvSectionCard(content: @Composable androidx.compose.foundation.layout.ColumnScope.() -> Unit) {
    val colors = MaterialTheme.colorScheme
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(colors.surface)
            .border(1.dp, colors.borderVariant, MaterialTheme.shapes.medium)
            .padding(TvDimens.Space2),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
        content = content,
    )
}

@Composable
private fun TvAccountNotice(
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
        notice?.takeIf { it.isNotBlank() }?.let {
            Text(text = it, style = MaterialTheme.typography.bodyMedium, color = colors.onSurface)
        }
        detail?.takeIf { it.isNotBlank() }?.let {
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
                    stringResource(
                        R.string.account_remove_active_text,
                        target.label.ifBlank { target.email },
                        next.label.ifBlank { next.email },
                    )
                } else {
                    stringResource(
                        R.string.account_remove_text,
                        target.label.ifBlank { target.email },
                    )
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
                    label = {
                        androidx.compose.material3.Text(
                            stringResource(R.string.account_rename_label),
                        )
                    },
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
        else -> R.string.account_method_browser
    },
)

private val retryable = setOf(
    AccountMessage.TIMEOUT,
    AccountMessage.LOAD_FAILED,
    AccountMessage.REFRESH_FAILED,
    AccountMessage.ACCOUNTS_FAILED,
)
