package dev.benyamin.kumiho.ui.logs

import dev.benyamin.kumiho.engine.LogEntry
import dev.benyamin.kumiho.engine.LogLevel
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/**
 * The level filter above the console. [ALL] is the UI's "everything" choice;
 * the engine's subscription takes [wire], which is null for all levels.
 */
enum class LogFilterLevel(val wire: String?) {
    ALL(null),
    DEBUG("debug"),
    INFO("info"),
    WARN("warn"),
    ERROR("error"),
    ;

    /** Whether [level] survives this minimum-level filter. */
    fun accepts(level: LogLevel): Boolean = when (this) {
        ALL, DEBUG -> true
        INFO -> level.ordinal >= LogLevel.INFO.ordinal
        WARN -> level.ordinal >= LogLevel.WARN.ordinal
        ERROR -> level == LogLevel.ERROR
    }
}

/**
 * The console's search and filter. It is pure and JVM-testable: no Android, no
 * Compose, no engine.
 */
object LogFilter {
    /** The engine's ring size, mirrored here as the UI merge cap. */
    const val MAX_ENTRIES = 2000

    /**
     * Snapshot entries filtered by level and a case-insensitive search over the
     * message, tag and level. Search whitespace is ignored at the edges; an
     * empty search matches everything.
     */
    fun apply(
        entries: List<LogEntry>,
        level: LogFilterLevel = LogFilterLevel.ALL,
        search: String = "",
    ): List<LogEntry> {
        val query = search.trim()
        return entries.filter { entry ->
            level.accepts(entry.level) &&
                (query.isEmpty() ||
                    entry.message.contains(query, ignoreCase = true) ||
                    entry.tag.contains(query, ignoreCase = true) ||
                    entry.level.wire.contains(query, ignoreCase = true))
        }
    }

    /**
     * Merges the snapshot and the streamed entries without duplicates, keeps
     * them oldest first and caps the result at [MAX_ENTRIES]. The engine also
     * caps its ring; this cap keeps a long-lived screen bounded when the two
     * sources overlap.
     */
    fun merge(vararg sources: List<LogEntry>): List<LogEntry> {
        val seen = HashSet<LogEntry>()
        val merged = ArrayList<LogEntry>()
        sources.forEach { source ->
            source.forEach { entry ->
                if (seen.add(entry)) merged.add(entry)
            }
        }
        merged.sortBy { it.time }
        return if (merged.size <= MAX_ENTRIES) merged else merged.takeLast(MAX_ENTRIES)
    }

    /** Appends one streamed batch to the current console, capped. */
    fun append(current: List<LogEntry>, incoming: List<LogEntry>): List<LogEntry> =
        merge(current, incoming)
}

/** Timestamp and export-line formatting, shared by the UI and the exporter. */
object LogFormat {
    private val timeFormatter = DateTimeFormatter.ofPattern("HH:mm:ss", Locale.ROOT)
    private val dateFormatter = DateTimeFormatter.ofPattern("yyyy-MM-dd", Locale.ROOT)

    /** The console's short clock time, e.g. `14:03:09`. */
    fun clock(time: Instant, zone: ZoneId = ZoneId.systemDefault()): String =
        timeFormatter.withZone(zone).format(time)

    /** The calendar day, used in the copy/export line. */
    fun day(time: Instant, zone: ZoneId = ZoneId.systemDefault()): String =
        dateFormatter.withZone(zone).format(time)

    /** One export/copy line: `2026-10-22T12:03:09Z warn net: message`. */
    fun line(entry: LogEntry): String {
        val stamp = DateTimeFormatter.ISO_INSTANT.format(entry.time)
        return "$stamp ${entry.level.wire} ${entry.tag}: ${entry.message}"
    }

    /** The line copied from one console row, with the local date for context. */
    fun copyLine(entry: LogEntry): String = line(entry)
}
