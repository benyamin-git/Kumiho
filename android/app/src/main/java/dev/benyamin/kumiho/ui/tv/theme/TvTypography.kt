package dev.benyamin.kumiho.ui.tv.theme

import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.sp
import androidx.tv.material3.Typography
import dev.benyamin.kumiho.data.AppLanguage
import dev.benyamin.kumiho.ui.theme.KumihoPersianFontFamily

/**
 * The ten-foot type scale: exactly 16/18/20/24/32/40sp, one family. This is the
 * approved house-style departure from the phone's 12/14/16/20/24/32sp scale.
 */
val TvTypography: Typography = Typography(
    displayLarge = tvStyle(40, 48),
    displayMedium = tvStyle(40, 48),
    displaySmall = tvStyle(32, 40),
    headlineLarge = tvStyle(32, 40),
    headlineMedium = tvStyle(24, 32),
    headlineSmall = tvStyle(24, 32),
    titleLarge = tvStyle(24, 32),
    titleMedium = tvStyle(20, 28),
    titleSmall = tvStyle(18, 24),
    bodyLarge = tvStyle(18, 26),
    bodyMedium = tvStyle(18, 26),
    bodySmall = tvStyle(16, 22),
    labelLarge = tvStyle(18, 24),
    labelMedium = tvStyle(16, 22),
    labelSmall = tvStyle(16, 20),
)

/** Persian keeps the TV sizes with Vazirmatn and more line height. */
val PersianTvTypography: Typography = Typography(
    displayLarge = tvStyle(40, 56, KumihoPersianFontFamily),
    displayMedium = tvStyle(40, 56, KumihoPersianFontFamily),
    displaySmall = tvStyle(32, 46, KumihoPersianFontFamily),
    headlineLarge = tvStyle(32, 46, KumihoPersianFontFamily),
    headlineMedium = tvStyle(24, 34, KumihoPersianFontFamily),
    headlineSmall = tvStyle(24, 34, KumihoPersianFontFamily),
    titleLarge = tvStyle(24, 34, KumihoPersianFontFamily),
    titleMedium = tvStyle(20, 28, KumihoPersianFontFamily),
    titleSmall = tvStyle(18, 26, KumihoPersianFontFamily),
    bodyLarge = tvStyle(18, 30, KumihoPersianFontFamily),
    bodyMedium = tvStyle(18, 30, KumihoPersianFontFamily),
    bodySmall = tvStyle(16, 27, KumihoPersianFontFamily),
    labelLarge = tvStyle(18, 27, KumihoPersianFontFamily),
    labelMedium = tvStyle(16, 24, KumihoPersianFontFamily),
    labelSmall = tvStyle(16, 24, KumihoPersianFontFamily),
)

/** The TV type scale for the effective language. */
fun tvTypography(language: AppLanguage): Typography =
    if (language == AppLanguage.PERSIAN) PersianTvTypography else TvTypography

private fun tvStyle(
    size: Int,
    lineHeight: Int,
    family: FontFamily = FontFamily.Default,
): TextStyle = TextStyle(
    fontFamily = family,
    fontSize = size.sp,
    lineHeight = lineHeight.sp,
)
