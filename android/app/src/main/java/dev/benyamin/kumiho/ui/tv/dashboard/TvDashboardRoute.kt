package dev.benyamin.kumiho.ui.tv.dashboard

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextAlign
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Text
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.ui.dashboard.DashboardPhase
import dev.benyamin.kumiho.ui.dashboard.DashboardRoute
import dev.benyamin.kumiho.ui.dashboard.DashboardState
import dev.benyamin.kumiho.ui.tv.theme.TvDimens

/**
 * The TV dashboard entry: the phone [DashboardRoute] owns the view model,
 * service binding, VPN consent and events; this route only chooses the TV
 * presentation and consumes the launcher's one-shot Connect request.
 */
@Composable
fun TvDashboardRoute(
    container: AppContainer,
    modifier: Modifier = Modifier,
    connectRequested: Boolean = false,
    onConnectConsumed: () -> Unit = {},
) {
    val serviceState by container.vpnServiceCoordinator.state.collectAsStateWithLifecycle()
    val engine by container.vpnServiceCoordinator.engine.collectAsStateWithLifecycle()
    if (engine == null) {
        TvDashboardWaiting(error = serviceState.error, modifier = modifier)
        return
    }

    DashboardRoute(container = container, modifier = modifier) { state, actions ->
        var connectHandled by rememberSaveable { mutableStateOf(false) }
        LaunchedEffect(connectRequested) {
            if (!connectRequested) connectHandled = false
        }
        LaunchedEffect(connectRequested, state.phase) {
            if (connectRequested &&
                !connectHandled &&
                state.phase != DashboardPhase.LOADING &&
                state.phase != DashboardPhase.CONNECTING
            ) {
                connectHandled = true
                if (!state.phase.active) actions.onPower()
                onConnectConsumed()
            }
        }
        TvDashboardScreen(state = state, actions = actions, modifier = modifier)
    }
}

@Composable
private fun TvDashboardWaiting(error: String?, modifier: Modifier = Modifier) {
    Box(
        modifier = modifier
            .fillMaxSize()
            .padding(horizontal = TvDimens.OverscanHorizontal, vertical = TvDimens.OverscanVertical),
        contentAlignment = Alignment.Center,
    ) {
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(TvDimens.Space2),
        ) {
            Text(
                text = if (error == null) "Loading the VPN service…" else "The VPN service is unavailable",
                style = MaterialTheme.typography.titleMedium,
                color = MaterialTheme.colorScheme.onSurface,
                textAlign = TextAlign.Center,
            )
            if (error != null) {
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
