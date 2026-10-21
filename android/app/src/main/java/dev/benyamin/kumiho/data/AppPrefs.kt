package dev.benyamin.kumiho.data

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

/**
 * The single preferences DataStore, app-private under
 * `filesDir/datastore/kumiho.preferences_pb`. The delegate keeps one instance
 * per process even though it is reached from several places.
 */
private val Context.kumihoDataStore: DataStore<Preferences> by preferencesDataStore(name = "kumiho")

/**
 * Typed access to the app preferences. Empty values mean "follow the system"
 * for the theme and "blue" for the accent; the caller resolves them.
 */
class AppPrefs(context: Context) {
    private val store = context.applicationContext.kumihoDataStore

    /** The stored theme id, or null when the user has not chosen one. */
    val theme: Flow<String?> = store.data.map { it[KEY_THEME] }

    /** The stored accent id, or null when the user has not chosen one. */
    val accent: Flow<String?> = store.data.map { it[KEY_ACCENT] }

    /** Whether the four-step setup finished. */
    val setupComplete: Flow<Boolean> = store.data.map { it[KEY_SETUP_COMPLETE] ?: false }

    suspend fun setTheme(id: String) {
        store.edit { it[KEY_THEME] = id }
    }

    suspend fun setAccent(id: String) {
        store.edit { it[KEY_ACCENT] = id }
    }

    suspend fun setSetupComplete(value: Boolean) {
        store.edit { it[KEY_SETUP_COMPLETE] = value }
    }

    private companion object {
        val KEY_THEME = stringPreferencesKey("theme")
        val KEY_ACCENT = stringPreferencesKey("accent")
        val KEY_SETUP_COMPLETE = booleanPreferencesKey("setup_complete")
    }
}
