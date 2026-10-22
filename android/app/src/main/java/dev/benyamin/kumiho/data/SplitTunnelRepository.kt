package dev.benyamin.kumiho.data

import dev.benyamin.kumiho.vpn.SplitMode
import dev.benyamin.kumiho.vpn.SplitSettings
import dev.benyamin.kumiho.vpn.SplitSettingsProvider
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/**
 * The narrow persistence seam behind the split-tunnel selection. [AppPrefs]
 * backs it with the preferences DataStore; JVM tests use an in-memory fake.
 *
 * Both writes are immediate: there is no separate Apply button, and a change
 * applies on the next connect.
 */
interface SplitTunnelStore {
    /** The persisted mode and package set; Off + empty for existing installs. */
    val splitSettings: Flow<SplitSettings>

    suspend fun setSplitMode(mode: SplitMode)

    suspend fun setSplitPackages(packages: Set<String>)
}

/** The repository's cached state: the last selection, a load flag and an error. */
data class SplitTunnelState(
    val settings: SplitSettings = SplitSettings(),
    val loading: Boolean = true,
    val error: String? = null,
)

/**
 * Owns the split-tunnel selection for the whole process. It is also the
 * [SplitSettingsProvider] the VPN bridge reads: [snapshot] returns the cached
 * value synchronously, so `acquireFD` never waits on DataStore. Until the
 * first DataStore emission the safe Off default is served.
 *
 * On load (and on every stored emission) selected packages that are no longer
 * installed are dropped and the cleaned set is persisted, so an uninstalled
 * app cannot linger in the selection. A failed package query keeps the stored
 * value rather than guessing.
 */
class SplitTunnelRepository(
    private val store: SplitTunnelStore,
    private val scope: CoroutineScope,
    private val installedPackages: suspend () -> Set<String>,
) : SplitSettingsProvider {

    private val stateFlow = MutableStateFlow(SplitTunnelState())

    /** The selection the screens render. */
    val state: StateFlow<SplitTunnelState> = stateFlow.asStateFlow()

    private var observeJob: Job? = null

    /** Starts watching the stored selection. Idempotent. */
    fun start() {
        if (observeJob?.isActive == true) return
        observeJob = scope.launch {
            try {
                store.splitSettings.collect { stored ->
                    publish(clean(stored))
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Throwable) {
                stateFlow.update {
                    it.copy(loading = false, error = messageOf(error))
                }
            }
        }
    }

    /** Re-runs the stored-data observation after a failure. */
    fun retry() {
        observeJob?.cancel()
        observeJob = null
        start()
    }

    /** The latest cached selection, synchronously; Off until DataStore loads. */
    override fun snapshot(): SplitSettings = stateFlow.value.settings

    /** Persists a mode and mirrors it immediately. */
    fun setMode(mode: SplitMode) {
        stateFlow.update { it.copy(settings = it.settings.copy(mode = mode), error = null) }
        write { store.setSplitMode(mode) }
    }

    /** Persists a whole package set and mirrors it immediately. */
    fun setPackages(packages: Set<String>) {
        val cleaned = packages.filter { it.isNotBlank() }.toSet()
        stateFlow.update { it.copy(settings = it.settings.copy(packages = cleaned), error = null) }
        write { store.setSplitPackages(cleaned) }
    }

    /** Adds or removes one package. */
    fun togglePackage(packageName: String) {
        val current = stateFlow.value.settings.packages
        setPackages(if (packageName in current) current - packageName else current + packageName)
    }

    private fun write(block: suspend () -> Unit) {
        scope.launch {
            try {
                block()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Throwable) {
                stateFlow.update { it.copy(error = messageOf(error)) }
            }
        }
    }

    private suspend fun clean(stored: SplitSettings): SplitSettings {
        if (stored.packages.isEmpty()) return stored
        val installed = try {
            installedPackages()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (_: Throwable) {
            // A failed query is not proof that anything was uninstalled.
            return stored
        }
        val kept = stored.packages.filter { it in installed }.toSet()
        if (kept == stored.packages) return stored
        try {
            store.setSplitPackages(kept)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Throwable) {
            stateFlow.update { it.copy(error = messageOf(error)) }
        }
        return stored.copy(packages = kept)
    }

    private fun publish(settings: SplitSettings) {
        stateFlow.update {
            it.copy(settings = settings, loading = false, error = null)
        }
    }
}

private fun messageOf(error: Throwable): String =
    error.message?.takeIf { it.isNotBlank() } ?: error::class.simpleName ?: "unknown error"
