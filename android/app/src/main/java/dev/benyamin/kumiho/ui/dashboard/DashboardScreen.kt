package dev.benyamin.kumiho.ui.dashboard

import android.content.ActivityNotFoundException
import android.content.Context
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Clear
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
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
import dev.benyamin.kumiho.permissions.PermissionController
import dev.benyamin.kumiho.ui.components.KumihoLoadingState
import dev.benyamin.kumiho.ui.theme.Dimens
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors

/**
 * The dashboard entry point. It waits for the app-scoped coordinator to bind the
 * service and start the engine, then owns one rotation-safe
 * [DashboardViewModel] keyed by that engine. The route is the only place that
 * touches Android: the VPN consent launcher, the settings intent and the
 * permission reads. The view model and [DashboardScreen] stay pure.
 */
@Composable
fun DashboardRoute(
    container: AppContainer,
    modifier: Modifier = Modifier,
    content: @Composable (DashboardState, DashboardActions) -> Unit = { state, actions ->
        DashboardScreen(state = state, actions = actions, modifier = modifier)
    },
) {
    val coordinator = container.vpnServiceCoordinator
    val serviceState by coordinator.state.collectAsStateWithLifecycle()
    val engine by coordinator.engine.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val client = engine

    if (client == null) {
        DashboardWaitingSurface(error = serviceState.error, modifier = modifier)
        return
    }

    val permissions = remember(context) { AndroidDashboardPermissions(context) }
    val gateway = remember(coordinator) { CoordinatorDashboardGateway(coordinator) }
    val key = remember(client) { "dashboard-${System.identityHashCode(client)}" }
    val dashboardViewModel: DashboardViewModel = viewModel(key = key) {
        DashboardViewModel(gateway = gateway, permissions = permissions)
    }
    val state by dashboardViewModel.state.collectAsStateWithLifecycle()

    val vpnLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.StartActivityForResult(),
    ) {
        // The result code is not the permission: re-read VpnService.prepare.
        dashboardViewModel.onVpnConsentResult(permissions.vpnConsentGranted())
    }

    // Resuming re-reads the engine status and Android's Always-on/lockdown flags.
    LifecycleResumeEffect(dashboardViewModel) {
        dashboardViewModel.onResume()
        onPauseOrDispose { }
    }

    LaunchedEffect(dashboardViewModel, context) {
        dashboardViewModel.events.collect { event ->
            when (event) {
                DashboardEvent.RequestVpnPermission -> {
                    val intent = PermissionController.vpnPrepareIntent(context)
                    if (intent == null) {
                        dashboardViewModel.onVpnConsentResult(true)
                    } else {
                        try {
                            vpnLauncher.launch(intent)
                        } catch (_: ActivityNotFoundException) {
                            dashboardViewModel.onVpnConsentResult(false)
                        }
                    }
                }
                DashboardEvent.OpenVpnSettings -> {
                    try {
                        context.startActivity(PermissionController.vpnSettingsIntent())
                    } catch (_: ActivityNotFoundException) {
                        dashboardViewModel.onSettingsUnavailable()
                    }
                }
            }
        }
    }

    val actions = DashboardActions(
        onPower = dashboardViewModel::onPower,
        onOpenOptions = dashboardViewModel::openOptions,
        onCloseOptions = dashboardViewModel::closeOptions,
        onSelectRecommended = dashboardViewModel::selectRecommended,
        onSelectSaved = dashboardViewModel::selectSaved,
        onToggleCountry = dashboardViewModel::toggleCountry,
        onSelectCity = dashboardViewModel::selectCity,
        onToggleProxyOnly = dashboardViewModel::setProxyOnly,
        onConnectFromOptions = dashboardViewModel::connectFromOptions,
        onReloadLocations = dashboardViewModel::loadLocations,
        onOpenVpnSettings = dashboardViewModel::openVpnSettings,
        onConfirmLockdownDisconnect = dashboardViewModel::confirmLockdownDisconnect,
        onCancelLockdown = dashboardViewModel::cancelLockdownPrompt,
        onDismissNotice = dashboardViewModel::dismissNotice,
        onRetryNotice = dashboardViewModel::retryNotice,
    )
    content(state, actions)
}

/** Every callback the stateless dashboard needs; grouped to stay readable. */
data class DashboardActions(
    val onPower: () -> Unit,
    val onOpenOptions: () -> Unit,
    val onCloseOptions: () -> Unit,
    val onSelectRecommended: () -> Unit,
    val onSelectSaved: () -> Unit,
    val onToggleCountry: (String) -> Unit,
    val onSelectCity: (String, String) -> Unit,
    val onToggleProxyOnly: (Boolean) -> Unit,
    val onConnectFromOptions: () -> Unit,
    val onReloadLocations: () -> Unit,
    val onOpenVpnSettings: () -> Unit,
    val onConfirmLockdownDisconnect: () -> Unit,
    val onCancelLockdown: () -> Unit,
    val onDismissNotice: () -> Unit,
    val onRetryNotice: () -> Unit,
)

/**
 * The stateless dashboard: hero, notice, cards and details, plus the options
 * sheet and the lockdown explanation when they are open. It makes no engine,
 * service or Android call, so a Compose test renders it from fake state.
 */
@Composable
fun DashboardScreen(
    state: DashboardState,
    actions: DashboardActions,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space4)
            .testTag(DashboardTags.SCREEN),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space4),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        PowerHero(
            state = state,
            onPower = actions.onPower,
            onOpenOptions = actions.onOpenOptions,
        )
        state.notice?.let { notice ->
            NoticeRow(
                notice = notice,
                onAction = actions.onRetryNotice,
                onDismiss = actions.onDismissNotice,
            )
        }
        SummaryCards(state = state, onOpenOptions = actions.onOpenOptions)
        StatusDetails(state = state, onOpenVpnSettings = actions.onOpenVpnSettings)
    }

    if (state.options.open) {
        ConnectOptionsSheet(
            state = state,
            onClose = actions.onCloseOptions,
            onSelectRecommended = actions.onSelectRecommended,
            onSelectSaved = actions.onSelectSaved,
            onToggleCountry = actions.onToggleCountry,
            onSelectCity = actions.onSelectCity,
            onReloadLocations = actions.onReloadLocations,
            onToggleProxyOnly = actions.onToggleProxyOnly,
            onOpenVpnSettings = actions.onOpenVpnSettings,
            onConnect = actions.onConnectFromOptions,
        )
    }

    if (state.lockdownPrompt) {
        LockdownDialog(
            onConfirm = actions.onConfirmLockdownDisconnect,
            onCancel = actions.onCancelLockdown,
            onOpenVpnSettings = actions.onOpenVpnSettings,
        )
    }
}

/** The latest notice with its icon, words and the one retry it can offer. */
@Composable
private fun NoticeRow(
    notice: DashboardNotice,
    onAction: () -> Unit,
    onDismiss: () -> Unit,
) {
    val colors = LocalKumihoColors.current
    val accent = when (notice.tone) {
        DashboardNoticeTone.INFO -> MaterialTheme.colorScheme.primary
        DashboardNoticeTone.WARNING -> colors.warning
        DashboardNoticeTone.ERROR -> colors.loss
    }
    val glyph: ImageVector = when (notice.tone) {
        DashboardNoticeTone.INFO -> Icons.Filled.Info
        DashboardNoticeTone.WARNING -> Icons.Filled.Warning
        DashboardNoticeTone.ERROR -> Icons.Filled.Clear
    }
    val detail = (notice.text as? NoticeText.Local)?.detail
    Surface(
        modifier = Modifier
            .fillMaxWidth()
            .testTag(DashboardTags.NOTICE),
        shape = MaterialTheme.shapes.medium,
        color = accent.copy(alpha = 0.12f),
        border = BorderStroke(1.dp, accent.copy(alpha = 0.5f)),
    ) {
        Column(modifier = Modifier.padding(Dimens.Space3)) {
            Row(
                horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
                verticalAlignment = Alignment.Top,
            ) {
                Icon(
                    imageVector = glyph,
                    contentDescription = null,
                    tint = accent,
                    modifier = Modifier
                        .padding(top = 2.dp)
                        .size(20.dp),
                )
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = noticeText(notice.text),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    if (!detail.isNullOrBlank()) {
                        Text(
                            text = detail,
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.End,
            ) {
                if (notice.action != DashboardNoticeAction.NONE) {
                    TextButton(
                        onClick = onAction,
                        modifier = Modifier
                            .heightIn(min = Dimens.TouchTarget)
                            .testTag(DashboardTags.NOTICE_ACTION),
                    ) {
                        Text(noticeActionLabel(notice.action))
                    }
                }
                TextButton(
                    onClick = onDismiss,
                    modifier = Modifier
                        .heightIn(min = Dimens.TouchTarget)
                        .testTag(DashboardTags.NOTICE_DISMISS),
                ) {
                    Text(stringResource(R.string.dashboard_notice_dismiss))
                }
            }
        }
    }
}

@Composable
internal fun noticeText(text: NoticeText): String = when (text) {
    is NoticeText.Raw -> text.text
    is NoticeText.Local -> stringResource(messageResource(text.message))
}

@Composable
internal fun noticeActionLabel(action: DashboardNoticeAction): String = stringResource(
    when (action) {
        DashboardNoticeAction.RETRY_REFRESH,
        DashboardNoticeAction.RETRY_CONNECT,
        -> R.string.dashboard_notice_retry
        DashboardNoticeAction.GRANT_PERMISSION -> R.string.dashboard_notice_grant
        DashboardNoticeAction.RELOAD_LOCATIONS -> R.string.dashboard_notice_reload
        DashboardNoticeAction.NONE -> R.string.dashboard_notice_retry
    },
)

private fun messageResource(message: DashboardMessage): Int = when (message) {
    DashboardMessage.SIGN_IN_REQUIRED -> R.string.dashboard_message_sign_in
    DashboardMessage.SERVICE_UNAVAILABLE -> R.string.dashboard_message_unavailable
    DashboardMessage.VPN_PERMISSION_DENIED -> R.string.dashboard_message_permission_denied
    DashboardMessage.STATUS_REFRESH_FAILED -> R.string.dashboard_message_refresh_failed
    DashboardMessage.CONNECT_FAILED -> R.string.dashboard_message_connect_failed
    DashboardMessage.DISCONNECT_FAILED -> R.string.dashboard_message_disconnect_failed
    DashboardMessage.LOCATIONS_FAILED -> R.string.dashboard_message_locations_failed
    DashboardMessage.SETTINGS_UNAVAILABLE -> R.string.dashboard_message_settings_unavailable
    DashboardMessage.ENGINE_NOTICE -> R.string.dashboard_message_engine_notice
}

/**
 * The lockdown explanation. Disconnecting under lockdown is allowed, but
 * Android blocks all traffic and may restart the tunnel on its own; the dialog
 * says both plainly and links to the OS settings that own the policy.
 */
@Composable
private fun LockdownDialog(
    onConfirm: () -> Unit,
    onCancel: () -> Unit,
    onOpenVpnSettings: () -> Unit,
) {
    val loss = LocalKumihoColors.current.loss
    AlertDialog(
        onDismissRequest = onCancel,
        title = {
            Text(
                text = stringResource(R.string.dashboard_lockdown_title),
                modifier = Modifier.testTag(DashboardTags.LOCKDOWN_DIALOG),
            )
        },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(Dimens.Space2)) {
                Text(stringResource(R.string.dashboard_lockdown_text))
                TextButton(
                    onClick = onOpenVpnSettings,
                    modifier = Modifier
                        .heightIn(min = Dimens.TouchTarget)
                        .testTag(DashboardTags.LOCKDOWN_VPN_SETTINGS),
                ) {
                    Text(stringResource(R.string.dashboard_lockdown_open_settings))
                }
            }
        },
        confirmButton = {
            TextButton(
                onClick = onConfirm,
                modifier = Modifier
                    .heightIn(min = Dimens.TouchTarget)
                    .testTag(DashboardTags.LOCKDOWN_CONFIRM),
            ) {
                Text(
                    text = stringResource(R.string.dashboard_lockdown_confirm),
                    color = loss,
                )
            }
        },
        dismissButton = {
            TextButton(
                onClick = onCancel,
                modifier = Modifier
                    .heightIn(min = Dimens.TouchTarget)
                    .testTag(DashboardTags.LOCKDOWN_CANCEL),
            ) {
                Text(stringResource(R.string.dashboard_lockdown_cancel))
            }
        },
    )
}

/** Themed loading while the service binds, or an honest failure if it cannot. */
@Composable
private fun DashboardWaitingSurface(error: String?, modifier: Modifier = Modifier) {
    Surface(
        modifier = modifier.fillMaxSize(),
        color = MaterialTheme.colorScheme.background,
    ) {
        Box(contentAlignment = Alignment.Center, modifier = Modifier.fillMaxSize()) {
            if (error == null) {
                KumihoLoadingState(
                    label = stringResource(R.string.dashboard_loading),
                    modifier = Modifier.testTag(DashboardTags.LOADING),
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
                        text = stringResource(R.string.dashboard_engine_unavailable_title),
                        style = MaterialTheme.typography.titleLarge,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    Text(
                        text = stringResource(R.string.dashboard_engine_unavailable),
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

/**
 * The production permission fact: VPN consent is re-derived from
 * `VpnService.prepare` every time, never from an ActivityResult code.
 */
class AndroidDashboardPermissions(context: Context) : DashboardPermissions {
    private val appContext = context.applicationContext

    override fun vpnConsentGranted(): Boolean =
        PermissionController.vpnPrepareIntent(appContext) == null
}
