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
            text = DiagnosticsReducer.summary(report, checks),
            style = MaterialTheme.typography.titleMedium,
            color = colors.onSurface,
        )
        Text(
            text = "Kumiho " + report.version + " · " + report.host + " · " + report.os,
            style = MaterialTheme.typography.bodySmall,
            color = colors.onSurfaceVariant,
        )
        Text(
            text = report.time,
            style = MaterialTheme.typography.bodySmall,
            color = colors.onSurfaceVariant,
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
                text = "Device " + facts.device + " · SDK " + facts.sdk +
                    " · Engine " + facts.engineState.ifBlank { "—" },
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
            Text(
                text = "VPN permission " + yesNo(facts.vpnPermission) +
                    " · Always-on " + yesNo(facts.alwaysOn) +
                    " · Lockdown " + yesNo(facts.lockdown),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
            Text(
                text = "Notifications " + yesNo(facts.notifications) +
                    " · Battery optimized " + yesNo(facts.batteryOptimized) +
                    " · Network " + yesNo(facts.network),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
            Text(
                text = "DNS " + facts.dnsServers.joinToString(", ").ifBlank { "—" } +
                    " · Private DNS " + facts.privateDnsMode,
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
            Text(
                text = "SOCKS listening " + yesNo(facts.socksPortListening) +
                    " · HTTP proxy listening " + yesNo(facts.httpProxyPortListening),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
        }
        Text(
            text = "Service " + if (serviceBound) "bound" else "not bound" +
                " · Split " + splitLabel(splitMode, splitAppCount),
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

private fun yesNo(value: Boolean): String = if (value) "yes" else "no"

private fun splitLabel(mode: SplitMode, count: Int): String = when (mode) {
    SplitMode.OFF -> "off"
    SplitMode.EXCLUDE -> "excluding $count"
    SplitMode.INCLUDE -> "including $count"
}
