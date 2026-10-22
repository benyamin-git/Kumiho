package dev.benyamin.kumiho.vpn

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** The busy predicate that makes a duplicate connect a no-op. */
class EngineStatesTest {

    @Test
    fun activeStatesAreBusy() {
        for (state in listOf(
            EngineStates.CONNECTING,
            EngineStates.CONNECTED,
            EngineStates.RECONNECTING,
            EngineStates.WAITING_NETWORK,
            EngineStates.PROXY_ONLY,
        )) {
            assertTrue(state, EngineStates.isActive(state))
        }
    }

    @Test
    fun settledStatesAreNotBusy() {
        for (state in listOf(
            EngineStates.IDLE,
            EngineStates.UNAUTHENTICATED,
            EngineStates.FATAL,
            null,
            "",
            "SOMETHING_NEW",
        )) {
            assertFalse("state=$state", EngineStates.isActive(state))
        }
    }
}
