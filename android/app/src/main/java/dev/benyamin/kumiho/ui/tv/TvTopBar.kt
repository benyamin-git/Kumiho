package dev.benyamin.kumiho.ui.tv

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Icon
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.style.TextAlign
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Text
import dev.benyamin.kumiho.ui.shell.Destination
import dev.benyamin.kumiho.ui.tv.components.TvButton
import dev.benyamin.kumiho.ui.tv.components.TvFocusableSurface
import dev.benyamin.kumiho.ui.tv.components.TvPageHeader
import dev.benyamin.kumiho.ui.tv.theme.TvDimens

/** Stable tags for every shell surface a connected test traverses. */
object TvShellTags {
    const val RAIL_PREFIX = "tv_rail_"
    const val MORE = "tv_more_page"
    const val MORE_LOGS = "tv_more_logs"
    const val MORE_DIAGNOSTICS = "tv_more_diagnostics"
    const val TOP_BAR = "tv_top_bar"
}

fun tvRailTag(destination: Destination): String = TvShellTags.RAIL_PREFIX + destination.route.ifEmpty { "more" }

/** The left rail: the only navigation on TV, plus the selected-tab marker. */
@Composable
fun TvRail(
    selected: Destination,
    onSelect: (Destination) -> Unit,
    modifier: Modifier = Modifier,
    focusRequesters: Map<Destination, FocusRequester> = emptyMap(),
) {
    val colors = MaterialTheme.colorScheme
    Column(
        modifier = modifier
            .width(TvDimens.RailWidth)
            .padding(end = TvDimens.Space2),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
    ) {
        Text(
            text = "Kumiho",
            style = MaterialTheme.typography.titleLarge,
            color = colors.onSurface,
            modifier = Modifier.padding(start = TvDimens.Space2, bottom = TvDimens.Space2),
        )
        Destination.tabs.forEach { destination ->
            val isSelected = destination == selected ||
                (selected == Destination.More && destination == Destination.More)
            val container = if (isSelected) colors.primaryContainer else Color.Transparent
            val content = if (isSelected) colors.onPrimaryContainer else colors.onSurface
            TvFocusableSurface(
                onClick = { onSelect(destination) },
                modifier = Modifier
                    .fillMaxWidth()
                    .height(TvDimens.MinTarget)
                    .testTag(tvRailTag(destination))
                    .then(
                        focusRequesters[destination]?.let { Modifier.focusRequester(it) } ?: Modifier,
                    ),
                containerColor = container,
            ) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
                ) {
                    Text(
                        text = destination.label,
                        style = MaterialTheme.typography.titleSmall,
                        color = content,
                    )
                }
            }
        }
    }
}

/** The TV top bar: the app wordmark and, on More, a Back affordance. */
@Composable
fun TvTopBar(title: String, modifier: Modifier = Modifier) {
    val colors = MaterialTheme.colorScheme
    Row(
        modifier = modifier
            .fillMaxWidth()
            .height(TvDimens.MinTarget)
            .testTag(TvShellTags.TOP_BAR),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text = title, style = MaterialTheme.typography.titleLarge, color = colors.onSurface)
    }
}

/** The More page, a full TV page rather than the phone bottom sheet. */
@Composable
fun TvMorePage(
    onLogs: () -> Unit,
    onDiagnostics: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(horizontal = TvDimens.Space2, vertical = TvDimens.Space1)
            .testTag(TvShellTags.MORE),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space2),
    ) {
        TvPageHeader(title = "More", subtitle = "Logs and diagnostics")
        Row(horizontalArrangement = Arrangement.spacedBy(TvDimens.Space2)) {
            TvButton(
                label = "Logs",
                onClick = onLogs,
                modifier = Modifier.testTag(TvShellTags.MORE_LOGS),
            )
            TvButton(
                label = "Diagnostics",
                onClick = onDiagnostics,
                modifier = Modifier.testTag(TvShellTags.MORE_DIAGNOSTICS),
            )
        }
        Text(
            text = "Press Back to return to the previous screen.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Start,
        )
    }
}

