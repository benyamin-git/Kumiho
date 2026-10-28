package dev.benyamin.kumiho.ui.tv.theme

import androidx.compose.ui.text.font.FontFamily
import org.junit.Assert.assertEquals
import org.junit.Test

class TvTypographyTest {
    private val styles = listOf(
        TvTypography.displayLarge, TvTypography.displayMedium, TvTypography.displaySmall,
        TvTypography.headlineLarge, TvTypography.headlineMedium, TvTypography.headlineSmall,
        TvTypography.titleLarge, TvTypography.titleMedium, TvTypography.titleSmall,
        TvTypography.bodyLarge, TvTypography.bodyMedium, TvTypography.bodySmall,
        TvTypography.labelLarge, TvTypography.labelMedium, TvTypography.labelSmall,
    )

    @Test
    fun theScaleUsesOnlyTheSixApprovedSizes() {
        val sizes = styles.map { it.fontSize.value }.toSet()
        assertEquals(setOf(16f, 18f, 20f, 24f, 32f, 40f), sizes)
    }

    @Test
    fun everyStyleUsesTheSystemFamily() {
        styles.forEach { assertEquals(FontFamily.Default, it.fontFamily) }
    }

    @Test
    fun bodyTextIsEighteenAndLabelsAreAtLeastSixteen() {
        assertEquals(18f, TvTypography.bodyLarge.fontSize.value)
        assertEquals(18f, TvTypography.bodyMedium.fontSize.value)
        assertEquals(16f, TvTypography.bodySmall.fontSize.value)
        assertEquals(16f, TvTypography.labelMedium.fontSize.value)
    }
}
