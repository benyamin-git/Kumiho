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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
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
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
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
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.LifecycleResumeEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.engine.Account
import dev.benyamin.kumiho.engine.AccountSummary
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

    const val ACCOUNTS = "account_accounts"
    const val ADD_ACCOUNT = "account_add"
    const val ADD_DIALOG = "account_add_dialog"
    const val ADD_BROWSER = "account_add_browser"
    const val ADD_PASSWORD = "account_add_password"
    const val ADD_CANCEL = "account_add_cancel"
    const val ACCOUNT_ROW = "account_row"
    const val ACTIVE = "account_active"
    const val METHOD = "account_method"
    const val ROW_QUOTA = "account_row_quota"
    const val REAUTH = "account_reauth"
    const val OVERFLOW = "account_overflow"
    const val ROTATION = "account_rotation"
    const val THRESHOLD = "account_threshold"
    const val THRESHOLD_DOWN = "account_threshold_down"
    const val THRESHOLD_UP = "account_threshold_up"
    const val BROWSER_WAITING = "account_browser_waiting"
    const val BROWSER_OPEN = "account_browser_open"
    const val BROWSER_CANCEL = "account_browser_cancel"
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

    fun rowQuota(id: String): String = "${ROW_QUOTA}_$id"

    fun quota(id: String): String = rowQuota(id)

    fun reauth(id: String): String = "${REAUTH}_$id"

    fun overflow(id: String): String = "${OVERFLOW}_$id"
}

/**
 * The Account entry point. It follows the Settings engine gate and owns one
 * rotation-safe view model per engine. The route opens each browser URL once;
 * the state machine never touches an Activity.
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

    val context = LocalContext.current
    val gateway = remember(client) { EngineAccountGateway(client) }
    val key = remember(client) { "account-${System.identityHashCode(client)}" }
    val viewModel: AccountViewModel = viewModel(key = key) {
        AccountViewModel(gateway)
    }
    val state by viewModel.state.collectAsStateWithLifecycle()
    var openedURL by rememberSaveable { mutableStateOf<String?>(null) }

    LifecycleResumeEffect(viewModel) {
        viewModel.setScreenResumed(true)
        onPauseOrDispose { viewModel.setScreenResumed(false) }
    }

    LaunchedEffect(state.browserLaunchURL) {
        val url = state.browserLaunchURL ?: return@LaunchedEffect
        if (openedURL == url) return@LaunchedEffect
        openedURL = url
        if (!openBrowserSignIn(context, url)) viewModel.browserUnavailable()
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
            onRotation = viewModel::setRotation,
            onThreshold = viewModel::setThreshold,
            onOpenBrowser = {
                val url = viewModel.state.value.browser?.url
                if (url == null || !openBrowserSignIn(context, url)) {
                    viewModel.browserUnavailable()
                }
            },
            onCancelBrowser = viewModel::cancelBrowser,
            onBrowserUnavailable = viewModel::browserUnavailable,
            onCancelAdd = viewModel::cancelAddAccount,
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
    val notice = accountNoticeText(state.message)
        ?: state.accounts?.error?.let { stringResource(R.string.account_accounts_failed) }
    val detail = state.error ?: state.accounts?.error ?: state.accounts?.notice

    Column(modifier = modifier.fillMaxSize().testTag(AccountTags.SCREEN)) {
        if (notice != null || detail != null) {
            AccountBanner(
                title = notice.orEmpty(),
                detail = detail,
                onRetry = if (state.message == AccountMessage.TIMEOUT ||
                    state.message == AccountMessage.LOAD_FAILED ||
                    state.message == AccountMessage.REFRESH_FAILED ||
                    state.message == AccountMessage.ACCOUNTS_FAILED
                ) {
                    actions.onRetry
                } else {
                    null
                },
                onDismiss = actions.onDismissMessage,
            )
        }

        when {
            state.loading && state.account == null && state.accounts == null ->
                KumihoLoadingState(
                    label = stringResource(R.string.account_loading),
                    modifier = Modifier.testTag(AccountTags.LOADING),
                )
            state.signedIn || state.hasAccounts || state.browser != null || state.addingAccount ->
                AccountsBody(state = state, actions = actions)
            else -> SignInSection(state = state, actions = actions)
        }
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
}

@Composable
private fun AccountsBody(state: AccountUiState, actions: AccountActions) {
    val loss = LocalKumihoColors.current.loss
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(Dimens.ScreenPadding),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space3),
    ) {
        state.browser?.let { browser ->
            item(key = "browser") {
                BrowserWaitingCard(browser = browser, actions = actions)
            }
        }

        if (state.addingAccount) {
            item(key = "add-title") {
                Text(
                    text = stringResource(R.string.account_add_password_title),
                    style = MaterialTheme.typography.titleLarge,
                    color = MaterialTheme.colorScheme.onSurface,
                )
            }
            item(key = "add-pane") {
                SignInPane(
                    state = state.signIn,
                    onSubmitEmail = actions.onSubmitEmail,
                    onSubmitPassword = actions.onSubmitPassword,
                    onSubmitCode = actions.onSubmitCode,
                    onRecheck = actions.onRecheck,
                    onBrowser = null,
                )
            }
            item(key = "add-cancel") {
                TextButton(
                    onClick = actions.onCancelAdd,
                    modifier = Modifier.testTag(AccountTags.ADD_PASSWORD_CANCEL),
                ) {
                    Text(stringResource(R.string.account_add_cancel))
                }
            }
        } else if (!state.signedIn && !state.hasAccounts && state.browser == null) {
            item(key = "signed-out-title") {
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
                    onBrowser = actions.onAddBrowser,
                    signedInNote = stringResource(R.string.account_signed_in_note),
                )
            }
        }

        if (state.hasAccounts) {
            item(key = "accounts") {
                AccountsSection(state = state, actions = actions)
            }
        }

        if (state.signedIn && state.account != null) {
            item(key = "card") {
                AccountCard(account = state.account)
            }
        }

        if (state.accounts != null) {
            item(key = "rotation") {
                RotationCard(state = state, actions = actions)
            }
        }

        if (state.signedIn || state.hasAccounts) {
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
                            } else if ((state.accounts?.accounts?.size ?: 0) > 1) {
                                stringResource(R.string.account_sign_out_all)
                            } else {
                                stringResource(R.string.account_sign_out)
                            },
                        )
                    }
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
                Text(
                    text = AccountFormat.quotaPercent(account.quota)?.let { percent ->
                        stringResource(R.string.account_row_quota_left, percent)
                    } ?: stringResource(R.string.account_row_quota_unknown),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.testTag(AccountTags.rowQuota(account.id)),
                )
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

@Composable
private fun BrowserWaitingCard(browser: BrowserSignIn, actions: AccountActions) {
    KumihoCard(modifier = Modifier.fillMaxWidth().testTag(AccountTags.BROWSER_WAITING)) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            if (browser.error == null) {
                CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp)
            } else {
                Icon(
                    imageVector = Icons.Filled.Warning,
                    contentDescription = null,
                    tint = LocalKumihoColors.current.warning,
                    modifier = Modifier.size(20.dp),
                )
            }
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = stringResource(R.string.account_browser_title),
                    style = MaterialTheme.typography.titleMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Text(
                    text = browser.error ?: stringResource(R.string.account_browser_hint),
                    style = MaterialTheme.typography.bodySmall,
                    color = if (browser.error == null) {
                        MaterialTheme.colorScheme.onSurfaceVariant
                    } else {
                        LocalKumihoColors.current.warning
                    },
                )
            }
        }
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            Button(
                onClick = actions.onOpenBrowser,
                modifier = Modifier
                    .weight(1f)
                    .heightIn(min = Dimens.ControlHeight)
                    .testTag(AccountTags.BROWSER_OPEN),
            ) {
                Text(stringResource(R.string.account_browser_open))
            }
            TextButton(
                onClick = actions.onCancelBrowser,
                modifier = Modifier
                    .heightIn(min = Dimens.ControlHeight)
                    .testTag(AccountTags.BROWSER_CANCEL),
            ) {
                Text(stringResource(R.string.account_browser_cancel))
            }
        }
    }
}

@Composable
private fun RotationCard(state: AccountUiState, actions: AccountActions) {
    val accounts = state.accounts ?: return
    KumihoCard(modifier = Modifier.fillMaxWidth()) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = stringResource(R.string.account_rotation_title),
                    style = MaterialTheme.typography.titleMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Text(
                    text = stringResource(R.string.account_rotation_hint),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Switch(
                checked = accounts.rotation,
                onCheckedChange = actions.onRotation,
                enabled = !state.rotationSaving,
                modifier = Modifier.testTag(AccountTags.ROTATION),
            )
        }
        Row(
            modifier = Modifier.fillMaxWidth().padding(top = Dimens.Space2),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = stringResource(R.string.account_threshold_title),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Text(
                    text = stringResource(R.string.account_threshold_hint),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            OutlinedButton(
                onClick = { actions.onThreshold(accounts.threshold - 1) },
                enabled = accounts.rotation && !state.rotationSaving && accounts.threshold > 1,
                modifier = Modifier.testTag(AccountTags.THRESHOLD_DOWN),
            ) {
                Text(stringResource(R.string.account_threshold_decrease_symbol))
            }
            Text(
                text = stringResource(R.string.account_threshold_value, accounts.threshold),
                style = MaterialTheme.typography.titleMedium,
                color = MaterialTheme.colorScheme.onSurface,
                modifier = Modifier
                    .width(56.dp)
                    .testTag(AccountTags.THRESHOLD),
                textAlign = TextAlign.Center,
            )
            OutlinedButton(
                onClick = { actions.onThreshold(accounts.threshold + 1) },
                enabled = accounts.rotation && !state.rotationSaving && accounts.threshold < 50,
                modifier = Modifier.testTag(AccountTags.THRESHOLD_UP),
            ) {
                Text(stringResource(R.string.account_threshold_increase_symbol))
            }
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
                onBrowser = actions.onAddBrowser,
                signedInNote = stringResource(R.string.account_signed_in_note),
            )
        }
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
    AccountMessage.ACCOUNTS_FAILED -> stringResource(R.string.account_accounts_failed)
    AccountMessage.ACTION_FAILED -> stringResource(R.string.account_action_failed)
    AccountMessage.ROTATION_FAILED -> stringResource(R.string.account_rotation_failed)
    AccountMessage.NO_BROWSER -> stringResource(R.string.account_no_browser)
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
    val onRotation: (Boolean) -> Unit = {},
    val onThreshold: (Int) -> Unit = {},
    val onOpenBrowser: () -> Unit = {},
    val onCancelBrowser: () -> Unit = {},
    val onBrowserUnavailable: () -> Unit = {},
    val onCancelAdd: () -> Unit = {},
)
