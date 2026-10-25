package dev.benyamin.kumiho.ui.account

import android.annotation.SuppressLint
import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.webkit.CookieManager
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebSettings
import android.webkit.WebStorage
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.components.KumihoLoadingState
import dev.benyamin.kumiho.ui.shell.KumihoTopBar
import dev.benyamin.kumiho.ui.theme.Dimens
import java.net.URI
import java.net.URISyntaxException
import java.net.URLDecoder
import java.nio.charset.StandardCharsets
import java.util.concurrent.atomic.AtomicBoolean

/** The Mozilla account host that is allowed to load inside the WebView. */
private const val MOZILLA_ACCOUNTS_HOST = "accounts.firefox.com"

/**
 * The OAuth redirect registered for the Firefox client. FxA navigates to
 * `urn:<this>` after sign-in; the WebView intercepts it before the navigation
 * leaves the app and hands the complete URL to the engine.
 */
private const val REDIRECT_OPAQUE = "ietf:wg:oauth:2.0:oob:oauth-redirect-webchannel"

/** Test tags for the Mozilla account WebView screen. */
object MozillaAccountWebViewTags {
    const val SCREEN = "account_webview_screen"
    const val CLOSE = "account_webview_close"
    const val WEBVIEW = "account_webview_content"
    const val LOADING = "account_webview_loading"
    const val ERROR = "account_webview_error"
    const val ERROR_CLOSE = "account_webview_error_close"
}

/**
 * The one redirect the engine can exchange. The code is single-use and the
 * state is the OAuth nonce, so neither value may be logged; only the complete
 * URL is handed to the engine, which validates both.
 */
data class BrowserRedirect(val code: String, val state: String)

/**
 * True only for a real HTTPS Firefox Accounts page with a path. This runs
 * before the initial URL is loaded, so a bad engine answer becomes an explicit
 * error instead of loading an arbitrary page in a JavaScript-enabled WebView.
 */
fun isMozillaAuthorizationUrl(raw: String): Boolean {
    val uri = parseUri(raw) ?: return false
    return uri.scheme.equals("https", ignoreCase = true) &&
        MOZILLA_ACCOUNTS_HOST.equals(uri.host, ignoreCase = true) &&
        !uri.rawPath.isNullOrEmpty()
}

/**
 * Parses the exact `urn:ietf:wg:oauth:2.0:oob:oauth-redirect-webchannel`
 * redirect and URL-decodes its values. Returns null for a different scheme or
 * URN, an error redirect, a missing/empty code or state, duplicate values or a
 * malformed query. The engine still validates the state; this parser only
 * keeps obvious or hostile shapes from reaching it.
 */
fun parseBrowserRedirect(raw: String): BrowserRedirect? {
    if (!isMozillaRedirectUrl(raw)) return null
    val value = raw.trim()
    val queryStart = value.indexOf('?')
    if (queryStart < 0) return null
    val query = value.substring(queryStart + 1)
    if (query.isEmpty() || query.contains('#')) return null
    val params = parseQuery(query) ?: return null
    if (params.containsKey("error")) return null
    val code = params["code"]?.trim().orEmpty()
    val state = params["state"]?.trim().orEmpty()
    if (code.isEmpty() || state.isEmpty()) return null
    return BrowserRedirect(code = code, state = state)
}

/**
 * Clears the WebView session with the local sign-out: the cookie jar and the
 * WebView's DOM storage. Mozilla account cookies and session state otherwise
 * persist across sign-in attempts by design; sign-out calls this so the
 * browser session does not outlive the tokens. Every step is best-effort and
 * never throws through to the local sign-out.
 */
@Suppress("UNUSED_PARAMETER")
fun clearMozillaCookies(context: Context) {
    CookieManager.getInstance().run {
        removeAllCookies(null)
        flush()
    }
    // FxA also keeps session state in DOM storage (the WebView has it enabled
    // for the login page), so cookies alone do not clear the browser session.
    WebStorage.getInstance().deleteAllData()
}

/** What the WebView should do with a navigation. */
internal enum class BrowserNavigation {
    /** Intercept the OAuth URN and hand it to the engine. */
    REDIRECT,

    /** Load an accounts.firefox.com page inside the WebView. */
    IN_APP,

    /** Open an HTTPS link outside the app. */
    EXTERNAL,

    /** Block every other scheme and malformed URL. */
    BLOCK,
}

/** The navigation policy, pure so the security rules stay unit-testable. */
internal fun classifyBrowserNavigation(raw: String): BrowserNavigation = when {
    isMozillaRedirectUrl(raw) -> BrowserNavigation.REDIRECT
    isMozillaHostUrl(raw) -> BrowserNavigation.IN_APP
    isExternalHttpsUrl(raw) -> BrowserNavigation.EXTERNAL
    else -> BrowserNavigation.BLOCK
}

/**
 * The one-shot gate for the redirect handed to the engine. The first URL,
 * valid or malformed, claims the login; every later callback is ignored. A
 * valid redirect is passed through unchanged, while a malformed one enters the
 * screen's error state. The engine still validates the OAuth state.
 */
internal class BrowserRedirectHandler(
    private val onRedirect: (String) -> Unit,
    private val onInvalid: () -> Unit,
) {
    private val handled = AtomicBoolean(false)

    fun handle(raw: String) {
        if (!handled.compareAndSet(false, true)) return
        if (parseBrowserRedirect(raw) == null) onInvalid() else onRedirect(raw)
    }
}

/**
 * A full-screen Mozilla account browser. It is a client component: the host
 * owns navigation, insets and the engine submission, and calls it with the
 * current login URL. Closing leaves the flow intact; every redirect callback
 * feeds one one-shot handler, so the engine can never receive two redirects for
 * a login.
 */
@Composable
fun MozillaAccountBrowserScreen(
    loginId: String,
    url: String,
    onRedirect: (String) -> Unit,
    onClose: () -> Unit,
    onUnavailable: () -> Unit,
    modifier: Modifier = Modifier,
) {
    MozillaAccountBrowserScreenForTest(
        loginId = loginId,
        url = url,
        onRedirect = onRedirect,
        onClose = onClose,
        onUnavailable = onUnavailable,
        modifier = modifier,
        webViewFactory = ::createMozillaWebView,
        externalLinkOpener = ::openExternalHttps,
        loadInitialPage = { webView, pageUrl -> webView.loadUrl(pageUrl) },
    )
}

/**
 * The screen with seams for the WebView factory, the external-link opener and
 * the initial page loader. Production passes the hardened factory, the real
 * `ACTION_VIEW` opener and `loadUrl`; tests inject a recording view, a no-op
 * opener and a fake accounts.firefox.com page. Not for callers outside this
 * module.
 */
@Composable
internal fun MozillaAccountBrowserScreenForTest(
    loginId: String,
    url: String,
    onRedirect: (String) -> Unit,
    onClose: () -> Unit,
    onUnavailable: () -> Unit,
    modifier: Modifier = Modifier,
    webViewFactory: (Context) -> WebView,
    externalLinkOpener: (Context, Uri) -> Boolean,
    loadInitialPage: (WebView, String) -> Unit,
) {
    val context = LocalContext.current
    val onRedirectState = rememberUpdatedState(onRedirect)
    val onUnavailableState = rememberUpdatedState(onUnavailable)
    val externalLinkState = rememberUpdatedState(externalLinkOpener)
    val loadPageState = rememberUpdatedState(loadInitialPage)

    // A new login id starts a new one-shot handler, error state and WebView;
    // retries inside the same flow can never submit the redirect twice.
    val failed = remember(loginId) { mutableStateOf(!isMozillaAuthorizationUrl(url)) }
    val firstPageLoaded = remember(loginId) { mutableStateOf(false) }
    val redirectHandler = remember(loginId) {
        BrowserRedirectHandler(
            onRedirect = { raw -> onRedirectState.value(raw) },
            onInvalid = { failed.value = true },
        )
    }

    val webViewResult = remember(loginId, context) {
        var created: WebView? = null
        try {
            val view = webViewFactory(context)
            created = view
            view.webViewClient = MozillaWebViewClient(
                onRedirect = redirectHandler::handle,
                onFirstPageFinished = { firstPageLoaded.value = true },
                onLoadError = { failed.value = true },
                onExternalLink = { uri -> externalLinkState.value(context, uri) },
            )
            Result.success(view)
        } catch (throwable: Throwable) {
            // A client setup failure must not leak the view it was applied to.
            created?.destroy()
            Result.failure(throwable)
        }
    }
    val webView = webViewResult.getOrNull()

    LaunchedEffect(loginId, webViewResult.isFailure) {
        if (webViewResult.isFailure) onUnavailableState.value()
    }

    LaunchedEffect(loginId, webView, url) {
        if (webView == null) {
            failed.value = true
            return@LaunchedEffect
        }
        firstPageLoaded.value = false
        if (!isMozillaAuthorizationUrl(url)) {
            failed.value = true
            return@LaunchedEffect
        }
        failed.value = false
        loadPageState.value(webView, url)
    }

    BackHandler(onBack = onClose)

    Box(
        modifier = modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background)
            .testTag(MozillaAccountWebViewTags.SCREEN),
    ) {
        Box(
            modifier = Modifier
                .fillMaxSize()
                .padding(top = Dimens.TopBarHeight),
        ) {
            if (webView != null) {
                val hosted = webView
                // The host group follows both the login and the view instance.
                // A new login (or a replaced view) mounts its own WebView instead
                // of leaving it created offscreen; releasing the group is the
                // only place a WebView is destroyed. Error overlays below keep
                // the current view mounted, so it is never re-parented or
                // destroyed while another login is on screen.
                key(loginId, hosted) {
                    AndroidView(
                        factory = { hosted },
                        modifier = Modifier
                            .fillMaxSize()
                            .testTag(MozillaAccountWebViewTags.WEBVIEW),
                        onRelease = { released ->
                            released.stopLoading()
                            released.destroy()
                        },
                        update = { it.isEnabled = !failed.value },
                    )
                }
            }

            if (webView == null || failed.value) {
                MozillaAccountWebViewError(
                    onClose = onClose,
                    modifier = Modifier
                        .fillMaxSize()
                        .testTag(MozillaAccountWebViewTags.ERROR),
                )
            } else if (!firstPageLoaded.value) {
                Box(
                    modifier = Modifier
                        .fillMaxSize()
                        .background(MaterialTheme.colorScheme.background),
                    contentAlignment = Alignment.Center,
                ) {
                    KumihoLoadingState(
                        label = stringResource(R.string.account_webview_loading),
                        modifier = Modifier.testTag(MozillaAccountWebViewTags.LOADING),
                    )
                }
            }
        }

        // The bar is composed after the WebView on purpose: an AndroidView is
        // drawn in the window's view hierarchy, so content composed before it
        // can be painted over by the WebView surface. The bar and its close
        // action must stay above the embedded page.
        KumihoTopBar(
            title = stringResource(R.string.account_webview_title),
            actions = {
                IconButton(
                    onClick = onClose,
                    modifier = Modifier
                        .size(Dimens.TouchTarget)
                        .testTag(MozillaAccountWebViewTags.CLOSE),
                ) {
                    Icon(
                        imageVector = Icons.Filled.Close,
                        contentDescription = stringResource(R.string.account_webview_close),
                        tint = MaterialTheme.colorScheme.onSurface,
                    )
                }
            },
            modifier = Modifier
                .align(Alignment.TopCenter)
                .fillMaxWidth(),
        )
    }
}

/**
 * Applies the WebView hardening the flow needs: JavaScript and DOM storage for
 * FxA, no file/content access, no mixed content, no popups and no saved form
 * data. JavaScript is required by Mozilla's page; the origin policy above and
 * the absent JavaScript bridge are the mitigation. There is deliberately no
 * `addJavascriptInterface`; the only value the app reads is the intercepted
 * URN.
 */
@SuppressLint("SetJavaScriptEnabled")
internal fun configureMozillaWebView(webView: WebView) {
    webView.settings.javaScriptEnabled = true
    webView.settings.domStorageEnabled = true
    webView.settings.allowFileAccess = false
    webView.settings.allowContentAccess = false
    webView.settings.mixedContentMode = WebSettings.MIXED_CONTENT_NEVER_ALLOW
    webView.settings.setSupportMultipleWindows(false)
    webView.settings.javaScriptCanOpenWindowsAutomatically = false
    @Suppress("DEPRECATION")
    webView.settings.saveFormData = false
    webView.setDownloadListener { _, _, _, _, _ -> }
}

/** Creates the hardened WebView used by the production screen. */
internal fun createMozillaWebView(context: Context): WebView =
    WebView(context).also(::configureMozillaWebView)

private class MozillaWebViewClient(
    private val onRedirect: (String) -> Unit,
    private val onFirstPageFinished: () -> Unit,
    private val onLoadError: () -> Unit,
    private val onExternalLink: (Uri) -> Unit,
) : WebViewClient() {

    override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
        val raw = request.url.toString()
        return when (classifyBrowserNavigation(raw)) {
            BrowserNavigation.REDIRECT -> {
                // Only the main frame owns the OAuth flow. A redirect from a
                // nested frame is blocked, not accepted, so a compromised
                // third-party frame cannot consume the one-shot submission.
                if (request.isForMainFrame) onRedirect(raw)
                true
            }

            BrowserNavigation.IN_APP -> false

            BrowserNavigation.EXTERNAL -> {
                if (!request.isForMainFrame) {
                    // Cross-origin HTTPS frames are content, not a user
                    // navigation; only a main-frame link leaves the app.
                    false
                } else {
                    onExternalLink(request.url)
                    true
                }
            }

            BrowserNavigation.BLOCK -> true
        }
    }

    override fun onPageFinished(view: WebView, url: String?) {
        onFirstPageFinished()
        // Some WebViews report the failed URN navigation as a finished page
        // instead of calling shouldOverrideUrlLoading. The caller's one-shot
        // flag makes this path harmless when the override already submitted.
        val current = url ?: view.url
        if (current != null && isMozillaRedirectUrl(current)) onRedirect(current)
    }

    override fun onReceivedError(
        view: WebView,
        request: WebResourceRequest,
        error: WebResourceError,
    ) {
        if (!request.isForMainFrame) return
        val raw = request.url.toString()
        // A navigation to the OAuth URN is expected to fail: the WebView has
        // nowhere to take `urn:`. Hand the URL to the same one-shot handler
        // here too, in case this WebView reports the failure without calling
        // shouldOverrideUrlLoading or onPageFinished. A malformed redirect
        // sets the error state in the caller.
        if (isMozillaRedirectUrl(raw)) {
            onRedirect(raw)
            return
        }
        onLoadError()
    }
}

@Composable
private fun MozillaAccountWebViewError(
    onClose: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Box(
        modifier = modifier.background(MaterialTheme.colorScheme.background),
        contentAlignment = Alignment.Center,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .verticalScroll(rememberScrollState())
                .padding(Dimens.ScreenPadding),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(Dimens.Space3),
        ) {
            Icon(
                imageVector = Icons.Filled.Warning,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.error,
                modifier = Modifier.size(40.dp),
            )
            Text(
                text = stringResource(R.string.account_webview_error_title),
                style = MaterialTheme.typography.titleLarge,
                color = MaterialTheme.colorScheme.onSurface,
                textAlign = TextAlign.Center,
            )
            Text(
                text = stringResource(R.string.account_webview_error_text),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                textAlign = TextAlign.Center,
            )
            Text(
                text = stringResource(R.string.account_webview_paste_hint),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                textAlign = TextAlign.Center,
            )
            Button(
                onClick = onClose,
                modifier = Modifier
                    .heightIn(min = Dimens.ControlHeight)
                    .testTag(MozillaAccountWebViewTags.ERROR_CLOSE),
            ) {
                Text(stringResource(R.string.account_webview_close))
            }
        }
    }
}

private fun isMozillaRedirectUrl(raw: String): Boolean {
    val value = raw.trim()
    val schemeEnd = value.indexOf(':')
    if (schemeEnd <= 0 || !value.substring(0, schemeEnd).equals("urn", ignoreCase = true)) {
        return false
    }
    val opaque = value.substring(schemeEnd + 1)
    val queryStart = opaque.indexOf('?')
    val urn = if (queryStart < 0) opaque else opaque.substring(0, queryStart)
    return urn == REDIRECT_OPAQUE
}

private fun isMozillaHostUrl(raw: String): Boolean {
    val uri = parseUri(raw) ?: return false
    return uri.scheme.equals("https", ignoreCase = true) &&
        MOZILLA_ACCOUNTS_HOST.equals(uri.host, ignoreCase = true)
}

private fun isExternalHttpsUrl(raw: String): Boolean {
    val uri = parseUri(raw) ?: return false
    return uri.scheme.equals("https", ignoreCase = true) && !uri.host.isNullOrBlank()
}

private fun parseUri(raw: String): URI? = try {
    URI(raw)
} catch (_: URISyntaxException) {
    null
}

private fun parseQuery(query: String): Map<String, String>? {
    val params = LinkedHashMap<String, String>()
    for (pair in query.split('&')) {
        if (pair.isEmpty()) return null
        val separator = pair.indexOf('=')
        if (separator <= 0) return null
        val key = decode(pair.substring(0, separator)) ?: return null
        val value = decode(pair.substring(separator + 1)) ?: return null
        if (key.isEmpty() || params.put(key, value) != null) return null
    }
    return params
}

private fun decode(raw: String): String? = try {
    URLDecoder.decode(raw, StandardCharsets.UTF_8.name())
} catch (_: IllegalArgumentException) {
    null
}

private fun openExternalHttps(context: Context, uri: Uri): Boolean {
    if (!uri.scheme.equals("https", ignoreCase = true) || uri.host.isNullOrBlank()) {
        return false
    }
    val intent = Intent(Intent.ACTION_VIEW, uri)
    if (context !is Activity) {
        intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    }
    return try {
        context.startActivity(intent)
        true
    } catch (_: ActivityNotFoundException) {
        false
    } catch (_: RuntimeException) {
        // A broken browser/provider must not crash the sign-in screen.
        false
    }
}
