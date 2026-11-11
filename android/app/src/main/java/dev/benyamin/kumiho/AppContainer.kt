package dev.benyamin.kumiho

import android.content.ComponentName
import android.content.Context
import android.service.quicksettings.TileService
import android.content.res.Configuration
import dev.benyamin.kumiho.data.AllowBypassRepository
import dev.benyamin.kumiho.data.AppPrefs
import dev.benyamin.kumiho.data.InstalledAppsRepository
import dev.benyamin.kumiho.data.LanguageRepository
import dev.benyamin.kumiho.data.PaletteState
import dev.benyamin.kumiho.data.SplitTunnelRepository
import dev.benyamin.kumiho.data.ThemeRepository
import dev.benyamin.kumiho.tv.TvChannelPublisher
import dev.benyamin.kumiho.ui.guide.GuideController
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.vpn.BypassSettingsProvider
import dev.benyamin.kumiho.vpn.QuickConnectTileService
import dev.benyamin.kumiho.vpn.SplitSettingsProvider
import dev.benyamin.kumiho.vpn.VpnServiceCoordinator
import dev.benyamin.kumiho.widget.WidgetRefresh
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch

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

    /** Cached language, QS/notification locale and the engine preference. */
    val languageRepository = LanguageRepository(
        prefs = appPrefs,
        scope = scope,
        systemLanguage = { appContext.resources.configuration.locales[0].language },
    )

    /** The shared Blocked region guide visibility (v0.11.0). */
    val guide = GuideController()

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
     * The opt-in allow-bypass choice and the provider the VPN bridge reads.
     * The snapshot is synchronous and Off until the DataStore loads.
     */
    val allowBypassRepository = AllowBypassRepository(
        store = appPrefs,
        scope = scope,
    )

    /** The seam `KumihoVpnService` hands to the bridge. */
    val bypassSettingsProvider: BypassSettingsProvider = allowBypassRepository

    /**
     * The app-scoped binding to the VPN service. It binds with
     * `BIND_AUTO_CREATE` in its constructor, so the engine is available to
     * every activity without any of them creating a session.
     */
    val vpnServiceCoordinator = VpnServiceCoordinator(appContext)

    /** Starts app-scoped work. Idempotent. */
    fun start() {
        languageRepository.start()
        themeRepository.start()
        splitTunnelRepository.start()
        allowBypassRepository.start()
        // Starts the tile/widget collector only after `container` is published,
        // so its first widget refresh can read the container back safely.
        vpnServiceCoordinator.start()
        scope.launch {
            val engine = vpnServiceCoordinator.engine.filterNotNull().first()
            val settings = try {
                engine.settings()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (_: Throwable) {
                return@launch
            }
            languageRepository.syncEngine(settings.language)
        }
        // A widget cannot render before the palette is ready; repaint both
        // whenever the theme or accent resolves, including after a change.
        scope.launch {
            themeRepository.state.collect { state ->
                if (state is PaletteState.Ready) WidgetRefresh.request(appContext)
            }
        }
        // One fan-out per effective-language change: repaint the widgets,
        // republish the TV channel and ask the tile to re-read its labels.
        scope.launch {
            languageRepository.effective.collect {
                WidgetRefresh.request(appContext)
                TvChannelPublisher.publish(appContext)
                try {
                    TileService.requestListeningState(
                        appContext,
                        ComponentName(appContext, QuickConnectTileService::class.java),
                    )
                } catch (_: RuntimeException) {
                    // No tile in this process/session; the next change asks again.
                }
            }
        }
    }
}

private fun Context.isNightMode(): Boolean =
    resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK ==
        Configuration.UI_MODE_NIGHT_YES
