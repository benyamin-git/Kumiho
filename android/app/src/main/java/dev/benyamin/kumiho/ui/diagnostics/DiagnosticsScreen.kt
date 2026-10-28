package dev.benyamin.kumiho.ui.diagnostics

import android.content.Intent
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
import androidx.compose.foundation.lazy.items
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
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.diagnostics.AndroidFacts
import dev.benyamin.kumiho.engine.DoctorCheck
import dev.benyamin.kumiho.engine.DoctorReport
import dev.benyamin.kumiho.engine.DoctorStatus
import dev.benyamin.kumiho.permissions.PermissionController
import dev.benyamin.kumiho.ui.components.KumihoCard
import dev.benyamin.kumiho.ui.components.KumihoErrorState
import dev.benyamin.kumiho.ui.components.KumihoLoadingState
import dev.benyamin.kumiho.ui.components.KumihoSectionHeader
import dev.benyamin.kumiho.ui.components.KumihoStatusChip
import dev.benyamin.kumiho.ui.components.StatusTone
import dev.benyamin.kumiho.ui.theme.Dimens
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors
import dev.benyamin.kumiho.vpn.SplitMode

/** Test tags for the diagnostics screen. */
object DiagnosticsTags {
    const val SCREEN = "diagnostics_screen"
    const val LIST = "diagnostics_list"
    const val LOADING = "diagnostics_loading"
    const val ERROR = "diagnostics_error"
    const val HEADER = "diagnostics_header"
    const val SERVICE_CARD = "diagnostics_service_card"
    const val SPLIT_CARD = "diagnostics_split_card"
    const val CHECKS = "diagnostics_checks"
    const val MESSAGE = "diagnostics_message"
    const val MESSAGE_DISMISS = "diagnostics_message_dismiss"
    const val RERUN = "diagnostics_rerun"
    const val COPY_ALL = "diagnostics_copy_all"
    const val RESET = "diagnostics_reset"
    const val RESET_DIALOG = "diagnostics_reset_dialog"
    const val RESET_CONFIRM = "diagnostics_reset_confirm"
    const val RESET_CANCEL = "diagnostics_reset_cancel"
    const val OPEN_VPN_SETTINGS = "diagnostics_open_vpn_settings"
    const val OPEN_NOTIFICATION_SETTINGS = "diagnostics_open_notification_settings"
    const val OPEN_BATTERY_SETTINGS = "diagnostics_open_battery_settings"

    fun check(name: String): String = "diagnostics_check_$name"
}

/**
 * The Diagnostics entry point. It mirrors Settings' engine gate: the route
 * waits for the app-scoped coordinator, owns one rotation-safe view model per
 * engine, and reads the split selection from DataStore. The view model collects
 * facts and calls the doctor once per visit; there is no polling.
 */
@Suppress("DEPRECATION") // LocalClipboardManager is the non-suspend API this screen needs.
@Composable
fun DiagnosticsRoute(
    container: AppContainer,
    modifier: Modifier = Modifier,
    content: @Composable (
        DiagnosticsUiState,
        Boolean,
        SplitMode,
        Int,
        DiagnosticsActions,
        String?,
    ) -> Unit = { state, serviceBound, splitMode, splitAppCount, actions, launchError ->
        DiagnosticsScreen(
            state = state,
            serviceBound = serviceBound,
            splitMode = splitMode,
            splitAppCount = splitAppCount,
            actions = actions,
            modifier = modifier,
            launchError = launchError,
        )
    },
) {
    val coordinator = container.vpnServiceCoordinator
    val serviceState by coordinator.state.collectAsStateWithLifecycle()
    val engine by coordinator.engine.collectAsStateWithLifecycle()
    val splitState by container.splitTunnelRepository.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val clipboard = LocalClipboardManager.current
    val client = engine
    if (client == null) {
        DiagnosticsWaitingSurface(error = serviceState.error, modifier = modifier)
        return
    }

    val gateway = remember(coordinator, context) {
        CoordinatorDiagnosticsGateway(context, coordinator)
    }
    val key = remember(client) { "diagnostics-${System.identityHashCode(client)}" }
    val viewModel: DiagnosticsViewModel = viewModel(key = key) {
        DiagnosticsViewModel(gateway)
    }
    val state by viewModel.state.collectAsStateWithLifecycle()

    var launchError by remember { mutableStateOf<String?>(null) }
    val launchFailed = stringResource(R.string.diagnostics_launch_failed)
    val open: (Intent) -> Unit = { intent ->
        try {
            context.startActivity(intent)
        } catch (error: RuntimeException) {
            launchError = error.message?.takeIf { it.isNotBlank() } ?: launchFailed
        }
    }

    val actions = DiagnosticsActions(
        onRerun = viewModel::rerun,
        onCopyReport = { report ->
            clipboard.setText(AnnotatedString(report))
            viewModel.notifyCopied()
        },
        onDismissMessage = {
            launchError = null
            viewModel.dismissMessage()
        },
        onOpenVpnSettings = { open(PermissionController.vpnSettingsIntent()) },
        onOpenNotificationSettings = {
            open(PermissionController.notificationSettingsIntent(context))
        },
        onOpenBatterySettings = { open(PermissionController.batterySettingsIntent()) },
        onRequestReset = viewModel::requestReset,
        onDismissReset = viewModel::dismissReset,
        onConfirmReset = viewModel::confirmReset,
    )
    content(
        state,
        serviceState.binding,
        splitState.settings.mode,
        splitState.settings.packages.size,
        actions,
        launchError,
    )
}

/** The stateless Diagnostics screen: report header, service card, checks, actions. */
@Composable
fun DiagnosticsScreen(
    state: DiagnosticsUiState,
    serviceBound: Boolean,
    splitMode: SplitMode,
    splitAppCount: Int,
    actions: DiagnosticsActions,
    modifier: Modifier = Modifier,
    launchError: String? = null,
) {
    val report = state.report
    val checks = state.checks
    val message = diagnosticsMessageText(state.message)

    Column(modifier = modifier.fillMaxSize().testTag(DiagnosticsTags.SCREEN)) {
        if (message != null || launchError != null || (report != null && state.error != null)) {
            DiagnosticsBanner(
                title = message.orEmpty(),
                detail = launchError ?: state.error,
                onDismiss = actions.onDismissMessage,
                modifier = Modifier.padding(
                    start = Dimens.ScreenPadding,
                    end = Dimens.ScreenPadding,
                    top = Dimens.Space3,
                ),
            )
        }
        when {
            report == null && state.loading -> KumihoLoadingState(
                label = stringResource(R.string.diagnostics_loading),
                modifier = Modifier.testTag(DiagnosticsTags.LOADING),
            )
            report == null && state.error != null -> KumihoErrorState(
                message = state.error,
                title = stringResource(R.string.diagnostics_load_failed_title),
                onRetry = actions.onRerun,
                modifier = Modifier
                    .padding(Dimens.ScreenPadding)
                    .testTag(DiagnosticsTags.ERROR),
            )
            report != null -> DiagnosticsList(
                report = report,
                facts = state.facts,
                serviceBound = serviceBound,
                splitMode = splitMode,
                splitAppCount = splitAppCount,
                state = state,
                checks = checks,
                actions = actions,
            )
            else -> KumihoLoadingState(label = stringResource(R.string.diagnostics_loading))
        }
    }

    if (state.resetDialog) {
        ResetDialog(
            onConfirm = actions.onConfirmReset,
            onDismiss = actions.onDismissReset,
        )
    }
}

@Composable
private fun DiagnosticsList(
    report: DoctorReport,
    facts: AndroidFacts?,
    serviceBound: Boolean,
    splitMode: SplitMode,
    splitAppCount: Int,
    state: DiagnosticsUiState,
    checks: List<DoctorCheck>,
    actions: DiagnosticsActions,
) {
    LazyColumn(
        modifier = Modifier.fillMaxSize().testTag(DiagnosticsTags.LIST),
        contentPadding = PaddingValues(bottom = Dimens.Space6),
    ) {
        item(key = "header") {
            ReportHeader(report = report, checks = checks)
        }
        item(key = "service") {
            ServiceCard(
                facts = facts,
                report = report,
                bound = serviceBound,
                modifier = Modifier.padding(horizontal = Dimens.ScreenPadding),
            )
        }
        item(key = "split") {
            SplitCard(
                mode = splitMode,
                count = splitAppCount,
                modifier = Modifier.padding(horizontal = Dimens.ScreenPadding),
            )
        }
        item(key = "shortcuts") {
            SystemShortcuts(actions = actions)
        }
        item(key = "checks_header") {
            KumihoSectionHeader(
                title = stringResource(R.string.diagnostics_group_checks),
                modifier = Modifier
                    .semantics { heading() }
                    .testTag(DiagnosticsTags.CHECKS),
            )
        }
        items(checks, key = { it.name }) { check ->
            CheckRow(
                check = check,
                modifier = Modifier
                    .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space1)
                    .testTag(DiagnosticsTags.check(check.name)),
            )
        }
        item(key = "actions") {
            DiagnosticsButtons(
                state = state,
                report = report,
                facts = facts,
                actions = actions,
            )
        }
    }
}

@Composable
private fun ReportHeader(report: DoctorReport, checks: List<DoctorCheck>) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(Dimens.ScreenPadding)
            .testTag(DiagnosticsTags.HEADER),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space1),
    ) {
        Text(
            text = stringResource(R.string.diagnostics_summary_title),
            style = MaterialTheme.typography.titleLarge,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Text(
            text = stringResource(
                R.string.diagnostics_report_meta,
                report.version,
                report.os,
            ),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(
            text = stringResource(R.string.diagnostics_report_host, report.host, report.time),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        val counts = DiagnosticsReducer.statusCounts(checks)
        Text(
            text = stringResource(
                R.string.diagnostics_summary,
                counts[DoctorStatus.FAIL] ?: 0,
                counts[DoctorStatus.WARN] ?: 0,
                counts[DoctorStatus.SKIP] ?: 0,
            ),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.padding(top = Dimens.Space2),
        )
    }
}

@Composable
private fun ServiceCard(
    facts: AndroidFacts?,
    report: DoctorReport,
    bound: Boolean,
    modifier: Modifier = Modifier,
) {
    val clock = DiagnosticsReducer.check(report, "clock-skew")
    val endpoints = DiagnosticsReducer.check(report, "endpoints")
    KumihoCard(modifier = modifier.testTag(DiagnosticsTags.SERVICE_CARD)) {
        Text(
            text = stringResource(R.string.diagnostics_service_title),
            style = MaterialTheme.typography.titleMedium,
            color = MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.padding(bottom = Dimens.Space2),
        )
        FactRow(
            stringResource(R.string.diagnostics_row_service),
            yesNo(bound),
        )
        FactRow(
            stringResource(R.string.diagnostics_row_engine),
            facts?.engineState?.takeIf { it.isNotBlank() }
                ?: stringResource(R.string.diagnostics_not_started),
        )
        FactRow(
            stringResource(R.string.diagnostics_row_service_running),
            yesNo(facts?.serviceRunning == true),
        )
        FactRow(
            stringResource(R.string.diagnostics_row_always_on),
            onOff(facts?.alwaysOn == true),
        )
        FactRow(
            stringResource(R.string.diagnostics_row_lockdown),
            onOff(facts?.lockdown == true),
        )
        FactRow(
            stringResource(R.string.diagnostics_row_notifications),
            allowedState(facts?.notifications == true),
        )
        FactRow(
            stringResource(R.string.diagnostics_row_battery),
            if (facts?.batteryOptimized == true) {
                stringResource(R.string.diagnostics_battery_optimized)
            } else {
                stringResource(R.string.diagnostics_battery_unrestricted)
            },
        )
        FactRow(
            stringResource(R.string.diagnostics_row_network),
            availableState(facts?.network == true),
        )
        FactRow(
            stringResource(R.string.diagnostics_row_dns),
            facts?.dnsServers?.takeIf { it.isNotEmpty() }?.joinToString(", ")
                ?: stringResource(R.string.diagnostics_not_reported),
        )
        FactRow(
            stringResource(R.string.diagnostics_row_private_dns),
            privateDns(facts?.privateDnsMode),
        )
        FactRow(
            stringResource(R.string.diagnostics_row_ports),
            stringResource(
                R.string.diagnostics_ports_value,
                listening(facts?.socksPortListening == true),
                listening(facts?.httpProxyPortListening == true),
            ),
        )
        if (clock != null) {
            FactRow(stringResource(R.string.diagnostics_row_clock), clock.detail)
        }
        if (endpoints != null) {
            FactRow(stringResource(R.string.diagnostics_row_endpoints), endpoints.detail)
        }
    }
}

@Composable
private fun FactRow(label: String, value: String) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = Dimens.Space1),
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
            modifier = Modifier.weight(1.4f),
        )
    }
}

@Composable
private fun SplitCard(mode: SplitMode, count: Int, modifier: Modifier = Modifier) {
    val summary = when (mode) {
        SplitMode.OFF -> stringResource(R.string.settings_split_summary_off)
        SplitMode.EXCLUDE -> stringResource(R.string.diagnostics_split_excluding, count)
        SplitMode.INCLUDE -> stringResource(R.string.diagnostics_split_including, count)
    }
    KumihoCard(modifier = modifier.fillMaxWidth().testTag(DiagnosticsTags.SPLIT_CARD)) {
        Text(
            text = stringResource(R.string.settings_split_title),
            style = MaterialTheme.typography.titleMedium,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Text(
            text = summary,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.padding(top = Dimens.Space1),
        )
        Text(
            text = stringResource(R.string.diagnostics_split_hint),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(top = Dimens.Space1),
        )
    }
}

@Composable
private fun SystemShortcuts(actions: DiagnosticsActions) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space3),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space2),
    ) {
        ShortcutButton(
            label = stringResource(R.string.diagnostics_open_vpn_settings),
            onClick = actions.onOpenVpnSettings,
            tag = DiagnosticsTags.OPEN_VPN_SETTINGS,
        )
        ShortcutButton(
            label = stringResource(R.string.diagnostics_open_notification_settings),
            onClick = actions.onOpenNotificationSettings,
            tag = DiagnosticsTags.OPEN_NOTIFICATION_SETTINGS,
        )
        ShortcutButton(
            label = stringResource(R.string.diagnostics_open_battery_settings),
            onClick = actions.onOpenBatterySettings,
            tag = DiagnosticsTags.OPEN_BATTERY_SETTINGS,
        )
    }
}

@Composable
private fun ShortcutButton(label: String, onClick: () -> Unit, tag: String) {
    OutlinedButton(
        onClick = onClick,
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = Dimens.ControlHeight)
            .testTag(tag),
    ) {
        Text(label)
    }
}

@Composable
private fun CheckRow(check: DoctorCheck, modifier: Modifier = Modifier) {
    Surface(
        modifier = modifier.fillMaxWidth(),
        color = MaterialTheme.colorScheme.surfaceContainer,
        shape = MaterialTheme.shapes.medium,
    ) {
        Column(
            modifier = Modifier.padding(Dimens.Space3),
            verticalArrangement = Arrangement.spacedBy(Dimens.Space1),
        ) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
            ) {
                Text(
                    text = checkTitle(check.name),
                    style = MaterialTheme.typography.titleSmall,
                    color = MaterialTheme.colorScheme.onSurface,
                    modifier = Modifier.weight(1f),
                )
                KumihoStatusChip(
                    tone = check.status.tone(),
                    label = stringResource(check.status.labelRes()),
                )
            }
            Text(
                text = check.detail,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
            if (!check.remediation.isNullOrBlank()) {
                Text(
                    text = stringResource(R.string.diagnostics_remediation, check.remediation),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun DiagnosticsButtons(
    state: DiagnosticsUiState,
    report: DoctorReport,
    facts: AndroidFacts?,
    actions: DiagnosticsActions,
) {
    val loss = LocalKumihoColors.current.loss
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(Dimens.ScreenPadding),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space2),
    ) {
        Button(
            onClick = actions.onRerun,
            enabled = !state.loading,
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = Dimens.ControlHeight)
                .testTag(DiagnosticsTags.RERUN),
        ) {
            Text(
                if (state.loading) {
                    stringResource(R.string.diagnostics_rerunning)
                } else {
                    stringResource(R.string.diagnostics_rerun)
                },
            )
        }
        OutlinedButton(
            onClick = {
                actions.onCopyReport(DiagnosticsReducer.reportText(report, facts))
            },
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = Dimens.ControlHeight)
                .testTag(DiagnosticsTags.COPY_ALL),
        ) {
            Text(stringResource(R.string.diagnostics_copy_all))
        }
        TextButton(
            onClick = actions.onRequestReset,
            enabled = !state.resetting,
            colors = ButtonDefaults.textButtonColors(contentColor = loss),
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = Dimens.ControlHeight)
                .testTag(DiagnosticsTags.RESET),
        ) {
            Text(
                if (state.resetting) {
                    stringResource(R.string.diagnostics_resetting)
                } else {
                    stringResource(R.string.diagnostics_reset)
                },
            )
        }
    }
}

@Composable
private fun ResetDialog(onConfirm: () -> Unit, onDismiss: () -> Unit) {
    val loss = LocalKumihoColors.current.loss
    AlertDialog(
        onDismissRequest = onDismiss,
        modifier = Modifier.testTag(DiagnosticsTags.RESET_DIALOG),
        title = { Text(stringResource(R.string.diagnostics_reset_title)) },
        text = { Text(stringResource(R.string.diagnostics_reset_text)) },
        confirmButton = {
            TextButton(
                onClick = onConfirm,
                colors = ButtonDefaults.textButtonColors(contentColor = loss),
                modifier = Modifier.testTag(DiagnosticsTags.RESET_CONFIRM),
            ) {
                Text(stringResource(R.string.diagnostics_reset_confirm))
            }
        },
        dismissButton = {
            TextButton(
                onClick = onDismiss,
                modifier = Modifier.testTag(DiagnosticsTags.RESET_CANCEL),
            ) {
                Text(stringResource(R.string.diagnostics_reset_cancel))
            }
        },
    )
}

@Composable
private fun DiagnosticsBanner(
    title: String,
    detail: String?,
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val warning = LocalKumihoColors.current.warning
    Surface(
        modifier = modifier.fillMaxWidth(),
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
                if (detail != null) {
                    Text(
                        text = detail,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                }
            }
            TextButton(
                onClick = onDismiss,
                modifier = Modifier.testTag(DiagnosticsTags.MESSAGE_DISMISS),
            ) {
                Text(stringResource(R.string.diagnostics_dismiss))
            }
        }
    }
}

@Composable
private fun DiagnosticsWaitingSurface(error: String?, modifier: Modifier = Modifier) {
    Surface(modifier = modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            if (error == null) {
                KumihoLoadingState(
                    label = stringResource(R.string.diagnostics_loading),
                    modifier = Modifier.testTag(DiagnosticsTags.LOADING),
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
                        text = stringResource(R.string.diagnostics_engine_unavailable_title),
                        style = MaterialTheme.typography.titleLarge,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    Text(
                        text = stringResource(R.string.diagnostics_engine_unavailable),
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
internal fun diagnosticsMessageText(message: DiagnosticsMessage?): String? = when (message) {
    null -> null
    DiagnosticsMessage.RESET_DONE -> stringResource(R.string.diagnostics_reset_done)
    DiagnosticsMessage.RESET_FAILED -> stringResource(R.string.diagnostics_reset_failed)
    DiagnosticsMessage.LOAD_FAILED -> stringResource(R.string.diagnostics_load_failed_title)
    DiagnosticsMessage.COPIED -> stringResource(R.string.diagnostics_copied)
}

@Composable
internal fun checkTitle(name: String): String {
    val res = when (name) {
        "vpn-permission" -> R.string.diagnostics_check_vpn_permission
        "always-on" -> R.string.diagnostics_row_always_on
        "lockdown" -> R.string.diagnostics_row_lockdown
        "notifications" -> R.string.diagnostics_row_notifications
        "battery-optimization" -> R.string.diagnostics_row_battery
        "network" -> R.string.diagnostics_row_network
        "dns" -> R.string.diagnostics_row_dns
        "local-ports" -> R.string.diagnostics_row_ports
        "engine-state" -> R.string.diagnostics_row_engine
        "clock-skew" -> R.string.diagnostics_row_clock
        "endpoints" -> R.string.diagnostics_row_endpoints
        else -> null
    }
    return if (res != null) stringResource(res) else name
}

@Composable
private fun yesNo(value: Boolean): String = stringResource(
    if (value) R.string.diagnostics_yes else R.string.diagnostics_no,
)

@Composable
private fun onOff(value: Boolean): String = stringResource(
    if (value) R.string.diagnostics_on else R.string.diagnostics_off,
)

@Composable
private fun allowedState(value: Boolean): String = stringResource(
    if (value) R.string.diagnostics_allowed else R.string.diagnostics_not_allowed,
)

@Composable
private fun availableState(value: Boolean): String = stringResource(
    if (value) R.string.diagnostics_available else R.string.diagnostics_unavailable,
)

@Composable
private fun listening(value: Boolean): String = stringResource(
    if (value) R.string.diagnostics_listening else R.string.diagnostics_not_listening,
)

@Composable
private fun privateDns(mode: String?): String = when (mode) {
    AndroidFacts.PRIVATE_DNS_HOSTNAME -> stringResource(R.string.diagnostics_private_dns_hostname)
    AndroidFacts.PRIVATE_DNS_OPPORTUNISTIC ->
        stringResource(R.string.diagnostics_private_dns_opportunistic)
    AndroidFacts.PRIVATE_DNS_OFF -> stringResource(R.string.diagnostics_private_dns_off)
    else -> stringResource(R.string.diagnostics_not_reported)
}

internal fun DoctorStatus.labelRes(): Int = when (this) {
    DoctorStatus.OK -> R.string.diagnostics_status_ok
    DoctorStatus.WARN -> R.string.diagnostics_status_warn
    DoctorStatus.FAIL -> R.string.diagnostics_status_fail
    DoctorStatus.SKIP -> R.string.diagnostics_status_skip
    DoctorStatus.UNKNOWN -> R.string.diagnostics_status_unknown
}

private fun DoctorStatus.tone(): StatusTone = when (this) {
    DoctorStatus.OK -> StatusTone.Profit
    DoctorStatus.WARN -> StatusTone.Warning
    DoctorStatus.FAIL -> StatusTone.Loss
    DoctorStatus.SKIP, DoctorStatus.UNKNOWN -> StatusTone.Neutral
}

/** The callbacks the stateless screen needs. */
data class DiagnosticsActions(
    val onRerun: () -> Unit,
    val onCopyReport: (String) -> Unit,
    val onDismissMessage: () -> Unit,
    val onOpenVpnSettings: () -> Unit,
    val onOpenNotificationSettings: () -> Unit,
    val onOpenBatterySettings: () -> Unit,
    val onRequestReset: () -> Unit,
    val onDismissReset: () -> Unit,
    val onConfirmReset: () -> Unit,
)
