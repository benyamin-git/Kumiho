package dev.benyamin.kumiho.crash

import android.content.Context
import java.io.ByteArrayOutputStream
import java.io.File
import java.io.FileOutputStream
import java.io.PrintWriter
import java.io.StringWriter
import java.time.Instant
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import kotlinx.coroutines.CancellationException

/**
 * The local crash log. It is written before the process dies and replayed into
 * the in-app log on the next engine start. Nothing is uploaded and no token or
 * configuration file is read.
 */
object CrashLog {
    /** The crash file name under `filesDir`. */
    const val FILE_NAME = "crash.log"

    /** The hard cap; a runaway stack trace can never fill the private storage. */
    const val MAX_BYTES = 64 * 1024

    /**
     * The complete crash text: timestamp, thread, exception and stack. The
     * stack includes every cause because [Throwable.printStackTrace] renders
     * the chain.
     */
    fun format(
        timestamp: Instant,
        thread: Thread,
        error: Throwable,
        zone: ZoneOffset = ZoneOffset.UTC,
    ): String {
        val stack = StringWriter().also { writer ->
            error.printStackTrace(PrintWriter(writer))
        }.toString()
        val exception = error::class.java.name +
            (error.message?.takeIf { it.isNotBlank() }?.let { ": $it" } ?: "")
        return buildString {
            append("time: ")
            append(DateTimeFormatter.ISO_INSTANT.withZone(zone).format(timestamp))
            append('\n')
            append("thread: ")
            append(thread.name)
            append('\n')
            append("exception: ")
            append(exception)
            append('\n')
            append("stack:\n")
            append(stack)
        }
    }

    /** Writes at most [MAX_BYTES] of UTF-8 text, truncating the tail. */
    fun write(file: File, text: String, maxBytes: Int = MAX_BYTES) {
        file.parentFile?.mkdirs()
        val bytes = text.toByteArray(Charsets.UTF_8)
        val length = minOf(bytes.size, maxBytes.coerceAtLeast(0))
        FileOutputStream(file).use { stream ->
            stream.write(bytes, 0, length)
            stream.flush()
        }
    }

    /**
     * Reads up to [maxBytes] from the crash file, or null when it does not
     * exist. A malformed or truncated file is returned as decoded text with
     * replacement characters; replay is best effort and never parses it.
     */
    fun readCapped(file: File, maxBytes: Int = MAX_BYTES): String? {
        if (!file.isFile) return null
        return try {
            file.inputStream().use { stream ->
                val buffer = ByteArrayOutputStream()
                val chunk = ByteArray(4096)
                var remaining = maxBytes.coerceAtLeast(0)
                while (remaining > 0) {
                    val read = stream.read(chunk, 0, minOf(chunk.size, remaining))
                    if (read <= 0) break
                    buffer.write(chunk, 0, read)
                    remaining -= read
                }
                buffer.toByteArray().toString(Charsets.UTF_8)
            }
        } catch (_: Exception) {
            null
        }
    }

    /**
     * Replays the file once through [append] and deletes it on success. A blank
     * or empty file is deleted without a call; a failed append keeps the file so
     * the next engine start can try again. Cancellation is never swallowed.
     */
    suspend fun replay(file: File, append: suspend (String) -> Unit): Boolean {
        if (!file.isFile) return false
        val text = readCapped(file) ?: return false
        if (text.isBlank()) {
            file.delete()
            return true
        }
        return try {
            append(text)
            file.delete()
            true
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (_: Throwable) {
            false
        }
    }
}

/**
 * The uncaught-exception handler. It writes the local crash file and always
 * delegates to the previous handler, so the process still dies exactly as
 * Android expects. Writing is bounded by [CrashLog.MAX_BYTES]; failures in the
 * handler never replace the original crash.
 */
class CrashHandler(
    private val file: File,
    private val previous: Thread.UncaughtExceptionHandler?,
) : Thread.UncaughtExceptionHandler {
    override fun uncaughtException(thread: Thread, error: Throwable) {
        try {
            CrashLog.write(file, CrashLog.format(Instant.now(), thread, error))
        } catch (_: Throwable) {
            // Never let the crash writer mask the crash it is recording.
        }
        if (previous != null) {
            previous.uncaughtException(thread, error)
        } else {
            android.util.Log.e(TAG, "uncaught exception", error)
        }
    }

    companion object {
        private const val TAG = "KumihoCrash"

        /** Installs the handler, preserving the one that was active before. */
        fun install(context: Context): CrashHandler {
            val previous = Thread.getDefaultUncaughtExceptionHandler()
            val handler = CrashHandler(File(context.filesDir, CrashLog.FILE_NAME), previous)
            Thread.setDefaultUncaughtExceptionHandler(handler)
            return handler
        }
    }
}
