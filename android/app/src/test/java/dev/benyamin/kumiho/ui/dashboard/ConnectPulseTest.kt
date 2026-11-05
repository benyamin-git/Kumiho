package dev.benyamin.kumiho.ui.dashboard

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ConnectPulseTest {

    @Test
    fun `only a wait that lands on connected pulses`() {
        val pulsing = mutableSetOf<DashboardPhase>()
        for (previous in DashboardPhase.entries) {
            for (current in DashboardPhase.entries) {
                val pulses = ConnectPulse.shouldPulse(previous, current)
                if (pulses) {
                    assertEquals(DashboardPhase.CONNECTED, current)
                    pulsing += previous
                }
            }
        }
        assertEquals(
            setOf(
                DashboardPhase.CONNECTING,
                DashboardPhase.RECONNECTING,
                DashboardPhase.WAITING,
            ),
            pulsing,
        )
    }

    @Test
    fun `an already-connected start does not pulse`() {
        assertTrue(!ConnectPulse.shouldPulse(DashboardPhase.CONNECTED, DashboardPhase.CONNECTED))
        assertTrue(!ConnectPulse.shouldPulse(DashboardPhase.OFF, DashboardPhase.CONNECTED))
    }
}
