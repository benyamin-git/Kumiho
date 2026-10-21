package dev.benyamin.kumiho.vpn

/**
 * Maps [ServiceState] to the single UI state the shell renders.
 *
 * Precedence is deliberate: a missing permission or a real service error is
 * always more useful to the user than a stale engine snapshot. A bound service
 * with no status yet is unavailable/loading, never Off, so the UI cannot claim
 * the tunnel is down before the engine has spoken.
 */
object VpnStateReducer {
    fun reduce(state: ServiceState): VpnUiState {
        if (state.permissionNeeded) return VpnUiState.PERMISSION_NEEDED
        if (state.error != null) return VpnUiState.ERROR
        if (!state.binding) return VpnUiState.SERVICE_UNAVAILABLE
        val status = state.status ?: return VpnUiState.SERVICE_UNAVAILABLE
        if (!status.authenticated || status.state == EngineStates.UNAUTHENTICATED) {
            return VpnUiState.NOT_SIGNED_IN
        }
        return when (status.state) {
            EngineStates.IDLE -> VpnUiState.OFF
            EngineStates.CONNECTING -> VpnUiState.CONNECTING
            EngineStates.CONNECTED -> VpnUiState.CONNECTED
            EngineStates.RECONNECTING -> VpnUiState.RECONNECTING
            EngineStates.WAITING_NETWORK -> VpnUiState.WAITING
            EngineStates.PROXY_ONLY -> VpnUiState.PROXY_ONLY
            // FATAL and any state this client does not know are failures.
            else -> VpnUiState.ERROR
        }
    }
}
