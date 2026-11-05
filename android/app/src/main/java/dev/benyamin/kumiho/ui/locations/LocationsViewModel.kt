package dev.benyamin.kumiho.ui.locations

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.engine.LocationRef
import dev.benyamin.kumiho.engine.LocationsResult
import dev.benyamin.kumiho.engine.PingDone
import dev.benyamin.kumiho.engine.PingResult
import dev.benyamin.kumiho.engine.Server
import dev.benyamin.kumiho.vpn.ConnectRequest
import dev.benyamin.kumiho.vpn.EngineStates
import dev.benyamin.kumiho.vpn.ServiceState
import dev.benyamin.kumiho.vpn.VpnServiceCoordinator
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
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
 * The engine/service calls the Locations screen needs; a fake implements this
 * in the JVM tests. Selection persists a choice, but this screen never
 * connects and never owns a tunnel.
 */
interface LocationsGateway {
    val service: StateFlow<ServiceState>

    /** Results arrive independently of the [ping] call's return. */
    val pings: Flow<PingResult>

    /** Exactly one run-level completion event per Ping all. */
    val pingDone: Flow<PingDone>

    suspend fun locations(refresh: Boolean): LocationsResult

    suspend fun selectLocation(countryCode: String, cityCode: String?, server: String)

    /** Resolves and persists the lowest fresh finite RTT server. */
    suspend fun selectFastest()

    suspend fun favorite(ref: LocationRef, favorite: Boolean)

    suspend fun clearRecents()

    /** Writes `allow_locked`; the next locations read is re-filtered. */
    suspend fun setAllowLocked(enabled: Boolean)

    suspend fun ping(hosts: List<String>)

    /** The engine's fresh status; the switch polls it after disconnecting. */
    suspend fun status(): EngineStatus

    /** Ends the current session; the switch waits for the status to settle. */
    suspend fun disconnect()

    /** Connects again with the saved selection after a switch. */
    suspend fun connect()
}

/** The real gateway over the app-scoped VPN coordinator. */
class CoordinatorLocationsGateway(
    private val coordinator: VpnServiceCoordinator,
) : LocationsGateway {
    override val service: StateFlow<ServiceState> = coordinator.state

    /**
     * Pings come from whichever engine the coordinator currently holds; a
     * rebind switches the source without the screen re-subscribing.
     */
    override val pings: Flow<PingResult> = channelFlow {
        coordinator.engine.filterNotNull().collectLatest { client ->
            client.pings.collect { send(it) }
        }
    }

    override val pingDone: Flow<PingDone> = channelFlow {
        coordinator.engine.filterNotNull().collectLatest { client ->
            client.pingDone.collect { send(it) }
        }
    }

    override suspend fun locations(refresh: Boolean): LocationsResult =
        coordinator.awaitEngine().locationsResult(refresh)

    override suspend fun selectLocation(countryCode: String, cityCode: String?, server: String) {
        coordinator.awaitEngine().selectLocation(countryCode, cityCode, server)
    }

    override suspend fun selectFastest() {
        coordinator.awaitEngine().selectLocation(fastest = true)
    }

    override suspend fun favorite(ref: LocationRef, favorite: Boolean) {
        coordinator.awaitEngine().favoriteLocation(ref, favorite)
    }

    override suspend fun clearRecents() {
        coordinator.awaitEngine().clearRecents()
    }

    override suspend fun setAllowLocked(enabled: Boolean) {
        coordinator.awaitEngine().setSetting("allow_locked", if (enabled) "on" else "off")
    }

    override suspend fun ping(hosts: List<String>) {
        coordinator.awaitEngine().ping(hosts)
    }

    override suspend fun status(): EngineStatus = coordinator.awaitEngine().refreshStatus()

    override suspend fun disconnect() {
        coordinator.disconnect()
    }

    override suspend fun connect() {
        coordinator.connect(ConnectRequest())
    }
}

/** The exact persisted tuple a row tap sends to `select_location`. */
data class SelectionRequest(
    val countryCode: String,
    val cityCode: String?,
    val address: String,
)

/** Local failure copy with an optional detail from the engine. */
enum class LocationsMessage {
    REFRESH_FAILED,
    SELECT_FAILED,
    PING_ALL_FAILED,
    PING_ALL_NO_RESULTS,
}

/** Notice text: either the engine's own words or a local message code. */
sealed interface LocationsNoticeText {
    data class Raw(val text: String) : LocationsNoticeText

    data class Local(val message: LocationsMessage, val detail: String? = null) : LocationsNoticeText
}

/** The notice row's tone; meaning is always also carried by an icon. */
enum class LocationsNoticeTone { WARNING, ERROR }

/** The one retry a notice can offer. */
enum class LocationsNoticeAction { NONE, RETRY_REFRESH, RETRY_SELECT, RETRY_PING_ALL }

/** The latest advisory message from a load, a save or a ping run. */
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

        fun pingAllFailed(detail: String): LocationsNotice = LocationsNotice(
            text = LocationsNoticeText.Local(LocationsMessage.PING_ALL_FAILED, detail),
            tone = LocationsNoticeTone.WARNING,
            action = LocationsNoticeAction.RETRY_PING_ALL,
        )

        fun pingAllNoResults(): LocationsNotice = LocationsNotice(
            text = LocationsNoticeText.Local(LocationsMessage.PING_ALL_NO_RESULTS),
            tone = LocationsNoticeTone.WARNING,
            action = LocationsNoticeAction.RETRY_PING_ALL,
        )
    }
}

/**
 * The whole screen state. The view model is its only producer; the screen only
 * reads it. Rows, ping targets and the latency cells are derived through
 * [LocationTree], so every rule is unit-testable on the JVM.
 */
data class LocationsState(
    /** The first load is running and there is no tree to show yet. */
    val loading: Boolean = true,
    /** A refresh is running; an existing tree stays on screen. */
    val refreshing: Boolean = false,
    /** At least one load succeeded (an empty successful load is honest, not an error). */
    val loaded: Boolean = false,
    /** The loaded tree as the last successful load returned it. */
    val countries: List<Country> = emptyList(),
    /** The first load failed and there is no tree; only then is it a screen state. */
    val loadError: String? = null,
    val notice: LocationsNotice? = null,
    val query: String = "",
    /** The engine's reported selection, resolved against the tree. */
    val selection: LocationSelection? = null,
    /** The last RTT per address key; a null value is a timeout, an absent key is untested. */
    val latency: Map<String, Double?> = emptyMap(),
    /** Country order by default; speed order only for the current visit after a good run. */
    val sortMode: LocationSortMode = LocationSortMode.COUNTRY,
    /** Changes when the list should jump back to its first row. */
    val listResetToken: Int = 0,
    /**
     * The latency snapshot speed order was last committed against. It stays
     * frozen while a newer run is in flight, so rows never jump under the user;
     * null means "use latency" when constructing a state directly.
     */
    val speedOrderLatency: Map<String, Double?>? = null,
    /** A Ping all run is in flight. */
    val pingingAll: Boolean = false,
    /** The static target count shown on the Ping all button for this run. */
    val pingAllCount: Int = 0,
    /** The address key of the server whose save is in flight. */
    val savingServer: String? = null,
    /** A Fastest selection is being resolved and saved. */
    val savingFastest: Boolean = false,
    /** The allow-locked write is in flight. */
    val savingLocked: Boolean = false,
    /** The engine-shared favorites and recents, newest recent first. */
    val favorites: List<LocationRef> = emptyList(),
    val recents: List<LocationRef> = emptyList(),
    /** Whether the locked rows are revealed. */
    val allowLocked: Boolean = false,
    /** The service reports a session family; a row tap asks before switching. */
    val sessionActive: Boolean = false,
    /** Android's Always-on lockdown flag; the switch dialog warns when it is on. */
    val lockdown: Boolean = false,
    /** The row awaiting confirmation for a switch; null while there is no dialog. */
    val pending: SwitchTarget? = null,
    /** The choreography's current step; Idle when no switch is running or failed. */
    val switchStep: SwitchStep = SwitchStep.Idle,
) {
    val hasTree: Boolean get() = countries.isNotEmpty()

    val searching: Boolean get() = query.isNotBlank()

    private val pinnedLatency: Map<String, Double?>
        get() = if (sortMode == LocationSortMode.SPEED) {
            speedOrderLatency ?: latency
        } else {
            latency
        }

    /** The pinned sections and the full list, in display order. */
    val sections: LocationSections
        get() = LocationTree.sections(
            countries = countries,
            favorites = favorites,
            recents = recents,
            query = query,
            latency = pinnedLatency,
            sortMode = sortMode,
            allowLocked = allowLocked,
        )

    /** The visible flat server rows for the current query and sort order. */
    val rows: List<LocationRow> get() = sections.rows

    /** The pinned Fastest shortcut; null disables it with the hint. */
    val fastest: FastestLocation? get() = sections.fastest

    /** Every usable loaded server, independent of the filter. */
    val allTargets: List<String> get() = LocationTree.pingAllTargets(countries, allowLocked)

    /** Honest screen states, each true only when it is the only thing to show. */
    val showLoading: Boolean get() = loading && !hasTree

    val showError: Boolean get() = loadError != null && !hasTree

    val showEmpty: Boolean get() = loaded && !hasTree

    val showNoMatch: Boolean get() = hasTree && searching && rows.isEmpty()

    /** Ping all is offered whenever the loaded tree has a target and no run is active. */
    val canPingAll: Boolean
        get() = !pingingAll && !loading && loadError == null && allTargets.isNotEmpty()

    fun latencyOf(row: LocationRow): LatencyState = LocationTree.latencyOf(latency, row.address)

    fun isSelected(country: Country, city: City, server: Server): Boolean =
        selection?.marks(country, city, server) == true

    fun isSaving(server: Server): Boolean =
        savingServer == LocationTree.connectTarget(server)?.let(LocationTree::addressKey)

    fun isFavorite(row: LocationRow): Boolean =
        LocationTree.isFavorite(favorites, row, countries, allowLocked)

    val canSelectFastest: Boolean
        get() = fastest != null && !savingFastest && savingServer == null && !savingLocked

    fun isFastestRow(row: LocationRow): Boolean =
        fastest?.row?.key == row.key
}

/**
 * The Locations state machine: the flat list, filter, current selection,
 * saving and the manual all-servers ping. It makes no Android or gomobile
 * call; the service is the source of truth for the current selection.
 */
class LocationsViewModel(
    private val gateway: LocationsGateway,
) : ViewModel() {

    private val mutableState = MutableStateFlow(LocationsState())

    /** The whole screen state; the UI only reads it. */
    val state: StateFlow<LocationsState> = mutableState.asStateFlow()

    /** The latest engine status, kept for re-resolving names after a tree change. */
    private var status: EngineStatus? = null

    private var loadJob: Job? = null
    private var pingJob: Job? = null
    private var switchJob: Job? = null
    private val switchLocation = SwitchLocation(gateway)

    init {
        viewModelScope.launch {
            gateway.service.collect(::onService)
        }
        viewModelScope.launch {
            gateway.pings.collect(::onPing)
        }
        viewModelScope.launch {
            gateway.pingDone.collect(::onPingDone)
        }
        load(refresh = false)
    }

    // ---- service ---------------------------------------------------------

    private fun onService(snapshot: ServiceState) {
        status = snapshot.status
        val active = EngineStates.isActive(snapshot.status?.state)
        mutableState.update {
            it.copy(sessionActive = active, lockdown = snapshot.lockdown)
        }
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

    // ---- loading ---------------------------------------------------------

    /** The Refresh action: forces a network refresh, resets RTTs and keeps the old tree on failure. */
    fun refresh() = load(refresh = true)

    /** The retry the initial error state offers. */
    fun retryLoad() = load(refresh = mutableState.value.hasTree)

    private fun load(refresh: Boolean) {
        if (loadJob?.isActive == true) return
        cancelPing()
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
                latency = emptyMap(),
                sortMode = LocationSortMode.COUNTRY,
                listResetToken = current.listResetToken + 1,
                speedOrderLatency = null,
            )
        }
        loadJob = viewModelScope.launch {
            try {
                applyTree(gateway.locations(refresh))
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                applyFailure(messageOf(error))
            }
        }
    }

    private fun applyTree(result: LocationsResult) {
        mutableState.update { current ->
            current.copy(
                countries = result.countries,
                favorites = result.favorites,
                recents = result.recents,
                allowLocked = result.allowLocked,
                latency = cachedLatency(result),
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

    private fun cachedLatency(result: LocationsResult): Map<String, Double?> =
        result.latency.entries.associate { (address, sample) ->
            LocationTree.addressKey(address) to sample.rttMs
        }

    /**
     * Re-reads the tree after a mutation without blanking it or committing a
     * fresh speed order. Pinned refs and allow-locked come from the engine.
     */
    private suspend fun reloadTree() {
        try {
            applyTree(gateway.locations(refresh = false))
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            mutableState.update {
                it.copy(notice = LocationsNotice.refreshFailed(messageOf(error)))
            }
        }
    }

    private fun applyFailure(detail: String) {
        mutableState.update { current ->
            if (current.hasTree) {
                // A refresh failure must never blank a list that worked.
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

    // ---- filter / visit --------------------------------------------------

    /** Every keystroke only touches the query: no load, no ping, no network. */
    fun onQueryChange(query: String) {
        mutableState.update { it.copy(query = query) }
    }

    fun clearQuery() {
        mutableState.update { it.copy(query = "") }
    }

    /**
     * A real re-entry into the screen resets speed order to country order for
     * the new visit. Latency labels stay cached; only their ordering authority
     * ends. Rotation is handled by the route and does not call this.
     */
    fun beginVisit() {
        mutableState.update {
            it.copy(
                sortMode = LocationSortMode.COUNTRY,
                listResetToken = it.listResetToken + 1,
                speedOrderLatency = null,
            )
        }
    }

    // ---- selection -------------------------------------------------------

    /**
     * A server row's tap; unusable servers are not selectable. While a session
     * is active the tap only asks: nothing is persisted or disconnected until
     * the dialog is confirmed.
     */
    fun select(country: Country, city: City, server: Server) {
        if (!LocationTree.isUsable(server)) return
        val address = LocationTree.connectTarget(server) ?: return
        val request = SelectionRequest(
            countryCode = country.code,
            cityCode = city.code,
            address = address,
        )
        if (mutableState.value.sessionActive) {
            val row = LocationRow(country, city, server, address)
            mutableState.update {
                it.copy(
                    pending = SwitchTarget(
                        countryCode = request.countryCode,
                        cityCode = request.cityCode,
                        server = request.address,
                        label = row.title,
                    ),
                    switchStep = SwitchStep.Idle,
                )
            }
            return
        }
        select(request)
    }

    /**
     * The dialog's Switch and Retry. It runs the choreography once, keeps the
     * target on failure so Retry repeats it, and closes only on success.
     */
    fun confirmSwitch() {
        val current = mutableState.value
        val target = current.pending ?: return
        if (current.switchStep.isRunning) return
        mutableState.update { it.copy(switchStep = SwitchStep.Disconnecting) }
        switchJob?.cancel()
        switchJob = viewModelScope.launch {
            val result = switchLocation.run(target) { step ->
                mutableState.update { it.copy(switchStep = step) }
            }
            val failure = result.exceptionOrNull()
            mutableState.update {
                if (failure == null) {
                    it.copy(pending = null, switchStep = SwitchStep.Idle)
                } else {
                    it.copy(switchStep = SwitchStep.Failed(messageOf(failure)))
                }
            }
        }
    }

    /** Cancel or Close; never interrupts a running step and never reconnects. */
    fun dismissSwitch() {
        if (mutableState.value.switchStep.isRunning) return
        mutableState.update { it.copy(pending = null, switchStep = SwitchStep.Idle) }
    }

    /**
     * Persists one choice. It never connects and never marks the row locally:
     * the engine pushes the new status and the mark follows that.
     */
    fun select(request: SelectionRequest) {
        if (mutableState.value.savingServer != null || mutableState.value.savingFastest) return
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

    /** The pinned Fastest row: resolve, persist the actual server, then re-read. */
    fun selectFastest() {
        val current = mutableState.value
        if (!current.canSelectFastest) return
        mutableState.update { it.copy(savingFastest = true) }
        viewModelScope.launch {
            try {
                gateway.selectFastest()
                reloadTree()
                mutableState.update { it.copy(savingFastest = false) }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                mutableState.update {
                    it.copy(
                        savingFastest = false,
                        notice = LocationsNotice(
                            text = LocationsNoticeText.Raw(messageOf(error)),
                            tone = LocationsNoticeTone.ERROR,
                        ),
                    )
                }
            }
        }
    }

    /** Toggles one favorite optimistically; a failure restores and re-reads. */
    fun toggleFavorite(row: LocationRow) {
        val ref = LocationTree.refOf(row)
        favorite(ref, !mutableState.value.isFavorite(row))
    }

    fun favorite(ref: LocationRef, favorite: Boolean) {
        val previous = mutableState.value.favorites
        val next = if (favorite) {
            if (previous.any { LocationTree.sameRef(it, ref) }) {
                previous
            } else {
                previous + ref
            }
        } else {
            previous.filterNot { LocationTree.sameRef(it, ref) }
        }
        if (next == previous) return
        mutableState.update { it.copy(favorites = next) }
        viewModelScope.launch {
            try {
                gateway.favorite(ref, favorite)
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                mutableState.update {
                    it.copy(
                        favorites = previous,
                        notice = LocationsNotice(
                            text = LocationsNoticeText.Raw(messageOf(error)),
                            tone = LocationsNoticeTone.ERROR,
                        ),
                    )
                }
                reloadTree()
            }
        }
    }

    /** Empties Recent immediately; a failure restores the old list. */
    fun clearRecents() {
        val previous = mutableState.value.recents
        if (previous.isEmpty()) return
        mutableState.update { it.copy(recents = emptyList()) }
        viewModelScope.launch {
            try {
                gateway.clearRecents()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                mutableState.update {
                    it.copy(
                        recents = previous,
                        notice = LocationsNotice(
                            text = LocationsNoticeText.Raw(messageOf(error)),
                            tone = LocationsNoticeTone.ERROR,
                        ),
                    )
                }
                reloadTree()
            }
        }
    }

    /** The locked-list footer action; the daemon re-filters its raw cache. */
    fun toggleLocked() = setAllowLocked(!mutableState.value.allowLocked)

    fun setAllowLocked(enabled: Boolean) {
        if (mutableState.value.savingLocked) return
        mutableState.update { it.copy(savingLocked = true) }
        viewModelScope.launch {
            try {
                gateway.setAllowLocked(enabled)
                reloadTree()
                mutableState.update { it.copy(savingLocked = false) }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                mutableState.update {
                    it.copy(
                        savingLocked = false,
                        notice = LocationsNotice(
                            text = LocationsNoticeText.Raw(messageOf(error)),
                            tone = LocationsNoticeTone.ERROR,
                        ),
                    )
                }
            }
        }
    }

    // ---- ping runs -------------------------------------------------------

    /** Ping all: every usable server in the loaded tree, whatever the filter shows. */
    fun pingAll() = startPing(mutableState.value.allTargets)

    /**
     * The only path that pings. A second tap while a run is in flight is
     * ignored. Results replace the values for the run's hosts as they arrive;
     * every other host keeps its last value.
     */
    private fun startPing(targets: List<String>) {
        val current = mutableState.value
        if (targets.isEmpty() || current.pingingAll) return

        mutableState.update {
            it.copy(
                pingingAll = true,
                pingAllCount = targets.size,
                notice = if (it.notice?.action == LocationsNoticeAction.RETRY_PING_ALL) {
                    null
                } else {
                    it.notice
                },
            )
        }
        pingJob?.cancel()
        pingJob = viewModelScope.launch {
            try {
                gateway.ping(targets)
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                finishPing(notice = LocationsNotice.pingAllFailed(messageOf(error)))
            }
        }
    }

    private fun onPing(result: PingResult) {
        val key = LocationTree.addressKey(result.host)
        mutableState.update { current ->
            if (current.pingingAll) {
                current.copy(latency = current.latency + (key to result.rttMs))
            } else {
                current
            }
        }
    }

    /**
     * `ping_done` is the only completion signal. A run-level error or no
     * answered hosts never commits speed order; a successful run does so once.
     */
    private fun onPingDone(done: PingDone) {
        if (!mutableState.value.pingingAll) return
        val notice = when {
            !done.error.isNullOrBlank() -> LocationsNotice.pingAllFailed(done.error)
            done.answered <= 0 -> LocationsNotice.pingAllNoResults()
            else -> null
        }
        finishPing(notice = notice)
    }

    /**
     * Ends the active run. Only a completed, non-failed run with at least one
     * answer commits speed order, and it freezes the whole run's latency once.
     */
    private fun finishPing(notice: LocationsNotice?) {
        if (!mutableState.value.pingingAll) {
            pingJob?.cancel()
            pingJob = null
            return
        }
        val switchToSpeed = notice == null
        pingJob?.cancel()
        pingJob = null
        mutableState.update { current ->
            current.copy(
                pingingAll = false,
                pingAllCount = 0,
                sortMode = if (switchToSpeed) LocationSortMode.SPEED else current.sortMode,
                listResetToken = if (switchToSpeed) {
                    current.listResetToken + 1
                } else {
                    current.listResetToken
                },
                speedOrderLatency = if (switchToSpeed) current.latency else current.speedOrderLatency,
                notice = notice ?: current.notice,
            )
        }
    }

    /** Stops a run without letting it change the order, for a fresh load. */
    private fun cancelPing() {
        pingJob?.cancel()
        pingJob = null
        mutableState.update { it.copy(pingingAll = false, pingAllCount = 0) }
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
            LocationsNoticeAction.RETRY_PING_ALL -> {
                mutableState.update { it.copy(notice = null) }
                pingAll()
            }
            LocationsNoticeAction.NONE -> Unit
        }
    }

    private fun messageOf(error: Throwable): String =
        error.message?.takeIf { it.isNotBlank() }
            ?: error::class.simpleName
            ?: "unknown error"
}
