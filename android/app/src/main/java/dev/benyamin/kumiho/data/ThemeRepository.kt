package dev.benyamin.kumiho.data

import dev.benyamin.kumiho.go.mobile.Mobile
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoColors
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import java.util.concurrent.ConcurrentHashMap
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** The theme pipeline's state: a splash-covered load, a palette, or a failure. */
sealed interface PaletteState {
    /** The palette has not been computed yet. */
    data object Loading : PaletteState

    /** The palette is ready and rendered. */
    data class Ready(val colors: KumihoColors) : PaletteState

    /** The palette could not be computed; the shell shows this, never a default. */
    data class Error(val message: String) : PaletteState
}

/**
 * Owns the active palette. It watches the stored theme and accent, resolves
 * empty values (theme from the OS night mode, accent to blue), calls the Go
 * palette on [Dispatchers.Default] and caches the result per theme/accent pair.
 *
 * OLED is never chosen implicitly: the default provider returns only Light or
 * Dark, and an empty accent is always blue.
 */
class ThemeRepository(
    private val prefs: ThemePreferences,
    private val scope: CoroutineScope,
    private val defaultTheme: () -> ThemeId,
    private val loadPaletteJson: suspend (theme: String, accent: String) -> String = { theme, accent ->
        Mobile.palette(theme, accent)
    },
) {
    private val stateFlow = MutableStateFlow<PaletteState>(PaletteState.Loading)

    /** The palette, once loaded. */
    val state: StateFlow<PaletteState> = stateFlow.asStateFlow()

    /** In-memory cache keyed by theme id + accent id. */
    private val cache = ConcurrentHashMap<Pair<String, String>, KumihoColors>()

    private var observeJob: Job? = null

    /** Starts watching stored preferences. Idempotent. */
    fun start() {
        if (observeJob?.isActive == true) return
        observeJob = scope.launch {
            prefs.theme.combine(prefs.accent) { theme, accent -> theme to accent }
                .distinctUntilChanged()
                .collectLatest { (theme, accent) -> load(theme, accent) }
        }
    }

    /** Suspends until the first palette attempt settles (ready or failed). */
    suspend fun awaitReady() {
        state.first { it !is PaletteState.Loading }
    }

    /** Re-runs the palette load after a failure. */
    fun retry() {
        scope.launch { load(prefs.theme.first(), prefs.accent.first()) }
    }

    /** Persists a theme choice; the observer picks it up. */
    fun setTheme(theme: ThemeId) {
        scope.launch { prefs.setTheme(theme.id) }
    }

    /** Persists an accent choice; the observer picks it up. */
    fun setAccent(accent: KumihoAccent) {
        scope.launch { prefs.setAccent(accent.id) }
    }

    private suspend fun load(storedTheme: String?, storedAccent: String?) {
        val theme = ThemeId.fromId(storedTheme) ?: defaultTheme()
        val accent = KumihoAccent.fromId(storedAccent) ?: KumihoAccent.Default
        val key = theme.id to accent.id
        cache[key]?.let {
            stateFlow.value = PaletteState.Ready(it)
            return
        }
        val result = try {
            val json = withContext(Dispatchers.Default) { loadPaletteJson(theme.id, accent.id) }
            PaletteMapping.fromJson(json, theme, accent)
        } catch (cancelled: CancellationException) {
            // collectLatest cancels superseded loads; that is not a palette failure.
            throw cancelled
        } catch (error: Throwable) {
            stateFlow.value = PaletteState.Error(
                error.message ?: error::class.simpleName ?: "palette unavailable",
            )
            return
        }
        cache[key] = result
        stateFlow.value = PaletteState.Ready(result)
    }
}
