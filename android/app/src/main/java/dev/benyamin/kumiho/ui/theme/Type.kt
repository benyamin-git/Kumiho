package dev.benyamin.kumiho.ui.theme

import androidx.compose.material3.Typography
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp

/** One family (the platform default, Segoe UI Variable on Windows 11 / Roboto). */
private val family = FontFamily.Default

/**
 * A closed scale, house rule: 12 / 14 / 16 / 20 / 24 / 32sp. Every Material slot
 * maps onto one of those six sizes; hierarchy is weight and size only, never a
 * second family.
 */
private fun style(
    size: Int,
    weight: FontWeight = FontWeight.Normal,
    lineHeight: Int = (size * 1.35f).toInt(),
) = TextStyle(
    fontFamily = family,
    fontWeight = weight,
    fontSize = size.sp,
    lineHeight = lineHeight.sp,
)

/** The house type scale. No slot leaves the 12/14/16/20/24/32 set. */
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
