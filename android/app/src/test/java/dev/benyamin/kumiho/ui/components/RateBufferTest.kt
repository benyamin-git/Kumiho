package dev.benyamin.kumiho.ui.components

import dev.benyamin.kumiho.engine.ByteRates
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RateBufferTest {

    private fun rates(down: Double = 1.0, up: Double = 2.0) = ByteRates(up = up, down = down)

    @Test
    fun `capacity trims the oldest samples`() {
        val buffer = RateBuffer(capacity = 3)
        (0..4).forEach { buffer.push(rates(), it * 1_000L) }
        assertEquals(3, buffer.size)
        assertEquals(listOf(2_000L, 3_000L, 4_000L), buffer.snapshot().map { it.atMillis })
    }

    @Test
    fun `a gap up to the limit keeps the trace`() {
        val buffer = RateBuffer(maxGapMillis = 5_000)
        buffer.push(rates(), 0)
        buffer.push(rates(), 5_000)
        assertEquals(2, buffer.size)
    }

    @Test
    fun `a gap past the limit clears first`() {
        val buffer = RateBuffer(maxGapMillis = 5_000)
        buffer.push(rates(), 0)
        buffer.push(rates(), 5_001)
        assertEquals(listOf(5_001L), buffer.snapshot().map { it.atMillis })
    }

    @Test
    fun `null rates change nothing`() {
        val buffer = RateBuffer()
        buffer.push(rates(down = 4.0), 1_000)
        assertFalse(buffer.push(null, 3_000))
        assertEquals(1, buffer.size)
        assertEquals(4.0, buffer.snapshot().single().down, 0.0)
    }

    @Test
    fun `negative and non-finite rates clamp to zero`() {
        val buffer = RateBuffer()
        buffer.push(rates(down = -1.0, up = Double.NaN), 0)
        buffer.push(rates(down = Double.POSITIVE_INFINITY, up = 3.0), 1_000)
        assertEquals(listOf(0.0, 0.0), buffer.snapshot().map { it.down })
        assertEquals(listOf(0.0, 3.0), buffer.snapshot().map { it.up })
    }

    @Test
    fun `a backwards clock replaces the buffer`() {
        val buffer = RateBuffer()
        buffer.push(rates(), 10_000)
        buffer.push(rates(), 9_000)
        assertEquals(listOf(9_000L), buffer.snapshot().map { it.atMillis })
    }

    @Test
    fun `snapshot is an immutable copy`() {
        val buffer = RateBuffer()
        buffer.push(rates(), 0)
        val snapshot = buffer.snapshot()
        buffer.push(rates(), 1_000)
        assertEquals(1, snapshot.size)
        assertEquals(2, buffer.size)
    }

    @Test
    fun `size is zero after clear`() {
        val buffer = RateBuffer()
        buffer.push(rates(), 0)
        buffer.clear()
        assertEquals(0, buffer.size)
        assertTrue(buffer.snapshot().isEmpty())
    }
}
