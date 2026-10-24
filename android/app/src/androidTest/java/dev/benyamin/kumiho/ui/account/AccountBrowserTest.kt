package dev.benyamin.kumiho.ui.account

import android.content.Context
import android.content.ContextWrapper
import android.content.Intent
import android.os.Bundle
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

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
