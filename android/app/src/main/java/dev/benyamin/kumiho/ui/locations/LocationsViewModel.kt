package dev.benyamin.kumiho.ui.locations

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.engine.PingResult
import dev.benyamin.kumiho.engine.Server
import dev.benyamin.kumiho.ui.dashboard.DashboardPhase
import dev.benyamin.kumiho.vpn.ServiceState
import dev.benyamin.kumiho.vpn.VpnServiceCoordinator
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.channelFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/**
 * The engine/service calls the Locations screen makes. It exists so the view
 * model is a plain JVM state machine: the production implementation talks to
 * the app-scoped [VpnServiceCoordinator], and every JVM test uses a fake.
 *
 * There is deliberately no connect method: `select_location` persists a choice
 * and updates the dashboard, and changing the live location is a disconnect
 * followed by a connect on Home (the T12 ruling). A gateway with no connect
 * cannot grow one by accident.
 */
interface LocationsGateway {
    /** The service's mirrored state; the current selection and session phase come from here. */
    val service: StateFlow<ServiceState>

    /** The server-list tree, optionally forcing a refresh. */
    suspend fun locations(refresh: Boolean): List<Country>

    /** Persists a location choice; never connects. */
    suspend fun selectLocation(countryCode: String, cityCode: String?, server: String)

    /** Starts the engine's 24-way probes; results stream through [pings]. */
    suspend fun ping(hosts: List<String>)

    /** One result per requested host; a null RTT means the probe timed out. */
    val pings: Flow<PingResult>
}

/** The production gateway: the same engine the dashboard and setup use. */
class CoordinatorLocationsGateway(
    private val coordinator: VpnServiceCoordinator,
) : LocationsGateway {
    override val service: StateFlow<ServiceState> = coordinator.state

    /**
     * Pings come from whichever engine the coordinator currently holds; a
     * rebind switches the source without the screen re-subscribing. This is
     * T11's notice pattern, applied to the engine's ping flow.
     */
    override val pings: Flow<PingResult> = channelFlow {
        coordinator.engine.filterNotNull().collectLatest { client ->
            client.pings.collect { send(it) }
        }
    }

    override suspend fun locations(refresh: Boolean): List<Country> =
        coordinator.awaitEngine().locations(refresh)

    override suspend fun selectLocation(countryCode: String, cityCode: String?, server: String) {
        coordinator.awaitEngine().selectLocation(countryCode, cityCode, server)
    }

    override suspend fun ping(hosts: List<String>) {
        coordinator.awaitEngine().ping(hosts)
    }
}

/** A selection request: exactly what `select_location` receives. */
data class SelectionRequest(
    val countryCode: String,
    val cityCode: String?,
    val address: String,
)

/** Copy the view model generates itself; the screen resolves it to a resource. */
enum class LocationsMessage {
    REFRESH_FAILED,
    SELECT_FAILED,
    TEST_FAILED,
}

/** Notice text: either the engine's own words or a local message code. */
sealed interface LocationsNoticeText {
    data class Raw(val text: String) : LocationsNoticeText

    data class Local(val message: LocationsMessage, val detail: String? = null) : LocationsNoticeText
}

/** The notice row's tone; meaning is always also carried by an icon. */
enum class LocationsNoticeTone { WARNING, ERROR }

/** The one retry a notice can offer. */
enum class LocationsNoticeAction { NONE, RETRY_REFRESH, RETRY_SELECT, RETRY_TEST }

/** The latest advisory message from a load, a save or a speed test. */
data class LocationsNotice(
    val text: LocationsNoticeText,
    val tone: LocationsNoticeTone,
    val action: LocationsNoticeAction = LocationsNoticeAction.NONE,
    /** The failed save a RETRY_SELECT repeats. */
    val request: SelectionRequest? = null,
) {
    companion object {
        fun refreshFailed(detail: String): LocationsNotice = LocationsNotice(
            text = LocationsNoticeText.Local(LocationsMessage.REFRESH_FAILED, detail),
            tone = LocationsNoticeTone.WARNING,
            action = LocationsNoticeAction.RETRY_REFRESH,
        )

        fun selectFailed(detail: String, request: SelectionRequest): LocationsNotice = LocationsNotice(
            text = LocationsNoticeText.Local(LocationsMessage.SELECT_FAILED, detail),
            tone = LocationsNoticeTone.ERROR,
            action = LocationsNoticeAction.RETRY_SELECT,
            request = request,
        )

        fun testFailed(detail: String): LocationsNotice = LocationsNotice(
            text = LocationsNoticeText.Local(LocationsMessage.TEST_FAILED, detail),
            tone = LocationsNoticeTone.WARNING,
            action = LocationsNoticeAction.RETRY_TEST,
        )
    }
}

/**
 * The whole screen state; the view model is its only producer and the screen
 * only reads it. The rows, ping targets and latency cells are derived here
 * through [LocationTree], so every rule is unit-testable on the JVM.
 */
data class LocationsState(
    /** The first load is running and there is no tree to show yet. */
    val loading: Boolean = true,
    /** A refresh is running; an existing tree stays on screen. */
    val refreshing: Boolean = false,
    /** At least one load succeeded (an empty successful load is honest, not an error). */
    val loaded: Boolean = false,
    /** The tree, REC first, as the last successful load returned it. */
    val countries: List<Country> = emptyList(),
    /** The first load failed and there is no tree; only then is it a screen state. */
    val loadError: String? = null,
    val notice: LocationsNotice? = null,
    val query: String = "",
    /** Country expansion keys ([LocationTree.countryKey]). */
    val expandedCountries: Set<String> = emptySet(),
    /** City expansion keys ([LocationTree.cityKey]). */
    val expandedCities: Set<String> = emptySet(),
    /** The engine's reported selection, resolved against the tree. */
    val selection: LocationSelection? = null,
    /** The last RTT per address key; a null value is a timeout, an absent key is untested. */
    val latency: Map<String, Double?> = emptyMap(),
    /** A Test speed run is in flight (its command plus the results it is waiting for). */
    val testing: Boolean = false,
    /** The address key of the server whose save is in flight. */
    val savingServer: String? = null,
    /** A tunnel or proxy session is up, so the saved choice only applies after a reconnect. */
    val sessionActive: Boolean = false,
) {
    val hasTree: Boolean get() = countries.isNotEmpty()

    val searching: Boolean get() = query.isNotBlank()

    /** The visible rows for the current query and expansion state. */
    val rows: List<LocationRow>
        get() = LocationTree.rows(countries, query, expandedCountries, expandedCities)

    /** The `host:port` targets a Test speed would send. */
    val testTargets: List<String> get() = LocationTree.pingTargets(rows)

    /** Honest screen states, each true only when it is the only thing to show. */
    val showLoading: Boolean get() = loading && !hasTree

    val showError: Boolean get() = loadError != null && !hasTree

    val showEmpty: Boolean get() = loaded && !hasTree

    val showNoMatch: Boolean get() = hasTree && searching && rows.isEmpty()

    /** Test speed is offered only when there is something visible to test. */
    val canTest: Boolean get() = !testing && !loading && loadError == null && testTargets.isNotEmpty()

    fun latencyOf(server: Server): LatencyState = LocationTree.latencyOf(latency, server)

    fun isSelected(country: Country, city: City, server: Server): Boolean =
        selection?.marks(country, city, server) == true

    fun isSaving(server: Server): Boolean =
        savingServer == LocationTree.addressKey(LocationTree.addressOf(server))
}

/**
 * The Locations state machine: the tree, the filter, expansion, the current
 * selection from the service, saving a choice and the manual speed test.
 *
 * It contains no Android call and no gomobile call. The service is the source
 * of truth for the current selection: `select_location` persists and pushes a
 * fresh status, and the mark moves when that push arrives, never from an
 * optimistic local guess. Nothing pings unless the user taps Test speed.
 */
class LocationsViewModel(
    private val gateway: LocationsGateway,
    private val pingWindowMillis: Long = DEFAULT_PING_WINDOW_MS,
) : ViewModel() {

    private val mutableState = MutableStateFlow(LocationsState())

    /** The whole screen state; the UI only reads it. */
    val state: StateFlow<LocationsState> = mutableState.asStateFlow()

    /** The latest engine status, kept for re-resolving names after a tree change. */
    private var status: EngineStatus? = null

    private var loadJob: Job? = null
    private var pingJob: Job? = null

    /** Address keys of the current test that have not answered yet. Main-confined. */
    private val pendingPings = mutableSetOf<String>()

    init {
        viewModelScope.launch {
            gateway.service.collect(::onService)
        }
        viewModelScope.launch {
            gateway.pings.collect(::onPing)
        }
        load(refresh = false)
    }

    // ---- service ---------------------------------------------------------

    private fun onService(snapshot: ServiceState) {
        status = snapshot.status
        val active = DashboardPhase.from(snapshot).active
        mutableState.update { it.copy(sessionActive = active) }
        refreshSelection()
    }

    /**
     * Resolves the engine's selection against the current tree. Called on every
     * service update and after every successful load; null before either side
     * knows anything, which the screen renders as "no server selected yet".
     */
    private fun refreshSelection() {
        val current = status
        mutableState.update { state ->
            state.copy(
                selection = LocationTree.currentSelection(
                    countries = state.countries,
                    location = current?.location,
                    server = current?.server,
                ),
            )
        }
    }

    // ---- tree ------------------------------------------------------------

    /** The Refresh action: forces a network refresh and keeps the old tree on failure. */
    fun refresh() = load(refresh = true)

    /** The retry the initial error state offers. */
    fun retryLoad() = load(refresh = mutableState.value.hasTree)

    private fun load(refresh: Boolean) {
        if (loadJob?.isActive == true) return
        mutableState.update { current ->
            current.copy(
                loading = !current.hasTree,
                refreshing = current.hasTree,
                loadError = if (current.hasTree) current.loadError else null,
                notice = if (current.notice?.action == LocationsNoticeAction.RETRY_REFRESH) {
                    null
                } else {
                    current.notice
                },
            )
        }
        loadJob = viewModelScope.launch {
            try {
                val countries = gateway.locations(refresh)
                applyTree(LocationTree.ordered(countries))
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                applyFailure(messageOf(error))
            }
        }
    }

    private fun applyTree(countries: List<Country>) {
        mutableState.update { current ->
            current.copy(
                countries = countries,
                loaded = true,
                loading = false,
                refreshing = false,
                loadError = null,
                notice = if (current.notice?.action == LocationsNoticeAction.RETRY_REFRESH) {
                    null
                } else {
                    current.notice
                },
            )
        }
        refreshSelection()
    }

    private fun applyFailure(detail: String) {
        mutableState.update { current ->
            if (current.hasTree) {
                // A refresh failure must never blank a tree that worked.
                current.copy(
                    loading = false,
                    refreshing = false,
                    notice = LocationsNotice.refreshFailed(detail),
                )
            } else {
                current.copy(
                    loading = false,
                    refreshing = false,
                    loaded = false,
                    countries = emptyList(),
                    loadError = detail,
                )
            }
        }
    }

    // ---- filter / expansion ---------------------------------------------

    /** Every keystroke only touches the query: no load, no ping, no network. */
    fun onQueryChange(query: String) {
        mutableState.update { it.copy(query = query) }
    }

    fun clearQuery() {
        mutableState.update { it.copy(query = "") }
    }

    fun toggleCountry(code: String) {
        val key = code.trim().lowercase()
        mutableState.update { current ->
            current.copy(
                expandedCountries = if (key in current.expandedCountries) {
                    current.expandedCountries - key
                } else {
                    current.expandedCountries + key
                },
            )
        }
    }

    fun toggleCity(countryCode: String, cityCode: String) {
        val key = countryCode.trim().lowercase() + "/" + cityCode.trim().lowercase()
        mutableState.update { current ->
            current.copy(
                expandedCities = if (key in current.expandedCities) {
                    current.expandedCities - key
                } else {
                    current.expandedCities + key
                },
            )
        }
    }

    // ---- selection -------------------------------------------------------

    /** A server row's tap; quarantined servers are not selectable. */
    fun select(country: Country, city: City, server: Server) {
        if (server.quarantined) return
        select(
            SelectionRequest(
                countryCode = country.code,
                cityCode = city.code,
                address = LocationTree.addressOf(server),
            ),
        )
    }

    /**
     * Persists one choice. It never connects and never marks the row locally:
     * the engine pushes the new status and the mark follows that.
     */
    fun select(request: SelectionRequest) {
        if (mutableState.value.savingServer != null) return
        mutableState.update { current ->
            current.copy(
                savingServer = LocationTree.addressKey(request.address),
                notice = if (current.notice?.action == LocationsNoticeAction.RETRY_SELECT) {
                    null
                } else {
                    current.notice
                },
            )
        }
        viewModelScope.launch {
            try {
                gateway.selectLocation(request.countryCode, request.cityCode, request.address)
                mutableState.update { it.copy(savingServer = null) }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                mutableState.update {
                    it.copy(
                        savingServer = null,
                        notice = LocationsNotice.selectFailed(messageOf(error), request),
                    )
                }
            }
        }
    }

    // ---- speed test ------------------------------------------------------

    /**
     * The only call that pings: the currently visible server rows, deduped by
     * address. Results replace the values for the hosts this run tested as they
     * arrive; every other host keeps its last value.
     */
    fun testSpeed() {
        val current = mutableState.value
        val targets = current.testTargets
        if (targets.isEmpty() || current.testing) return

        pendingPings.clear()
        targets.mapTo(pendingPings, LocationTree::addressKey)
        mutableState.update {
            it.copy(
                testing = true,
                notice = if (it.notice?.action == LocationsNoticeAction.RETRY_TEST) {
                    null
                } else {
                    it.notice
                },
            )
        }
        pingJob?.cancel()
        pingJob = viewModelScope.launch {
            // The engine's per-host probe deadline is 10 s; this bounds the
            // "Testing…" state when a host never answers at all.
            delay(pingWindowMillis)
            stopTesting()
        }
        viewModelScope.launch {
            try {
                gateway.ping(targets)
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                stopTesting()
                mutableState.update { it.copy(notice = LocationsNotice.testFailed(messageOf(error))) }
            }
        }
    }

    private fun onPing(result: PingResult) {
        val key = LocationTree.addressKey(result.host)
        val wasPending = pendingPings.remove(key)
        mutableState.update { current ->
            current.copy(
                latency = current.latency + (key to result.rttMs),
                testing = if (wasPending && pendingPings.isEmpty()) false else current.testing,
            )
        }
        if (wasPending && pendingPings.isEmpty()) stopTesting()
    }

    private fun stopTesting() {
        pendingPings.clear()
        pingJob?.cancel()
        pingJob = null
        mutableState.update { it.copy(testing = false) }
    }

    // ---- notices ---------------------------------------------------------

    fun dismissNotice() {
        mutableState.update { it.copy(notice = null) }
    }

    /** Runs the one retry the notice offers, when it offers one. */
    fun retryNotice() {
        val notice = mutableState.value.notice ?: return
        when (notice.action) {
            LocationsNoticeAction.RETRY_REFRESH -> {
                mutableState.update { it.copy(notice = null) }
                refresh()
            }
            LocationsNoticeAction.RETRY_SELECT -> {
                val request = notice.request ?: return
                mutableState.update { it.copy(notice = null) }
                select(request)
            }
            LocationsNoticeAction.RETRY_TEST -> {
                mutableState.update { it.copy(notice = null) }
                testSpeed()
            }
            LocationsNoticeAction.NONE -> Unit
        }
    }

    private fun messageOf(error: Throwable): String =
        error.message?.takeIf { it.isNotBlank() }
            ?: error::class.simpleName
            ?: "unknown error"

    private companion object {
        /** Longer than the engine's 10 s per-host probe deadline. */
        const val DEFAULT_PING_WINDOW_MS = 12_000L
    }
}
