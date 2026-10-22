package dev.benyamin.kumiho.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.painter.Painter
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import dev.benyamin.kumiho.ui.components.KumihoPowerButton
import dev.benyamin.kumiho.ui.theme.Dimens

/**
 * An honest placeholder for a shell route that has no data yet. It says what it
 * is ("Not connected" / a named load) and never shows data that does not exist.
 */
@Composable
fun PlaceholderScreen(
    title: String,
    message: String,
    modifier: Modifier = Modifier,
    icon: Painter? = null,
    hero: (@Composable () -> Unit)? = null,
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(Dimens.ScreenPadding),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(Dimens.Space3, Alignment.CenterVertically),
    ) {
        if (hero != null) {
            hero()
        } else if (icon != null) {
            Icon(
                painter = icon,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(48.dp),
            )
        }
        Text(
            text = title,
            style = MaterialTheme.typography.titleLarge,
            color = MaterialTheme.colorScheme.onSurface,
            textAlign = TextAlign.Center,
        )
        Text(
            text = message,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center,
        )
    }
}

/**
 * The Home route before the dashboard exists. It shows the power control in its
 * disconnected, disabled state with the honest "Not connected" label; the real
 * dashboard arrives in T11.
 */
@Composable
fun HomePlaceholder(modifier: Modifier = Modifier) {
    PlaceholderScreen(
        title = "Not connected",
        message = "The engine client is not wired yet. The dashboard arrives with it.",
        modifier = modifier,
        hero = {
            KumihoPowerButton(
                stateLabel = "Not connected",
                connected = false,
                enabled = false,
                onToggle = {},
            )
        },
    )
}
