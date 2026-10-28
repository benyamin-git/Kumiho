package dev.benyamin.kumiho.ui.tv.theme

import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.sp
import androidx.tv.material3.Typography

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

private fun tvStyle(size: Int, lineHeight: Int): TextStyle = TextStyle(
    fontFamily = FontFamily.Default,
    fontSize = size.sp,
    lineHeight = lineHeight.sp,
)
