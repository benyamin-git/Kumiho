package dev.benyamin.kumiho.ui.theme

import androidx.compose.material3.Typography
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.data.AppLanguage

/** One family for English (Segoe UI Variable on Windows 11 / Roboto). */
private val systemFamily = FontFamily.Default

/** Vazirmatn is the only extra family, and only for Persian. */
internal val KumihoPersianFontFamily = FontFamily(
    Font(R.font.vazirmatn_regular, FontWeight.Normal),
    Font(R.font.vazirmatn_medium, FontWeight.Medium),
    Font(R.font.vazirmatn_semibold, FontWeight.SemiBold),
    Font(R.font.vazirmatn_bold, FontWeight.Bold),
)

/**
 * A closed scale, house rule: 12 / 14 / 16 / 20 / 24 / 32sp. Every Material slot
 * maps onto one of those six sizes; hierarchy is weight and size only, never a
 * second family.
 */
private fun style(
    size: Int,
    weight: FontWeight = FontWeight.Normal,
    lineHeight: Int = (size * 1.35f).toInt(),
    family: FontFamily = systemFamily,
) = TextStyle(
    fontFamily = family,
    fontWeight = weight,
    fontSize = size.sp,
    lineHeight = lineHeight.sp,
)

/** The English house type scale. */
val KumihoTypography = Typography(
    displayLarge = style(32, FontWeight.SemiBold, lineHeight = 40),
    displayMedium = style(24, FontWeight.SemiBold, lineHeight = 32),
    displaySmall = style(24, lineHeight = 32),
    headlineLarge = style(24, FontWeight.SemiBold, lineHeight = 32),
    headlineMedium = style(20, FontWeight.SemiBold, lineHeight = 28),
    headlineSmall = style(20, FontWeight.Medium, lineHeight = 28),
    titleLarge = style(20, FontWeight.Medium, lineHeight = 28),
    titleMedium = style(16, FontWeight.Medium, lineHeight = 24),
    titleSmall = style(14, FontWeight.Medium, lineHeight = 20),
    bodyLarge = style(16, lineHeight = 24),
    bodyMedium = style(14, lineHeight = 20),
    bodySmall = style(12, lineHeight = 16),
    labelLarge = style(14, FontWeight.Medium, lineHeight = 20),
    labelMedium = style(12, FontWeight.Medium, lineHeight = 16),
    labelSmall = style(12, lineHeight = 16),
)

/**
 * Persian keeps the six house sizes. Vazirmatn needs more vertical room:
 * body at 1.6–1.75, labels at 1.5 and headings at 1.4.
 */
val PersianKumihoTypography = Typography(
    displayLarge = style(32, FontWeight.SemiBold, lineHeight = 44, family = KumihoPersianFontFamily),
    displayMedium = style(24, FontWeight.SemiBold, lineHeight = 34, family = KumihoPersianFontFamily),
    displaySmall = style(24, lineHeight = 34, family = KumihoPersianFontFamily),
    headlineLarge = style(24, FontWeight.SemiBold, lineHeight = 34, family = KumihoPersianFontFamily),
    headlineMedium = style(20, FontWeight.SemiBold, lineHeight = 28, family = KumihoPersianFontFamily),
    headlineSmall = style(20, FontWeight.Medium, lineHeight = 28, family = KumihoPersianFontFamily),
    titleLarge = style(20, FontWeight.Medium, lineHeight = 28, family = KumihoPersianFontFamily),
    titleMedium = style(16, FontWeight.Medium, lineHeight = 24, family = KumihoPersianFontFamily),
    titleSmall = style(14, FontWeight.Medium, lineHeight = 20, family = KumihoPersianFontFamily),
    bodyLarge = style(16, lineHeight = 28, family = KumihoPersianFontFamily),
    bodyMedium = style(14, lineHeight = 24, family = KumihoPersianFontFamily),
    bodySmall = style(12, lineHeight = 20, family = KumihoPersianFontFamily),
    labelLarge = style(14, FontWeight.Medium, lineHeight = 22, family = KumihoPersianFontFamily),
    labelMedium = style(12, FontWeight.Medium, lineHeight = 18, family = KumihoPersianFontFamily),
    labelSmall = style(12, lineHeight = 18, family = KumihoPersianFontFamily),
)

/** The type scale for the effective language. */
fun kumihoTypography(language: AppLanguage): Typography =
    if (language == AppLanguage.PERSIAN) PersianKumihoTypography else KumihoTypography
