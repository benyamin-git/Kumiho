package dev.benyamin.kumiho.tv

import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsFocused
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performKeyInput
import androidx.compose.ui.test.pressKey
import dev.benyamin.kumiho.go.mobile.Mobile
import dev.benyamin.kumiho.ui.shell.Destination
import dev.benyamin.kumiho.ui.shell.ShellNavigation
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoColors
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.ui.tv.TvAppShell
import dev.benyamin.kumiho.ui.tv.TvShellTags
import dev.benyamin.kumiho.ui.tv.tvRailTag
import dev.benyamin.kumiho.ui.tv.theme.TvKumihoTheme
import org.junit.Assume.assumeTrue
import org.junit.Rule
import org.junit.Test

/**
 * The TV shell: the five rail items are reachable, tabs swap the content slot,
 * More opens the TV page rather than a sheet, Logs opens its slot, and Back
 * walks More -> tab. TV only; skipped on a phone emulator.
 */
class TvShellTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private fun show() {
        rule.setContent {
            TvKumihoTheme(palette()) {
                TvAppShell(
                    nav = ShellNavigation(),
                    home = { Screen("home") },
                    locations = { Screen("locations") },
                    account = { Screen("account") },
                    settings = { Screen("settings") },
                    logs = { Screen("logs") },
                    diagnostics = { Screen("diagnostics") },
                )
            }
        }
        rule.waitForIdle()
    }

    @Composable
    private fun Screen(tag: String) {
        androidx.compose.material3.Text(
            text = tag,
            modifier = Modifier.fillMaxSize().testTag(tag),
        )
    }

    @Test
    fun theRailIsTraversableAndEveryTabSwapsContent() {
        assumeTrue(TvEnvironment.isTelevision(rule.activity))
        show()

        rule.onNodeWithTag(tvRailTag(Destination.Home)).assertIsFocused()
        rule.onNodeWithTag(tvRailTag(Destination.Home)).performKeyInput { pressKey(Key.DirectionDown) }
        rule.onNodeWithTag(tvRailTag(Destination.Locations)).assertIsFocused()
        rule.onNodeWithTag(tvRailTag(Destination.Locations)).performClick()
        rule.onNodeWithTag("locations").assertIsDisplayed()

        rule.onNodeWithTag(tvRailTag(Destination.Account)).performClick()
        rule.onNodeWithTag("account").assertIsDisplayed()
        rule.onNodeWithTag(tvRailTag(Destination.Settings)).performClick()
        rule.onNodeWithTag("settings").assertIsDisplayed()
        rule.onNodeWithTag(tvRailTag(Destination.Home)).performClick()
        rule.onNodeWithTag("home").assertIsDisplayed()
    }

    @Test
    fun moreIsAPageAndLogsAndBackReturnToIt() {
        assumeTrue(TvEnvironment.isTelevision(rule.activity))
        show()

        rule.onNodeWithTag(tvRailTag(Destination.More)).performClick()
        rule.onNodeWithTag(TvShellTags.MORE).assertIsDisplayed()

        rule.onNodeWithTag(TvShellTags.MORE_LOGS).performClick()
        rule.onNodeWithTag("logs").assertIsDisplayed()

        rule.runOnUiThread { rule.activity.onBackPressedDispatcher.onBackPressed() }
        rule.waitForIdle()
        rule.onNodeWithTag(TvShellTags.MORE).assertIsDisplayed()

        rule.runOnUiThread { rule.activity.onBackPressedDispatcher.onBackPressed() }
        rule.waitForIdle()
        rule.onNodeWithTag("home").assertIsDisplayed()
    }

    private fun palette(): KumihoColors = PaletteMapping.fromJson(
        Mobile.palette(ThemeId.DARK.id, KumihoAccent.BLUE.id),
        ThemeId.DARK,
        KumihoAccent.BLUE,
    )
}
