package dev.benyamin.kumiho.vpn

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * The Always-on restart table (D51): only a signed-in, settled, non-connected
 * engine on an Always-on device may auto-connect. Lockdown and signed-out
 * states are reported, never fought.
 */
class AlwaysOnPolicyTest {

    private fun decide(
        alwaysOn: Boolean = true,
        authenticated: Boolean = true,
        state: String? = EngineStates.IDLE,
        freshStart: Boolean = true,
    ) = AlwaysOnPolicy.decide(alwaysOn, authenticated, state, freshStart)

    @Test
    fun alwaysOnOffNeverAutoConnects() {
        for (state in listOf(
            EngineStates.IDLE,
            EngineStates.WAITING_NETWORK,
            EngineStates.FATAL,
            null,
        )) {
            for (fresh in listOf(true, false)) {
                assertEquals(
                    "state=$state fresh=$fresh",
                    AlwaysOnDecision.NONE,
                    decide(alwaysOn = false, state = state, freshStart = fresh),
                )
            }
        }
    }

    @Test
    fun signedOutAlwaysOffersSignIn() {
        assertEquals(
            AlwaysOnDecision.SIGN_IN_NOTIFICATION,
            decide(authenticated = false, state = EngineStates.IDLE),
        )
        assertEquals(
            AlwaysOnDecision.SIGN_IN_NOTIFICATION,
            decide(authenticated = false, state = EngineStates.UNAUTHENTICATED),
        )
        assertEquals(
            AlwaysOnDecision.SIGN_IN_NOTIFICATION,
            decide(authenticated = false, state = EngineStates.FATAL),
        )
    }

    @Test
    fun lockdownWithASignedOutAccountStillOffersSignIn() {
        // Lockdown itself is not a parameter: the OS keeps it, and the service
        // must not attempt a connect that cannot authenticate.
        assertEquals(
            AlwaysOnDecision.SIGN_IN_NOTIFICATION,
            decide(alwaysOn = true, authenticated = false, state = EngineStates.IDLE),
        )
        assertEquals(
            AlwaysOnDecision.SIGN_IN_NOTIFICATION,
            decide(alwaysOn = true, authenticated = false, state = EngineStates.WAITING_NETWORK),
        )
    }

    @Test
    fun activeStatesNeverAutoConnect() {
        for (state in listOf(
            EngineStates.CONNECTED,
            EngineStates.CONNECTING,
            EngineStates.RECONNECTING,
            EngineStates.PROXY_ONLY,
        )) {
            for (fresh in listOf(true, false)) {
                assertEquals(
                    "state=$state fresh=$fresh",
                    AlwaysOnDecision.NONE,
                    decide(state = state, freshStart = fresh),
                )
            }
        }
    }

    @Test
    fun idleAutoConnectsOnEveryStart() {
        assertEquals(AlwaysOnDecision.AUTO_CONNECT, decide(freshStart = true))
        assertEquals(AlwaysOnDecision.AUTO_CONNECT, decide(freshStart = false))
    }

    @Test
    fun waitingForNetworkAutoConnects() {
        assertEquals(
            AlwaysOnDecision.AUTO_CONNECT,
            decide(state = EngineStates.WAITING_NETWORK),
        )
        assertEquals(
            AlwaysOnDecision.AUTO_CONNECT,
            decide(state = EngineStates.WAITING_NETWORK, freshStart = false),
        )
    }

    @Test
    fun fatalRetriesOnlyOnAFreshStart() {
        assertEquals(
            AlwaysOnDecision.AUTO_CONNECT,
            decide(state = EngineStates.FATAL, freshStart = true),
        )
        assertEquals(
            AlwaysOnDecision.NONE,
            decide(state = EngineStates.FATAL, freshStart = false),
        )
    }

    @Test
    fun unknownAndUnauthenticatedStatesNeverAutoConnect() {
        assertEquals(AlwaysOnDecision.NONE, decide(state = null))
        assertEquals(AlwaysOnDecision.NONE, decide(state = ""))
        assertEquals(AlwaysOnDecision.NONE, decide(state = "SOMETHING_NEW"))
        assertEquals(AlwaysOnDecision.NONE, decide(state = EngineStates.UNAUTHENTICATED))
    }
}
