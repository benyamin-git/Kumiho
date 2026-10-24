package dev.benyamin.kumiho.ui.theme

import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Shapes
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/**
 * Density and geometry, house rule: 4dp spacing steps, 48dp touch targets,
 * 64dp top bar, and the radius scale 6 / 8 / 12 / 16 / full.
 */
object Dimens {
    val Space1: Dp = 4.dp
    val Space2: Dp = 8.dp
    val Space3: Dp = 12.dp
    val Space4: Dp = 16.dp
    val Space5: Dp = 24.dp
    val Space6: Dp = 32.dp

    /** Minimum touch target on mobile. */
    val TouchTarget: Dp = 48.dp

    /** The shell top bar. */
    val TopBarHeight: Dp = 64.dp

    /** A list row. */
    val RowHeight: Dp = 56.dp

    /** A control (button, input). */
    val ControlHeight: Dp = 48.dp

    /** Horizontal padding inside a screen. */
    val ScreenPadding: Dp = 16.dp

    /** Reading content stops widening past this measure on large screens. */
    val ContentMaxWidth: Dp = 720.dp
}

/**
 * The radius scale: 6 for chips and inputs, 8 for buttons, 12 for cards, 16 for
 * dialogs and sheets, full for pills.
 */
val KumihoShapes = Shapes(
    extraSmall = RoundedCornerShape(6.dp),
    small = RoundedCornerShape(8.dp),
    medium = RoundedCornerShape(12.dp),
    large = RoundedCornerShape(16.dp),
    // Material3 dialogs use extraLarge through AlertDialogDefaults. Keep it at
    // the house dialog radius; PillShape is the explicit full-radius shape.
    extraLarge = RoundedCornerShape(16.dp),
)

/** An explicit pill radius, for chips and progress bars. */
val PillShape = RoundedCornerShape(percent = 50)
