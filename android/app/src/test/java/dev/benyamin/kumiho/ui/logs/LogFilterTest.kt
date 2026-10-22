package dev.benyamin.kumiho.ui.logs

import dev.benyamin.kumiho.data.LogExporter
import dev.benyamin.kumiho.engine.LogEntry
import dev.benyamin.kumiho.engine.LogLevel
import java.nio.file.Files
import java.time.Instant
import java.time.ZoneId
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

/**
 * The console's pure filter/format/export rules. No Android, no Compose and no
 * engine: timestamps are fixed so a line is deterministic across machines.
 */
class LogFilterTest {

    @get:Rule
    val temp = TemporaryFolder()

    private val utc = ZoneId.of("UTC")

    private fun entry(
        second: Long,
        level: LogLevel = LogLevel.INFO,
        tag: String = "net",
        message: String = "connected",
    ) = LogEntry(Instant.parse("2026-10-22T12:00:00Z").plusSeconds(second), level, tag, message)

    // ---- level filter ----------------------------------------------------

    @Test
    fun `each filter is a minimum level and ALL accepts everything`() {
        val debug = entry(1, LogLevel.DEBUG)
        val info = entry(2, LogLevel.INFO)
        val warn = entry(3, LogLevel.WARN)
        val error = entry(4, LogLevel.ERROR)
        val all = listOf(debug, info, warn, error)

        assertEquals(all, LogFilter.apply(all, LogFilterLevel.ALL))
        assertEquals(all, LogFilter.apply(all, LogFilterLevel.DEBUG))
        assertEquals(listOf(info, warn, error), LogFilter.apply(all, LogFilterLevel.INFO))
        assertEquals(listOf(warn, error), LogFilter.apply(all, LogFilterLevel.WARN))
        assertEquals(listOf(error), LogFilter.apply(all, LogFilterLevel.ERROR))
    }

    @Test
    fun `the wire value is null only for ALL`() {
        assertEquals(null, LogFilterLevel.ALL.wire)
        assertEquals("debug", LogFilterLevel.DEBUG.wire)
        assertEquals("info", LogFilterLevel.INFO.wire)
        assertEquals("warn", LogFilterLevel.WARN.wire)
        assertEquals("error", LogFilterLevel.ERROR.wire)
    }

    // ---- search ----------------------------------------------------------

    @Test
    fun `search is case-insensitive over message tag and level`() {
        val entries = listOf(
            entry(1, LogLevel.WARN, "net", "the edge answered"),
            entry(2, LogLevel.ERROR, "auth", "token rejected"),
        )

        assertEquals(listOf(entries[0]), LogFilter.apply(entries, search = "EDGE"))
        assertEquals(listOf(entries[1]), LogFilter.apply(entries, search = "AUTH"))
        assertEquals(listOf(entries[1]), LogFilter.apply(entries, search = "error"))
        assertEquals(entries, LogFilter.apply(entries, search = "   "))
    }

    @Test
    fun `search and level filter combine`() {
        val entries = listOf(
            entry(1, LogLevel.INFO, "net", "connected"),
            entry(2, LogLevel.WARN, "net", "retrying"),
            entry(3, LogLevel.ERROR, "net", "connected refused"),
        )

        assertEquals(
            listOf(entries[2]),
            LogFilter.apply(entries, LogFilterLevel.ERROR, "connected"),
        )
    }

    // ---- merge -----------------------------------------------------------

    @Test
    fun `merge dedupes overlapping sources and keeps them oldest first`() {
        val first = entry(1)
        val second = entry(2)
        val third = entry(3)

        val merged = LogFilter.merge(listOf(second, first), listOf(third, second))

        assertEquals(listOf(first, second, third), merged)
    }

    @Test
    fun `merge caps at the engine ring size and keeps the newest`() {
        val entries = (1..(LogFilter.MAX_ENTRIES + 5)).map { entry(it.toLong()) }

        val merged = LogFilter.merge(entries)

        assertEquals(LogFilter.MAX_ENTRIES, merged.size)
        assertEquals(entry(1), entries.first())
        assertEquals(entry((LogFilter.MAX_ENTRIES + 5).toLong()), merged.last())
        assertFalse(merged.contains(entry(1)))
        assertTrue(merged.contains(entry(6)))
    }

    @Test
    fun `append is a capped merge of the current console and one batch`() {
        val current = (1..3).map { entry(it.toLong()) }
        val batch = listOf(entry(3), entry(4))

        assertEquals((1..4).map { entry(it.toLong()) }, LogFilter.append(current, batch))
    }

    // ---- formatting ------------------------------------------------------

    @Test
    fun `clock renders local wall time and the export line uses ISO`() {
        val entry = entry(369, LogLevel.WARN, "net", "slow")

        assertEquals("12:06:09", LogFormat.clock(entry.time, utc))
        assertEquals("2026-10-22", LogFormat.day(entry.time, utc))
        assertEquals(
            "2026-10-22T12:06:09Z warn net: slow",
            LogFormat.line(entry),
        )
        assertEquals(LogFormat.line(entry), LogFormat.copyLine(entry))
    }

    // ---- export ----------------------------------------------------------

    @Test
    fun `the export name carries the timestamp and has a log suffix`() {
        assertEquals(
            "kumiho-logs-20261022-120309.log",
            LogExporter.fileName(Instant.parse("2026-10-22T12:03:09Z"), utc),
        )
    }

    @Test
    fun `render writes one newline-terminated line per entry`() {
        val entries = listOf(
            entry(1, LogLevel.INFO, "net", "connected"),
            entry(2, LogLevel.ERROR, "crash", "boom"),
        )

        assertEquals(
            "2026-10-22T12:00:01Z info net: connected\n" +
                "2026-10-22T12:00:02Z error crash: boom\n",
            LogExporter.render(entries),
        )
        assertEquals("", LogExporter.render(emptyList()))
    }

    @Test
    fun `writeTo creates the exports directory and writes UTF-8`() {
        val directory = LogExporter.exportsDir(temp.root)
        val now = Instant.parse("2026-10-22T12:03:09Z")

        val file = LogExporter.writeTo(
            directory,
            listOf(entry(1, LogLevel.WARN, "dns", "résumé")),
            now,
            utc,
        )

        assertTrue(file.isFile)
        assertEquals(directory, file.parentFile)
        assertEquals("kumiho-logs-20261022-120309.log", file.name)
        assertEquals(
            "2026-10-22T12:00:01Z warn dns: résumé\n",
            file.readText(Charsets.UTF_8),
        )
        assertTrue(Files.isDirectory(directory.toPath()))
    }
}
