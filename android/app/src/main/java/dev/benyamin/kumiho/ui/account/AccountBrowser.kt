package dev.benyamin.kumiho.ui.account

import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.browser.customtabs.CustomTabsIntent

/** A test seam for opening the engine's browser sign-in URL. */
fun interface BrowserLauncher {
    /** Returns false when no activity can handle the URL. */
    fun open(context: Context, url: String): Boolean
}

/**
 * Opens the engine's browser sign-in URL in a Custom Tab. The engine returns
 * Mozilla's HTTPS login page; its loopback callback is internal to the engine,
 * so the URL opened here is deliberately not a loopback URL. Plain http is
 * accepted only for loopback, which keeps dev/test harnesses safe.
 *
 * Returns false when the URL is not a remote HTTPS page (or loopback HTTP) or
 * no browser accepts it; the caller keeps the flow cancellable and shows the
 * password fallback instead.
 *
 * There is deliberately no custom URL scheme: the engine owns the loopback
 * server, and Android only needs a browser to reach the login page.
 */
fun openBrowserSignIn(context: Context, url: String): Boolean {
    val uri = Uri.parse(url)
    val allowed = when (uri.scheme) {
        "https" -> !uri.host.isNullOrBlank()
        "http" -> uri.host == "127.0.0.1" || uri.host.equals("localhost", ignoreCase = true)
        else -> false
    }
    if (!allowed) return false
    return try {
        val customTabs = CustomTabsIntent.Builder().build()
        if (context !is Activity) {
            customTabs.intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        customTabs.launchUrl(context, uri)
        true
    } catch (_: ActivityNotFoundException) {
        false
    } catch (_: RuntimeException) {
        // A broken browser/provider must become the documented fallback, not a
        // crash on a sign-in attempt.
        false
    }
}
