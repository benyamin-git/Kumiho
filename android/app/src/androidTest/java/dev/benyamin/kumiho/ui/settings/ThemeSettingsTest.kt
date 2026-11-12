package dev.benyamin.kumiho.ui.settings

import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.LayoutDirection
import dev.benyamin.kumiho.go.mobile.Mobile
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * The theme row must redraw its segmented shapes when the app switches between
 * RTL and LTR. Material3 caches the directional outline on the button surface,
 * so without an explicit remount the row keeps the previous direction's corners
 * even though the layout bounds are already correct.
 */
class ThemeSettingsTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    @Test
    fun theThemeRowRedrawsItsShapesAfterADirectionChange() {
        var direction by mutableStateOf(LayoutDirection.Rtl)
        var generation by mutableIntStateOf(0)

        rule.setContent {
            KumihoTheme(
                colors = PaletteMapping.fromJson(
                    Mobile.palette(ThemeId.LIGHT.id, KumihoAccent.BLUE.id),
                    ThemeId.LIGHT,
                    KumihoAccent.BLUE,
                ),
            ) {
                CompositionLocalProvider(LocalLayoutDirection provides direction) {
                    key(generation) {
                        ThemeSetting(theme = ThemeId.LIGHT, onThemeSelected = {})
                    }
                }
            }
        }
        rule.waitForIdle()

        // Change the direction in place, as the language preference does.
        rule.runOnIdle { direction = LayoutDirection.Ltr }
        rule.waitForIdle()
        val switched = capture()

        // A fresh LTR composition is the reference: it never saw RTL.
        rule.runOnIdle { generation++ }
        rule.waitForIdle()
        val fresh = capture()

        assertTrue(
            "the theme row kept its old RTL shapes after switching to LTR",
            switched.sameAs(fresh),
        )
    }

    private fun capture(): Bitmap = rule.onNodeWithTag(SettingsTags.THEME_CONTROL)
        .captureToImage()
        .asAndroidBitmap()
}
