package dev.benyamin.kumiho.ui.tv.components

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.tv.material3.MaterialTheme
import dev.benyamin.kumiho.ui.tv.theme.TvDimens

/**
 * The one focusable TV surface: a 2dp primary ring, a subtle 1.03 scale and a
 * button role. Clickable handles D-pad Center/Enter and touch; the ring is
 * never optional, so a focused control is always visible.
 */
@Composable
fun TvFocusableSurface(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    shape: Shape = MaterialTheme.shapes.medium,
    containerColor: Color = Color.Unspecified,
    content: @Composable BoxScope.() -> Unit,
) {
    var focused by remember { mutableStateOf(false) }
    val scale by animateFloatAsState(
        targetValue = if (focused) 1.03f else 1f,
        label = "tv-focus-scale",
    )
    val colors = MaterialTheme.colorScheme
    Box(
        modifier = modifier
            .graphicsLayer {
                scaleX = scale
                scaleY = scale
            }
            .clip(shape)
            .background(if (containerColor == Color.Unspecified) colors.surfaceVariant else containerColor)
            .border(
                width = if (focused) TvDimens.FocusRing else 1.dp,
                color = if (focused) colors.primary else colors.borderVariant,
                shape = shape,
            )
            .onFocusChanged { focused = it.isFocused }
            .semantics { role = Role.Button }
            .clickable(enabled = enabled, onClick = onClick)
            .padding(TvDimens.MinTarget / 4),
        content = content,
    )
}
