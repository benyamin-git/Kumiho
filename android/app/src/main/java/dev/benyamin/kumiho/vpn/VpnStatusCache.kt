package dev.benyamin.kumiho.vpn

/**
 * The process-local status the Quick Settings tile reads.
 *
 * The tile service runs in this app's process, so it can read what the
 * app-scoped [VpnServiceCoordinator] already knows without another binder. The
 * coordinator writes every service snapshot here and asks the tile to refresh;
 * the cache is cleared when the engine stops or the binding dies, so the tile
 * can never show a connected state for a service that is gone.
 *
 * All access is guarded by one lock: the coordinator writes from the main
 * thread and `TileService.onStartListening`/`onClick` read from the tile's
 * thread, which is not guaranteed to be the same one.
 */
object VpnStatusCache {
    /** A duplicate tap inside this window is ignored while a connect starts. */
    const val CONNECT_IN_FLIGHT_MILLIS = 10_000L

    /**
     * How long a consent prompt may still be on screen. The tile cannot wait
     * for the system dialog, so a listening pass inside this window does not
     * treat a still-missing consent as a denial.
     */
    const val CONSENT_SETTLE_MILLIS = 2_000L

    private val lock = Any()
    private var snapshot: TileSnapshot? = null
    private var connectRequestedAtMillis: Long? = null
    private var consentPendingAtMillis: Long? = null

    /** Records one service snapshot; called on every coordinator update. */
    fun update(
        state: ServiceState,
        nowMillis: Long = System.currentTimeMillis(),
    ): TileSnapshot {
        val uiState = VpnStateReducer.reduce(state)
        return synchronized(lock) {
            // As soon as the service reports anything but Idle, the connect is
            // no longer merely requested: the service owns the state again.
            if (uiState != VpnUiState.OFF) connectRequestedAtMillis = null
            val next = TileSnapshot(
                state = uiState,
                location = state.status?.location,
                proxyOnly = uiState == VpnUiState.PROXY_ONLY ||
                    state.status?.proxyOnly == true,
                connectInFlight = inFlight(nowMillis),
            )
            snapshot = next
            next
        }
    }

    /** Notes that the tile asked for a connect, until the service answers. */
    fun markConnectRequested(nowMillis: Long = System.currentTimeMillis()) {
        synchronized(lock) {
            connectRequestedAtMillis = nowMillis
        }
    }

    /** Notes that the tile showed Android's VPN consent dialog. */
    fun markConsentPending(nowMillis: Long = System.currentTimeMillis()) {
        synchronized(lock) {
            consentPendingAtMillis = nowMillis
        }
    }

    /** Whether a consent prompt is still waiting to be resolved. */
    fun hasPendingConsent(): Boolean = synchronized(lock) {
        consentPendingAtMillis != null
    }

    /** True once the prompt has had time to close, so a missing consent is a denial. */
    fun consentPendingExpired(nowMillis: Long = System.currentTimeMillis()): Boolean =
        synchronized(lock) {
            val requested = consentPendingAtMillis ?: return false
            nowMillis - requested >= CONSENT_SETTLE_MILLIS
        }

    fun clearConsentPending() {
        synchronized(lock) {
            consentPendingAtMillis = null
        }
    }

    /** The latest snapshot, or null when the service is not reachable. */
    fun current(nowMillis: Long = System.currentTimeMillis()): TileSnapshot? =
        synchronized(lock) {
            snapshot?.copy(connectInFlight = inFlight(nowMillis))
        }

    /**
     * Drops everything. Called when the engine stops or the binding dies; a
     * later service snapshot repopulates the tile.
     */
    fun clear() {
        synchronized(lock) {
            snapshot = null
            connectRequestedAtMillis = null
            consentPendingAtMillis = null
        }
    }

    private fun inFlight(nowMillis: Long): Boolean {
        val requested = connectRequestedAtMillis ?: return false
        return nowMillis - requested < CONNECT_IN_FLIGHT_MILLIS
    }
}
