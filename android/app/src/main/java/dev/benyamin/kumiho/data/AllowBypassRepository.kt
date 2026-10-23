package dev.benyamin.kumiho.data

import dev.benyamin.kumiho.vpn.BypassSettingsProvider
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.distinctUntilChanged
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
 * Writes go through one FIFO worker, so two quick toggles cannot land out of
 * order. A failed load serves Off. A failed write restores the last
 * successfully persisted value once no newer toggle is queued. [start] is
 * idempotent and restarts a failed collector, so reopening Settings is the
 * read-recovery path without a retry button.
 */
class AllowBypassRepository(
    private val store: BypassSettingsStore,
    private val scope: CoroutineScope,
) : BypassSettingsProvider {

    private val stateFlow = MutableStateFlow(AllowBypassState())

    /** The choice the screen renders. */
    val state: StateFlow<AllowBypassState> = stateFlow.asStateFlow()

    /** Guards the cached values and every state transition. */
    private val lock = Any()

    /** The last successfully stored value, used to undo a failed write. */
    private var persisted = false

    /** The value handed to the interface build that has not settled yet. */
    private var pendingApplied: Boolean? = null

    /** Writes queued or in flight; the last success or failure settles the row. */
    private var pendingWrites = 0

    private val writeQueue = Channel<Boolean>(Channel.UNLIMITED)

    private var observeJob: Job? = null
    private var writerJob: Job? = null

    /** Starts watching the stored choice. Idempotent. */
    fun start() {
        startWriter()
        if (observeJob?.isActive == true) return
        observeJob = scope.launch {
            try {
                // A theme or split write re-emits the whole preferences map;
                // only a real allow-bypass change may touch the row's error.
                store.allowBypass.distinctUntilChanged().collect { stored ->
                    synchronized(lock) {
                        persisted = stored
                        if (pendingWrites == 0) {
                            stateFlow.update {
                                it.copy(enabled = stored, loading = false, error = null)
                            }
                        } else {
                            stateFlow.update { it.copy(loading = false) }
                        }
                    }
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (_: Throwable) {
                synchronized(lock) {
                    stateFlow.update {
                        it.copy(enabled = false, loading = false, error = BypassError.LOAD)
                    }
                }
            }
        }
    }

    private fun startWriter() {
        if (writerJob?.isActive == true) return
        writerJob = scope.launch {
            for (value in writeQueue) {
                try {
                    store.setAllowBypass(value)
                    synchronized(lock) {
                        persisted = value
                        pendingWrites -= 1
                        if (pendingWrites == 0) {
                            stateFlow.update { it.copy(error = null) }
                        }
                    }
                } catch (cancelled: CancellationException) {
                    throw cancelled
                } catch (_: Throwable) {
                    synchronized(lock) {
                        pendingWrites -= 1
                        if (pendingWrites == 0) {
                            stateFlow.update {
                                it.copy(enabled = persisted, error = BypassError.SAVE)
                            }
                        }
                    }
                }
            }
        }
    }

    /** The latest cached choice, synchronously; Off until DataStore loads. */
    override fun snapshot(): Boolean = synchronized(lock) {
        val current = stateFlow.value.enabled
        pendingApplied = current
        current
    }

    /** Commits the value the established interface was built with. */
    override fun markApplied() {
        synchronized(lock) {
            val pending = pendingApplied ?: return
            pendingApplied = null
            stateFlow.update { it.copy(applied = pending) }
        }
    }

    /** Persists a choice and mirrors it immediately; ignored during the first load. */
    fun setEnabled(value: Boolean) {
        synchronized(lock) {
            if (stateFlow.value.loading) return
            pendingWrites += 1
            stateFlow.update { it.copy(enabled = value, error = null) }
        }
        writeQueue.trySend(value)
    }
}
