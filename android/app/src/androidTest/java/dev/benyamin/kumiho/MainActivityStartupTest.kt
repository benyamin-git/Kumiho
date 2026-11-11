package dev.benyamin.kumiho

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import org.junit.Rule
import org.junit.Test

/**
 * Launches the real application path, not a bare ComponentActivity. The app
 * root replaces LocalContext with a configuration context; this test keeps the
 * setup flow's activity-result launchers covered after that swap.
 */
class MainActivityStartupTest {
    @get:Rule
    val rule = createAndroidComposeRule<MainActivity>()

    @Test
    fun setupRendersAfterStartup() {
        val title = rule.activity.getString(R.string.setup_title)
        rule.waitUntil(timeoutMillis = 30_000) {
            rule.onAllNodesWithText(title).fetchSemanticsNodes().isNotEmpty()
        }
        rule.onNodeWithText(title).assertIsDisplayed()
    }
}
