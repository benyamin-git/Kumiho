package dev.benyamin.kumiho.ui.account

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The WebView's URL policy and redirect parser. Both are pure JVM code: the
 * origin allow list decides what may stay inside the WebView and what leaves,
 * and the parser decides which redirect is handed to the engine. No Android
 * `Uri`, cookie jar or WebView is involved, so the security rules are pinned
 * without an emulator.
 */
class MozillaAccountWebViewTest {

    // ---- initial URL policy ----------------------------------------------

    @Test
    fun `only the Firefox Accounts origin with a path is loaded`() {
        assertTrue(isMozillaAuthorizationUrl("https://accounts.firefox.com/"))
        assertTrue(
            isMozillaAuthorizationUrl(
                "https://accounts.firefox.com/signin?service=sync&context=fx_desktop_v3",
            ),
        )
        assertTrue(isMozillaAuthorizationUrl("https://accounts.firefox.com/oauth/signin"))
        assertTrue("scheme and host are case-insensitive", isMozillaAuthorizationUrl("HTTPS://ACCOUNTS.FIREFOX.COM/signin"))
        assertTrue("the default port does not change the origin", isMozillaAuthorizationUrl("https://accounts.firefox.com:443/signin"))
    }

    @Test
    fun `other schemes, hosts and empty paths are refused`() {
        val refused = listOf(
            "http://accounts.firefox.com/signin",
            "ftp://accounts.firefox.com/signin",
            "https://evil.example/signin",
            "https://accounts.firefox.com.evil.example/signin",
            "https://evil-accounts.firefox.com/signin",
            "https://firefox.com/signin",
            "https://accounts.firefox.com",
            "https://accounts.firefox.com?email=user@example.com",
            "//accounts.firefox.com/signin",
            "not a url",
            "",
        )

        refused.forEach { raw ->
            assertFalse(raw, isMozillaAuthorizationUrl(raw))
        }
    }

    // ---- navigation policy ------------------------------------------------

    @Test
    fun `the redirect urn is intercepted and the account origin stays inside`() {
        assertEquals(BrowserNavigation.REDIRECT, classifyBrowserNavigation(REDIRECT))
        assertEquals(
            BrowserNavigation.REDIRECT,
            classifyBrowserNavigation(REDIRECT_WITH_VALUES),
        )
        assertEquals(
            "a malformed redirect is still a redirect, not a page to load",
            BrowserNavigation.REDIRECT,
            classifyBrowserNavigation("$URN?code=only-a-code"),
        )
        assertEquals(
            BrowserNavigation.IN_APP,
            classifyBrowserNavigation("https://accounts.firefox.com/signin"),
        )
        assertEquals(
            "the bare origin stays inside too",
            BrowserNavigation.IN_APP,
            classifyBrowserNavigation("https://accounts.firefox.com"),
        )
    }

    @Test
    fun `other https links leave and everything else is blocked`() {
        assertEquals(
            BrowserNavigation.EXTERNAL,
            classifyBrowserNavigation("https://www.mozilla.org/privacy/"),
        )
        assertEquals(
            BrowserNavigation.EXTERNAL,
            classifyBrowserNavigation("https://example.com/help?q=1"),
        )

        listOf(
            "http://www.mozilla.org/privacy/",
            "javascript:alert(1)",
            "intent://scan/#Intent;scheme=zxing;end",
            "file:///sdcard/secret.txt",
            "content://com.example/secret",
            "data:text/html,<h1>hi</h1>",
            "about:blank",
            "//example.com/path",
            "",
        ).forEach { raw ->
            assertEquals(raw, BrowserNavigation.BLOCK, classifyBrowserNavigation(raw))
        }
    }

    // ---- redirect parser --------------------------------------------------

    @Test
    fun `the exact urn is parsed and both values are decoded`() {
        val parsed = parseBrowserRedirect(
            "$URN?code=code%2Fone%2Btwo%3D&state=state%2Fthree%2Bfour",
        )

        assertEquals(BrowserRedirect("code/one+two=", "state/three+four"), parsed)
    }

    @Test
    fun `unknown query parameters do not hide the redirect`() {
        val parsed = parseBrowserRedirect(
            "$URN?foo=bar&code=the-code&state=the-state&extra=1",
        )

        assertEquals(BrowserRedirect("the-code", "the-state"), parsed)
    }

    @Test
    fun `the urn scheme and surrounding whitespace tolerate browser delivery`() {
        assertEquals(
            BrowserRedirect("the-code", "the-state"),
            parseBrowserRedirect("  URN:ietf:wg:oauth:2.0:oob:oauth-redirect-webchannel?code=the-code&state=the-state  "),
        )
    }

    @Test
    fun `wrong scheme, opaque part or origin are rejected`() {
        val rejected = listOf(
            "https://accounts.firefox.com/signin?code=the-code&state=the-state",
            "http:ietf:wg:oauth:2.0:oob:oauth-redirect-webchannel?code=the-code&state=the-state",
            "urn:ietf:wg:oauth:2.0:oob?code=the-code&state=the-state",
            "urn:ietf:wg:oauth:2.0:oob:oauth-redirect-webchannel-other?code=the-code&state=the-state",
            "$URN",
            "",
        )

        rejected.forEach { raw ->
            assertNull(raw, parseBrowserRedirect(raw))
        }
    }

    @Test
    fun `a redirect must carry both a code and a state`() {
        val rejected = listOf(
            "$URN?code=the-code",
            "$URN?state=the-state",
            "$URN?code=&state=the-state",
            "$URN?code=the-code&state=",
            "$URN?code&state=the-state",
            "$URN?code=the-code&state",
            "$URN?code=+&state=the-state",
            "$URN?code=the-code&state=%20",
        )

        rejected.forEach { raw ->
            assertNull(raw, parseBrowserRedirect(raw))
        }
    }

    @Test
    fun `error redirects are rejected even when a code is present`() {
        assertNull(parseBrowserRedirect("$URN?error=access_denied"))
        assertNull(
            parseBrowserRedirect(
                "$URN?error=access_denied&error_description=No+access&code=the-code&state=the-state",
            ),
        )
        assertNull(parseBrowserRedirect("$URN?code=the-code&state=the-state&error="))
    }

    @Test
    fun `ambiguous, malformed or fragmented redirects are rejected`() {
        val rejected = listOf(
            "$URN?code=one&code=two&state=the-state",
            "$URN?code=the-code&state=one&state=two",
            "$URN?code=the-code&state=the-state#fragment",
            "$URN?code=%ZZ&state=the-state",
            "$URN?code=the-code&state=%ZZ",
            "$URN?code=the-code&&state=the-state",
            "$URN?code=the-code&state=the-state&",
            "$URN?=value&code=the-code&state=the-state",
        )

        rejected.forEach { raw ->
            assertNull(raw, parseBrowserRedirect(raw))
        }
    }

    private companion object {
        const val URN = "urn:ietf:wg:oauth:2.0:oob:oauth-redirect-webchannel"
        const val REDIRECT = "$URN?code=the-code&state=the-state"
        const val REDIRECT_WITH_VALUES =
            "$URN?code=b7d4b0f0-1d5a-4c44-9f0a-8a8f1d0c0f00&state=9f4c2a6e8b1d4c3a"
    }
}
