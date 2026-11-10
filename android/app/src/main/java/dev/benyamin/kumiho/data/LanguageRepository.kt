package dev.benyamin.kumiho.data

import android.content.Context
import android.content.res.Configuration
import java.util.Locale
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch

/** The two UI languages the app can render. */
enum class AppLanguage(val tag: String) {
    ENGLISH("en"),
    PERSIAN("fa"),
}

/**
 * Combines the engine `language` preference with the DataStore mirror and the
 * OS locale. The engine setting is authoritative once it arrives; the mirror
 * only serves the first paint and native surfaces until then.
 */
class LanguageRepository(
    private val prefs: LanguagePreferences,
    private val scope: CoroutineScope,
    private val systemLanguage: () -> String = { Locale.getDefault().language },
) {
    private val preferenceState = MutableStateFlow(SYSTEM)
    private val effectiveState = MutableStateFlow(resolve(SYSTEM, systemLanguage()))
    private val readyState = MutableStateFlow(false)
    private val lock = Any()
    private val localChoiceVersion = AtomicInteger(0)
    private val engineSynced = AtomicBoolean(false)

    /** The normalized engine preference (`system|en|fa`). */
    val preference: StateFlow<String> = preferenceState.asStateFlow()

    /** The language the UI renders. */
    val effective: StateFlow<AppLanguage> = effectiveState.asStateFlow()

    /** True once the cached preference has been read. */
    val ready: StateFlow<Boolean> = readyState.asStateFlow()

    private var observeJob: Job? = null

    /** Starts reading the DataStore mirror. Idempotent. */
    fun start() {
        if (observeJob?.isActive == true) return
        observeJob = scope.launch {
            prefs.language
                .catch { emit("") }
                .collect { cached ->
                    synchronized(lock) {
                        if (!readyState.value && localChoiceVersion.get() == 0 &&
                            !engineSynced.get()
                        ) {
                            apply(cached)
                        }
                        readyState.value = true
                    }
                }
        }
    }

    /** Suspends until the cached preference has been read. */
    suspend fun awaitReady() {
        ready.first { it }
    }

    /**
     * Normalizes, persists and immediately applies a user choice. A later
     * engine sync must not overwrite it while the engine catches up.
     */
    fun setPreference(value: String) {
        val version = localChoiceVersion.incrementAndGet()
        synchronized(lock) { apply(value) }
        persist(value, version)
    }

    /**
     * Adopts the engine's setting and mirrors it. Ignored after a local choice,
     * which is newer than the startup read this sync came from.
     */
    fun syncEngine(value: String) {
        synchronized(lock) {
            if (localChoiceVersion.get() != 0) return
            engineSynced.set(true)
            apply(value)
        }
        persist(value, 0)
    }

    /** A context whose resources and layout direction match [effective]. */
    fun localizedContext(base: Context): Context {
        val locale = Locale.forLanguageTag(effectiveState.value.tag)
        val configuration = Configuration(base.resources.configuration)
        configuration.setLocale(locale)
        configuration.setLayoutDirection(locale)
        return base.createConfigurationContext(configuration)
    }

    private fun apply(value: String) {
        val normalized = normalize(value)
        preferenceState.value = normalized
        effectiveState.value = resolve(normalized, systemLanguage())
    }

    /** A newer local choice supersedes this write before it reaches DataStore. */
    private fun persist(value: String, version: Int) {
        scope.launch {
            if (localChoiceVersion.get() == version) prefs.setLanguage(normalize(value))
        }
    }

    companion object {
        private const val SYSTEM = "system"

        /** Unknown preferences are treated as `system`; writes stay strict. */
        fun normalize(value: String): String = when (value.trim().lowercase(Locale.ROOT)) {
            "en" -> "en"
            "fa" -> "fa"
            else -> SYSTEM
        }

        /** Explicit en/fa win; everything else follows the OS language. */
        fun resolve(preference: String, systemLanguage: String): AppLanguage =
            when (normalize(preference)) {
                "en" -> AppLanguage.ENGLISH
                "fa" -> AppLanguage.PERSIAN
                else -> {
                    val language = systemLanguage.trim().lowercase(Locale.ROOT)
                        .substringBefore('-')
                        .substringBefore('_')
                    if (language == "fa") AppLanguage.PERSIAN else AppLanguage.ENGLISH
                }
            }
    }
}
