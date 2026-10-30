package dev.benyamin.kumiho.ui.shell

import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import org.junit.Rule
import org.junit.Test

/**
 * The shell's T15 wiring: the More sheet opens Logs and Diagnostics as routes,
 * the Account tab renders its slot, and back returns to the tab underneath.
 */
class ShellMoreRoutesTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private var state by mutableStateOf(ShellNavState())

    private fun show(widthDp: Int? = null) {
        rule.setContent {
            KumihoTheme(
                colors = PaletteMapping.fromJson(
                    dev.benyamin.kumiho.go.mobile.Mobile.palette(
                        ThemeId.LIGHT.id,
                        KumihoAccent.BLUE.id,
                    ),
                    ThemeId.LIGHT,
                    KumihoAccent.BLUE,
                ),
            ) {
                if (widthDp == null) {
                    Shell()
                } else {
                    Box(modifier = Modifier.width(widthDp.dp).fillMaxHeight()) {
                        Shell()
                    }
                }
            }
        }
        rule.waitForIdle()
    }

    @Composable
    private fun Shell() {
        AppShell(
            state = state,
            onSelectTab = { state = state.copy(tab = it, moreRoute = null) },
            onOpenMoreRoute = { state = state.copy(moreRoute = it) },
            onBack = {
                if (state.moreRoute != null) {
                    state = state.copy(moreRoute = null)
                    true
                } else {
                    false
                }
            },
            home = { Slot("home") },
            locations = { Slot("locations") },
            account = { Slot("account") },
            settings = { Slot("settings") },
            logs = { Slot("logs") },
            diagnostics = { Slot("diagnostics") },
        )
    }

    @Test
    fun theMoreSheetOpensTheLogsRoute() {
        show()
        rule.onNodeWithTag("slot_home").assertIsDisplayed()

        rule.onNodeWithText("More").performClick()
        rule.waitForIdle()
        rule.onNodeWithText("Logs").performClick()
        rule.waitForIdle()

        rule.onNodeWithTag("slot_logs").assertIsDisplayed()
    }

    @Test
    fun theMoreSheetOpensTheDiagnosticsRoute() {
        show()

        rule.onNodeWithText("More").performClick()
        rule.waitForIdle()
        rule.onNodeWithText("Diagnostics").performClick()
        rule.waitForIdle()

        rule.onNodeWithTag("slot_diagnostics").assertIsDisplayed()
    }

    @Test
    fun theAccountTabRendersItsSlot() {
        show()

        rule.onNodeWithText("Account").performClick()
        rule.waitForIdle()

        rule.onNodeWithTag("slot_account").assertIsDisplayed()
    }

    /**
     * The regression this guards: on a narrow phone the five labels do not fit,
     * and "Locations" used to wrap its last letter instead of the bar giving a
     * page up. 200dp is a stress width, chosen so the assertion does not ride
     * on the exact label metrics of one Roboto version.
     */
    @Test
    fun aNarrowBarMovesSettingsIntoTheMoreSheet() {
        show(widthDp = 200)

        rule.onNodeWithText("Locations").assertIsDisplayed()
        rule.onNodeWithText("Settings").assertDoesNotExist()

        rule.onNodeWithText("More").performClick()
        rule.waitForIdle()
        rule.onNodeWithText("Settings").performClick()
        rule.waitForIdle()

        rule.onNodeWithTag("slot_settings").assertIsDisplayed()
    }

    @Composable
    private fun Slot(name: String) {
        Text(text = "$name slot", modifier = Modifier.testTag("slot_$name"))
    }
}
