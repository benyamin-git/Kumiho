package dev.benyamin.kumiho.tv

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsFocused
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performKeyInput
import androidx.compose.ui.test.pressKey
import androidx.tv.material3.Text
import dev.benyamin.kumiho.ui.shell.Destination
import dev.benyamin.kumiho.ui.shell.ShellNavigation
import dev.benyamin.kumiho.ui.tv.TvAppShell
import dev.benyamin.kumiho.ui.tv.TvShellTags
import dev.benyamin.kumiho.ui.tv.tvRailTag
import dev.benyamin.kumiho.ui.tv.theme.TvKumihoTheme
import org.junit.Test

/**
 * Remote-only traversal of the TV shell: the rail is one vertical list, every
 * tab is reachable with Up/Down, and the More page keeps the same focus order.
 */
class TvTraversalTest : TvTestBase() {

    @Test
    fun everyRailItemIsReachableWithTheDpad() {
        show()

        rule.onNodeWithTag(tvRailTag(Destination.Home)).assertIsFocused()
        Destination.tabs.drop(1).forEach { destination ->
            rule.onNodeWithTag(tvRailTag(destination)).performKeyInput {
                pressKey(Key.DirectionDown)
            }
            rule.waitForIdle()
            rule.onNodeWithTag(tvRailTag(destination)).assertIsFocused()
        }
        Destination.tabs.dropLast(1).reversed().forEach { destination ->
            rule.onNodeWithTag(tvRailTag(destination)).performKeyInput {
                pressKey(Key.DirectionUp)
            }
            rule.waitForIdle()
            rule.onNodeWithTag(tvRailTag(destination)).assertIsFocused()
        }
    }

    @Test
    fun morePageIsTraversableAndOpensItsRoutes() {
        show()

        rule.onNodeWithTag(tvRailTag(Destination.More)).performClick()
        rule.onNodeWithTag(TvShellTags.MORE).assertIsDisplayed()

        rule.onNodeWithTag(TvShellTags.MORE_LOGS).performClick()
        rule.onNodeWithTag("logs").assertIsDisplayed()

        rule.runOnUiThread { rule.activity.onBackPressedDispatcher.onBackPressed() }
        rule.waitForIdle()
        rule.onNodeWithTag(TvShellTags.MORE).assertIsDisplayed()

        rule.onNodeWithTag(TvShellTags.MORE_DIAGNOSTICS).performClick()
        rule.onNodeWithTag("diagnostics").assertIsDisplayed()
    }

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
        Text(text = tag, modifier = Modifier.fillMaxSize().testTag(tag))
    }
}
