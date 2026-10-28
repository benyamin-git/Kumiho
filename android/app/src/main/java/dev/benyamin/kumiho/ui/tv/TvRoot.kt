package dev.benyamin.kumiho.ui.tv

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Text
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.shell.ShellNavigation
import dev.benyamin.kumiho.ui.tv.dashboard.TvDashboardRoute
import dev.benyamin.kumiho.ui.tv.account.TvAccountRoute
import dev.benyamin.kumiho.ui.tv.locations.TvLocationsRoute
import dev.benyamin.kumiho.ui.tv.settings.TvSettingsRoute
import dev.benyamin.kumiho.ui.tv.logs.TvLogsRoute
import dev.benyamin.kumiho.ui.tv.diagnostics.TvDiagnosticsRoute
import dev.benyamin.kumiho.ui.tv.setup.TvSetupRoute
import dev.benyamin.kumiho.ui.tv.theme.TvDimens

/**
 * The television root: waits for the stored setup flag, then shows TV setup or
 * the TV shell. The phone root never enters this tree.
 */
@Composable
fun TvRoot(
    container: AppContainer,
    tvConnectRequested: Boolean = false,
    onConnectConsumed: () -> Unit = {},
    modifier: Modifier = Modifier,
) {
    val setupComplete by container.appPrefs.setupComplete
        .collectAsStateWithLifecycle(initialValue = null)
    val nav = remember { ShellNavigation() }
    var pendingConnect by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(tvConnectRequested) {
        if (tvConnectRequested) pendingConnect = true
    }
    when (setupComplete) {
        null -> TvSetupGateLoading(modifier = modifier)
        false -> TvSetupRoute(container = container, modifier = modifier)
        true -> TvAppShell(
            nav = nav,
            home = {
                TvDashboardRoute(
                    container = container,
                    connectRequested = pendingConnect,
                    onConnectConsumed = {
                        pendingConnect = false
                        onConnectConsumed()
                    },
                )
            },
            locations = { TvLocationsRoute(container = container) },
            account = { TvAccountRoute(container = container) },
            settings = { TvSettingsRoute(container = container) },
            logs = { TvLogsRoute(container = container) },
            diagnostics = { TvDiagnosticsRoute(container = container) },
            tvConnectRequested = tvConnectRequested,
            modifier = modifier,
        )
    }
}

/** The themed wait while DataStore answers; no setup flash on a returning TV. */
@Composable
private fun TvSetupGateLoading(modifier: Modifier = Modifier) {
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
                text = stringResource(R.string.tv_loading),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}
