package dev.benyamin.kumiho.ui.components

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.dashboard.DashboardFormat
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors
import kotlin.math.max

/**
 * The live 60 s throughput: download as a filled area, upload as a stroked
 * line, one accent. Drawn directly on the page background, because the data
 * roles are pre-composited there. The numbers are also printed as text.
 */
@Composable
fun ThroughputChart(samples: List<RateSample>, modifier: Modifier = Modifier) {
    val colors = LocalKumihoColors.current
    val tabular = MaterialTheme.typography.bodySmall.copy(fontFeatureSettings = "tnum")
    Column(modifier = modifier.fillMaxWidth()) {
        if (samples.isEmpty()) {
            Text(
                text = stringResource(R.string.diagnostics_chart_no_trace),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            return@Column
        }
        val peak = samples.maxOf { max(it.down, it.up) }.coerceAtLeast(1.0)
        Canvas(
            modifier = Modifier
                .fillMaxWidth()
                .height(120.dp),
        ) {
            val bottom = size.height
            val stepX = if (samples.size > 1) size.width / (samples.size - 1) else size.width
            fun x(index: Int) = if (samples.size == 1) size.width else index * stepX
            fun y(value: Double) =
                bottom - (value / peak * size.height).toFloat().coerceIn(0f, size.height)

            drawLine(
                color = colors.dataGrid,
                start = Offset(0f, bottom),
                end = Offset(size.width, bottom),
                strokeWidth = 1.dp.toPx(),
            )

            val down = Path()
            samples.forEachIndexed { index, sample ->
                val px = x(index)
                val py = y(sample.down)
                if (index == 0) down.moveTo(px, py) else down.lineTo(px, py)
            }
            down.lineTo(x(samples.lastIndex), bottom)
            down.lineTo(x(0), bottom)
            down.close()
            drawPath(down, color = colors.dataFill)

            val up = Path()
            samples.forEachIndexed { index, sample ->
                val px = x(index)
                val py = y(sample.up)
                if (index == 0) up.moveTo(px, py) else up.lineTo(px, py)
            }
            drawPath(
                path = up,
                color = colors.dataLine,
                style = Stroke(width = 2.dp.toPx(), cap = StrokeCap.Round),
            )
        }
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            Text(
                text = stringResource(R.string.diagnostics_chart_ago),
                style = tabular,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text(
                text = stringResource(R.string.diagnostics_chart_now),
                style = tabular,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        val last = samples.last()
        Text(
            text = stringResource(
                R.string.diagnostics_chart_summary,
                DashboardFormat.rateLabel(last.down),
                DashboardFormat.rateLabel(last.up),
                DashboardFormat.rateLabel(peak),
            ),
            style = tabular,
            color = MaterialTheme.colorScheme.onSurface,
            textAlign = TextAlign.Start,
            modifier = Modifier.fillMaxWidth(),
        )
    }
}

/** The note line that replaces a chart with no data. */
@Composable
fun ChartNote(note: String, modifier: Modifier = Modifier) {
    Text(
        text = note,
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = modifier.fillMaxWidth(),
    )
}
