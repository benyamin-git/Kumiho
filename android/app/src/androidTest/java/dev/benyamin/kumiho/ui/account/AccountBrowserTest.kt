package dev.benyamin.kumiho.ui.account

import android.content.Context
import android.content.ContextWrapper
import android.content.Intent
import android.os.Bundle
import android.webkit.CookieManager
import android.webkit.WebSettings
import androidx.activity.ComponentActivity
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.platform.app.InstrumentationRegistry
import dev.benyamin.kumiho.go.mobile.Mobile
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * The system-browser fallback for devices whose WebView cannot be created. The
 * in-app WebView path itself is covered by
 * [MozillaAccountWebViewInstrumentedTest].
 */
class AccountBrowserTest {

    @Test
    fun opensTheMozillaLoginPage() {
        val context = RecordingContext(InstrumentationRegistry.getInstrumentation().targetContext)
        val url = "https://vpn.mozilla.org/api/v2/vpn/login/android" +
            "?code_challenge=abc&code_challenge_method=S256&port=49152"

        assertTrue(openBrowserSignIn(context, url))

        val intent = context.started.single()
        assertEquals(Intent.ACTION_VIEW, intent.action)
        assertEquals(url, intent.data.toString())
        assertTrue(intent.flags and Intent.FLAG_ACTIVITY_NEW_TASK != 0)
    }

    @Test
    fun rejectsRemoteCleartextAndNonWebSchemes() {
        val context = RecordingContext(InstrumentationRegistry.getInstrumentation().targetContext)

        assertFalse(openBrowserSignIn(context, "http://vpn.mozilla.org/login"))
        assertFalse(openBrowserSignIn(context, "ftp://vpn.mozilla.org/login"))
        assertFalse(openBrowserSignIn(context, "not a URL"))
        assertTrue(context.started.isEmpty())
    }

    private class RecordingContext(base: Context) : ContextWrapper(base) {
        val started = mutableListOf<Intent>()

        override fun startActivity(intent: Intent) {
            started += intent
        }

        override fun startActivity(intent: Intent, options: Bundle?) {
            started += intent
        }
    }
}

/**
 * The in-app Mozilla account surface on a real WebView. The emulator never
 * reaches Mozilla: the URL policy is pinned by JVM tests, the hardening flags
 * are read from a real WebView, and the error path uses an invalid initial URL.
 */
class MozillaAccountWebViewInstrumentedTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    @Test
    @Suppress("DEPRECATION")
    fun createsAHardenedWebView() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        instrumentation.runOnMainSync {
            val webView = createMozillaWebView(instrumentation.targetContext)
            try {
                val settings = webView.settings
                assertTrue("JavaScript is required by the FxA page", settings.javaScriptEnabled)
                assertTrue("FxA stores its session in DOM storage", settings.domStorageEnabled)
                assertFalse(settings.allowFileAccess)
                assertFalse(settings.allowContentAccess)
                assertEquals(WebSettings.MIXED_CONTENT_NEVER_ALLOW, settings.mixedContentMode)
                assertFalse(settings.supportMultipleWindows())
                assertFalse(settings.javaScriptCanOpenWindowsAutomatically)
                assertFalse(settings.saveFormData)
                // There is deliberately no JavaScript bridge to assert against:
                // the production code never calls addJavascriptInterface.
            } finally {
                webView.destroy()
            }
        }
    }

    @Test
    fun invalidInitialUrlShowsThePasteFallbackAndClose() {
        var closeCalls = 0
        var unavailable = false
        var redirected: String? = null

        rule.setContent {
            KumihoTheme(
                colors = PaletteMapping.fromJson(
                    Mobile.palette(ThemeId.LIGHT.id, KumihoAccent.BLUE.id),
                    ThemeId.LIGHT,
                    KumihoAccent.BLUE,
                ),
            ) {
                MozillaAccountBrowserScreen(
                    loginId = "login-1",
                    url = "https://evil.example/signin",
                    onRedirect = { redirected = it },
                    onClose = { closeCalls++ },
                    onUnavailable = { unavailable = true },
                )
            }
        }
        rule.waitForIdle()

        rule.onNodeWithText("Mozilla account").assertIsDisplayed()
        rule.onNodeWithText("Sign-in page did not load").assertIsDisplayed()
        rule.onNodeWithText("The Mozilla account page could not be loaded.").assertIsDisplayed()
        rule.onNodeWithText(
            "Close this screen and paste the redirect URL in Account if sign-in does not finish.",
        ).assertIsDisplayed()
        rule.onNodeWithTag(MozillaAccountWebViewTags.ERROR).assertIsDisplayed()
        rule.onNodeWithTag(MozillaAccountWebViewTags.LOADING).assertDoesNotExist()
        rule.onNodeWithContentDescription("Close").assertIsDisplayed()
        assertFalse("the emulator must provide a usable WebView", unavailable)
        assertNull(redirected)
        assertEquals(0, closeCalls)

        rule.onNodeWithTag(MozillaAccountWebViewTags.CLOSE).performClick()
        rule.waitForIdle()
        assertEquals(1, closeCalls)

        rule.onNodeWithTag(MozillaAccountWebViewTags.ERROR_CLOSE).performClick()
        rule.waitForIdle()
        assertEquals(2, closeCalls)
    }

    @Test
    fun clearMozillaCookiesRemovesThePersistedJar() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val cookies = CookieManager.getInstance()
        cookies.setAcceptCookie(true)
        cookies.setCookie(COOKIE_URL, "kumiho_webview_test=1; Path=/")
        cookies.flush()
        assertTrue(
            "the cookie is persisted before sign-out",
            cookies.getCookie(COOKIE_URL)?.contains("kumiho_webview_test=1") == true,
        )

        clearMozillaCookies(context)

        val deadline = System.currentTimeMillis() + 5_000
        while (cookies.getCookie(COOKIE_URL)?.contains("kumiho_webview_test=1") == true &&
            System.currentTimeMillis() < deadline
        ) {
            Thread.sleep(50)
        }
        assertFalse(
            "sign-out clears the WebView cookie jar",
            cookies.getCookie(COOKIE_URL)?.contains("kumiho_webview_test=1") == true,
        )
    }

    private companion object {
        const val COOKIE_URL = "https://accounts.firefox.com/"
    }
}
