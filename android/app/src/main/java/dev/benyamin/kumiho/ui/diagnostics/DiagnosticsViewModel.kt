package dev.benyamin.kumiho.ui.diagnostics

import android.content.Context
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dev.benyamin.kumiho.diagnostics.AndroidFacts
import dev.benyamin.kumiho.diagnostics.AndroidFactsCollector
import dev.benyamin.kumiho.engine.DoctorReport
import dev.benyamin.kumiho.vpn.VpnServiceCoordinator
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/**
 * The diagnostics calls. Production talks to the app-scoped coordinator and the
 * Android facts collector; JVM tests use an in-memory fake.
 */
interface DiagnosticsGateway {
    suspend fun facts(): AndroidFacts

    suspend fun doctor(facts: AndroidFacts): DoctorReport

    /** Clears caches and logs only; tokens and the account are never touched. */
    suspend fun resetState()
}

/** The production gateway: Android facts in, the engine's doctor out. */
class CoordinatorDiagnosticsGateway(
    context: Context,
    private val coordinator: VpnServiceCoordinator,
) : DiagnosticsGateway {
    private val collector = AndroidFactsCollector(context.applicationContext, coordinator)

    override suspend fun facts(): AndroidFacts = collector.collect()

    override suspend fun doctor(facts: AndroidFacts): DoctorReport =
        coordinator.awaitEngine().doctor(facts.toJson())

    override suspend fun resetState() {
        coordinator.awaitEngine().resetState()
    }
}

/**
 * Diagnostics state machine. It collects the facts and asks the engine's doctor
 * once on the first visit each launch, caches the report for the visit, and
 * only runs again when the user asks or after a reset. There is no polling.
 */
class DiagnosticsViewModel(
    private val gateway: DiagnosticsGateway,
) : ViewModel() {
    private val mutableState = MutableStateFlow(DiagnosticsUiState())

    /** The whole screen state; the UI only reads it. */
    val state: StateFlow<DiagnosticsUiState> = mutableState.asStateFlow()

    private var loadJob: Job? = null

    init {
        load()
    }

    /** Re-collects the facts and re-runs the doctor on demand. */
    fun rerun() = load()

    fun dismissMessage() {
        mutableState.update { it.copy(message = null, error = null) }
    }

    /** Feedback after the report was copied to the clipboard. */
    fun notifyCopied() {
        mutableState.update { it.copy(message = DiagnosticsMessage.COPIED, error = null) }
    }

    // ---- reset local state ----------------------------------------------

    fun requestReset() {
        mutableState.update { it.copy(resetDialog = true) }
    }

    fun dismissReset() {
        mutableState.update { it.copy(resetDialog = false) }
    }

    /**
     * The confirmation is the only path to `reset_state`: it clears the server
     * cache, log ring and failure counters, never the account or its tokens.
     */
    fun confirmReset() {
        val current = mutableState.value
        if (current.resetting) return
        mutableState.update {
            it.copy(resetting = true, resetDialog = false, message = null, error = null)
        }
        viewModelScope.launch {
            try {
                gateway.resetState()
                mutableState.update {
                    it.copy(resetting = false, message = DiagnosticsMessage.RESET_DONE)
                }
                load()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                mutableState.update {
                    it.copy(
                        resetting = false,
                        message = DiagnosticsMessage.RESET_FAILED,
                        error = messageOf(error),
                    )
                }
            }
        }
    }

    // ---- load ------------------------------------------------------------

    private fun load() {
        if (loadJob?.isActive == true) return
        val hadReport = mutableState.value.hasReport
        mutableState.update {
            it.copy(
                loading = true,
                error = null,
                message = if (hadReport) it.message else null,
            )
        }
        loadJob = viewModelScope.launch {
            try {
                val facts = gateway.facts()
                val report = gateway.doctor(facts)
                mutableState.update {
                    it.copy(
                        loading = false,
                        facts = facts,
                        report = report,
                        error = null,
                    )
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                mutableState.update {
                    it.copy(
                        loading = false,
                        message = DiagnosticsMessage.LOAD_FAILED,
                        error = messageOf(error),
                    )
                }
            }
        }
    }

    private fun messageOf(error: Throwable): String =
        error.message?.takeIf { it.isNotBlank() }
            ?: error::class.simpleName
            ?: error.javaClass.name
}
