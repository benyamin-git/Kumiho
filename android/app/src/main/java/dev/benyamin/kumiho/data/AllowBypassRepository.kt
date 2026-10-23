package dev.benyamin.kumiho.data

import dev.benyamin.kumiho.vpn.BypassSettingsProvider
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
 * The narrow persistence seam behind the allow-bypass choice. [AppPrefs] backs
 * it with the preferences DataStore; JVM tests use an in-memory fake.
 */
interface BypassSettingsStore {
    /** The persisted choice; an absent key means Off. */
    val allowBypass: Flow<Boolean>

    suspend fun setAllowBypass(enabled: Boolean)
}

/** A fixed, user-facing allow-bypass failure; exception details stay internal. */
enum class BypassError { LOAD, SAVE }

/**
 * The repository's cached state: the stored choice, the value the live
 * interface was built with, the first-read flag and the last failure.
 */
data class AllowBypassState(
    val enabled: Boolean = false,
    val applied: Boolean = false,
    val loading: Boolean = true,
    val error: BypassError? = null,
)

/**
 * Owns the allow-bypass choice for the whole process. It is also the
 * [BypassSettingsProvider] the VPN bridge reads: [snapshot] returns the cached
 * value synchronously, so `acquireFD` never waits on DataStore, and records it
 * as pending until [markApplied] confirms `establish()` succeeded.
 *
 * A failed load serves Off. A failed write restores the last successfully
 * persisted value. [start] is idempotent and restarts a failed collector, so
 * reopening Settings is the read-recovery path without a retry button.
 */
class AllowBypassRepository(
    private val store: BypassSettingsStore,
    private val scope: CoroutineScope,
) : BypassSettingsProvider {

    private val stateFlow = MutableStateFlow(AllowBypassState())

    /** The choice the screen renders. */
    val state: StateFlow<AllowBypassState> = stateFlow.asStateFlow()

    /** The last successfully stored value, used to undo a failed write. */
    @Volatile
    private var persisted = false

    /** The value handed to the interface build that has not settled yet. */
    @Volatile
    private var pendingApplied: Boolean? = null

    private var observeJob: Job? = null

    /** Starts watching the stored choice. Idempotent. */
    fun start() {
        if (observeJob?.isActive == true) return
        observeJob = scope.launch {
            try {
                store.allowBypass.collect { stored ->
                    persisted = stored
                    stateFlow.update {
                        it.copy(enabled = stored, loading = false, error = null)
                    }
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (_: Throwable) {
                stateFlow.update {
                    it.copy(enabled = false, loading = false, error = BypassError.LOAD)
                }
            }
        }
    }

    /** The latest cached choice, synchronously; Off until DataStore loads. */
    override fun snapshot(): Boolean {
        val current = stateFlow.value.enabled
        pendingApplied = current
        return current
    }

    /** Commits the value the established interface was built with. */
    override fun markApplied() {
        val pending = pendingApplied ?: return
        pendingApplied = null
        stateFlow.update { it.copy(applied = pending) }
    }

    /** Persists a choice and mirrors it immediately; ignored during the first load. */
    fun setEnabled(value: Boolean) {
        if (stateFlow.value.loading) return
        stateFlow.update { it.copy(enabled = value, error = null) }
        scope.launch {
            try {
                store.setAllowBypass(value)
                persisted = value
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (_: Throwable) {
                stateFlow.update {
                    it.copy(enabled = persisted, error = BypassError.SAVE)
                }
            }
        }
    }
}
