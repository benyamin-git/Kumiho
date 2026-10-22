package dev.benyamin.kumiho.ui.shell

import androidx.activity.ComponentActivity
import androidx.compose.material3.Text
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

    private fun show() {
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
        }
        rule.waitForIdle()
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

    @androidx.compose.runtime.Composable
    private fun Slot(name: String) {
        Text(text = "$name slot", modifier = Modifier.testTag("slot_$name"))
    }
}
