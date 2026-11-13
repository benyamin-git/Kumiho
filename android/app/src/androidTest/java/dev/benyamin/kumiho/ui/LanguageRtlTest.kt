package dev.benyamin.kumiho.ui

import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.requiredSize
import androidx.compose.foundation.layout.requiredWidth
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onFirst
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import dev.benyamin.kumiho.data.AppLanguage
import dev.benyamin.kumiho.go.mobile.Mobile
import dev.benyamin.kumiho.ui.shell.AppShell
import dev.benyamin.kumiho.ui.shell.ShellNavState
import dev.benyamin.kumiho.ui.shell.ShellTags
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import java.util.Locale
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * The phone shell in Persian: the layout direction, the mirrored navigation
 * order and the Persian labels, at both the bottom-bar and rail breakpoints.
 */
class LanguageRtlTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private var direction by mutableStateOf(LayoutDirection.Ltr)

    private fun show(language: AppLanguage, width: Int = 420) {
        rule.setContent {
            val base = LocalContext.current
            val baseConfiguration = LocalConfiguration.current
            val localized = remember(language, baseConfiguration) {
                val locale = Locale.forLanguageTag(language.tag)
                val configuration = android.content.res.Configuration(
                    baseConfiguration,
                ).apply {
                    setLocale(locale)
                    setLayoutDirection(locale)
                }
                base.createConfigurationContext(configuration)
            }
            CompositionLocalProvider(
                LocalContext provides localized,
                LocalConfiguration provides localized.resources.configuration,
                LocalLayoutDirection provides if (language == AppLanguage.PERSIAN) {
                    LayoutDirection.Rtl
                } else {
                    LayoutDirection.Ltr
                },
            ) {
                direction = LocalLayoutDirection.current
                KumihoTheme(
                    colors = PaletteMapping.fromJson(
                        Mobile.palette(ThemeId.LIGHT.id, KumihoAccent.BLUE.id),
                        ThemeId.LIGHT,
                        KumihoAccent.BLUE,
                    ),
                    language = language,
                ) {
                    Box(
                        modifier = if (width >= 600) {
                            Modifier.requiredSize(width.dp, 700.dp)
                        } else {
                            Modifier.requiredWidth(width.dp).fillMaxHeight()
                        },
                    ) {
                        AppShell(
                            state = ShellNavState(),
                            onSelectTab = {},
                            onOpenMoreRoute = {},
                            onBack = { false },
                            home = {},
                            locations = {},
                            settings = {},
                            logs = {},
                            diagnostics = {},
                        )
                    }
                }
            }
        }
        rule.waitForIdle()
    }

    private fun bounds(tag: String) =
        rule.onNodeWithTag(tag).fetchSemanticsNode().boundsInRoot

    @Test
    fun persianMakesThePhoneShellRtl() {
        show(AppLanguage.PERSIAN)

        assertEquals(LayoutDirection.Rtl, direction)
        rule.onAllNodesWithText("خانه").onFirst().assertIsDisplayed()
        rule.onNodeWithTag(ShellTags.BOTTOM_BAR).assertExists()
    }

    @Test
    fun persianPutsTheFirstBottomBarItemOnTheRight() {
        show(AppLanguage.PERSIAN)

        val home = bounds(ShellTags.tab("home"))
        val locations = bounds(ShellTags.tab("locations"))
        assertTrue(
            "Home must be the rightmost item in RTL",
            home.center.x > locations.center.x,
        )
    }

    @Test
    fun persianRailSitsAtTheRightEdgeAndReplacesTheBottomBar() {
        show(AppLanguage.PERSIAN, width = 800)

        rule.onNodeWithTag(ShellTags.RAIL).assertExists()
        rule.onNodeWithTag(ShellTags.BOTTOM_BAR).assertDoesNotExist()

        val rail = bounds(ShellTags.RAIL)
        val root = rule.onRoot().fetchSemanticsNode().boundsInRoot
        assertTrue("The rail must be on the right half in RTL", rail.center.x > root.center.x)
    }

    @Test
    fun englishKeepsTheFirstBottomBarItemOnTheLeft() {
        show(AppLanguage.ENGLISH)

        assertEquals(LayoutDirection.Ltr, direction)
        val home = bounds(ShellTags.tab("home"))
        val locations = bounds(ShellTags.tab("locations"))
        assertTrue(home.center.x < locations.center.x)
    }
}
