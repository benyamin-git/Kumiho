package dev.benyamin.kumiho

import android.content.Context
import android.content.res.Configuration
import dev.benyamin.kumiho.data.AppPrefs
import dev.benyamin.kumiho.data.InstalledAppsRepository
import dev.benyamin.kumiho.data.SplitTunnelRepository
import dev.benyamin.kumiho.data.ThemeRepository
import dev.benyamin.kumiho.ui.theme.ThemeId
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

    /** The installed-app list behind the split screen's picker. */
    val installedAppsRepository = InstalledAppsRepository(appContext)

    /**
     * The split-tunnel selection and the provider the VPN bridge reads. Its
     * `snapshot()` is synchronous and serves Off until the DataStore loads, so
     * `acquireFD` never waits on storage.
     */
    val splitTunnelRepository = SplitTunnelRepository(
        store = appPrefs,
        scope = scope,
        installedPackages = installedAppsRepository::installedPackageNames,
    )

    /** The seam `KumihoVpnService` hands to the bridge. */
    val splitSettingsProvider: SplitSettingsProvider = splitTunnelRepository

    /**
     * The app-scoped binding to the VPN service. It binds with
     * `BIND_AUTO_CREATE` in its constructor, so the engine is available to
     * every activity without any of them creating a session.
     */
    val vpnServiceCoordinator = VpnServiceCoordinator(appContext)

    /** Starts app-scoped work. Idempotent. */
    fun start() {
        themeRepository.start()
        splitTunnelRepository.start()
    }
}

private fun Context.isNightMode(): Boolean =
    resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK ==
        Configuration.UI_MODE_NIGHT_YES
