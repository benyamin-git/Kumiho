package dev.benyamin.kumiho.ui.account

import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.browser.customtabs.CustomTabsIntent

/** A test seam for opening the daemon's loopback sign-in URL. */
fun interface BrowserLauncher {
    /** Returns false when no activity can handle the URL. */
    fun open(context: Context, url: String): Boolean
}

/**
 * Opens the engine's one-shot loopback URL in a Custom Tab. Returns false when
 * the URL is not loopback HTTP(S) or no browser accepts it; the caller keeps
 * the flow cancellable and shows the password fallback instead.
 *
 * There is deliberately no custom URL scheme: the engine owns the loopback
 * server, and Android only needs a browser to reach it.
 */
fun openBrowserSignIn(context: Context, url: String): Boolean {
    val uri = Uri.parse(url)
    if (uri.scheme !in setOf("http", "https")) return false
    if (uri.host !in setOf("127.0.0.1", "localhost")) return false
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
