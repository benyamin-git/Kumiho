package dev.benyamin.kumiho.ui.components

import dev.benyamin.kumiho.engine.ByteRates

/** One engine sample: the wall clock and the two byte rates. */
data class RateSample(val atMillis: Long, val down: Double, val up: Double)

/**
 * The last [capacity] samples of a live session. A gap wider than
 * [maxGapMillis] clears the buffer so the trace never draws a straight line
 * across a pause. In memory only, never persisted.
 */
class RateBuffer(
    private val capacity: Int = 30,
    private val maxGapMillis: Long = 5_000,
) {
    private val samples = ArrayDeque<RateSample>()

    /**
     * Records one sample and returns true. A null [rates] is a no-op: the
     * buffer keeps what it has and returns false.
     */
    fun push(rates: ByteRates?, nowMillis: Long): Boolean {
        if (rates == null) return false
        val sample = RateSample(nowMillis, rates.down.sanitized(), rates.up.sanitized())
        val last = samples.lastOrNull()
        if (last != null && (nowMillis < last.atMillis || nowMillis - last.atMillis > maxGapMillis)) {
            samples.clear()
        }
        samples.addLast(sample)
        while (samples.size > capacity) samples.removeFirst()
        return true
    }

    /** An immutable copy of the window, oldest first. */
    fun snapshot(): List<RateSample> = samples.toList()

    fun clear() = samples.clear()

    val size: Int get() = samples.size

    private fun Double.sanitized(): Double = if (isFinite() && this >= 0.0) this else 0.0
}
