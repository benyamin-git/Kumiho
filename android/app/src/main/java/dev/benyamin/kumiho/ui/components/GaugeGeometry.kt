package dev.benyamin.kumiho.ui.components

/**
 * The gauge's geometry, kept pure so the drawing rules are JVM-tested. Sweeps
 * are degrees clockwise from [START_DEGREES] (twelve o'clock).
 */
object GaugeGeometry {
    const val START_DEGREES = -90f
    const val WINDOW_MILLIS = 60_000L
    const val ARC_STROKE_DP = 8f
    const val TRACE_BAND_DP = 18f

    /** The quota arc: unknown is empty, a fraction clamps to 0..360°. */
    fun quotaSweep(fraction: Float?): Float {
        if (fraction == null || !fraction.isFinite()) return 0f
        return fraction.coerceIn(0f, 1f) * 360f
    }

    /** The trace sweep for a sample: newest is 0°, 30 s old is 180°, stale is 0°. */
    fun sampleSweep(atMillis: Long, nowMillis: Long): Float {
        val age = nowMillis - atMillis
        if (age < 0 || age > WINDOW_MILLIS) return 0f
        return age.toFloat() / WINDOW_MILLIS * 360f
    }

    /** A rate as a 0..1 radius fraction; a zero or negative maximum is 0. */
    fun radiusFraction(value: Double, max: Double): Float {
        if (!(max > 0.0) || !value.isFinite() || value <= 0.0) return 0f
        return (value / max).coerceIn(0.0, 1.0).toFloat()
    }
}
