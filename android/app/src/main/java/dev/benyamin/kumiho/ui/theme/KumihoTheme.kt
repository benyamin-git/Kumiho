package dev.benyamin.kumiho.ui.theme

import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.platform.LocalView
import androidx.core.view.WindowCompat

/**
 * The active [KumihoColors]. Provided by [KumihoTheme]; components read status
 * roles (profit/loss/warning) that Material's scheme has no slot for.
 */
val LocalKumihoColors = staticCompositionLocalOf<KumihoColors> {
    error("KumihoTheme is not applied")
}

/** The house colours for the current theme, for reads inside components. */
object KumihoTheme {
    val colors: KumihoColors
        @Composable
        @ReadOnlyComposable
        get() = LocalKumihoColors.current
}

/**
 * Applies the house palette, type scale and shapes. There is no dynamic colour
 * and no Material You: every role comes from the Go palette.
 */
@Composable
fun KumihoTheme(
    colors: KumihoColors,
    content: @Composable () -> Unit,
) {
    ApplySystemBarAppearance(dark = colors.theme != ThemeId.LIGHT)
    CompositionLocalProvider(LocalKumihoColors provides colors) {
        MaterialTheme(
            colorScheme = colors.toColorScheme(),
            typography = KumihoTypography,
            shapes = KumihoShapes,
            content = content,
        )
    }
}

/**
 * Status- and navigation-bar icon appearance follows the house theme, not the
 * OS night mode: a stored Dark or OLED choice on a light system must not leave
 * dark system icons on a near-black surface.
 */
@Composable
private fun ApplySystemBarAppearance(dark: Boolean) {
    val view = LocalView.current
    if (view.isInEditMode) return
    SideEffect {
        val window = view.context.findActivity()?.window ?: return@SideEffect
        val controller = WindowCompat.getInsetsController(window, view)
        controller.isAppearanceLightStatusBars = !dark
        controller.isAppearanceLightNavigationBars = !dark
    }
}

private tailrec fun Context.findActivity(): Activity? = when (this) {
    is Activity -> this
    is ContextWrapper -> baseContext.findActivity()
    else -> null
}
