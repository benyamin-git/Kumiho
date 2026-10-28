package dev.benyamin.kumiho.ui.tv.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Text
import dev.benyamin.kumiho.ui.tv.theme.TvDimens

/** A non-interactive TV panel with the house card border. */
@Composable
fun TvCard(
    modifier: Modifier = Modifier,
    title: String? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    val colors = MaterialTheme.colorScheme
    Column(
        modifier = modifier
            .clip(MaterialTheme.shapes.medium)
            .background(colors.surface)
            .border(1.dp, colors.borderVariant, MaterialTheme.shapes.medium)
            .padding(TvDimens.CardPadding),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
    ) {
        if (title != null) {
            Text(text = title, style = MaterialTheme.typography.titleMedium, color = colors.onSurface)
            Spacer(Modifier.height(TvDimens.Space1))
        }
        content()
    }
}

/** The primary or secondary TV action. Focus is always visible. */
@Composable
fun TvButton(
    label: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    primary: Boolean = false,
) {
    val colors = MaterialTheme.colorScheme
    val container = when {
        !enabled -> colors.surfaceVariant
        primary -> colors.primary
        else -> colors.surfaceVariant
    }
    val content = when {
        !enabled -> colors.onSurfaceVariant
        primary -> colors.onPrimary
        else -> colors.onSurface
    }
    TvFocusableSurface(
        onClick = onClick,
        modifier = modifier,
        enabled = enabled,
        shape = MaterialTheme.shapes.small,
        containerColor = container,
    ) {
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = TvDimens.Space2, vertical = TvDimens.Space1),
            contentAlignment = Alignment.Center,
        ) {
            Text(text = label, style = MaterialTheme.typography.labelLarge, color = content)
        }
    }
}

/** The top-of-page title block used by every TV screen. */
@Composable
fun TvPageHeader(
    title: String,
    modifier: Modifier = Modifier,
    subtitle: String? = null,
) {
    val colors = MaterialTheme.colorScheme
    Column(modifier = modifier) {
        Text(text = title, style = MaterialTheme.typography.displaySmall, color = colors.onSurface)
        if (subtitle != null) {
            Text(
                text = subtitle,
                style = MaterialTheme.typography.bodyMedium,
                color = colors.onSurfaceVariant,
            )
        }
    }
}

/** A label/value pair used by the dashboard and diagnostics. */
@Composable
fun TvValue(
    label: String,
    value: String,
    modifier: Modifier = Modifier,
) {
    val colors = MaterialTheme.colorScheme
    Column(modifier = modifier, verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(text = label, style = MaterialTheme.typography.bodySmall, color = colors.onSurfaceVariant)
        Text(text = value, style = MaterialTheme.typography.titleSmall, color = colors.onSurface)
    }
}

/**
 * A focus-trapped TV dialog. [initialFocus] must be attached by the caller to
 * the primary action, so the remote lands on a useful control, never nowhere.
 */
@Composable
fun TvDialog(
    title: String,
    onDismiss: () -> Unit,
    initialFocus: FocusRequester,
    modifier: Modifier = Modifier,
    content: @Composable ColumnScope.() -> Unit,
    actions: @Composable RowScope.() -> Unit,
) {
    Dialog(
        onDismissRequest = onDismiss,
        properties = DialogProperties(usePlatformDefaultWidth = false),
    ) {
        LaunchedEffect(Unit) { runCatching { initialFocus.requestFocus() } }
        val colors = MaterialTheme.colorScheme
        Box(
            modifier = Modifier
                .fillMaxSize()
                .background(Color.Black.copy(alpha = 0.72f))
                .padding(horizontal = 96.dp, vertical = 54.dp),
            contentAlignment = Alignment.Center,
        ) {
            Column(
                modifier = modifier
                    .widthIn(max = 760.dp)
                    .clip(RoundedCornerShape(16.dp))
                    .background(colors.surface)
                    .border(1.dp, colors.borderVariant, RoundedCornerShape(16.dp))
                    .padding(TvDimens.Space4),
                verticalArrangement = Arrangement.spacedBy(TvDimens.Space2),
            ) {
                Text(text = title, style = MaterialTheme.typography.titleLarge, color = colors.onSurface)
                content()
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(TvDimens.Space2, Alignment.End),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    actions()
                }
            }
        }
    }
}

/** A quiet empty-loading line, used where a list has no rows yet. */
@Composable
fun TvEmpty(message: String, modifier: Modifier = Modifier) {
    Text(
        text = message,
        style = MaterialTheme.typography.bodyMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = modifier,
    )
}
