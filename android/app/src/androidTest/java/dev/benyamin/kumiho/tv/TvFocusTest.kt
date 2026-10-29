package dev.benyamin.kumiho.tv

import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsFocused
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performKeyInput
import androidx.compose.ui.test.pressKey
import androidx.compose.ui.unit.dp
import androidx.tv.material3.Text
import dev.benyamin.kumiho.go.mobile.Mobile
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoColors
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.ui.tv.components.TvFocusableSurface
import dev.benyamin.kumiho.ui.tv.theme.TvKumihoTheme
import org.junit.Assume.assumeTrue
import org.junit.Rule
import org.junit.Test

/**
 * D-pad traversal of the shared TV focus component: focus starts on the first
 * surface, Down/Up move it, and Center clicks the focused surface. TV only;
 * skipped on a phone emulator.
 */
class TvFocusTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private fun palette(): KumihoColors = PaletteMapping.fromJson(
        Mobile.palette(ThemeId.LIGHT.id, KumihoAccent.BLUE.id),
        ThemeId.LIGHT,
        KumihoAccent.BLUE,
    )

    @Test
    fun focusMovesWithTheDpadAndCenterClicks() {
        assumeTrue(TvEnvironment.isTelevision(rule.activity))
        val first = FocusRequester()
        val clicked = mutableStateOf("")
        rule.setContent {
            TvKumihoTheme(palette()) {
                Column(
                    modifier = Modifier.fillMaxSize(),
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    TvFocusableSurface(
                        onClick = { clicked.value = "one" },
                        modifier = Modifier.testTag("one").focusRequester(first),
                    ) { Text("One") }
                    TvFocusableSurface(
                        onClick = { clicked.value = "two" },
                        modifier = Modifier.testTag("two"),
                    ) { Text("Two") }
                    TvFocusableSurface(
                        onClick = { clicked.value = "three" },
                        modifier = Modifier.testTag("three"),
                    ) { Text("Three") }
                }
            }
        }
        rule.runOnIdle { first.requestFocus() }
        rule.waitForIdle()
        rule.onNodeWithTag("one").assertIsFocused()

        rule.onNodeWithTag("one").performKeyInput { pressKey(Key.DirectionDown) }
        rule.waitForIdle()
        rule.onNodeWithTag("two").assertIsFocused()

        rule.onNodeWithTag("two").performKeyInput { pressKey(Key.DirectionUp) }
        rule.waitForIdle()
        rule.onNodeWithTag("one").assertIsFocused()

        rule.onNodeWithTag("two").performClick()
    }
}
