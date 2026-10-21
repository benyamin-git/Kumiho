package dev.benyamin.kumiho.ui.dashboard

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.components.KumihoPowerButton
import dev.benyamin.kumiho.ui.components.KumihoStatusChip
import dev.benyamin.kumiho.ui.components.StatusTone
import dev.benyamin.kumiho.ui.theme.Dimens

/**
 * The power hero: one large power control, the state word, and only the facts
 * that are real right now (location, server, uptime, the proxy-only badge).
 * The state word and the icon carry the state; colour is never the only signal.
 */
@Composable
fun PowerHero(
    state: DashboardState,
    onPower: () -> Unit,
    onOpenOptions: () -> Unit,
    modifier: Modifier = Modifier,
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

    Column(
        modifier = modifier.fillMaxWidth(),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(Dimens.Space3),
    ) {
        KumihoPowerButton(
            stateLabel = label,
            connected = state.powerConnected,
            onToggle = onPower,
            busy = state.powerBusy,
            enabled = state.powerEnabled,
            modifier = Modifier.testTag(DashboardTags.POWER),
        )
        Text(
            text = label,
            style = MaterialTheme.typography.titleLarge,
            color = MaterialTheme.colorScheme.onSurface,
            textAlign = TextAlign.Center,
            modifier = Modifier.testTag(DashboardTags.STATE_WORD),
        )
        if (state.phase == DashboardPhase.PROXY_ONLY) {
            KumihoStatusChip(
                tone = StatusTone.Neutral,
                label = stringResource(R.string.dashboard_proxy_only_chip),
            )
        }
        if (connectionLine != null) {
            Text(
                text = connectionLine,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                textAlign = TextAlign.Center,
            )
        }
        if (uptime != null) {
            Text(
                text = stringResource(R.string.dashboard_uptime, uptime),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.Center,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            TextButton(
                onClick = onOpenOptions,
                modifier = Modifier
                    .heightIn(min = Dimens.TouchTarget)
                    .testTag(DashboardTags.CONNECT_OPTIONS),
            ) {
                Text(
                    text = stringResource(R.string.dashboard_connect_options),
                    style = MaterialTheme.typography.labelLarge,
                )
            }
        }
    }
}

/** The one visible state word per dashboard phase. */
@Composable
fun phaseLabel(phase: DashboardPhase): String = stringResource(
    when (phase) {
        DashboardPhase.LOADING -> R.string.dashboard_state_loading
        DashboardPhase.OFF -> R.string.dashboard_state_off
        DashboardPhase.CONNECTED -> R.string.dashboard_state_connected
        DashboardPhase.PROXY_ONLY -> R.string.dashboard_state_proxy_only
        DashboardPhase.CONNECTING -> R.string.dashboard_state_connecting
        DashboardPhase.RECONNECTING -> R.string.dashboard_state_reconnecting
        DashboardPhase.WAITING -> R.string.dashboard_state_waiting
        DashboardPhase.NOT_SIGNED_IN -> R.string.dashboard_state_not_signed_in
        DashboardPhase.ERROR -> R.string.dashboard_state_error
        DashboardPhase.SERVICE_UNAVAILABLE -> R.string.dashboard_state_unavailable
        DashboardPhase.PERMISSION_NEEDED -> R.string.dashboard_state_permission_needed
    },
)
