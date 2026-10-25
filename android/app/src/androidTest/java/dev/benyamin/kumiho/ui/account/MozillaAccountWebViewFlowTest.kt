package dev.benyamin.kumiho.ui.account

import android.content.Context
import android.net.Uri
import android.view.View
import android.view.ViewGroup
import android.webkit.WebView
import androidx.activity.ComponentActivity
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onNodeWithTag
import dev.benyamin.kumiho.go.mobile.Mobile
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotSame
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * The WebView hosting and redirect wiring that the pure parser tests cannot
 * reach. The fake pages are loaded with an accounts.firefox.com base URL, so
 * the real client sees the real origin; no request reaches Mozilla.
 */
class MozillaAccountWebViewFlowTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    // ---- hosting ----------------------------------------------------------

    @Test
    fun aNewLoginHostsTheNewWebViewAndReleasesTheOld() {
        var loginId by mutableStateOf("login-1")
        var show by mutableStateOf(true)
        val created = CopyOnWriteArrayList<RecordingWebView>()
        var unavailable = 0

        rule.setContent {
            KumihoTheme(colors = testColors()) {
                if (show) {
                    MozillaAccountBrowserScreenForTest(
                        loginId = loginId,
                        url = INITIAL_URL,
                        onRedirect = {},
                        onClose = {},
                        onUnavailable = { unavailable++ },
                        webViewFactory = { context ->
                            RecordingWebView(context).also {
                                configureMozillaWebView(it)
                                created += it
                            }
                        },
                        externalLinkOpener = { _, _ -> true },
                        loadInitialPage = { view, _ ->
                            view.loadData(HOSTED_PAGE, "text/html", "UTF-8")
                        },
                    )
                }
            }
        }
        rule.waitForIdle()

        val first = created.single()
        assertEquals(listOf<WebView>(first), mountedWebViews())
        assertEquals(0, unavailable)

        rule.runOnIdle { loginId = "login-2" }
        rule.waitForIdle()

        assertEquals(2, created.size)
        val second = created[1]
        assertNotSame(first, second)
        assertEquals("the replaced login's WebView is destroyed", 1, first.destroyCalls.get())
        assertEquals(0, second.destroyCalls.get())
        assertEquals("only the new login's WebView stays mounted", listOf<WebView>(second), mountedWebViews())

        rule.runOnIdle { show = false }
        rule.waitForIdle()

        assertEquals("leaving the screen destroys the mounted WebView", 1, second.destroyCalls.get())
        assertTrue("no WebView may stay attached", mountedWebViews().isEmpty())
    }

    @Test
    fun aSecondFailedLoginCallsUnavailableAgain() {
        var loginId by mutableStateOf("login-1")
        var unavailable = 0

        rule.setContent {
            KumihoTheme(colors = testColors()) {
                MozillaAccountBrowserScreenForTest(
                    loginId = loginId,
                    url = INITIAL_URL,
                    onRedirect = {},
                    onClose = {},
                    onUnavailable = { unavailable++ },
                    webViewFactory = { throw IllegalStateException("no WebView on this device") },
                    externalLinkOpener = { _, _ -> true },
                    loadInitialPage = { _, _ -> },
                )
            }
        }
        rule.waitForIdle()

        rule.onNodeWithTag(MozillaAccountWebViewTags.ERROR).assertIsDisplayed()
        assertEquals(1, unavailable)

        rule.runOnIdle { loginId = "login-2" }
        rule.waitForIdle()

        rule.onNodeWithTag(MozillaAccountWebViewTags.ERROR).assertIsDisplayed()
        assertEquals("a new login must re-offer the fallback", 2, unavailable)
    }

    // ---- redirect wiring --------------------------------------------------

    @Test
    fun theFullRedirectIsSubmittedExactlyOnce() {
        val redirects = CopyOnWriteArrayList<String>()
        var webView: WebView? = null
        var unavailable = 0

        showPage(
            html = navigationPage(FULL_REDIRECT),
            onRedirect = { redirects += it },
            onLoaded = { webView = it },
            onUnavailable = { unavailable++ },
        )

        rule.waitUntil(5_000) { redirects.isNotEmpty() }
        assertEquals(listOf(FULL_REDIRECT), redirects)
        assertEquals(0, unavailable)

        rule.runOnIdle {
            webView?.evaluateJavascript(
                "location.href='$SECOND_REDIRECT';" +
                    "setTimeout(function(){document.title='second-attempt';}, 250);",
                null,
            )
        }
        rule.waitUntil(5_000) { webViewTitle(webView) == "second-attempt" }

        assertEquals("the first redirect must claim the flow", 1, redirects.size)
    }

    @Test
    fun aMalformedRedirectShowsTheErrorWithoutSubmitting() {
        val redirects = CopyOnWriteArrayList<String>()
        var unavailable = 0

        showPage(
            html = navigationPage(MALFORMED_REDIRECT),
            onRedirect = { redirects += it },
            onUnavailable = { unavailable++ },
        )

        rule.waitUntil(5_000) {
            rule.onAllNodesWithTag(MozillaAccountWebViewTags.ERROR)
                .fetchSemanticsNodes()
                .isNotEmpty()
        }
        rule.onNodeWithTag(MozillaAccountWebViewTags.ERROR).assertIsDisplayed()
        rule.onNodeWithTag(MozillaAccountWebViewTags.WEBVIEW).assertExists()
        assertTrue(redirects.isEmpty())
        assertEquals(0, unavailable)
    }

    @Test
    fun anExternalHttpsLinkLeavesTheWebView() {
        val redirects = CopyOnWriteArrayList<String>()
        val opened = CopyOnWriteArrayList<Uri>()
        var webView: WebView? = null

        showPage(
            html = navigationPage(EXTERNAL_URL),
            onRedirect = { redirects += it },
            onLoaded = { webView = it },
            onExternal = { _, uri ->
                opened += uri
                true
            },
        )

        rule.waitUntil(5_000) { opened.isNotEmpty() }
        assertEquals(Uri.parse(EXTERNAL_URL), opened.single())
        rule.waitForIdle()

        assertFalse(
            "the external page must not load in the WebView",
            webViewUrl(webView)?.contains("example.com") == true,
        )
        assertTrue(redirects.isEmpty())
    }

    @Test
    fun aNonHttpsLinkIsBlocked() {
        val redirects = CopyOnWriteArrayList<String>()
        val opened = CopyOnWriteArrayList<Uri>()
        var webView: WebView? = null

        showPage(
            html = """
                <html><head><title>hosted</title></head><body><script>
                  location.href = "mailto:help@example.com";
                  setTimeout(function() { document.title = "blocked"; }, 250);
                </script></body></html>
            """.trimIndent(),
            onRedirect = { redirects += it },
            onLoaded = { webView = it },
            onExternal = { _, uri ->
                opened += uri
                true
            },
        )

        rule.waitUntil(5_000) { webViewTitle(webView) == "blocked" }
        rule.waitForIdle()

        assertTrue("a non-HTTPS link must not leave the app", opened.isEmpty())
        assertTrue(redirects.isEmpty())
    }

    // ---- helpers ----------------------------------------------------------

    private fun showPage(
        html: String,
        onRedirect: (String) -> Unit = {},
        onLoaded: (WebView) -> Unit = {},
        onUnavailable: () -> Unit = {},
        onExternal: (Context, Uri) -> Boolean = { _, _ -> false },
    ) {
        rule.setContent {
            KumihoTheme(colors = testColors()) {
                MozillaAccountBrowserScreenForTest(
                    loginId = "login-1",
                    url = INITIAL_URL,
                    onRedirect = onRedirect,
                    onClose = {},
                    onUnavailable = onUnavailable,
                    webViewFactory = { context ->
                        createMozillaWebView(context).also(onLoaded)
                    },
                    externalLinkOpener = onExternal,
                    loadInitialPage = { view, _ ->
                        view.loadDataWithBaseURL(BASE_URL, html, "text/html", "UTF-8", null)
                    },
                )
            }
        }
        rule.waitForIdle()
    }

    private fun testColors() = PaletteMapping.fromJson(
        Mobile.palette(ThemeId.LIGHT.id, KumihoAccent.BLUE.id),
        ThemeId.LIGHT,
        KumihoAccent.BLUE,
    )

    private fun navigationPage(target: String): String = """
        <html><head><title>hosted</title></head><body><script>
          location.href = "$target";
        </script></body></html>
    """.trimIndent()

    /** The WebViews currently in the activity's view tree, on the main thread. */
    private fun mountedWebViews(): List<WebView> {
        var found: List<WebView> = emptyList()
        rule.runOnIdle {
            val views = mutableListOf<WebView>()
            fun walk(view: View) {
                if (view is WebView) views += view
                if (view is ViewGroup) {
                    for (index in 0 until view.childCount) walk(view.getChildAt(index))
                }
            }
            walk(rule.activity.window.decorView)
            found = views
        }
        return found
    }

    private fun webViewTitle(webView: WebView?): String? {
        var title: String? = null
        rule.runOnIdle { title = webView?.title }
        return title
    }

    private fun webViewUrl(webView: WebView?): String? {
        var url: String? = null
        rule.runOnIdle { url = webView?.url }
        return url
    }

    private class RecordingWebView(context: Context) : WebView(context) {
        val destroyCalls = AtomicInteger()

        override fun destroy() {
            destroyCalls.incrementAndGet()
            super.destroy()
        }
    }

    private companion object {
        const val URN = "urn:ietf:wg:oauth:2.0:oob:oauth-redirect-webchannel"
        const val INITIAL_URL = "https://accounts.firefox.com/authorization?test=1"
        const val BASE_URL = "https://accounts.firefox.com/"
        const val FULL_REDIRECT = "$URN?code=code-1&state=state-1"
        const val SECOND_REDIRECT = "$URN?code=code-2&state=state-2"
        const val MALFORMED_REDIRECT = "$URN?code=only-a-code"
        const val EXTERNAL_URL = "https://example.com/help"
        const val HOSTED_PAGE = "<html><head><title>hosted</title></head><body>hosted</body></html>"
    }
}
