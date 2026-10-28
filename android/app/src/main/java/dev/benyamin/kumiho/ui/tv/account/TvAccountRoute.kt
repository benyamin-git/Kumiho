package dev.benyamin.kumiho.ui.tv.account

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextAlign
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Text
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.ui.account.AccountRoute
import dev.benyamin.kumiho.ui.tv.theme.TvDimens

/** The TV account entry: the phone route owns the view model and engine calls. */
@Composable
fun TvAccountRoute(container: AppContainer, modifier: Modifier = Modifier) {
    val serviceState by container.vpnServiceCoordinator.state.collectAsStateWithLifecycle()
    val engine by container.vpnServiceCoordinator.engine.collectAsStateWithLifecycle()
    if (engine == null) {
        Box(
            modifier = modifier
                .fillMaxSize()
                .padding(TvDimens.OverscanHorizontal, TvDimens.OverscanVertical),
            contentAlignment = Alignment.Center,
        ) {
            Column(
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(TvDimens.Space2),
            ) {
                Text(
                    text = "Loading your account…",
                    style = MaterialTheme.typography.titleMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                    textAlign = TextAlign.Center,
                )
                serviceState.error?.let { error ->
                    Text(
                        text = error,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        textAlign = TextAlign.Center,
                    )
                }
            }
        }
        return
    }
    AccountRoute(container = container, modifier = modifier) { state, actions ->
        TvAccountScreen(state = state, actions = actions, modifier = modifier)
    }
}
