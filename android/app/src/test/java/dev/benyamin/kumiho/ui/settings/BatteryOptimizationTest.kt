package dev.benyamin.kumiho.ui.settings

import org.junit.Assert.assertEquals
import org.junit.Test

/** The Android battery facts map to one of the four visible states. */
class BatteryOptimizationTest {

    @Test
    fun `an unreadable exemption is unknown`() {
        assertEquals(
            BatteryOptimization.UNKNOWN,
            batteryOptimizationState(ignoringOptimizations = null, backgroundRestricted = true),
        )
    }

    @Test
    fun `an exemption wins over a background restriction`() {
        assertEquals(
            BatteryOptimization.UNRESTRICTED,
            batteryOptimizationState(ignoringOptimizations = true, backgroundRestricted = true),
        )
    }

    @Test
    fun `a background restriction is restricted`() {
        assertEquals(
            BatteryOptimization.RESTRICTED,
            batteryOptimizationState(ignoringOptimizations = false, backgroundRestricted = true),
        )
    }

    @Test
    fun `otherwise the app is optimized`() {
        assertEquals(
            BatteryOptimization.OPTIMIZED,
            batteryOptimizationState(ignoringOptimizations = false, backgroundRestricted = false),
        )
        assertEquals(
            BatteryOptimization.OPTIMIZED,
            batteryOptimizationState(ignoringOptimizations = false, backgroundRestricted = null),
        )
    }
}
