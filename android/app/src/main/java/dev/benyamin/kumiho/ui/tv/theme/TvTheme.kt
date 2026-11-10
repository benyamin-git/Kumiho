package dev.benyamin.kumiho.ui.tv.theme

import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.ui.unit.dp
import androidx.tv.material3.ColorScheme
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Shapes
import dev.benyamin.kumiho.data.AppLanguage
import dev.benyamin.kumiho.ui.theme.KumihoColors
import dev.benyamin.kumiho.ui.theme.KumihoShapes
import dev.benyamin.kumiho.ui.theme.LocalAppLanguage
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors

/**
 * Applies the house palette to both Material themes. No raw hex lives here:
 * every role is the matching [KumihoColors] role. The phone `KumihoTheme`
 * remains the only theme for phone routes; the Material 3 theme is supplied
 * too because the shared sign-in and editor text fields are Material 3.
 */
@Composable
fun TvKumihoTheme(
    colors: KumihoColors,
    language: AppLanguage = AppLanguage.ENGLISH,
    content: @Composable () -> Unit,
) {
    CompositionLocalProvider(
        LocalKumihoColors provides colors,
        LocalAppLanguage provides language,
    ) {
        androidx.compose.material3.MaterialTheme(
            colorScheme = colors.toColorScheme(),
            typography = if (language == AppLanguage.PERSIAN) {
                PersianTvM3Typography
            } else {
                EnglishTvM3Typography
            },
            shapes = KumihoShapes,
        ) {
            MaterialTheme(
                colorScheme = colors.toTvColorScheme(),
                typography = tvTypography(language),
                shapes = TvShapes,
                content = content,
            )
        }
    }
}

/** The TV radius scale mirrors the house 6/8/12/16 scale. */
val TvShapes = Shapes(
    extraSmall = RoundedCornerShape(6.dp),
    small = RoundedCornerShape(8.dp),
    medium = RoundedCornerShape(12.dp),
    large = RoundedCornerShape(16.dp),
    extraLarge = RoundedCornerShape(16.dp),
)

/**
 * The same TV scale expressed for Compose Material 3, used only where a shared
 * phone pane (sign-in, browser waiting) is embedded in a TV page.
 */
val TvM3Typography: androidx.compose.material3.Typography
    @Composable
    @ReadOnlyComposable
    get() = if (LocalAppLanguage.current == AppLanguage.PERSIAN) {
        PersianTvM3Typography
    } else {
        EnglishTvM3Typography
    }

private val EnglishTvM3Typography = androidx.compose.material3.Typography(
    displayLarge = TvTypography.displayLarge,
    displayMedium = TvTypography.displayMedium,
    displaySmall = TvTypography.displaySmall,
    headlineLarge = TvTypography.headlineLarge,
    headlineMedium = TvTypography.headlineMedium,
    headlineSmall = TvTypography.headlineSmall,
    titleLarge = TvTypography.titleLarge,
    titleMedium = TvTypography.titleMedium,
    titleSmall = TvTypography.titleSmall,
    bodyLarge = TvTypography.bodyLarge,
    bodyMedium = TvTypography.bodyMedium,
    bodySmall = TvTypography.bodySmall,
    labelLarge = TvTypography.labelLarge,
    labelMedium = TvTypography.labelMedium,
    labelSmall = TvTypography.labelSmall,
)

private val PersianTvM3Typography = androidx.compose.material3.Typography(
    displayLarge = PersianTvTypography.displayLarge,
    displayMedium = PersianTvTypography.displayMedium,
    displaySmall = PersianTvTypography.displaySmall,
    headlineLarge = PersianTvTypography.headlineLarge,
    headlineMedium = PersianTvTypography.headlineMedium,
    headlineSmall = PersianTvTypography.headlineSmall,
    titleLarge = PersianTvTypography.titleLarge,
    titleMedium = PersianTvTypography.titleMedium,
    titleSmall = PersianTvTypography.titleSmall,
    bodyLarge = PersianTvTypography.bodyLarge,
    bodyMedium = PersianTvTypography.bodyMedium,
    bodySmall = PersianTvTypography.bodySmall,
    labelLarge = PersianTvTypography.labelLarge,
    labelMedium = PersianTvTypography.labelMedium,
    labelSmall = PersianTvTypography.labelSmall,
)

/**
 * TV Material has a different role list than Material 3. Status colours live in
 * [LocalKumihoColors] and are read directly by TV components.
 */
fun KumihoColors.toTvColorScheme(): ColorScheme = ColorScheme(
    primary = primary,
    onPrimary = onPrimary,
    primaryContainer = primaryContainer,
    onPrimaryContainer = onPrimaryContainer,
    inversePrimary = inversePrimary,
    secondary = secondary,
    onSecondary = onSecondary,
    secondaryContainer = secondaryContainer,
    onSecondaryContainer = onSecondaryContainer,
    tertiary = tertiary,
    onTertiary = onTertiary,
    tertiaryContainer = tertiaryContainer,
    onTertiaryContainer = onTertiaryContainer,
    background = background,
    onBackground = onBackground,
    surface = surface,
    onSurface = onSurface,
    surfaceVariant = surfaceContainerLow,
    onSurfaceVariant = onSurfaceVariant,
    surfaceTint = surfaceTint,
    inverseSurface = inverseSurface,
    inverseOnSurface = inverseOnSurface,
    error = error,
    onError = onError,
    errorContainer = errorContainer,
    onErrorContainer = onErrorContainer,
    border = outline,
    borderVariant = outlineVariant,
    scrim = scrim,
)
