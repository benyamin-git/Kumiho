package dev.benyamin.kumiho.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.colorResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.data.PaletteState
import dev.benyamin.kumiho.ui.components.KumihoLoadingState
import dev.benyamin.kumiho.ui.dashboard.DashboardRoute
import dev.benyamin.kumiho.ui.locations.LocationsRoute
import dev.benyamin.kumiho.ui.setup.SetupRoute
import dev.benyamin.kumiho.ui.shell.AppShell
import dev.benyamin.kumiho.ui.shell.ShellNavigation
import dev.benyamin.kumiho.ui.theme.Dimens
import dev.benyamin.kumiho.ui.theme.KumihoShapes
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.KumihoTypography

/**
 * The app root. It renders nothing themed until the stored palette has loaded,
 * so a dark or OLED choice never flashes light; a palette failure gets its own
 * explicit surface rather than a raw-colour fallback.
 */
@Composable
fun KumihoApp(container: AppContainer) {
    val paletteState by container.themeRepository.state.collectAsStateWithLifecycle()
    // null means "not read yet": a returning user must never see the setup
    // screen flash before the stored completion arrives.
    val setupComplete by container.appPrefs.setupComplete
        .collectAsStateWithLifecycle(initialValue = null)
    val nav = remember { ShellNavigation() }
    when (val state = paletteState) {
        PaletteState.Loading -> KumihoLoadingSurface()
        is PaletteState.Error -> PaletteErrorSurface(state.message, container.themeRepository::retry)
        is PaletteState.Ready -> KumihoTheme(colors = state.colors) {
            when (setupComplete) {
                null -> SetupGateLoading()
                false -> SetupRoute(container)
                true -> AppShell(
                    nav = nav,
                    home = { DashboardRoute(container) },
                    locations = { LocationsRoute(container) },
                )
            }
        }
    }
}

/** The themed wait while the stored setup state is read from DataStore. */
@Composable
private fun SetupGateLoading() {
    Surface(
        modifier = Modifier.fillMaxSize(),
        color = MaterialTheme.colorScheme.background,
    ) {
        Box(
            modifier = Modifier.fillMaxSize(),
            contentAlignment = Alignment.Center,
        ) {
            KumihoLoadingState(label = stringResource(R.string.setup_loading))
        }
    }
}

/** The frame or two before the palette; the splash theme is still painted. */
@Composable
private fun KumihoLoadingSurface() {
    Surface(
        modifier = Modifier.fillMaxSize(),
        color = colorResource(R.color.kumiho_background),
    ) {}
}

/**
 * The explicit palette-error surface. It uses the qualified neutral resource
 * colours (never a raw accent) and a border-only retry control, so no Material
 * default purple is ever visible.
 */
@Composable
private fun PaletteErrorSurface(message: String, onRetry: () -> Unit) {
    val background = colorResource(R.color.kumiho_background)
    val foreground = colorResource(R.color.kumiho_on_surface)
    MaterialTheme(typography = KumihoTypography, shapes = KumihoShapes) {
        Surface(modifier = Modifier.fillMaxSize(), color = background) {
            Column(
                modifier = Modifier
                    .fillMaxSize()
                    .safeDrawingPadding()
                    .padding(Dimens.Space5),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(Dimens.Space3, Alignment.CenterVertically),
            ) {
                Icon(
                    imageVector = Icons.Filled.Warning,
                    contentDescription = null,
                    tint = foreground,
                    modifier = Modifier.size(40.dp),
                )
                Text(
                    text = "Theme unavailable",
                    style = MaterialTheme.typography.titleLarge,
                    color = foreground,
                )
                Text(
                    text = message,
                    style = MaterialTheme.typography.bodyMedium,
                    color = foreground,
                    textAlign = TextAlign.Center,
                )
                Surface(
                    onClick = onRetry,
                    shape = MaterialTheme.shapes.small,
                    color = Color.Transparent,
                    border = BorderStroke(1.dp, foreground.copy(alpha = 0.5f)),
                ) {
                    Text(
                        text = "Retry",
                        style = MaterialTheme.typography.labelLarge,
                        color = foreground,
                        modifier = Modifier.padding(horizontal = Dimens.Space4, vertical = Dimens.Space2),
                    )
                }
            }
        }
    }
}
