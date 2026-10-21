package dev.benyamin.kumiho

import android.content.Context
import android.content.res.Configuration
import dev.benyamin.kumiho.data.AppPrefs
import dev.benyamin.kumiho.data.ThemeRepository
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.vpn.OffSplitSettingsProvider
import dev.benyamin.kumiho.vpn.SplitSettingsProvider
import dev.benyamin.kumiho.vpn.VpnServiceCoordinator
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob

/**
 * The manual application-scoped container. It owns the process-wide coroutine
 * scope and the pieces that outlive any activity: preferences, the theme and
 * the VPN service coordinator.
 */
class AppContainer(context: Context) {
    private val appContext = context.applicationContext
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)

    /** The app preferences DataStore. */
    val appPrefs = AppPrefs(appContext)

    /** The theme pipeline: stored choice, Go palette, cache. */
    val themeRepository = ThemeRepository(
        prefs = appPrefs,
        scope = scope,
        // An unset theme follows the OS, Light or Dark only; OLED is opt-in.
        defaultTheme = { if (appContext.isNightMode()) ThemeId.DARK else ThemeId.LIGHT },
    )

    /** The split-tunnel selection; Off until T14 replaces this seam. */
    val splitSettingsProvider: SplitSettingsProvider = OffSplitSettingsProvider

    /**
     * The app-scoped binding to the VPN service. It binds with
     * `BIND_AUTO_CREATE` in its constructor, so the engine is available to
     * every activity without any of them creating a session.
     */
    val vpnServiceCoordinator = VpnServiceCoordinator(appContext)

    /** Starts app-scoped work. Idempotent. */
    fun start() {
        themeRepository.start()
    }
}

private fun Context.isNightMode(): Boolean =
    resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK ==
        Configuration.UI_MODE_NIGHT_YES
