package dev.benyamin.kumiho.data

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.core.stringSetPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import dev.benyamin.kumiho.vpn.SplitMode
import dev.benyamin.kumiho.vpn.SplitSettings
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

/**
 * The single preferences DataStore, app-private under
 * `filesDir/datastore/kumiho.preferences_pb`. The delegate keeps one instance
 * per process even though it is reached from several places.
 */
private val Context.kumihoDataStore: DataStore<Preferences> by preferencesDataStore(name = "kumiho")

/**
 * The preferences [ThemeRepository] needs. [AppPrefs] is the DataStore-backed
 * implementation; tests supply a plain in-memory fake.
 */
interface ThemePreferences {
    /** The stored theme id, or null when the user has not chosen one. */
    val theme: Flow<String?>

    /** The stored accent id, or null when the user has not chosen one. */
    val accent: Flow<String?>

    suspend fun setTheme(id: String)

    suspend fun setAccent(id: String)
}

/**
 * Typed access to the app preferences. Empty values mean "follow the system"
 * for the theme and "blue" for the accent; the caller resolves them.
 */
class AppPrefs(context: Context) : ThemePreferences, SplitTunnelStore, BypassSettingsStore {
    private val store = context.applicationContext.kumihoDataStore

    /** The stored theme id, or null when the user has not chosen one. */
    override val theme: Flow<String?> = store.data.map { it[KEY_THEME] }

    /** The stored accent id, or null when the user has not chosen one. */
    override val accent: Flow<String?> = store.data.map { it[KEY_ACCENT] }

    /** Whether the four-step setup finished. */
    val setupComplete: Flow<Boolean> = store.data.map { it[KEY_SETUP_COMPLETE] ?: false }

    /**
     * The persisted split-tunnel selection. An absent mode means Off (the safe
     * default for installs that predate split tunneling); an absent set is
     * empty. An unknown stored mode also falls back to Off.
     */
    override val splitSettings: Flow<SplitSettings> = store.data.map { prefs ->
        val mode = prefs[KEY_SPLIT_MODE]?.let { stored ->
            SplitMode.entries.firstOrNull { it.name == stored }
        } ?: SplitMode.OFF
        SplitSettings(mode = mode, packages = prefs[KEY_SPLIT_PACKAGES].orEmpty())
    }

    /** The persisted allow-bypass choice; an absent key means Off. */
    override val allowBypass: Flow<Boolean> = store.data.map { it[KEY_ALLOW_BYPASS] ?: false }

    override suspend fun setTheme(id: String) {
        store.edit { it[KEY_THEME] = id }
    }

    override suspend fun setAccent(id: String) {
        store.edit { it[KEY_ACCENT] = id }
    }

    override suspend fun setSplitMode(mode: SplitMode) {
        store.edit { it[KEY_SPLIT_MODE] = mode.name }
    }

    override suspend fun setSplitPackages(packages: Set<String>) {
        store.edit { it[KEY_SPLIT_PACKAGES] = packages }
    }

    override suspend fun setAllowBypass(enabled: Boolean) {
        store.edit { it[KEY_ALLOW_BYPASS] = enabled }
    }

    suspend fun setSetupComplete(value: Boolean) {
        store.edit { it[KEY_SETUP_COMPLETE] = value }
    }

    private companion object {
        val KEY_THEME = stringPreferencesKey("theme")
        val KEY_ACCENT = stringPreferencesKey("accent")
        val KEY_SETUP_COMPLETE = booleanPreferencesKey("setup_complete")
        val KEY_SPLIT_MODE = stringPreferencesKey("split_mode")
        val KEY_SPLIT_PACKAGES = stringSetPreferencesKey("split_packages")
        val KEY_ALLOW_BYPASS = booleanPreferencesKey("allow_bypass")
    }
}
