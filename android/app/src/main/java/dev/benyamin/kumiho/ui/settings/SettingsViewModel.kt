package dev.benyamin.kumiho.ui.settings

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dev.benyamin.kumiho.data.LanguageRepository
import dev.benyamin.kumiho.engine.RelayTest
import dev.benyamin.kumiho.engine.Settings
import dev.benyamin.kumiho.vpn.VpnServiceCoordinator
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/**
 * The engine calls Settings makes. It exists so the view model is a plain JVM
 * state machine: production talks to the app-scoped [VpnServiceCoordinator],
 * every JVM test uses an in-memory fake.
 *
 * There is deliberately no kill-switch or autoconnect method: Android owns
 * those rows, and a gateway with no such call cannot write them by accident
 * (A10).
 */
interface SettingsGateway {
    /** The runtime snapshot from `settings_get`. */
    suspend fun settings(): Settings

    /** One `settings_set`; [reset] drops the runtime override instead. */
    suspend fun setSetting(key: String, value: String, reset: Boolean = false)

    /** Probes the configured relay once; never touches the data plane. */
    suspend fun relayTest(): RelayTest
}

/** The production gateway over the same engine the other screens use. */
class CoordinatorSettingsGateway(
    private val coordinator: VpnServiceCoordinator,
) : SettingsGateway {
    override suspend fun settings(): Settings = coordinator.awaitEngine().settings()

    override suspend fun setSetting(key: String, value: String, reset: Boolean) {
        coordinator.awaitEngine().setSetting(key, value, reset)
    }

    override suspend fun relayTest(): RelayTest = coordinator.awaitEngine().relayTest()
}

/**
 * The Settings state machine: load/retry, the collapsed group, one engine
 * change at a time, the reload after a write (`settings_set` only pushes
 * status) and the override marks. The update state is owned by the
 * app-scoped [dev.benyamin.kumiho.update.UpdateManager].
 *
 * A mutation never guesses: the visible values come from the engine snapshot,
 * so a failed change leaves the old snapshot and a successful one is only
 * believed after a fresh `settings_get`. `kill_switch`/`autoconnect` are
 * rejected before any engine call.
 */
class SettingsViewModel(
    private val gateway: SettingsGateway,
    val appVersion: String,
    val engineVersion: String,
) : ViewModel() {

    private val mutableState = MutableStateFlow(
        SettingsState(appVersion = appVersion, engineVersion = engineVersion),
    )

    /** The whole screen state; the UI only reads it. */
    val state: StateFlow<SettingsState> = mutableState.asStateFlow()

    private var loadJob: Job? = null

    init {
        load()
    }

    // ---- load ------------------------------------------------------------

    fun retryLoad() = load()

    /** Clears a notice and asks the engine again, keeping the last snapshot on failure. */
    fun refresh() = load()

    private fun load() {
        if (loadJob?.isActive == true) return
        mutableState.update { current ->
            current.copy(
                loading = !current.hasSnapshot,
                loadError = null,
                notice = null,
            )
        }
        loadJob = viewModelScope.launch {
            try {
                val snapshot = gateway.settings()
                mutableState.update { current ->
                    current.copy(
                        snapshot = snapshot,
                        loaded = true,
                        loading = false,
                        loadError = null,
                        notice = null,
                    )
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                val detail = messageOf(error)
                mutableState.update { current ->
                    if (current.hasSnapshot) {
                        current.copy(
                            loading = false,
                            notice = SettingsNotice(
                                message = SettingsMessage.REFRESH_FAILED,
                                detail = detail,
                                tone = SettingsNoticeTone.WARNING,
                            ),
                        )
                    } else {
                        current.copy(
                            loading = false,
                            loaded = false,
                            loadError = detail,
                        )
                    }
                }
            }
        }
    }

    // ---- groups ----------------------------------------------------------

    fun toggleAdvanced() {
        mutableState.update { it.copy(advancedExpanded = !it.advancedExpanded) }
    }

    // ---- engine rows -----------------------------------------------------

    /** A toggle row: the screen already turned it into "on"/"off". */
    fun toggle(key: String, checked: Boolean) {
        apply(key, if (checked) "on" else "off")
    }

    /**
     * Validates and sends one value. A client error becomes an inline row error
     * and never reaches the engine; a second change while one is in flight is
     * ignored outright.
     */
    fun apply(key: String, raw: String) {
        val spec = SettingsCatalog.byKey(key) ?: return
        if (spec.readOnly || spec.kind == SettingKind.ACTION) return
        if (mutableState.value.applying) return
        when (val input = SettingsMapper.serialize(spec, raw)) {
            is SettingInput.Invalid -> {
                mutableState.update {
                    it.copy(rowError = SettingsRowError(key = key, validation = input))
                }
            }
            is SettingInput.Valid -> mutate(SettingChange(key = key, value = input.value))
        }
    }

    /**
     * Writes the app language. It is app-level, not a catalog row: the row has
     * its own three-way control, and an unknown value falls back to `system`.
     */
    fun setLanguage(value: String) {
        if (mutableState.value.applying) return
        mutate(SettingChange(key = "language", value = LanguageRepository.normalize(value)))
    }

    /** Drops the engine override; read-only rows are refused. */
    fun reset(key: String) {
        val spec = SettingsCatalog.byKey(key) ?: return
        if (spec.readOnly || spec.kind == SettingKind.ACTION) return
        if (mutableState.value.applying) return
        val change = SettingsMapper.reset(spec) ?: return
        mutate(change)
    }

    private fun mutate(change: SettingChange) {
        if (mutableState.value.applying) return
        mutableState.update {
            it.copy(
                applyingKey = change.key,
                rowError = null,
                notice = null,
                loadError = null,
                relayTest = if (change.key == "relay_url" || change.key == "relay_token") {
                    RelayTestState.Idle
                } else {
                    it.relayTest
                },
            )
        }
        viewModelScope.launch {
            try {
                gateway.setSetting(change.key, change.value, change.reset)
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                // The old snapshot stays on screen; the engine's own message is inline.
                mutableState.update {
                    it.copy(
                        applyingKey = null,
                        rowError = SettingsRowError(key = change.key, text = messageOf(error)),
                    )
                }
                return@launch
            }
            reloadAfterChange(change.key)
        }
    }

    /**
     * `settings_set` pushes status but not settings, so a successful change is
     * only visible after a fresh snapshot. A refresh failure keeps the old
     * snapshot and offers a retry; it never claims the new value.
     */
    private suspend fun reloadAfterChange(key: String) {
        try {
            val snapshot = gateway.settings()
            mutableState.update {
                it.copy(
                    snapshot = snapshot,
                    loaded = true,
                    loading = false,
                    loadError = null,
                    applyingKey = null,
                    rowError = null,
                    notice = null,
                )
            }
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            mutableState.update {
                it.copy(
                    applyingKey = null,
                    notice = SettingsNotice(
                        message = SettingsMessage.REFRESH_AFTER_SAVE_FAILED,
                        detail = messageOf(error),
                        tone = SettingsNoticeTone.WARNING,
                    ),
                )
            }
        }
    }

    /** A new edit or a reopened editor clears the last inline error. */
    fun clearError(key: String) {
        mutableState.update { current ->
            if (current.rowError?.key == key) current.copy(rowError = null) else current
        }
    }

    // ---- notices ---------------------------------------------------------

    fun dismissNotice() {
        mutableState.update { it.copy(notice = null) }
    }

    fun retryNotice() {
        if (mutableState.value.notice?.retry != true) return
        refresh()
    }

    // ---- relay test ------------------------------------------------------

    /**
     * One explicit probe of the configured relay. It never writes a setting
     * and never exposes the token: the result carries only a status, latency
     * or a transport-level error.
     */
    fun testRelay() {
        if (mutableState.value.relayTest is RelayTestState.Running) return
        mutableState.update { it.copy(relayTest = RelayTestState.Running) }
        viewModelScope.launch {
            val next = try {
                relayState(gateway.relayTest())
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                RelayTestState.Unreachable(messageOf(error))
            }
            mutableState.update { it.copy(relayTest = next) }
        }
    }

    private fun relayState(result: RelayTest): RelayTestState = when {
        result.ok -> RelayTestState.Ok(result.latencyMs)
        result.status == 401 || result.status == 403 -> RelayTestState.Rejected
        else -> RelayTestState.Unreachable(
            result.error?.takeIf { it.isNotBlank() }
                ?: if (result.status != 0) "HTTP ${result.status}" else "",
        )
    }

    private fun messageOf(error: Throwable): String =
        error.message?.takeIf { it.isNotBlank() }
            ?: error::class.simpleName
            ?: "unknown error"
}
