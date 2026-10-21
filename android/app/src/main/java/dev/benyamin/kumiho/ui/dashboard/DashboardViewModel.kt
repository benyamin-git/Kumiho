package dev.benyamin.kumiho.ui.dashboard

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.Notice
import dev.benyamin.kumiho.vpn.ConnectRequest
import dev.benyamin.kumiho.vpn.ServiceState
import dev.benyamin.kumiho.vpn.VpnServiceCoordinator
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.channelFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/**
 * The engine/service calls the dashboard makes. It exists so the view model is
 * a plain JVM state machine: the production implementation talks to the
 * app-scoped [VpnServiceCoordinator], and every dashboard test uses a fake.
 */
interface DashboardGateway {
    /** The service's mirrored state; T9 keeps it fresh. */
    val service: StateFlow<ServiceState>

    /** Asks the engine for a fresh status snapshot. */
    suspend fun refreshStatus()

    /** The server-list tree, for name resolution and the compact picker. */
    suspend fun locations(): List<Country>

    /** Advisory engine notices (`signed_out`, `fatal`, protocol errors). */
    val notices: Flow<Notice>

    /** One-way connect command; the service is authoritative afterwards. */
    fun connect(request: ConnectRequest)

    /** One-way disconnect command. */
    fun disconnect()

    /** Re-reads Android's Always-on/lockdown flags from the bound service (R16). */
    fun refreshPolicy()
}

/** The production gateway: the same control protocol the setup flow uses. */
class CoordinatorDashboardGateway(
    private val coordinator: VpnServiceCoordinator,
) : DashboardGateway {
    override val service: StateFlow<ServiceState> = coordinator.state

    /**
     * Notices come from whichever engine the coordinator currently holds; a
     * rebind switches the source without the dashboard re-subscribing.
     */
    override val notices: Flow<Notice> = channelFlow {
        coordinator.engine.filterNotNull().collectLatest { client ->
            client.notices.collect { send(it) }
        }
    }

    override suspend fun refreshStatus() {
        coordinator.awaitEngine().refreshStatus()
    }

    override suspend fun locations(): List<Country> = coordinator.awaitEngine().locations()

    override fun connect(request: ConnectRequest) = coordinator.connect(request)

    override fun disconnect() = coordinator.disconnect()

    override fun refreshPolicy() {
        coordinator.refreshPolicy()
    }
}

/** The Android permission fact the dashboard needs; faked in JVM tests. */
interface DashboardPermissions {
    /** True when `VpnService.prepare` has nothing left to ask. */
    fun vpnConsentGranted(): Boolean
}

/** One-shot requests the route turns into ActivityResult launches. */
sealed interface DashboardEvent {
    /** Launch Android's VPN consent dialog (or re-check when it is already granted). */
    data object RequestVpnPermission : DashboardEvent

    /** Open Android's VPN settings for Always-on/lockdown. */
    data object OpenVpnSettings : DashboardEvent
}

/**
 * The dashboard state machine: phase, power action, one-in-flight guard,
 * refresh, notices, the options sheet and the compact picker.
 *
 * It contains no Android call and no gomobile call. The service remains the
 * source of truth: a connect is a one-way command, the visible state follows
 * [DashboardGateway.service], and a dashboard never issues a second connect
 * while a session is active (the T12 disconnect-first ruling).
 */
class DashboardViewModel(
    private val gateway: DashboardGateway,
    private val permissions: DashboardPermissions,
    private val inFlightTimeoutMillis: Long = DEFAULT_IN_FLIGHT_TIMEOUT_MS,
) : ViewModel() {

    private val mutableState = MutableStateFlow(DashboardState.fromService(gateway.service.value))

    /** The whole screen state; the UI only reads it. */
    val state: StateFlow<DashboardState> = mutableState.asStateFlow()

    private val eventFlow = MutableSharedFlow<DashboardEvent>(
        extraBufferCapacity = EVENT_BUFFER,
        onBufferOverflow = BufferOverflow.DROP_OLDEST,
    )

    /** One-shot permission/settings requests, collected by the route. */
    val events: SharedFlow<DashboardEvent> = eventFlow.asSharedFlow()

    /** The phase the current command started from, to detect a real change. */
    private var preCommandPhase: DashboardPhase = DashboardPhase.LOADING

    private var inFlightJob: Job? = null
    private var loadJob: Job? = null

    /** The last service error already turned into a notice; avoids repeats. */
    private var lastServiceError: String? = gateway.service.value.error

    init {
        viewModelScope.launch {
            gateway.service.collect(::onService)
        }
        viewModelScope.launch {
            gateway.notices.collect { notice ->
                mutableState.update { it.copy(notice = DashboardNotice.engineNotice(notice)) }
            }
        }
        loadLocations()
    }

    // ---- service updates -------------------------------------------------

    private fun onService(snapshot: ServiceState) {
        val phase = DashboardPhase.from(snapshot)
        val error = snapshot.error
        mutableState.update { current ->
            var next = current.copy(
                phase = phase,
                status = snapshot.status,
                alwaysOn = snapshot.alwaysOn,
                lockdown = snapshot.lockdown,
            )
            // A new service error supersedes whatever the row showed; a
            // dismissed one stays dismissed until the service reports a new one.
            if (error != null && error != lastServiceError) {
                next = next.copy(notice = DashboardNotice.serviceError(error))
            }
            next
        }
        lastServiceError = error
        if (inFlightJob != null && phase != preCommandPhase) settleInFlight()
    }

    // ---- power -----------------------------------------------------------

    /**
     * The single power action. A second tap while a command is in flight, or
     * while the service says CONNECTING, does nothing: no duplicate intent.
     */
    fun onPower() {
        val current = mutableState.value
        if (current.inFlight || current.phase == DashboardPhase.CONNECTING) return
        when (current.phase) {
            DashboardPhase.CONNECTED,
            DashboardPhase.PROXY_ONLY,
            DashboardPhase.RECONNECTING,
            DashboardPhase.WAITING,
            -> {
                if (current.lockdown) {
                    mutableState.update { it.copy(lockdownPrompt = true) }
                } else {
                    disconnectNow()
                }
            }
            DashboardPhase.OFF, DashboardPhase.ERROR -> connectNow()
            DashboardPhase.NOT_SIGNED_IN -> showNotice(DashboardNotice.signInRequired())
            DashboardPhase.SERVICE_UNAVAILABLE -> showNotice(DashboardNotice.serviceUnavailable())
            DashboardPhase.PERMISSION_NEEDED -> requestVpnPermission()
            DashboardPhase.LOADING, DashboardPhase.CONNECTING -> Unit
        }
    }

    /** The user returned from Android's VPN consent dialog. */
    fun onVpnConsentResult(granted: Boolean) {
        if (granted || permissions.vpnConsentGranted()) {
            // The consent is real now; do not re-check-and-re-request in a loop.
            issueConnect(mutableState.value.connectRequest())
        } else {
            // A consent asked for from the sheet leaves it open, and the notice
            // would land behind the scrim; close it so the denial is visible.
            mutableState.update {
                it.copy(
                    options = it.options.copy(open = false),
                    notice = DashboardNotice.permissionDenied(),
                )
            }
        }
    }

    /** Confirms the lockdown disconnect explanation. */
    fun confirmLockdownDisconnect() {
        mutableState.update { it.copy(lockdownPrompt = false) }
        disconnectNow()
    }

    /** Keeps the connection: lockdown stays in force until the user reconnects. */
    fun cancelLockdownPrompt() {
        mutableState.update { it.copy(lockdownPrompt = false) }
    }

    private fun connectNow() {
        if (!permissions.vpnConsentGranted()) {
            requestVpnPermission()
            return
        }
        issueConnect(mutableState.value.connectRequest())
    }

    private fun requestVpnPermission() {
        eventFlow.tryEmit(DashboardEvent.RequestVpnPermission)
    }

    private fun issueConnect(request: ConnectRequest) {
        val current = mutableState.value
        // Defence in depth: the service refuses a connect while active, and the
        // dashboard must never send one either (T12 changes location by
        // disconnecting first).
        if (current.phase.active || current.phase == DashboardPhase.CONNECTING) return
        if (current.phase == DashboardPhase.LOADING) return
        mutableState.update {
            it.copy(options = it.options.copy(open = false), notice = null)
        }
        beginInFlight()
        try {
            gateway.connect(request)
        } catch (error: RuntimeException) {
            settleInFlight()
            showNotice(DashboardNotice.connectFailed(messageOf(error)))
        }
    }

    private fun disconnectNow() {
        mutableState.update { it.copy(notice = null) }
        beginInFlight()
        try {
            gateway.disconnect()
        } catch (error: RuntimeException) {
            settleInFlight()
            showNotice(DashboardNotice.disconnectFailed(messageOf(error)))
        }
    }

    /**
     * The in-flight guard ends when the service leaves the phase the command
     * started from, or after the bounded timeout, so a lost intent cannot wedge
     * the power control forever. The service stays authoritative either way.
     */
    private fun beginInFlight() {
        preCommandPhase = mutableState.value.phase
        mutableState.update { it.copy(inFlight = true) }
        inFlightJob?.cancel()
        inFlightJob = viewModelScope.launch {
            delay(inFlightTimeoutMillis)
            inFlightJob = null
            mutableState.update { it.copy(inFlight = false) }
        }
    }

    private fun settleInFlight() {
        val job = inFlightJob
        inFlightJob = null
        job?.cancel()
        mutableState.update { it.copy(inFlight = false) }
    }

    // ---- resume / refresh ------------------------------------------------

    /**
     * A resume refreshes the status and re-reads Android's Always-on/lockdown
     * policy (the user may have changed it in system settings). A refresh
     * failure keeps every card's last status and offers a Retry.
     */
    fun onResume() {
        gateway.refreshPolicy()
        refreshStatus()
    }

    private fun refreshStatus() {
        viewModelScope.launch {
            try {
                gateway.refreshStatus()
                if (mutableState.value.notice?.isStatusRefresh == true) {
                    mutableState.update { it.copy(notice = null) }
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                showNotice(DashboardNotice.statusRefreshFailed(messageOf(error)))
            }
        }
    }

    // ---- notices ---------------------------------------------------------

    /** Dismisses the current notice without changing any service state. */
    fun dismissNotice() {
        mutableState.update { it.copy(notice = null) }
    }

    /** Runs the one retry the notice offers, when it offers one. */
    fun retryNotice() {
        val notice = mutableState.value.notice ?: return
        when (notice.action) {
            DashboardNoticeAction.RETRY_REFRESH -> {
                mutableState.update { it.copy(notice = null) }
                refreshStatus()
            }
            DashboardNoticeAction.RETRY_CONNECT -> {
                mutableState.update { it.copy(notice = null) }
                onPower()
            }
            DashboardNoticeAction.GRANT_PERMISSION -> requestVpnPermission()
            DashboardNoticeAction.RELOAD_LOCATIONS -> {
                mutableState.update { it.copy(notice = null) }
                loadLocations()
            }
            DashboardNoticeAction.NONE -> Unit
        }
    }

    /** Android would not open its VPN settings screen. */
    fun onSettingsUnavailable() {
        showNotice(DashboardNotice.settingsUnavailable())
    }

    /** The user asked for Android's VPN settings. */
    fun openVpnSettings() {
        eventFlow.tryEmit(DashboardEvent.OpenVpnSettings)
    }

    private fun showNotice(notice: DashboardNotice) {
        mutableState.update { it.copy(notice = notice) }
    }

    // ---- options sheet ---------------------------------------------------

    /** Opens the sheet, hydrates its initial choice and loads the tree. */
    fun openOptions() {
        gateway.refreshPolicy()
        mutableState.update { current ->
            current.copy(
                options = current.options.copy(
                    open = true,
                    choice = current.options.choice ?: current.defaultChoice,
                ),
            )
        }
        if (mutableState.value.locations !is LocationLoad.Ready) loadLocations()
    }

    fun closeOptions() {
        mutableState.update { it.copy(options = it.options.copy(open = false)) }
    }

    fun selectRecommended() {
        select(LocationChoice.Recommended)
    }

    /** Only meaningful when the engine has a persisted selection. */
    fun selectSaved() {
        if (mutableState.value.status?.location == null) return
        select(LocationChoice.Saved)
    }

    fun selectCity(countryCode: String, cityCode: String) {
        select(LocationChoice.Picked(countryCode = countryCode, cityCode = cityCode))
    }

    private fun select(choice: LocationChoice) {
        mutableState.update { it.copy(options = it.options.copy(choice = choice)) }
    }

    /** Expands or collapses one country's city list in the compact picker. */
    fun toggleCountry(code: String) {
        mutableState.update { current ->
            val expanded = current.options.expanded
            current.copy(
                options = current.options.copy(
                    expanded = if (code in expanded) expanded - code else expanded + code,
                ),
            )
        }
    }

    fun setProxyOnly(proxyOnly: Boolean) {
        mutableState.update { it.copy(options = it.options.copy(proxyOnly = proxyOnly)) }
    }

    /** The options sheet's Connect; disabled while a session is active. */
    fun connectFromOptions() {
        if (!mutableState.value.optionsConnectEnabled) return
        if (!permissions.vpnConsentGranted()) {
            requestVpnPermission()
            return
        }
        issueConnect(mutableState.value.connectRequest())
    }

    // ---- locations -------------------------------------------------------

    /** Loads the tree once for name resolution; a retry keeps a good tree. */
    fun loadLocations() {
        loadJob?.cancel()
        if (mutableState.value.locations !is LocationLoad.Ready) {
            mutableState.update { it.copy(locations = LocationLoad.Loading) }
        }
        loadJob = viewModelScope.launch {
            try {
                val countries = gateway.locations()
                mutableState.update { it.copy(locations = LocationLoad.Ready(countries)) }
                if (mutableState.value.notice?.action == DashboardNoticeAction.RELOAD_LOCATIONS) {
                    mutableState.update { it.copy(notice = null) }
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                val detail = messageOf(error)
                mutableState.update { current ->
                    // A refresh failure must not throw away a tree that worked.
                    val kept = current.locations as? LocationLoad.Ready
                    current.copy(
                        locations = kept ?: LocationLoad.Failed(detail),
                        notice = DashboardNotice.locationsFailed(detail),
                    )
                }
            }
        }
    }

    private fun messageOf(error: Throwable): String =
        error.message?.takeIf { it.isNotBlank() }
            ?: error::class.simpleName
            ?: "unknown error"

    private companion object {
        /** The bounded guard; a connect can legitimately take up to 120 s. */
        const val DEFAULT_IN_FLIGHT_TIMEOUT_MS = 130_000L
        const val EVENT_BUFFER = 4
    }
}
