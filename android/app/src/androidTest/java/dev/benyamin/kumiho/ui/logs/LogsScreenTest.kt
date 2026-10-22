package dev.benyamin.kumiho.ui.logs

import androidx.activity.ComponentActivity
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextReplacement
import dev.benyamin.kumiho.engine.LogEntry
import dev.benyamin.kumiho.engine.LogLevel
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import java.time.Instant
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * The logs console as drawn: the list, the level chips, search, Follow, Clear,
 * Share and per-line copy. Every callback is recorded; no engine or file is
 * touched.
 */
class LogsScreenTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private val calls = mutableListOf<String>()

    private fun show(state: LogsUiState, launchError: String? = null) {
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
                LogsScreen(
                    state = state,
                    launchError = launchError,
                    actions = LogsActions(
                        onLevelSelected = { calls += "level-${it.name}" },
                        onSearchChange = { calls += "search-$it" },
                        onClearSearch = { calls += "clear-search" },
                        onFollowChange = { calls += "follow-$it" },
                        onClear = { calls += "clear" },
                        onCopyLine = { calls += "copy-${it.message}" },
                        onShare = { calls += "share" },
                        onDismissMessage = { calls += "dismiss" },
                        onRetry = { calls += "retry" },
                    ),
                )
            }
        }
        rule.waitForIdle()
    }

    @Test
    fun theConsoleListsEntriesAndTheCopyButtonForwardsTheLine() {
        show(LogsUiState(loading = false, entries = ENTRIES))

        rule.onNodeWithTag(LogsTags.SCREEN).assertIsDisplayed()
        rule.onNodeWithTag(LogsTags.LIST).assertIsDisplayed()
        rule.onNodeWithText("connected").assertIsDisplayed()
        rule.onNodeWithText("token rejected").assertIsDisplayed()

        rule.onNodeWithTag(LogsTags.copy(1)).performClick()
        rule.waitForIdle()
        assertTrue(calls.contains("copy-token rejected"))
    }

    @Test
    fun eachLevelChipReportsItsSelection() {
        show(LogsUiState(loading = false, entries = ENTRIES))

        for (level in LogFilterLevel.entries) {
            rule.onNodeWithTag(LogsTags.filter(level)).performClick()
        }
        rule.waitForIdle()

        assertTrue(calls.contains("level-ALL"))
        assertTrue(calls.contains("level-DEBUG"))
        assertTrue(calls.contains("level-INFO"))
        assertTrue(calls.contains("level-WARN"))
        assertTrue(calls.contains("level-ERROR"))
    }

    @Test
    fun typingInSearchReportsTheQueryAndFollowToggles() {
        show(LogsUiState(loading = false, entries = ENTRIES))

        rule.onNodeWithTag(LogsTags.SEARCH).performTextReplacement("token")
        rule.onNodeWithTag(LogsTags.FOLLOW).performClick()
        rule.waitForIdle()

        assertTrue(calls.contains("search-token"))
        assertTrue(calls.contains("follow-false"))
    }

    @Test
    fun clearAndShareCallTheirActions() {
        show(LogsUiState(loading = false, entries = ENTRIES))

        rule.onNodeWithTag(LogsTags.CLEAR).performClick()
        rule.onNodeWithTag(LogsTags.SHARE).performClick()
        rule.waitForIdle()

        assertTrue(calls.contains("clear"))
        assertTrue(calls.contains("share"))
    }

    @Test
    fun anEmptyConsoleExplainsItselfAndASearchMissNamesTheQuery() {
        show(LogsUiState(loading = false, entries = emptyList()))

        rule.onNodeWithTag(LogsTags.EMPTY).assertIsDisplayed()
        rule.onNodeWithText("No log lines").assertIsDisplayed()
    }

    @Test
    fun aSearchMissShowsTheQueryAndClearIsOffered() {
        show(LogsUiState(loading = false, entries = emptyList(), search = "edg"))

        rule.onNodeWithTag(LogsTags.SEARCH_CLEAR).assertIsDisplayed()
        rule.onNodeWithTag(LogsTags.EMPTY).assertIsDisplayed()
        rule.onNodeWithText("No line matches “edg” at this level.").assertIsDisplayed()

        rule.onNodeWithTag(LogsTags.SEARCH_CLEAR).performClick()
        rule.waitForIdle()
        assertTrue(calls.contains("clear-search"))
    }

    @Test
    fun aClearFailureBannerCanBeDismissed() {
        show(
            LogsUiState(
                loading = false,
                entries = ENTRIES,
                message = LogsMessage.CLEAR_FAILED,
                error = "the file is read-only",
            ),
        )

        rule.onNodeWithTag(LogsTags.MESSAGE_DISMISS).assertIsDisplayed().performClick()
        rule.waitForIdle()
        assertTrue(calls.contains("dismiss"))
    }

    @Test
    fun aShareFailureShowsTheLaunchError() {
        show(
            LogsUiState(loading = false, entries = ENTRIES, message = LogsMessage.EXPORT_FAILED),
            launchError = "No app can share this file.",
        )

        rule.onNodeWithText("No app can share this file.").assertIsDisplayed()
    }

    private companion object {
        val ENTRIES = listOf(
            LogEntry(
                time = Instant.parse("2026-10-22T12:00:01Z"),
                level = LogLevel.DEBUG,
                tag = "net",
                message = "connected",
            ),
            LogEntry(
                time = Instant.parse("2026-10-22T12:00:02Z"),
                level = LogLevel.ERROR,
                tag = "auth",
                message = "token rejected",
            ),
        )
    }
}
