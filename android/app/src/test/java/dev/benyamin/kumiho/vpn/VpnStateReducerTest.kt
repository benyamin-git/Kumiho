package dev.benyamin.kumiho.vpn

import dev.benyamin.kumiho.engine.EngineStatus
import org.junit.Assert.assertEquals
import org.junit.Test

/** The ten UI states and their precedence. */
class VpnStateReducerTest {

    private fun status(state: String, authenticated: Boolean = true) = EngineStatus(
        state = state,
        authenticated = authenticated,
        email = if (authenticated) "user@example.com" else null,
        since = 0,
        tokenExpires = 0,
        passExpires = 0,
        killSwitch = true,
        autoconnect = true,
        location = null,
        server = null,
        proxyOnly = false,
        quota = null,
        dns = null,
        ipv6 = null,
        exitIp = null,
        exitCountry = null,
        totals = null,
        rates = null,
    )

    private fun reduce(
        state: String = EngineStates.IDLE,
        authenticated: Boolean = true,
        binding: Boolean = true,
        engineReady: Boolean = true,
        error: String? = null,
        permissionNeeded: Boolean = false,
    ) = VpnStateReducer.reduce(
        ServiceState(
            binding = binding,
            engineReady = engineReady,
            status = status(state, authenticated),
            error = error,
            permissionNeeded = permissionNeeded,
        ),
    )

    @Test
    fun permissionNeededWinsOverEverything() {
        assertEquals(
            VpnUiState.PERMISSION_NEEDED,
            VpnStateReducer.reduce(
                ServiceState(
                    binding = false,
                    status = status(EngineStates.CONNECTED),
                    error = "boom",
                    permissionNeeded = true,
                ),
            ),
        )
    }

    @Test
    fun serviceErrorsAreErrors() {
        assertEquals(VpnUiState.ERROR, reduce(error = "the engine failed"))
        assertEquals(
            VpnUiState.ERROR,
            reduce(state = EngineStates.CONNECTED, error = "the engine failed"),
        )
    }

    @Test
    fun serviceUnavailableWithoutBinding() {
        assertEquals(VpnUiState.SERVICE_UNAVAILABLE, reduce(binding = false))
        assertEquals(
            VpnUiState.SERVICE_UNAVAILABLE,
            reduce(binding = false, state = EngineStates.CONNECTED),
        )
    }

    @Test
    fun serviceUnavailableBeforeTheFirstStatus() {
        assertEquals(
            VpnUiState.SERVICE_UNAVAILABLE,
            VpnStateReducer.reduce(
                ServiceState(binding = true, engineReady = false, status = null),
            ),
        )
        assertEquals(
            VpnUiState.SERVICE_UNAVAILABLE,
            VpnStateReducer.reduce(
                ServiceState(binding = true, engineReady = true, status = null),
            ),
        )
    }

    @Test
    fun serviceViewIsAvailableDuringTheBootWindow() {
        // The UI reducer reads an unbound service as unavailable; during the
        // boot auto-connect no Activity is bound yet.
        val boot = ServiceState(
            binding = false,
            engineReady = false,
            status = status(EngineStates.CONNECTING),
        )
        assertEquals(VpnUiState.SERVICE_UNAVAILABLE, VpnStateReducer.reduce(boot))
        // The service's own notification reports the connect attempt instead.
        assertEquals(VpnUiState.CONNECTING, VpnStateReducer.reduceForService(boot))
        assertEquals(
            VpnUiState.WAITING,
            VpnStateReducer.reduceForService(
                boot.copy(status = status(EngineStates.WAITING_NETWORK)),
            ),
        )
    }

    @Test
    fun serviceViewWithNoStatusIsConnecting() {
        assertEquals(
            VpnUiState.CONNECTING,
            VpnStateReducer.reduceForService(
                ServiceState(binding = false, engineReady = false, status = null),
            ),
        )
        // A real error or a missing permission still outranks the boot view.
        assertEquals(
            VpnUiState.ERROR,
            VpnStateReducer.reduceForService(
                ServiceState(binding = false, status = null, error = "the engine failed"),
            ),
        )
        assertEquals(
            VpnUiState.PERMISSION_NEEDED,
            VpnStateReducer.reduceForService(
                ServiceState(binding = false, status = null, permissionNeeded = true),
            ),
        )
    }

    @Test
    fun signedOutMapsToNotSignedIn() {
        assertEquals(
            VpnUiState.NOT_SIGNED_IN,
            reduce(state = EngineStates.UNAUTHENTICATED, authenticated = false),
        )
        assertEquals(
            VpnUiState.NOT_SIGNED_IN,
            reduce(state = EngineStates.IDLE, authenticated = false),
        )
        // The engine's wire state for signed-out is authoritative even if a
        // stale snapshot still says authenticated.
        assertEquals(
            VpnUiState.NOT_SIGNED_IN,
            reduce(state = EngineStates.UNAUTHENTICATED, authenticated = true),
        )
    }

    @Test
    fun idleIsOff() {
        assertEquals(VpnUiState.OFF, reduce(state = EngineStates.IDLE))
    }

    @Test
    fun connectedAndProxyOnlyAreDistinct() {
        assertEquals(VpnUiState.CONNECTED, reduce(state = EngineStates.CONNECTED))
        assertEquals(VpnUiState.PROXY_ONLY, reduce(state = EngineStates.PROXY_ONLY))
    }

    @Test
    fun inFlightStatesMapOneToOne() {
        assertEquals(VpnUiState.CONNECTING, reduce(state = EngineStates.CONNECTING))
        assertEquals(VpnUiState.RECONNECTING, reduce(state = EngineStates.RECONNECTING))
        assertEquals(VpnUiState.WAITING, reduce(state = EngineStates.WAITING_NETWORK))
    }

    @Test
    fun fatalIsError() {
        assertEquals(VpnUiState.ERROR, reduce(state = EngineStates.FATAL))
    }

    @Test
    fun unknownStatesAreErrors() {
        assertEquals(VpnUiState.ERROR, reduce(state = "SOMETHING_NEW"))
        assertEquals(VpnUiState.ERROR, reduce(state = ""))
    }
}
