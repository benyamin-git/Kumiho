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
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
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
 * Clears the WebView cookie jar with the local session. Mozilla account cookies
 * otherwise persist across sign-in attempts by design; sign-out calls this so
 * the browser session does not outlive the tokens.
 */
@Suppress("UNUSED_PARAMETER")
fun clearMozillaCookies(context: Context) {
    CookieManager.getInstance().run {
        removeAllCookies(null)
        flush()
    }
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
 * A full-screen Mozilla account browser. It is a client component: the host
 * owns navigation, insets and the engine submission, and calls it with the
 * current login URL. Closing leaves the flow intact; submitting the redirect
 * is one-shot even if both the navigation override and the page-finished
 * fallback observe the same URN.
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
    val context = LocalContext.current
    val onRedirectState = rememberUpdatedState(onRedirect)
    val onUnavailableState = rememberUpdatedState(onUnavailable)
    // A new login id starts a new one-shot guard and WebView; retries inside
    // the same flow can never submit the redirect twice.
    val redirectHandled = remember(loginId) { AtomicBoolean(false) }
    val failed = remember(loginId) { mutableStateOf(!isMozillaAuthorizationUrl(url)) }
    val firstPageLoaded = remember(loginId) { mutableStateOf(false) }

    val handleRedirect: (String) -> Unit = remember(loginId) {
        { raw ->
            if (redirectHandled.compareAndSet(false, true)) {
                if (parseBrowserRedirect(raw) == null) {
                    failed.value = true
                } else {
                    onRedirectState.value(raw)
                }
            }
        }
    }

    val webViewResult = remember(loginId, context) {
        runCatching {
            createMozillaWebView(context).apply {
                webViewClient = MozillaWebViewClient(
                    onRedirect = handleRedirect,
                    onFirstPageFinished = { firstPageLoaded.value = true },
                    onLoadError = { failed.value = true },
                    onExternalLink = { uri -> openExternalHttps(context, uri) },
                )
            }
        }
    }
    val webView = webViewResult.getOrNull()

    LaunchedEffect(webViewResult.isFailure) {
        if (webViewResult.isFailure) onUnavailableState.value()
    }

    LaunchedEffect(webView, url) {
        if (webView == null) {
            failed.value = true
            return@LaunchedEffect
        }
        failed.value = false
        firstPageLoaded.value = false
        if (!isMozillaAuthorizationUrl(url)) {
            failed.value = true
            return@LaunchedEffect
        }
        webView.loadUrl(url)
    }

    DisposableEffect(webView) {
        onDispose {
            webView?.let {
                it.stopLoading()
                it.destroy()
            }
        }
    }

    BackHandler(onBack = onClose)

    Column(
        modifier = modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background)
            .testTag(MozillaAccountWebViewTags.SCREEN),
    ) {
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
        )

        Box(modifier = Modifier.weight(1f).fillMaxWidth()) {
            if (webView != null && !failed.value) {
                AndroidView(
                    factory = { webView },
                    modifier = Modifier
                        .fillMaxSize()
                        .testTag(MozillaAccountWebViewTags.WEBVIEW),
                )
                if (!firstPageLoaded.value) {
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
            } else {
                MozillaAccountWebViewError(
                    onClose = onClose,
                    modifier = Modifier
                        .fillMaxSize()
                        .testTag(MozillaAccountWebViewTags.ERROR),
                )
            }
        }
    }
}

/**
 * Creates the WebView with the hardening the flow needs: JavaScript and DOM
 * storage for FxA, no file/content access, no mixed content, no popups and no
 * saved form data. JavaScript is required by Mozilla's page; the origin policy
 * above and the absent JavaScript bridge are the mitigation. There is
 * deliberately no `addJavascriptInterface`; the only value the app reads is the
 * intercepted URN.
 */
@SuppressLint("SetJavaScriptEnabled")
internal fun createMozillaWebView(context: Context): WebView = WebView(context).apply {
    settings.javaScriptEnabled = true
    settings.domStorageEnabled = true
    settings.allowFileAccess = false
    settings.allowContentAccess = false
    settings.mixedContentMode = WebSettings.MIXED_CONTENT_NEVER_ALLOW
    settings.setSupportMultipleWindows(false)
    settings.javaScriptCanOpenWindowsAutomatically = false
    @Suppress("DEPRECATION")
    settings.saveFormData = false
    setDownloadListener { _, _, _, _, _ -> }
}

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
    Box(modifier = modifier, contentAlignment = Alignment.Center) {
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
