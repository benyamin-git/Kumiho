package dev.benyamin.kumiho.ui.setup

import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onRoot
import androidx.test.platform.app.InstrumentationRegistry
import dev.benyamin.kumiho.engine.LoginState
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * Renders the four setup steps in the three house themes and writes one PNG per
 * (step, theme) pair to the app's external files directory, so the design
 * self-check can look at what the emulator actually drew rather than at the
 * source. The palette is the real Go palette; there is no fake colour.
 *
 * Pull the files with:
 * `adb pull /sdcard/Android/data/dev.benyamin.kumiho.debug/files/setup-screenshots`
 */
class SetupScreenshotsTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private var theme by mutableStateOf(ThemeId.LIGHT)

    @Test
    fun captureEveryStepInEveryTheme() {
        val engine = FakeSetupEngine()
        engine.onPassword = { LoginState("done", EMAIL, null, null, null) }
        val permissions = FakeSetupPermissions().apply {
            vpn = true
            notifications = false
            notificationsRuntimeRequired = true
        }
        val viewModel = SetupViewModel(
            engine = engine,
            permissions = permissions,
            persistSetupComplete = {},
            connectNow = {},
        )
        rule.setContent {
            KumihoTheme(
                colors = PaletteMapping.fromJson(
                    dev.benyamin.kumiho.go.mobile.Mobile.palette(theme.id, ACCENT.id),
                    theme,
                    ACCENT,
                ),
            ) {
                SetupFlow(viewModel = viewModel)
            }
        }
        val output = InstrumentationRegistry.getInstrumentation()
            .targetContext
            .getExternalFilesDir("setup-screenshots")
            ?: error("the app has no external files directory")
        output.mkdirs()

        capture(output, "1-consent")

        rule.runOnIdle {
            viewModel.setConsentAccepted(true)
            viewModel.next()
        }
        rule.waitUntil(TIMEOUT) { viewModel.state.value.step == SetupStep.SIGN_IN }
        capture(output, "2-sign-in")

        rule.runOnIdle { viewModel.submitEmail(EMAIL) }
        rule.waitUntil(TIMEOUT) { viewModel.state.value.signIn.phase == SignInPhase.PASSWORD }
        rule.runOnIdle { viewModel.submitPassword("hunter2") }
        rule.waitUntil(TIMEOUT) { viewModel.state.value.signedIn }

        permissions.alwaysOn = true
        rule.runOnIdle { viewModel.next() }
        rule.waitUntil(TIMEOUT) { viewModel.state.value.step == SetupStep.PERMISSIONS }
        capture(output, "3-permissions")

        rule.runOnIdle { viewModel.next() }
        rule.waitUntil(TIMEOUT) { viewModel.state.value.step == SetupStep.ALWAYS_ON }
        capture(output, "4-always-on")
    }

    private fun capture(output: File, step: String) {
        for (entry in ThemeId.entries) {
            rule.runOnIdle { theme = entry }
            rule.waitForIdle()
            val file = File(output, "t10-$step-${entry.id}.png")
            file.outputStream().use { stream ->
                rule.onRoot().captureToImage().asAndroidBitmap()
                    .compress(Bitmap.CompressFormat.PNG, 100, stream)
            }
            assertTrue("a screenshot must not be empty: ${file.absolutePath}", file.length() > 0)
        }
    }

    private companion object {
        const val EMAIL = "user@example.com"
        const val TIMEOUT = 10_000L
        val ACCENT = KumihoAccent.BLUE
    }
}
