package dev.benyamin.kumiho.crash

import java.io.File
import java.time.Instant
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

/**
 * Crash formatting, the size cap and the replay contract on the JVM. The
 * handler itself is exercised with a recording previous handler, so nothing
 * reaches Android's logger.
 */
class CrashFormatTest {

    @get:Rule
    val temp = TemporaryFolder()

    @Test
    fun `format carries the timestamp thread exception and stack`() {
        val error = IllegalStateException(
            "the tunnel went away",
            IllegalArgumentException("no edge"),
        )

        val text = CrashLog.format(
            timestamp = Instant.parse("2026-10-22T12:03:09Z"),
            thread = Thread.currentThread(),
            error = error,
        )

        assertTrue(text.startsWith("time: 2026-10-22T12:03:09Z\n"))
        assertTrue(text.contains("thread: ${Thread.currentThread().name}\n"))
        assertTrue(text.contains("exception: java.lang.IllegalStateException: the tunnel went away\n"))
        assertTrue(text.contains("stack:\n"))
        assertTrue(text.contains("java.lang.IllegalStateException: the tunnel went away"))
        // The cause chain is part of the same stack trace.
        assertTrue(text.contains("Caused by: java.lang.IllegalArgumentException: no edge"))
    }

    @Test
    fun `format omits a blank exception message`() {
        val text = CrashLog.format(
            timestamp = Instant.parse("2026-10-22T12:03:09Z"),
            thread = Thread.currentThread(),
            error = RuntimeException(),
        )

        assertTrue(text.contains("exception: java.lang.RuntimeException\n"))
    }

    @Test
    fun `write caps the file and readCapped reads at most the cap`() {
        val file = File(temp.root, "crash.log")

        CrashLog.write(file, "0123456789", maxBytes = 4)

        assertEquals("0123", file.readText())
        assertEquals("01", CrashLog.readCapped(file, maxBytes = 2))
        assertEquals("0123", CrashLog.readCapped(file, maxBytes = 100))
    }

    @Test
    fun `readCapped returns null for an absent file`() {
        assertNull(CrashLog.readCapped(File(temp.root, "missing.log")))
    }

    @Test
    fun `a successful replay appends once and deletes the file`() = runTest {
        val file = File(temp.root, "crash.log").apply { writeText("boom") }
        val appended = mutableListOf<String>()

        val replayed = CrashLog.replay(file) { appended += it }

        assertTrue(replayed)
        assertEquals(listOf("boom"), appended)
        assertFalse(file.exists())
    }

    @Test
    fun `a failed append keeps the file for the next start`() = runTest {
        val file = File(temp.root, "crash.log").apply { writeText("boom") }

        val replayed = CrashLog.replay(file) { error("the log is unavailable") }

        assertFalse(replayed)
        assertTrue(file.isFile)
        assertEquals("boom", file.readText())
    }

    @Test
    fun `a blank file is deleted without a call and a missing file is a no-op`() = runTest {
        val blank = File(temp.root, "blank.log").apply { writeText("  \n") }
        val appended = mutableListOf<String>()

        assertTrue(CrashLog.replay(blank) { appended += it })
        assertFalse(blank.exists())
        assertTrue(appended.isEmpty())

        assertFalse(CrashLog.replay(File(temp.root, "missing.log")) { appended += it })
        assertTrue(appended.isEmpty())
    }

    @Test
    fun `the handler writes the crash and delegates to the previous handler`() {
        val file = File(temp.root, "crash.log")
        var delegated: Throwable? = null
        val previous = Thread.UncaughtExceptionHandler { _, error -> delegated = error }
        val handler = CrashHandler(file, previous)
        val crash = IllegalStateException("process death")

        handler.uncaughtException(Thread.currentThread(), crash)

        assertEquals(crash, delegated)
        val text = file.readText()
        assertTrue(text.contains("exception: java.lang.IllegalStateException: process death"))
    }
}
