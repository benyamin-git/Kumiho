package dev.benyamin.kumiho

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.core.splashscreen.SplashScreen.Companion.installSplashScreen
import androidx.lifecycle.lifecycleScope
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
 */
class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        val splash = installSplashScreen()
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()

        val container = (application as KumihoApplication).container
        var themeReady = false
        splash.setKeepOnScreenCondition { !themeReady }
        lifecycleScope.launch {
            container.themeRepository.awaitReady()
            themeReady = true
        }

        setContent {
            KumihoApp(container)
        }
    }
}
