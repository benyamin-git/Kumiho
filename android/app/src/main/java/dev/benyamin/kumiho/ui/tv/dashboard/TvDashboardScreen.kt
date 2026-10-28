package dev.benyamin.kumiho.ui.tv.dashboard

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Text
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.dashboard.DashboardActions
import dev.benyamin.kumiho.ui.dashboard.DashboardFormat
import dev.benyamin.kumiho.ui.dashboard.DashboardNotice
import dev.benyamin.kumiho.ui.dashboard.DashboardNoticeAction
import dev.benyamin.kumiho.ui.dashboard.DashboardNoticeTone
import dev.benyamin.kumiho.ui.dashboard.DashboardPhase
import dev.benyamin.kumiho.ui.dashboard.DashboardState
import dev.benyamin.kumiho.ui.dashboard.DashboardTags
import dev.benyamin.kumiho.ui.dashboard.noticeActionLabel
import dev.benyamin.kumiho.ui.dashboard.noticeText
import dev.benyamin.kumiho.ui.dashboard.phaseLabel
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors
import dev.benyamin.kumiho.ui.tv.components.TvButton
import dev.benyamin.kumiho.ui.tv.components.TvDialog
import dev.benyamin.kumiho.ui.tv.components.TvFocusableSurface
import dev.benyamin.kumiho.ui.tv.theme.TvDimens

/**
 * The stateless TV dashboard: power hero on the left, real facts on the right,
 * a full-page options flow and a focus-trapped lockdown dialog. It performs no
 * engine or Android work, so a Compose test can drive it from fake state.
 */
@Composable
fun TvDashboardScreen(
    state: DashboardState,
    actions: DashboardActions,
    modifier: Modifier = Modifier,
) {
    val powerFocus = remember { FocusRequester() }
    val optionsFocus = remember { FocusRequester() }
    val locationFocus = remember { FocusRequester() }

    Box(
        modifier = modifier
            .fillMaxSize()
            .testTag(DashboardTags.SCREEN),
    ) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(TvDimens.Space2),
            verticalArrangement = Arrangement.spacedBy(TvDimens.Space2),
        ) {
            state.notice?.let { notice ->
                TvNotice(
                    notice = notice,
                    onAction = actions.onRetryNotice,
                    onDismiss = actions.onDismissNotice,
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
                    horizontalAlignment = Alignment.CenterHorizontally,
                    verticalArrangement = Arrangement.spacedBy(TvDimens.Space2),
                ) {
                    PowerHero(
                        state = state,
                        onPower = actions.onPower,
                        onOpenOptions = actions.onOpenOptions,
                        powerFocus = powerFocus,
                        optionsFocus = optionsFocus,
                        locationFocus = locationFocus,
                        hasLocationCard = state.showLocationCard,
                    )
                }
                TvDashboardCards(
                    state = state,
                    onOpenOptions = actions.onOpenOptions,
                    powerFocus = powerFocus,
                    optionsFocus = optionsFocus,
                    locationFocus = locationFocus,
                    modifier = Modifier
                        .weight(1f)
                        .fillMaxSize(),
                )
            }
        }

        if (state.options.open) {
            TvConnectOptionsPage(
                state = state,
                actions = actions,
                onClose = actions.onCloseOptions,
            )
        }

        if (state.lockdownPrompt) {
            TvLockdownDialog(
                onConfirm = actions.onConfirmLockdownDisconnect,
                onCancel = actions.onCancelLockdown,
                onOpenVpnSettings = actions.onOpenVpnSettings,
            )
        }
    }
}

@Composable
private fun PowerHero(
    state: DashboardState,
    onPower: () -> Unit,
    onOpenOptions: () -> Unit,
    powerFocus: FocusRequester,
    optionsFocus: FocusRequester,
    locationFocus: FocusRequester,
    hasLocationCard: Boolean,
) {
    val label = phaseLabel(state.phase)
    val status = state.status
    val location = when {
        status == null -> null
        status.location == null -> stringResource(R.string.dashboard_location_recommended)
        status.location.equals(DashboardState.RECOMMENDED_CODE, ignoreCase = true) ->
            stringResource(R.string.dashboard_location_recommended)
        else -> state.countryName(status.location) ?: status.location
    }
    val server = if (state.phase.active) DashboardFormat.hostOf(status?.server) else null
    val connectionLine = when {
        !state.phase.active -> null
        location != null && server != null -> "$location · $server"
        location != null -> location
        server != null -> server
        else -> null
    }
    val uptime = if (state.phase.active) {
        DashboardFormat.uptimeLabel(
            since = status?.since ?: 0L,
            nowSeconds = System.currentTimeMillis() / 1000,
        )
    } else {
        null
    }
    val colors = MaterialTheme.colorScheme
    val container = if (state.powerConnected) colors.primary else colors.surfaceVariant
    val iconTint = if (state.powerConnected) colors.onPrimary else colors.onSurface

    TvFocusableSurface(
        onClick = onPower,
        enabled = state.powerEnabled,
        shape = CircleShape,
        containerColor = container,
        modifier = Modifier
            .size(200.dp)
            .testTag(DashboardTags.POWER)
            .focusRequester(powerFocus)
            .focusProperties {
                down = optionsFocus
                if (hasLocationCard) right = locationFocus
            },
    ) {
        Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            Icon(
                painter = painterResource(R.drawable.ic_power),
                contentDescription = label,
                tint = iconTint,
                modifier = Modifier.size(84.dp),
            )
        }
    }
    Text(
        text = label,
        style = MaterialTheme.typography.displaySmall,
        color = colors.onSurface,
        textAlign = TextAlign.Center,
        modifier = Modifier.testTag(DashboardTags.STATE_WORD),
    )
    if (state.phase == DashboardPhase.PROXY_ONLY) {
        Text(
            text = stringResource(R.string.dashboard_proxy_only_chip),
            style = MaterialTheme.typography.labelMedium,
            color = colors.primary,
            modifier = Modifier
                .clip(MaterialTheme.shapes.small)
                .border(1.dp, colors.primary, MaterialTheme.shapes.small)
                .padding(horizontal = TvDimens.Space1, vertical = 4.dp),
        )
    }
    if (connectionLine != null) {
        Text(
            text = connectionLine,
            style = MaterialTheme.typography.bodyMedium,
            color = colors.onSurfaceVariant,
            textAlign = TextAlign.Center,
        )
    }
    if (uptime != null) {
        Text(
            text = stringResource(R.string.dashboard_uptime, uptime),
            style = MaterialTheme.typography.bodySmall,
            color = colors.onSurfaceVariant,
        )
    }
    TvButton(
        label = stringResource(R.string.dashboard_connect_options),
        onClick = onOpenOptions,
        modifier = Modifier
            .fillMaxWidth()
            .testTag(DashboardTags.CONNECT_OPTIONS)
            .focusRequester(optionsFocus)
            .focusProperties {
                up = powerFocus
                if (hasLocationCard) right = locationFocus
            },
    )
    if (state.powerBusy) {
        Text(
            text = stringResource(R.string.dashboard_options_waiting),
            style = MaterialTheme.typography.bodySmall,
            color = colors.onSurfaceVariant,
        )
    }
}

@Composable
private fun TvNotice(
    notice: DashboardNotice,
    onAction: () -> Unit,
    onDismiss: () -> Unit,
) {
    val house = LocalKumihoColors.current
    val accent = when (notice.tone) {
        DashboardNoticeTone.INFO -> MaterialTheme.colorScheme.primary
        DashboardNoticeTone.WARNING -> house.warning
        DashboardNoticeTone.ERROR -> house.loss
    }
    val detail = (notice.text as? dev.benyamin.kumiho.ui.dashboard.NoticeText.Local)?.detail
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(MaterialTheme.shapes.medium)
            .background(accent.copy(alpha = 0.12f))
            .border(1.dp, accent.copy(alpha = 0.5f), MaterialTheme.shapes.medium)
            .padding(TvDimens.Space2)
            .testTag(DashboardTags.NOTICE),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
    ) {
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
        Row(horizontalArrangement = Arrangement.spacedBy(TvDimens.Space2)) {
            if (notice.action != DashboardNoticeAction.NONE) {
                TvButton(
                    label = noticeActionLabel(notice.action),
                    onClick = onAction,
                    modifier = Modifier.testTag(DashboardTags.NOTICE_ACTION),
                )
            }
            TvButton(
                label = stringResource(R.string.dashboard_notice_dismiss),
                onClick = onDismiss,
                modifier = Modifier.testTag(DashboardTags.NOTICE_DISMISS),
            )
        }
    }
}

@Composable
private fun TvLockdownDialog(
    onConfirm: () -> Unit,
    onCancel: () -> Unit,
    onOpenVpnSettings: () -> Unit,
) {
    val cancelFocus = remember { FocusRequester() }
    val loss = LocalKumihoColors.current.loss
    TvDialog(
        title = stringResource(R.string.dashboard_lockdown_title),
        onDismiss = onCancel,
        initialFocus = cancelFocus,
        content = {
            Text(
                text = stringResource(R.string.dashboard_lockdown_text),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
            TvButton(
                label = stringResource(R.string.dashboard_lockdown_open_settings),
                onClick = onOpenVpnSettings,
                modifier = Modifier.testTag(DashboardTags.LOCKDOWN_VPN_SETTINGS),
            )
        },
        actions = {
            TvButton(
                label = stringResource(R.string.dashboard_lockdown_cancel),
                onClick = onCancel,
                modifier = Modifier
                    .focusRequester(cancelFocus)
                    .testTag(DashboardTags.LOCKDOWN_CANCEL),
            )
            TvFocusableSurface(
                onClick = onConfirm,
                modifier = Modifier.testTag(DashboardTags.LOCKDOWN_CONFIRM),
            ) {
                Text(
                    text = stringResource(R.string.dashboard_lockdown_confirm),
                    style = MaterialTheme.typography.labelLarge,
                    color = loss,
                    modifier = Modifier.padding(horizontal = TvDimens.Space1),
                )
            }
        },
    )
}
