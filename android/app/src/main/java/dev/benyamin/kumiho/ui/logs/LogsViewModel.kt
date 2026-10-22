package dev.benyamin.kumiho.ui.logs

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dev.benyamin.kumiho.engine.LogEntry
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** Copy the logs screen generates itself, resolved by the UI. */
enum class LogsMessage {
    CLEARED,
    COPIED,
    CLEAR_FAILED,
    LOAD_FAILED,
    EXPORT_FAILED,
}

/** Everything the logs console renders. [entries] is already filtered. */
data class LogsUiState(
    val entries: List<LogEntry> = emptyList(),
    val level: LogFilterLevel = LogFilterLevel.ALL,
    val search: String = "",
    val follow: Boolean = true,
    val loading: Boolean = true,
    val error: String? = null,
    val clearing: Boolean = false,
    val message: LogsMessage? = null,
) {
    /** True when the console has nothing to show, either truly empty or filtered out. */
    val isEmpty: Boolean get() = !loading && entries.isEmpty()
}

/** The engine calls Logs makes; JVM tests use an in-memory fake. */
interface LogsGateway {
    suspend fun subscribe(level: String?)

    suspend fun snapshot(level: String?): List<LogEntry>

    suspend fun unsubscribe()

    /** Clears the engine's ring and rotating file. */
    suspend fun clear()

    /** The client's live log mirror, oldest first, capped by the engine client. */
    val stream: Flow<List<LogEntry>>
}

/**
 * The logs state machine: subscribe + snapshot on open, merge without
 * duplicates, level/search filtering, Follow, Clear and export/copy feedback.
 *
 * The subscription is not left on while the screen is gone: the route calls
 * [onVisible] and [onHidden], so returning to the tab subscribes again.
 */
class LogsViewModel(
    private val gateway: LogsGateway,
) : ViewModel() {
    private val mutableState = MutableStateFlow(LogsUiState())

    /** The whole screen state; the UI only reads it. */
    val state: StateFlow<LogsUiState> = mutableState.asStateFlow()

    private var snapshot: List<LogEntry> = emptyList()
    private var streamed: List<LogEntry> = emptyList()
    private var suppressed: Set<LogEntry> = emptySet()
    private var loadJob: Job? = null
    private var streamJob: Job? = null

    @Volatile
    private var visible = false

    /** Starts the subscription and the first snapshot while the screen is shown. */
    fun onVisible() {
        if (visible) return
        visible = true
        streamJob?.cancel()
        streamJob = viewModelScope.launch {
            gateway.stream.collect { entries ->
                streamed = entries
                recompute()
            }
        }
        reload()
    }

    /**
     * Stops the live subscription when the screen leaves composition. A failure
     * is ignored: there is no useful surface left to report it on.
     */
    fun onHidden() {
        if (!visible) return
        visible = false
        loadJob?.cancel()
        loadJob = null
        streamJob?.cancel()
        streamJob = null
        viewModelScope.launch {
            try {
                gateway.unsubscribe()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (_: Exception) {
                // Nothing is listening; the next onVisible starts a fresh one.
            }
        }
    }

    fun retry() = reload()

    fun dismissMessage() {
        mutableState.update { it.copy(message = null, error = null) }
    }

    // ---- controls --------------------------------------------------------

    fun setLevel(level: LogFilterLevel) {
        if (level == mutableState.value.level) return
        mutableState.update { it.copy(level = level) }
        recompute()
        reload()
    }

    fun setSearch(query: String) {
        mutableState.update { it.copy(search = query, follow = true) }
        recompute()
    }

    fun setFollow(follow: Boolean) {
        mutableState.update { it.copy(follow = follow) }
    }

    fun clearSearch() = setSearch("")

    fun notifyCopied() {
        mutableState.update { it.copy(message = LogsMessage.COPIED) }
    }

    fun notifyExportFailed() {
        mutableState.update { it.copy(message = LogsMessage.EXPORT_FAILED) }
    }

    fun clear() {
        if (mutableState.value.clearing) return
        mutableState.update { it.copy(clearing = true, message = null, error = null) }
        viewModelScope.launch {
            try {
                gateway.clear()
                suppressed = suppressed + streamed + snapshot
                snapshot = emptyList()
                mutableState.update { it.copy(clearing = false, message = LogsMessage.CLEARED) }
                recompute()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                mutableState.update {
                    it.copy(
                        clearing = false,
                        message = LogsMessage.CLEAR_FAILED,
                        error = messageOf(error),
                    )
                }
            }
        }
    }

    // ---- load ------------------------------------------------------------

    private fun reload() {
        if (loadJob?.isActive == true) return
        val level = mutableState.value.level
        mutableState.update {
            it.copy(loading = it.entries.isEmpty(), error = null)
        }
        loadJob = viewModelScope.launch {
            try {
                gateway.subscribe(level.wire)
                snapshot = gateway.snapshot(level.wire)
                recompute()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                mutableState.update {
                    it.copy(
                        loading = false,
                        message = LogsMessage.LOAD_FAILED,
                        error = messageOf(error),
                    )
                }
            }
        }
    }

    private fun recompute() {
        val merged = LogFilter.merge(snapshot, streamed)
            .filter { it !in suppressed }
        val visibleEntries = LogFilter.apply(
            merged,
            mutableState.value.level,
            mutableState.value.search,
        )
        mutableState.update {
            it.copy(entries = visibleEntries, loading = false)
        }
    }

    private fun messageOf(error: Throwable): String =
        error.message?.takeIf { it.isNotBlank() }
            ?: error::class.simpleName
            ?: error.javaClass.name

    override fun onCleared() {
        // The route normally unsubscribes before this; this is the safety net
        // for an activity that is destroyed while the console is visible.
        if (visible) onHidden()
    }
}
