package dev.benyamin.kumiho.ui.account

import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.browser.customtabs.CustomTabsIntent

/**
 * Opens the engine's browser sign-in URL in a Custom Tab. This is the fallback
 * used only when the in-app [MozillaAccountBrowserScreen] cannot be created:
 * the WebView path intercepts Mozilla's URN redirect itself, while this path
 * leaves the complete redirect URL to be pasted on the sign-in screen.
 *
 * The engine returns Mozilla's HTTPS Firefox Accounts page; plain http is
 * accepted only for loopback, which keeps dev/test harnesses safe.
 *
 * Returns false when the URL is not a remote HTTPS page (or loopback HTTP) or
 * no browser accepts it; the caller keeps the flow cancellable and shows the
 * paste and password fallbacks instead.
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
