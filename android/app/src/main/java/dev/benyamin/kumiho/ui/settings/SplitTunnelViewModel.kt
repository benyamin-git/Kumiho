package dev.benyamin.kumiho.ui.settings

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dev.benyamin.kumiho.data.InstalledApp
import dev.benyamin.kumiho.data.InstalledAppsSource
import dev.benyamin.kumiho.data.SplitTunnelRepository
import dev.benyamin.kumiho.vpn.SplitMode
import dev.benyamin.kumiho.vpn.SplitSettings
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/**
 * Everything the stateless split screen renders. The repository owns the
 * persisted selection; the view model adds the installed-app list and the two
 * picker controls (search and the System-apps reveal).
 */
data class SplitTunnelUiState(
    val settings: SplitSettings = SplitSettings(),
    val loading: Boolean = true,
    val repositoryError: String? = null,
    val apps: List<InstalledApp> = emptyList(),
    val appsLoading: Boolean = true,
    val appsError: String? = null,
    val search: String = "",
    val showSystem: Boolean = false,
) {
    val mode: SplitMode get() = settings.mode

    /** The saved selection size; Kumiho itself is never counted. */
    val selectedCount: Int get() = settings.packages.size

    /** Include with no apps would route nothing, so the engine falls back. */
    val warnEmptyInclude: Boolean get() = mode == SplitMode.INCLUDE && settings.packages.isEmpty()
}

/**
 * The split-tunnel state machine. Mode and package changes go straight to the
 * repository, which persists them immediately; nothing here connects, applies
 * or reconnects. The installed-app read is off the main thread and retryable.
 */
class SplitTunnelViewModel(
    private val repository: SplitTunnelRepository,
    private val appsSource: InstalledAppsSource,
) : ViewModel() {

    private val mutableState = MutableStateFlow(SplitTunnelUiState())

    /** The whole screen state; the UI only reads it. */
    val state: StateFlow<SplitTunnelUiState> = mutableState.asStateFlow()

    private var appsJob: Job? = null

    init {
        repository.start()
        viewModelScope.launch {
            repository.state.collect { stored ->
                mutableState.update {
                    it.copy(
                        settings = stored.settings,
                        loading = stored.loading,
                        repositoryError = stored.error,
                    )
                }
            }
        }
        loadApps()
    }

    fun setMode(mode: SplitMode) {
        repository.setMode(mode)
    }

    fun togglePackage(packageName: String) {
        repository.togglePackage(packageName)
    }

    fun onSearchChange(query: String) {
        mutableState.update { it.copy(search = query) }
    }

    fun clearSearch() {
        mutableState.update { it.copy(search = "") }
    }

    fun setShowSystem(show: Boolean) {
        mutableState.update { it.copy(showSystem = show) }
    }

    fun retryRepository() {
        repository.retry()
    }

    fun retryApps() {
        loadApps()
    }

    private fun loadApps() {
        if (appsJob?.isActive == true) return
        mutableState.update { it.copy(appsLoading = true, appsError = null) }
        appsJob = viewModelScope.launch {
            try {
                val apps = appsSource.load()
                mutableState.update {
                    it.copy(apps = apps, appsLoading = false, appsError = null)
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                mutableState.update {
                    it.copy(
                        appsLoading = false,
                        appsError = error.message?.takeIf { message -> message.isNotBlank() }
                            ?: error::class.simpleName
                            ?: "unknown error",
                    )
                }
            }
        }
    }
}
