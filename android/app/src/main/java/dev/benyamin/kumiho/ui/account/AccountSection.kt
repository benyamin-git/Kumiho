package dev.benyamin.kumiho.ui.account

import android.webkit.WebView
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.LifecycleResumeEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.engine.AccountSummary
import dev.benyamin.kumiho.ui.components.KumihoCard
import dev.benyamin.kumiho.ui.setup.SignInPane
import dev.benyamin.kumiho.ui.theme.Dimens
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors

/** Test tags for the Account section inside Settings. */
object AccountTags {
    const val SECTION = "account_section"
    const val SIGN_OUT = "account_sign_out"
    const val LOADING = "account_loading"
    const val ERROR = "account_error"
    const val DIALOG = "account_sign_out_dialog"
    const val DIALOG_CONFIRM = "account_sign_out_confirm"
    const val DIALOG_CANCEL = "account_sign_out_cancel"
    const val SIGNED_OUT_TITLE = "account_signed_out_title"
    const val NOTICE = "account_notice"

    const val ACCOUNTS = "account_accounts"
    const val ADD_ACCOUNT = "account_add"
    const val ADD_DIALOG = "account_add_dialog"
    const val ADD_BROWSER = "account_add_browser"
    const val ADD_PASSWORD = "account_add_password"
    const val ADD_CANCEL = "account_add_cancel"
    const val ACCOUNT_ROW = "account_row"
    const val ACTIVE = "account_active"
    const val METHOD = "account_method"
    const val REAUTH = "account_reauth"
    const val OVERFLOW = "account_overflow"
    const val BROWSER_WAITING = BrowserWaitingTags.CARD
    const val BROWSER_URL = BrowserWaitingTags.URL
    const val BROWSER_OPEN = BrowserWaitingTags.OPEN
    const val BROWSER_PASTE_TOGGLE = BrowserWaitingTags.PASTE_TOGGLE
    const val BROWSER_PASTE_FIELD = BrowserWaitingTags.PASTE_FIELD
    const val BROWSER_PASTE_ERROR = BrowserWaitingTags.PASTE_ERROR
    const val BROWSER_PASTE_SUBMIT = BrowserWaitingTags.PASTE_SUBMIT
    const val BROWSER_USE_PASSWORD = BrowserWaitingTags.USE_PASSWORD
    const val BROWSER_CANCEL = BrowserWaitingTags.CANCEL
    const val RENAME_DIALOG = "account_rename_dialog"
    const val RENAME_INPUT = "account_rename_input"
    const val RENAME_CONFIRM = "account_rename_confirm"
    const val RENAME_CANCEL = "account_rename_cancel"
    const val REMOVE_DIALOG = "account_remove_dialog"
    const val REMOVE_CONFIRM = "account_remove_confirm"
    const val REMOVE_CANCEL = "account_remove_cancel"
    const val ADD_PASSWORD_CANCEL = "account_add_password_cancel"

    fun accountRow(id: String): String = "${ACCOUNT_ROW}_$id"

    fun active(id: String): String = "${ACTIVE}_$id"

    fun method(id: String): String = "${METHOD}_$id"

    fun reauth(id: String): String = "${REAUTH}_$id"

    fun overflow(id: String): String = "${OVERFLOW}_$id"
}

/** One account section and the callbacks it needs, supplied by the route. */
data class AccountSectionUi(
    val state: AccountUiState = AccountUiState(),
    val actions: AccountActions = AccountActions(),
)

/**
 * Wires the account view model into Settings. It owns one rotation-safe view
 * model per engine and opens each browser URL once; the state machine never
 * touches an Activity. Without an engine the section reports its own load
 * instead of pretending there are no accounts.
 */
@Composable
fun rememberAccountSection(container: AppContainer): AccountSectionUi {
    val coordinator = container.vpnServiceCoordinator
    val engine by coordinator.engine.collectAsStateWithLifecycle()
    val client = engine ?: return remember {
        AccountSectionUi(
            state = AccountUiState(loading = true, message = AccountMessage.LOAD_FAILED),
            actions = AccountActions(),
        )
    }

    val context = LocalContext.current
    val appContext = context.applicationContext
    val gateway = remember(client) { EngineAccountGateway(client) }
    val key = remember(client) { "account-${System.identityHashCode(client)}" }
    val viewModel: AccountViewModel = viewModel(key = key) {
        AccountViewModel(
            gateway = gateway,
            onSignedOut = {
                // A device without a usable WebView package has no cookie jar
                // to clear; never let that block the local sign-out.
                if (WebView.getCurrentWebViewPackage() != null) {
                    try {
                        clearMozillaCookies(appContext)
                    } catch (_: RuntimeException) {
                        // The WebView provider died between the probe and use.
                    }
                }
            },
            openExternalBrowser = { url -> openBrowserSignIn(appContext, url) },
        )
    }
    val state by viewModel.state.collectAsStateWithLifecycle()

    LifecycleResumeEffect(viewModel) {
        viewModel.setScreenResumed(true)
        onPauseOrDispose { viewModel.setScreenResumed(false) }
    }

    val actions = remember(viewModel) {
        AccountActions(
            onRetry = viewModel::retry,
            onDismissMessage = viewModel::dismissMessage,
            onSubmitEmail = viewModel::submitEmail,
            onSubmitPassword = viewModel::submitPassword,
            onSubmitCode = viewModel::submitCode,
            onRecheck = viewModel::recheckEmailLink,
            onSignOut = viewModel::requestSignOut,
            onDismissSignOut = viewModel::dismissSignOut,
            onConfirmSignOut = viewModel::confirmSignOut,
            onAddAccount = viewModel::openAddDialog,
            onDismissAdd = viewModel::dismissAddDialog,
            onAddBrowser = { viewModel.startBrowser() },
            onAddPassword = viewModel::startPassword,
            onUse = viewModel::useAccount,
            onReauth = viewModel::reauth,
            onRemoveRequest = viewModel::requestRemove,
            onRemoveConfirm = viewModel::confirmRemove,
            onDismissRemove = viewModel::dismissRemove,
            onRenameRequest = viewModel::requestRename,
            onRenameConfirm = viewModel::confirmRename,
            onDismissRename = viewModel::dismissRename,
            onOpenBrowser = viewModel::openBrowser,
            onCloseBrowser = viewModel::closeBrowserWebView,
            onBrowserRedirect = viewModel::submitBrowserRedirect,
            onToggleBrowserPaste = viewModel::toggleBrowserPaste,
            onBrowserPasteChange = viewModel::setBrowserPasteValue,
            onBrowserPasteSubmit = viewModel::submitBrowserPaste,
            onUseBrowserPassword = viewModel::startPassword,
            onCancelBrowser = viewModel::cancelBrowser,
            onBrowserUnavailable = viewModel::browserUnavailable,
            onCancelAdd = viewModel::cancelAddAccount,
        )
    }
    return AccountSectionUi(state = state, actions = actions)
}

/**
 * The in-list Account section: a status line, the account list with Add, the
 * sign-in pane when there is nothing stored, and Sign out. Dialogs live at the
 * screen root in [AccountSectionDialogs] so they are not clipped by the list.
 */
@Composable
fun AccountSection(
    state: AccountUiState,
    actions: AccountActions,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .testTag(AccountTags.SECTION),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space2),
    ) {
        AccountStatus(state = state, actions = actions)

        state.browser?.let { browser ->
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

        if (state.addingAccount) {
            Text(
                text = stringResource(R.string.account_add_password_title),
                style = MaterialTheme.typography.titleMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
            SignInPane(
                state = state.signIn,
                onSubmitEmail = actions.onSubmitEmail,
                onSubmitPassword = actions.onSubmitPassword,
                onSubmitCode = actions.onSubmitCode,
                onRecheck = actions.onRecheck,
                onBrowser = null,
            )
            TextButton(
                onClick = actions.onCancelAdd,
                modifier = Modifier.testTag(AccountTags.ADD_PASSWORD_CANCEL),
            ) {
                Text(stringResource(R.string.account_add_cancel))
            }
        } else if (!state.signedIn && !state.hasAccounts && state.browser == null &&
            !state.loading
        ) {
            Column(verticalArrangement = Arrangement.spacedBy(Dimens.Space1)) {
                Text(
                    text = stringResource(R.string.account_signed_out_title),
                    style = MaterialTheme.typography.titleMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                    modifier = Modifier.testTag(AccountTags.SIGNED_OUT_TITLE),
                )
                Text(
                    text = stringResource(R.string.account_signed_out_text),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
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

        if (state.hasAccounts) {
            AccountsSection(state = state, actions = actions)
        }

        if (state.signedIn || state.hasAccounts) {
            Column(verticalArrangement = Arrangement.spacedBy(Dimens.Space1)) {
                OutlinedButton(
                    onClick = actions.onSignOut,
                    enabled = !state.signingOut,
                    colors = ButtonDefaults.outlinedButtonColors(
                        contentColor = LocalKumihoColors.current.loss,
                    ),
                    border = BorderStroke(1.dp, LocalKumihoColors.current.loss.copy(alpha = 0.5f)),
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(min = Dimens.ControlHeight)
                        .testTag(AccountTags.SIGN_OUT),
                ) {
                    Text(
                        if (state.signingOut) {
                            stringResource(R.string.account_signing_out)
                        } else if ((state.accounts?.accounts?.size ?: 0) > 1) {
                            stringResource(R.string.account_sign_out_all)
                        } else {
                            stringResource(R.string.account_sign_out)
                        },
                    )
                }
                Text(
                    text = stringResource(R.string.account_sign_out_note),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

/** The compact status line above the account list. */
@Composable
private fun AccountStatus(state: AccountUiState, actions: AccountActions) {
    val notice = accountNoticeText(state.message)
        ?: state.accounts?.error?.let { stringResource(R.string.account_accounts_failed) }
    val detail = state.error ?: state.accounts?.error ?: state.accounts?.notice
    val informational = notice == null && state.error == null &&
        state.accounts?.error == null && !state.accounts?.notice.isNullOrBlank()

    if (notice != null || detail != null) {
        AccountBanner(
            title = notice.orEmpty(),
            detail = detail,
            informational = informational,
            onRetry = if (state.message in retryable) actions.onRetry else null,
            onDismiss = actions.onDismissMessage,
        )
        return
    }
    if (state.loading && state.account == null && state.accounts == null) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .testTag(AccountTags.LOADING),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            CircularProgressIndicator(
                modifier = Modifier.size(16.dp),
                strokeWidth = 2.dp,
            )
            Text(
                text = stringResource(R.string.account_loading),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun AccountsSection(state: AccountUiState, actions: AccountActions) {
    KumihoCard(modifier = Modifier.fillMaxWidth().testTag(AccountTags.ACCOUNTS)) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = stringResource(R.string.account_accounts_title),
                    style = MaterialTheme.typography.titleMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Text(
                    text = stringResource(R.string.account_accounts_hint),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            TextButton(
                onClick = actions.onAddAccount,
                modifier = Modifier.testTag(AccountTags.ADD_ACCOUNT),
            ) {
                Text(stringResource(R.string.account_add))
            }
        }

        val records = state.accounts?.accounts.orEmpty()
        records.forEachIndexed { index, account ->
            if (index > 0) {
                HorizontalDivider(
                    modifier = Modifier.padding(top = Dimens.Space2),
                    color = MaterialTheme.colorScheme.outlineVariant,
                )
            }
            AccountListRow(
                account = account,
                activeID = state.activeID,
                actions = actions,
            )
        }
    }
}

@Composable
private fun AccountListRow(
    account: AccountSummary,
    activeID: String?,
    actions: AccountActions,
) {
    var menu by remember(account.id) { mutableStateOf(false) }
    val loss = LocalKumihoColors.current.loss
    val label = account.label.ifBlank { account.email }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = Dimens.Space2)
            .testTag(AccountTags.accountRow(account.id)),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Dimens.Space1),
    ) {
        RadioButton(
            selected = account.id == activeID,
            onClick = { actions.onUse(account.id) },
            modifier = Modifier.testTag(AccountTags.active(account.id)),
        )
        Column(
            modifier = Modifier.weight(1f),
            verticalArrangement = Arrangement.spacedBy(Dimens.Space1),
        ) {
            Text(
                text = label,
                style = MaterialTheme.typography.titleSmall,
                color = MaterialTheme.colorScheme.onSurface,
            )
            if (label != account.email) {
                Text(
                    text = account.email,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
            ) {
                Surface(
                    shape = RoundedCornerShape(999.dp),
                    color = MaterialTheme.colorScheme.secondaryContainer,
                ) {
                    Text(
                        text = if (account.method == "browser") {
                            stringResource(R.string.account_method_browser)
                        } else {
                            stringResource(R.string.account_method_password)
                        },
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSecondaryContainer,
                        modifier = Modifier
                            .padding(horizontal = Dimens.Space2, vertical = 2.dp)
                            .testTag(AccountTags.method(account.id)),
                    )
                }
            }
            if (account.reauthRequired) {
                TextButton(
                    onClick = { actions.onReauth(account) },
                    contentPadding = PaddingValues(0.dp),
                    modifier = Modifier.testTag(AccountTags.reauth(account.id)),
                ) {
                    Icon(
                        imageVector = Icons.Filled.Warning,
                        contentDescription = null,
                        tint = LocalKumihoColors.current.warning,
                        modifier = Modifier.size(16.dp),
                    )
                    Text(
                        text = stringResource(R.string.account_reauth_required),
                        style = MaterialTheme.typography.labelMedium,
                    )
                }
            }
        }
        val localizedContext = LocalContext.current
        Box {
            IconButton(
                onClick = { menu = true },
                modifier = Modifier.testTag(AccountTags.overflow(account.id)),
            ) {
                Icon(
                    imageVector = Icons.Filled.MoreVert,
                    contentDescription = stringResource(R.string.account_actions),
                )
            }
            DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                // The menu composes in its own window; re-provide the
                // localized context captured outside it so its labels stay
                // Persian there.
                CompositionLocalProvider(LocalContext provides localizedContext) {
                    if (account.id != activeID) {
                        DropdownMenuItem(
                            text = { Text(stringResource(R.string.account_use)) },
                            onClick = {
                                menu = false
                                actions.onUse(account.id)
                            },
                        )
                    }
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.account_reauth)) },
                        onClick = {
                            menu = false
                            actions.onReauth(account)
                        },
                    )
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.account_rename)) },
                        onClick = {
                            menu = false
                            actions.onRenameRequest(account)
                        },
                    )
                    DropdownMenuItem(
                        text = {
                            Text(
                                text = stringResource(R.string.account_remove),
                                color = loss,
                            )
                        },
                        onClick = {
                            menu = false
                            actions.onRemoveRequest(account)
                        },
                    )
                }
            }
        }
    }
}

/** The Account dialogs, rendered at the screen root by Settings and TV Settings. */
@Composable
fun AccountSectionDialogs(state: AccountUiState, actions: AccountActions) {
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
private fun AddAccountDialog(
    onBrowser: () -> Unit,
    onPassword: () -> Unit,
    onDismiss: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        modifier = Modifier.testTag(AccountTags.ADD_DIALOG),
        title = { Text(stringResource(R.string.account_add_title)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(Dimens.Space3)) {
                Text(
                    text = stringResource(R.string.account_add_text),
                    style = MaterialTheme.typography.bodyMedium,
                )
                Button(
                    onClick = onBrowser,
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(min = Dimens.ControlHeight)
                        .testTag(AccountTags.ADD_BROWSER),
                ) {
                    Text(stringResource(R.string.account_add_browser))
                }
                OutlinedButton(
                    onClick = onPassword,
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(min = Dimens.ControlHeight)
                        .testTag(AccountTags.ADD_PASSWORD),
                ) {
                    Text(stringResource(R.string.account_add_password))
                }
            }
        },
        confirmButton = {
            TextButton(
                onClick = onDismiss,
                modifier = Modifier.testTag(AccountTags.ADD_CANCEL),
            ) {
                Text(stringResource(R.string.account_add_dismiss))
            }
        },
    )
}

@Composable
private fun RenameAccountDialog(
    target: AccountSummary,
    onConfirm: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    var label by rememberSaveable(target.id) { mutableStateOf(target.label) }
    AlertDialog(
        onDismissRequest = onDismiss,
        modifier = Modifier.testTag(AccountTags.RENAME_DIALOG),
        title = { Text(stringResource(R.string.account_rename_title)) },
        text = {
            OutlinedTextField(
                value = label,
                onValueChange = { label = it },
                label = { Text(stringResource(R.string.account_rename_label)) },
                singleLine = true,
                modifier = Modifier
                    .fillMaxWidth()
                    .testTag(AccountTags.RENAME_INPUT),
            )
        },
        confirmButton = {
            TextButton(
                onClick = { onConfirm(label) },
                modifier = Modifier.testTag(AccountTags.RENAME_CONFIRM),
            ) {
                Text(stringResource(R.string.account_rename_confirm))
            }
        },
        dismissButton = {
            TextButton(
                onClick = onDismiss,
                modifier = Modifier.testTag(AccountTags.RENAME_CANCEL),
            ) {
                Text(stringResource(R.string.account_rename_cancel))
            }
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
    val loss = LocalKumihoColors.current.loss
    AlertDialog(
        onDismissRequest = onDismiss,
        modifier = Modifier.testTag(AccountTags.REMOVE_DIALOG),
        title = { Text(stringResource(R.string.account_remove_title)) },
        text = {
            Text(
                if (next != null) {
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
            )
        },
        confirmButton = {
            TextButton(
                onClick = onConfirm,
                colors = ButtonDefaults.textButtonColors(contentColor = loss),
                modifier = Modifier.testTag(AccountTags.REMOVE_CONFIRM),
            ) {
                Text(stringResource(R.string.account_remove_confirm))
            }
        },
        dismissButton = {
            TextButton(
                onClick = onDismiss,
                modifier = Modifier.testTag(AccountTags.REMOVE_CANCEL),
            ) {
                Text(stringResource(R.string.account_remove_cancel))
            }
        },
    )
}

@Composable
private fun SignOutDialog(
    accountCount: Int,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
) {
    val loss = LocalKumihoColors.current.loss
    AlertDialog(
        onDismissRequest = onDismiss,
        modifier = Modifier.testTag(AccountTags.DIALOG),
        title = { Text(stringResource(R.string.account_sign_out_title)) },
        text = {
            Text(
                if (accountCount > 1) {
                    stringResource(R.string.account_sign_out_all_text)
                } else {
                    stringResource(R.string.account_sign_out_text)
                },
            )
        },
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
    informational: Boolean,
    onRetry: (() -> Unit)?,
    onDismiss: () -> Unit,
) {
    val warning = LocalKumihoColors.current.warning
    val accent = if (informational) MaterialTheme.colorScheme.primary else warning
    Surface(
        modifier = Modifier
            .fillMaxWidth()
            .testTag(AccountTags.NOTICE),
        shape = MaterialTheme.shapes.medium,
        color = accent.copy(alpha = 0.12f),
        border = BorderStroke(1.dp, accent.copy(alpha = 0.5f)),
    ) {
        Row(
            modifier = Modifier.padding(Dimens.Space3),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            Icon(
                imageVector = if (informational) Icons.Filled.Info else Icons.Filled.Warning,
                contentDescription = if (informational) {
                    stringResource(R.string.account_notice_information)
                } else {
                    null
                },
                tint = accent,
                modifier = Modifier.size(20.dp),
            )
            Column(modifier = Modifier.weight(1f)) {
                if (title.isNotBlank()) {
                    Text(
                        text = title,
                        style = MaterialTheme.typography.titleSmall,
                        color = accent,
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
internal fun accountNoticeText(message: AccountMessage?): String? = when (message) {
    null -> null
    AccountMessage.TIMEOUT -> stringResource(R.string.account_timeout)
    AccountMessage.LOAD_FAILED -> stringResource(R.string.account_load_failed)
    AccountMessage.REFRESH_FAILED -> stringResource(R.string.account_refresh_failed)
    AccountMessage.SIGN_OUT_FAILED -> stringResource(R.string.account_sign_out_failed)
    AccountMessage.ACCOUNTS_FAILED -> stringResource(R.string.account_accounts_failed)
    AccountMessage.ACTION_FAILED -> stringResource(R.string.account_action_failed)
    AccountMessage.NO_BROWSER -> stringResource(R.string.account_no_browser)
}

private val retryable = setOf(
    AccountMessage.TIMEOUT,
    AccountMessage.LOAD_FAILED,
    AccountMessage.REFRESH_FAILED,
    AccountMessage.ACCOUNTS_FAILED,
)

/** The callbacks the stateless Account section needs. */
data class AccountActions(
    val onRetry: () -> Unit = {},
    val onDismissMessage: () -> Unit = {},
    val onSubmitEmail: (String) -> Unit = {},
    val onSubmitPassword: (String) -> Unit = {},
    val onSubmitCode: (String) -> Unit = {},
    val onRecheck: () -> Unit = {},
    val onSignOut: () -> Unit = {},
    val onDismissSignOut: () -> Unit = {},
    val onConfirmSignOut: () -> Unit = {},
    val onAddAccount: () -> Unit = {},
    val onDismissAdd: () -> Unit = {},
    val onAddBrowser: () -> Unit = {},
    val onAddPassword: () -> Unit = {},
    val onUse: (String) -> Unit = {},
    val onReauth: (AccountSummary) -> Unit = {},
    val onRemoveRequest: (AccountSummary) -> Unit = {},
    val onRemoveConfirm: () -> Unit = {},
    val onDismissRemove: () -> Unit = {},
    val onRenameRequest: (AccountSummary) -> Unit = {},
    val onRenameConfirm: (String) -> Unit = {},
    val onDismissRename: () -> Unit = {},
    val onOpenBrowser: () -> Unit = {},
    val onCloseBrowser: () -> Unit = {},
    val onBrowserRedirect: (String) -> Unit = {},
    val onToggleBrowserPaste: () -> Unit = {},
    val onBrowserPasteChange: (String) -> Unit = {},
    val onBrowserPasteSubmit: () -> Unit = {},
    val onUseBrowserPassword: () -> Unit = {},
    val onCancelBrowser: () -> Unit = {},
    val onBrowserUnavailable: () -> Unit = {},
    val onCancelAdd: () -> Unit = {},
)
