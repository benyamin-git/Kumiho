package dev.benyamin.kumiho.ui.components

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsPressedAsState
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ripple
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.theme.PillShape
import dev.benyamin.kumiho.ui.theme.rememberMotion

/**
 * The power control: one large circular button with an expressive press
 * spring that collapses to snap under reduced motion. The state word and icon
 * carry the state, so colour is never the only signal.
 */
@Composable
fun KumihoPowerButton(
    stateLabel: String,
    connected: Boolean,
    onToggle: () -> Unit,
    modifier: Modifier = Modifier,
    busy: Boolean = false,
    enabled: Boolean = true,
    size: Dp = 160.dp,
) {
    val motion = rememberMotion()
    val interaction = remember { MutableInteractionSource() }
    val pressed by interaction.collectIsPressedAsState()
    val scale by animateFloatAsState(
        targetValue = if (pressed && enabled && !busy) 0.94f else 1f,
        animationSpec = motion.expressive,
        label = "kumihoPowerScale",
    )

    val scheme = MaterialTheme.colorScheme
    val container: Color = when {
        !enabled -> scheme.surfaceContainerHighest
        connected -> scheme.primary
        else -> scheme.primaryContainer
    }
    val content: Color = when {
        !enabled -> scheme.onSurfaceVariant
        connected -> scheme.onPrimary
        else -> scheme.onPrimaryContainer
    }

    Box(
        modifier = modifier
            .size(size)
            .scale(scale)
            .clip(PillShape)
            .background(container)
            .clickable(
                enabled = enabled && !busy,
                role = Role.Button,
                interactionSource = interaction,
                indication = ripple(),
                onClick = onToggle,
            )
            .semantics {
                contentDescription = "Power"
                stateDescription = stateLabel
            },
        contentAlignment = Alignment.Center,
    ) {
        if (busy) {
            CircularProgressIndicator(
                color = content,
                strokeWidth = 3.dp,
                modifier = Modifier.size(size / 3),
            )
        } else {
            Icon(
                painter = painterResource(R.drawable.ic_power),
                contentDescription = null,
                tint = content,
                modifier = Modifier.size(size / 3),
            )
        }
    }
}
