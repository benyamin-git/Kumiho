package dev.benyamin.kumiho.ui.tv.theme

import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/**
 * TV geometry: the 8dp grid inside the Android TV overscan-safe frame, with
 * 48dp minimum targets. The horizontal/vertical overscan constants come from
 * the Android TV guidance and are applied by the TV shell, not by screens.
 */
object TvDimens {
    val OverscanHorizontal: Dp = 48.dp
    val OverscanVertical: Dp = 27.dp
    val Space1: Dp = 8.dp
    val Space2: Dp = 16.dp
    val Space3: Dp = 24.dp
    val Space4: Dp = 32.dp
    val MinTarget: Dp = 48.dp

    /** The left navigation rail. */
    val RailWidth: Dp = 232.dp

    /** A standard TV card inset. */
    val CardPadding: Dp = 20.dp

    /** Focus ring thickness. */
    val FocusRing: Dp = 2.dp
}
