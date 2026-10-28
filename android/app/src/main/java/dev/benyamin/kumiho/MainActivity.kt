package dev.benyamin.kumiho

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.core.splashscreen.SplashScreen.Companion.installSplashScreen
import androidx.lifecycle.lifecycleScope
import dev.benyamin.kumiho.tv.TvChannelPublisher
import dev.benyamin.kumiho.tv.TvEnvironment
import dev.benyamin.kumiho.ui.KumihoApp
import kotlinx.coroutines.launch

/**
 * Single activity host. It holds the splash screen until the stored palette has
 * loaded, so a dark or OLED session never flashes the light theme, then hands
 * the shell the application-scoped container.
 *
 * The order matters: `installSplashScreen()` runs before `super.onCreate()` so
 * it can swap in `postSplashScreenTheme`, and before `enableEdgeToEdge()` so the
 * window decor is not created while the platform default theme (which carries
 * an ActionBar) is still applied.
 *
 * On a television the activity also reads the Google TV channel cards' deep
 * links and publishes the channel itself. Both are no-ops on a phone.
 */
class MainActivity : ComponentActivity() {
    private var tvConnectRequested by mutableStateOf(false)

    override fun onCreate(savedInstanceState: Bundle?) {
        val splash = installSplashScreen()
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()

        val container = (application as KumihoApplication).container
        val isTelevision = TvEnvironment.isTelevision(this)
        handleTelevisionIntent(intent)
        if (isTelevision) {
            lifecycleScope.launch { TvChannelPublisher.publish(this@MainActivity) }
        }

        var themeReady = false
        splash.setKeepOnScreenCondition { !themeReady }
        lifecycleScope.launch {
            container.themeRepository.awaitReady()
            themeReady = true
        }

        setContent {
            KumihoApp(
                container = container,
                isTv = isTelevision,
                tvConnectRequested = tvConnectRequested,
                onTvConnectConsumed = { tvConnectRequested = false },
            )
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        handleTelevisionIntent(intent)
    }

    /** The connect card sets a one-shot request; the open card needs nothing. */
    private fun handleTelevisionIntent(intent: Intent?) {
        val data = intent?.data ?: return
        if (data.scheme == "kumiho" &&
            data.host == "tv" &&
            data.pathSegments.firstOrNull() == "connect"
        ) {
            tvConnectRequested = true
        }
    }
}
