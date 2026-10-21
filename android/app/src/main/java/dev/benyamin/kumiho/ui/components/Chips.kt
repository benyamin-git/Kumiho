package dev.benyamin.kumiho.ui.components

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Clear
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp
import dev.benyamin.kumiho.ui.theme.Dimens
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors
import dev.benyamin.kumiho.ui.theme.PillShape

/** A selectable chip, for filters and multi-choice rows. */
@Composable
fun KumihoChip(
    label: String,
    selected: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    FilterChip(
        selected = selected,
        onClick = onClick,
        label = { Text(label) },
        modifier = modifier,
        shape = MaterialTheme.shapes.extraSmall,
    )
}

/** A status meaning that must never be carried by colour alone. */
enum class StatusTone { Profit, Loss, Warning, Neutral }

/**
 * A status chip: a tinted pill with an icon and text, so meaning survives
 * without colour. Used for quota, connectivity and diagnostics results.
 */
@Composable
fun KumihoStatusChip(
    tone: StatusTone,
    label: String,
    modifier: Modifier = Modifier,
) {
    val colors = LocalKumihoColors.current
    val accent: Color = when (tone) {
        StatusTone.Profit -> colors.profit
        StatusTone.Loss -> colors.loss
        StatusTone.Warning -> colors.warning
        StatusTone.Neutral -> MaterialTheme.colorScheme.onSurfaceVariant
    }
    val glyph: ImageVector = when (tone) {
        StatusTone.Profit -> Icons.Filled.CheckCircle
        StatusTone.Loss -> Icons.Filled.Clear
        StatusTone.Warning -> Icons.Filled.Warning
        StatusTone.Neutral -> Icons.Filled.Info
    }
    Surface(
        modifier = modifier,
        shape = PillShape,
        color = accent.copy(alpha = 0.16f),
        border = BorderStroke(1.dp, accent.copy(alpha = 0.5f)),
    ) {
        Row(
            modifier = Modifier
                .padding(horizontal = Dimens.Space3, vertical = Dimens.Space1)
                .defaultMinSize(minHeight = 24.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space1),
        ) {
            Icon(
                imageVector = glyph,
                contentDescription = null,
                tint = accent,
                modifier = Modifier.size(16.dp),
            )
            Text(text = label, style = MaterialTheme.typography.labelMedium, color = accent)
        }
    }
}
