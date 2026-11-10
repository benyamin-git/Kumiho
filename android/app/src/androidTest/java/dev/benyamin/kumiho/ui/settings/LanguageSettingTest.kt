package dev.benyamin.kumiho.ui.settings

import androidx.activity.ComponentActivity
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import dev.benyamin.kumiho.data.AppLanguage
import dev.benyamin.kumiho.go.mobile.Mobile
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/** The app-level language row: selection, the System note and its retry. */
class LanguageSettingTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private fun show(
        preference: String = "system",
        applying: Boolean = false,
        rawError: String? = null,
        onSelected: (String) -> Unit = {},
        onRetry: () -> Unit = {},
    ) {
        rule.setContent {
            KumihoTheme(
                colors = PaletteMapping.fromJson(
                    Mobile.palette(ThemeId.LIGHT.id, KumihoAccent.BLUE.id),
                    ThemeId.LIGHT,
                    KumihoAccent.BLUE,
                ),
                language = if (preference == "fa") AppLanguage.PERSIAN else AppLanguage.ENGLISH,
            ) {
                LanguageSetting(
                    preference = preference,
                    effective = if (preference == "fa") AppLanguage.PERSIAN else AppLanguage.ENGLISH,
                    applying = applying,
                    rawError = rawError,
                    onSelected = onSelected,
                    onRetry = onRetry,
                )
            }
        }
        rule.waitForIdle()
    }

    @Test
    fun everyLanguageIsReachableAndReportsItsValue() {
        val selected = mutableListOf<String>()
        show(onSelected = selected::add)

        rule.onNodeWithTag(SettingsTags.languageOption("system")).performClick()
        rule.onNodeWithTag(SettingsTags.languageOption("en")).performClick()
        rule.onNodeWithTag(SettingsTags.languageOption("fa")).performClick()

        assertEquals(listOf("system", "en", "fa"), selected)
    }

    @Test
    fun systemShowsTheResolvedLanguageNote() {
        show()

        rule.onNodeWithTag(SettingsTags.LANGUAGE_SYSTEM_NOTE).assertExists()
    }

    @Test
    fun aFailedWriteShowsTheRetryActionAndCanRetry() {
        var retried = 0
        show(rawError = "settings_set failed", onRetry = { retried++ })

        rule.onNodeWithTag(SettingsTags.LANGUAGE_ERROR).assertExists()
        rule.onNodeWithTag(SettingsTags.LANGUAGE_RETRY).performClick()
        assertTrue(retried == 1)
    }

    @Test
    fun anApplyingRowDisablesItsOptions() {
        show(applying = true)

        rule.onNodeWithTag(SettingsTags.languageOption("fa")).assertIsNotEnabled()
    }
}
