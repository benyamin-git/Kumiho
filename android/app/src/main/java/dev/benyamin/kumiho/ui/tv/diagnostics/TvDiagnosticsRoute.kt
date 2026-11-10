package dev.benyamin.kumiho.ui.tv.diagnostics

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Text
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.diagnostics.AndroidFacts
import dev.benyamin.kumiho.engine.DoctorCheck
import dev.benyamin.kumiho.engine.DoctorReport
import dev.benyamin.kumiho.engine.DoctorStatus
import dev.benyamin.kumiho.ui.diagnostics.DiagnosticsActions
import dev.benyamin.kumiho.ui.diagnostics.DiagnosticsReducer
import dev.benyamin.kumiho.ui.diagnostics.DiagnosticsRoute
import dev.benyamin.kumiho.ui.diagnostics.DiagnosticsTags
import dev.benyamin.kumiho.ui.diagnostics.DiagnosticsUiState
import dev.benyamin.kumiho.ui.diagnostics.checkTitle
import dev.benyamin.kumiho.ui.diagnostics.diagnosticsMessageText
import dev.benyamin.kumiho.ui.diagnostics.labelRes
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors
import dev.benyamin.kumiho.ui.tv.components.TvButton
import dev.benyamin.kumiho.ui.tv.components.TvDialog
import dev.benyamin.kumiho.ui.tv.components.TvPageHeader
import dev.benyamin.kumiho.ui.tv.theme.TvDimens
import dev.benyamin.kumiho.vpn.SplitMode

/** The television Diagnostics entry; the phone route owns the doctor run and system intents. */
@Composable
fun TvDiagnosticsRoute(container: AppContainer, modifier: Modifier = Modifier) {
    DiagnosticsRoute(container = container, modifier = modifier) {
            state,
            serviceBound,
            splitMode,
            splitAppCount,
            actions,
            launchError,
        ->
        TvDiagnosticsScreen(
            state = state,
            serviceBound = serviceBound,
            splitMode = splitMode,
            splitAppCount = splitAppCount,
            actions = actions,
            modifier = modifier,
            launchError = launchError,
        )
    }
}

/**
 * The ten-foot diagnostics page: the report summary, Android facts, the visible
 * checks, the system shortcuts and the reset confirmation.
 */
@Composable
fun TvDiagnosticsScreen(
    state: DiagnosticsUiState,
    serviceBound: Boolean,
    splitMode: SplitMode,
    splitAppCount: Int,
    actions: DiagnosticsActions,
    modifier: Modifier = Modifier,
    launchError: String? = null,
) {
    val colors = MaterialTheme.colorScheme
    val report = state.report
    val checks = state.checks
    val message = diagnosticsMessageText(state.message)

    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(TvDimens.Space2)
            .testTag(DiagnosticsTags.SCREEN),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
    ) {
        TvPageHeader(title = stringResource(R.string.tv_diagnostics_title))
        if (message != null || launchError != null || (report != null && state.error != null)) {
            DiagnosticsBanner(
                title = message.orEmpty(),
                detail = launchError ?: state.error,
                onDismiss = actions.onDismissMessage,
            )
        }

        when {
            report == null && state.loading -> Text(
                text = stringResource(R.string.diagnostics_loading),
                style = MaterialTheme.typography.titleMedium,
                color = colors.onSurfaceVariant,
                modifier = Modifier
                    .padding(TvDimens.Space2)
                    .testTag(DiagnosticsTags.LOADING),
            )
            report == null && state.error != null -> Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(TvDimens.Space2)
                    .testTag(DiagnosticsTags.ERROR),
                verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
            ) {
                Text(
                    text = stringResource(R.string.diagnostics_load_failed_title),
                    style = MaterialTheme.typography.titleLarge,
                    color = colors.onSurface,
                )
                Text(
                    text = state.error,
                    style = MaterialTheme.typography.bodyMedium,
                    color = colors.onSurfaceVariant,
                )
                TvButton(
                    label = stringResource(R.string.locations_retry),
                    onClick = actions.onRerun,
                )
            }
            report != null -> Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f)
                    .verticalScroll(rememberScrollState())
                    .testTag(DiagnosticsTags.LIST),
                verticalArrangement = Arrangement.spacedBy(TvDimens.Space2),
            ) {
                SummaryCard(report = report, checks = checks)
                FactsCard(
                    facts = state.facts,
                    report = report,
                    serviceBound = serviceBound,
                    splitMode = splitMode,
                    splitAppCount = splitAppCount,
                )
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
                ) {
                    TvButton(
                        label = stringResource(R.string.diagnostics_rerun),
                        onClick = actions.onRerun,
                        modifier = Modifier.testTag(DiagnosticsTags.RERUN),
                        primary = true,
                    )
                    TvButton(
                        label = stringResource(R.string.diagnostics_copy_all),
                        onClick = { actions.onCopyReport(DiagnosticsReducer.reportText(report, state.facts)) },
                        modifier = Modifier.testTag(DiagnosticsTags.COPY_ALL),
                    )
                    TvButton(
                        label = stringResource(R.string.diagnostics_reset),
                        onClick = actions.onRequestReset,
                        modifier = Modifier.testTag(DiagnosticsTags.RESET),
                    )
                }
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
                ) {
                    TvButton(
                        label = stringResource(R.string.diagnostics_open_vpn_settings),
                        onClick = actions.onOpenVpnSettings,
                        modifier = Modifier.testTag(DiagnosticsTags.OPEN_VPN_SETTINGS),
                    )
                    TvButton(
                        label = stringResource(R.string.diagnostics_open_notification_settings),
                        onClick = actions.onOpenNotificationSettings,
                        modifier = Modifier.testTag(DiagnosticsTags.OPEN_NOTIFICATION_SETTINGS),
                    )
                }
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
                ) {
                    TvButton(
                        label = stringResource(R.string.diagnostics_open_battery_settings),
                        onClick = actions.onOpenBatterySettings,
                        modifier = Modifier.testTag(DiagnosticsTags.OPEN_BATTERY_SETTINGS),
                    )
                }

                Text(
                    text = stringResource(R.string.diagnostics_group_checks),
                    style = MaterialTheme.typography.titleLarge,
                    color = colors.primary,
                    modifier = Modifier.testTag(DiagnosticsTags.CHECKS),
                )
                checks.forEach { check ->
                    CheckRow(check = check)
                }
            }
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
private fun SummaryCard(report: DoctorReport, checks: List<DoctorCheck>) {
    val colors = MaterialTheme.colorScheme
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(colors.surface)
            .border(1.dp, colors.borderVariant, MaterialTheme.shapes.medium)
            .padding(TvDimens.Space2)
            .testTag(DiagnosticsTags.HEADER),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
    ) {
        Text(
            text = stringResource(R.string.diagnostics_summary_title),
            style = MaterialTheme.typography.titleMedium,
            color = colors.onSurface,
        )
        Text(
            text = stringResource(
                R.string.diagnostics_report_meta,
                report.version,
                report.os,
            ),
            style = MaterialTheme.typography.bodySmall,
            color = colors.onSurfaceVariant,
        )
        Text(
            text = stringResource(R.string.diagnostics_report_host, report.host, report.time),
            style = MaterialTheme.typography.bodySmall,
            color = colors.onSurfaceVariant,
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
            color = colors.onSurface,
        )
    }
}

@Composable
private fun FactsCard(
    facts: AndroidFacts?,
    report: DoctorReport,
    serviceBound: Boolean,
    splitMode: SplitMode,
    splitAppCount: Int,
) {
    val colors = MaterialTheme.colorScheme
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(colors.surface)
            .border(1.dp, colors.borderVariant, MaterialTheme.shapes.medium)
            .padding(TvDimens.Space2)
            .testTag(DiagnosticsTags.SERVICE_CARD),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Text(
            text = stringResource(R.string.tv_diagnostics_facts),
            style = MaterialTheme.typography.titleSmall,
            color = colors.onSurface,
        )
        if (facts != null) {
            Text(
                text = stringResource(
                    R.string.diagnostics_facts_device,
                    facts.device,
                    facts.sdk,
                    facts.engineState.ifBlank { "—" },
                ),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
            Text(
                text = stringResource(
                    R.string.diagnostics_facts_permission,
                    yesNo(facts.vpnPermission),
                    yesNo(facts.alwaysOn),
                    yesNo(facts.lockdown),
                ),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
            Text(
                text = stringResource(
                    R.string.diagnostics_facts_notifications,
                    yesNo(facts.notifications),
                    yesNo(facts.batteryOptimized),
                    yesNo(facts.network),
                ),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
            Text(
                text = stringResource(
                    R.string.diagnostics_facts_dns,
                    facts.dnsServers.joinToString(", ").ifBlank { "—" },
                    privateDns(facts.privateDnsMode),
                ),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
            Text(
                text = stringResource(
                    R.string.diagnostics_facts_ports,
                    yesNo(facts.socksPortListening),
                    yesNo(facts.httpProxyPortListening),
                ),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
        }
        Text(
            text = stringResource(
                R.string.diagnostics_facts_service,
                stringResource(
                    if (serviceBound) {
                        R.string.diagnostics_service_bound
                    } else {
                        R.string.diagnostics_service_not_bound
                    },
                ),
                splitLabel(splitMode, splitAppCount),
            ),
            style = MaterialTheme.typography.bodySmall,
            color = colors.onSurfaceVariant,
            modifier = Modifier.testTag(DiagnosticsTags.SPLIT_CARD),
        )
    }
}

@Composable
private fun CheckRow(check: DoctorCheck) {
    val colors = MaterialTheme.colorScheme
    val tone = when (check.status) {
        DoctorStatus.FAIL -> LocalKumihoColors.current.loss
        DoctorStatus.WARN -> LocalKumihoColors.current.warning
        DoctorStatus.OK -> colors.primary
        else -> colors.onSurfaceVariant
    }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(colors.surface)
            .border(1.dp, colors.borderVariant, MaterialTheme.shapes.medium)
            .padding(TvDimens.Space2),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = checkTitle(check.name),
                style = MaterialTheme.typography.titleSmall,
                color = colors.onSurface,
                modifier = Modifier.weight(1f),
            )
            Text(
                text = stringResource(check.status.labelRes()),
                style = MaterialTheme.typography.labelLarge,
                color = tone,
            )
        }
        Text(
            text = check.detail,
            style = MaterialTheme.typography.bodySmall,
            color = colors.onSurfaceVariant,
        )
        check.remediation?.takeIf { it.isNotBlank() }?.let { remediation ->
            Text(
                text = remediation,
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurface,
            )
        }
    }
}

@Composable
private fun DiagnosticsBanner(title: String, detail: String?, onDismiss: () -> Unit) {
    val colors = MaterialTheme.colorScheme
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(colors.surfaceVariant)
            .border(1.dp, colors.borderVariant, MaterialTheme.shapes.medium)
            .padding(TvDimens.Space2)
            .testTag(DiagnosticsTags.MESSAGE),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
    ) {
        if (title.isNotBlank()) {
            Text(text = title, style = MaterialTheme.typography.bodyMedium, color = colors.onSurface)
        }
        if (!detail.isNullOrBlank()) {
            Text(text = detail, style = MaterialTheme.typography.bodySmall, color = colors.onSurfaceVariant)
        }
        TvButton(
            label = stringResource(R.string.account_dismiss),
            onClick = onDismiss,
            modifier = Modifier.testTag(DiagnosticsTags.MESSAGE_DISMISS),
        )
    }
}

@Composable
private fun ResetDialog(onConfirm: () -> Unit, onDismiss: () -> Unit) {
    val focus = remember { FocusRequester() }
    TvDialog(
        title = stringResource(R.string.diagnostics_reset_title),
        onDismiss = onDismiss,
        initialFocus = focus,
        modifier = Modifier.testTag(DiagnosticsTags.RESET_DIALOG),
        content = {
            Text(
                text = stringResource(R.string.diagnostics_reset_text),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
        },
        actions = {
            TvButton(
                label = stringResource(R.string.diagnostics_reset_cancel),
                onClick = onDismiss,
                modifier = Modifier
                    .focusRequester(focus)
                    .testTag(DiagnosticsTags.RESET_CANCEL),
            )
            TvButton(
                label = stringResource(R.string.diagnostics_reset_confirm),
                onClick = onConfirm,
                modifier = Modifier.testTag(DiagnosticsTags.RESET_CONFIRM),
            )
        },
    )
}

@Composable
private fun yesNo(value: Boolean): String = stringResource(
    if (value) R.string.diagnostics_yes else R.string.diagnostics_no,
)

@Composable
private fun privateDns(mode: String?): String = when (mode) {
    AndroidFacts.PRIVATE_DNS_HOSTNAME -> stringResource(R.string.diagnostics_private_dns_hostname)
    AndroidFacts.PRIVATE_DNS_OPPORTUNISTIC ->
        stringResource(R.string.diagnostics_private_dns_opportunistic)
    AndroidFacts.PRIVATE_DNS_OFF -> stringResource(R.string.diagnostics_private_dns_off)
    else -> stringResource(R.string.diagnostics_not_reported)
}

@Composable
private fun splitLabel(mode: SplitMode, count: Int): String = when (mode) {
    SplitMode.OFF -> stringResource(R.string.diagnostics_split_off)
    SplitMode.EXCLUDE -> stringResource(R.string.diagnostics_split_excluding, count)
    SplitMode.INCLUDE -> stringResource(R.string.diagnostics_split_including, count)
}
