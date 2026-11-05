package dev.benyamin.kumiho.ui.components

import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color

/**
 * A single-choice segmented control, used for small option sets such as the
 * three themes or the split-tunnel modes. The selected segment takes the primary
 * tint and an outline; the unselected segments stay transparent with an
 * outline, so selection never depends on colour alone.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun <T> KumihoSegmentedControl(
    options: List<T>,
    selected: T,
    onSelect: (T) -> Unit,
    modifier: Modifier = Modifier,
    label: (T) -> String = { it.toString() },
) {
    val scheme = MaterialTheme.colorScheme
    val colors = SegmentedButtonDefaults.colors(
        activeContainerColor = scheme.primaryContainer,
        activeContentColor = scheme.onPrimaryContainer,
        activeBorderColor = scheme.primary,
        inactiveContainerColor = Color.Transparent,
        inactiveContentColor = scheme.onSurfaceVariant,
        inactiveBorderColor = scheme.outline,
    )
    SingleChoiceSegmentedButtonRow(modifier = modifier) {
        options.forEachIndexed { index, option ->
            SegmentedButton(
                selected = option == selected,
                onClick = { onSelect(option) },
                shape = SegmentedButtonDefaults.itemShape(index = index, count = options.size),
                colors = colors,
                label = { Text(label(option)) },
            )
        }
    }
}
