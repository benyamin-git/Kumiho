package dev.benyamin.kumiho.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.diagnostics.LatencyBar
import dev.benyamin.kumiho.ui.theme.Dimens
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors

/**
 * The cached round trips as horizontal bars, fastest first: one accent, the
 * host and the exact milliseconds as text. An empty list is the caller's note,
 * never an empty axis.
 */
@Composable
fun LatencyChart(bars: List<LatencyBar>, modifier: Modifier = Modifier) {
    val colors = LocalKumihoColors.current
    val tabular = MaterialTheme.typography.bodySmall.copy(fontFeatureSettings = "tnum")
    val slowest = bars.maxOfOrNull { it.rttMs }?.coerceAtLeast(1.0) ?: 1.0
    Column(
        modifier = modifier.fillMaxWidth(),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space2),
    ) {
        bars.forEach { bar ->
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = bar.host,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.width(132.dp),
                )
                Box(
                    modifier = Modifier
                        .weight(1f)
                        .padding(horizontal = Dimens.Space2)
                        .height(8.dp)
                        .background(colors.dataGrid, RoundedCornerShape(4.dp)),
                ) {
                    Box(
                        modifier = Modifier
                            .fillMaxWidth((bar.rttMs / slowest).toFloat().coerceIn(0.02f, 1f))
                            .height(8.dp)
                            .background(colors.dataLine, RoundedCornerShape(4.dp)),
                    )
                }
                Text(
                    text = stringResource(R.string.locations_latency, bar.rttMs.toInt()),
                    style = tabular,
                    color = MaterialTheme.colorScheme.onSurface,
                    textAlign = TextAlign.End,
                    modifier = Modifier.width(56.dp),
                )
            }
        }
    }
}
