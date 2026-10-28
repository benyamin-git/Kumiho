package dev.benyamin.kumiho.tv

import androidx.activity.ComponentActivity
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import dev.benyamin.kumiho.go.mobile.Mobile
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoColors
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import org.junit.Assume.assumeTrue
import org.junit.Before
import org.junit.Rule

/**
 * Shared setup for the TV connected tests: the composition rule, the real Go
 * palette, and the television assumption so the phone suite skips the TV
 * package instead of failing on a phone-shaped window.
 */
abstract class TvTestBase {
    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    @Before
    fun requireTelevision() {
        assumeTrue(TvEnvironment.isTelevision(rule.activity))
    }

    protected fun palette(theme: ThemeId = ThemeId.DARK): KumihoColors = PaletteMapping.fromJson(
        Mobile.palette(theme.id, KumihoAccent.BLUE.id),
        theme,
        KumihoAccent.BLUE,
    )
}
