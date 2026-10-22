package dev.benyamin.kumiho.data

import dev.benyamin.kumiho.engine.LogEntry
import dev.benyamin.kumiho.ui.logs.LogFormat
import java.io.File
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/**
 * Writes a log snapshot to the app's cache and names it for sharing. The file
 * is private to the app; the caller shares it through a FileProvider URI, so no
 * storage permission is ever needed and no folder is exposed.
 *
 * The formatting and writing are pure JVM code, so a unit test can export to a
 * temporary directory. The share intent is built by the screen, where Android
 * context belongs.
 */
object LogExporter {
    private val nameFormatter = DateTimeFormatter.ofPattern("yyyyMMdd-HHmmss", Locale.ROOT)

    /** The export directory under `cacheDir`; it is never in a shared path. */
    fun exportsDir(cacheDir: File): File = File(cacheDir, "exports")

    /** `kumiho-logs-20261022-120309.log`, in the caller's zone. */
    fun fileName(now: Instant, zone: ZoneId = ZoneId.systemDefault()): String =
        "kumiho-logs-${nameFormatter.withZone(zone).format(now)}.log"

    /** One line per entry, oldest first, each line newline-terminated. */
    fun render(entries: List<LogEntry>): String {
        if (entries.isEmpty()) return ""
        return buildString {
            entries.forEach { entry ->
                append(LogFormat.line(entry))
                append('\n')
            }
        }
    }

    /**
     * Writes the snapshot and returns the file. The exports directory is
     * created if needed and the file is replaced on a same-second export.
     */
    fun writeTo(
        directory: File,
        entries: List<LogEntry>,
        now: Instant,
        zone: ZoneId = ZoneId.systemDefault(),
    ): File {
        directory.mkdirs()
        val file = File(directory, fileName(now, zone))
        file.writeText(render(entries), Charsets.UTF_8)
        return file
    }
}
