package dev.benyamin.kumiho.ui.components

import org.junit.Assert.assertEquals
import org.junit.Test

class GaugeGeometryTest {

    @Test
    fun `unknown quota draws no arc`() {
        assertEquals(0f, GaugeGeometry.quotaSweep(null), 0f)
    }

    @Test
    fun `quota sweep clamps to a full circle`() {
        assertEquals(0f, GaugeGeometry.quotaSweep(-0.5f), 0f)
        assertEquals(90f, GaugeGeometry.quotaSweep(0.25f), 0f)
        assertEquals(360f, GaugeGeometry.quotaSweep(1.5f), 0f)
        assertEquals(0f, GaugeGeometry.quotaSweep(Float.NaN), 0f)
    }

    @Test
    fun `the newest sample sits at the start of the sweep`() {
        assertEquals(0f, GaugeGeometry.sampleSweep(atMillis = 50_000, nowMillis = 50_000), 0f)
    }

    @Test
    fun `a sample half a window old is halfway round`() {
        assertEquals(180f, GaugeGeometry.sampleSweep(atMillis = 20_000, nowMillis = 50_000), 0f)
    }

    @Test
    fun `a sample older than the window is not drawn`() {
        assertEquals(0f, GaugeGeometry.sampleSweep(atMillis = 0, nowMillis = 60_001), 0f)
    }

    @Test
    fun `a sample from the future is not drawn`() {
        assertEquals(0f, GaugeGeometry.sampleSweep(atMillis = 60_000, nowMillis = 50_000), 0f)
    }

    @Test
    fun `radius fraction clamps and handles a zero maximum`() {
        assertEquals(0.5f, GaugeGeometry.radiusFraction(5.0, 10.0), 0f)
        assertEquals(0f, GaugeGeometry.radiusFraction(-1.0, 10.0), 0f)
        assertEquals(1f, GaugeGeometry.radiusFraction(20.0, 10.0), 0f)
        assertEquals(0f, GaugeGeometry.radiusFraction(1.0, 0.0), 0f)
        assertEquals(0f, GaugeGeometry.radiusFraction(Double.NaN, 10.0), 0f)
    }
}
