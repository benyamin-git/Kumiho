package dev.benyamin.kumiho.update

import java.io.ByteArrayOutputStream
import java.io.File
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/** The two reads the updater needs; tests replace both with canned bytes. */
interface UpdateDownloader {
    /** The small checksum file, bounded so a wrong URL cannot exhaust memory. */
    suspend fun text(url: String): String

    /** Streams one APK to [destination], reporting 0–100 as bytes arrive. */
    suspend fun download(url: String, destination: File, onProgress: (Int) -> Unit): File
}

/**
 * Finds the exact APK's hash in a `sha256sum`-style file. Stable aliases and
 * versioned names are different lines, so the caller must pass the selected
 * asset name.
 */
internal fun checksumFor(text: String, assetName: String): String? {
    for (line in text.lineSequence()) {
        val parts = line.trim().split(Regex("\\s+"), limit = 2)
        if (parts.size != 2) continue
        val hash = parts[0].lowercase()
        val name = parts[1].removePrefix("*")
        if (name != assetName) continue
        if (hash.length == 64 && hash.all { it in '0'..'9' || it in 'a'..'f' }) return hash
    }
    return null
}

/** Lower-case SHA-256 of a file. */
internal fun sha256(file: File): String {
    val digest = MessageDigest.getInstance("SHA-256")
    file.inputStream().use { input ->
        val buffer = ByteArray(64 * 1024)
        while (true) {
            val read = input.read(buffer)
            if (read < 0) break
            digest.update(buffer, 0, read)
        }
    }
    return digest.digest().joinToString("") { "%02x".format(it.toInt() and 0xff) }
}

/** Production downloader: HTTPS, one redirect chain, bounded bodies. */
class HttpUpdateDownloader : UpdateDownloader {

    override suspend fun text(url: String): String = withContext(Dispatchers.IO) {
        val connection = open(url)
        try {
            val body = ByteArrayOutputStream()
            connection.inputStream.use { input ->
                val buffer = ByteArray(8 * 1024)
                while (true) {
                    val read = input.read(buffer)
                    if (read < 0) break
                    body.write(buffer, 0, read)
                    if (body.size() > MAX_TEXT_BYTES) {
                        throw IOException("checksum file is too large")
                    }
                }
            }
            body.toString(Charsets.UTF_8.name())
        } finally {
            connection.disconnect()
        }
    }

    override suspend fun download(
        url: String,
        destination: File,
        onProgress: (Int) -> Unit,
    ): File = withContext(Dispatchers.IO) {
        val connection = open(url)
        try {
            val total = connection.contentLengthLong
            if (total > MAX_APK_BYTES) throw IOException("APK is too large")
            destination.parentFile?.mkdirs()
            var written = 0L
            destination.outputStream().use { output ->
                connection.inputStream.use { input ->
                    val buffer = ByteArray(64 * 1024)
                    while (true) {
                        val read = input.read(buffer)
                        if (read < 0) break
                        written += read
                        if (written > MAX_APK_BYTES) throw IOException("APK is too large")
                        output.write(buffer, 0, read)
                        if (total > 0) {
                            onProgress(((written * 100) / total).toInt().coerceIn(0, 100))
                        }
                    }
                }
            }
            if (total > 0 && written != total) {
                destination.delete()
                throw IOException("APK download was truncated")
            }
            onProgress(100)
            destination
        } catch (error: Exception) {
            destination.delete()
            throw error
        } finally {
            connection.disconnect()
        }
    }

    private fun open(url: String): HttpURLConnection {
        val connection = URL(url).openConnection() as HttpURLConnection
        connection.requestMethod = "GET"
        connection.connectTimeout = CONNECT_TIMEOUT_MS
        connection.readTimeout = READ_TIMEOUT_MS
        connection.instanceFollowRedirects = true
        connection.setRequestProperty("User-Agent", USER_AGENT)
        val code = connection.responseCode
        if (code !in 200..299) {
            connection.disconnect()
            throw IOException("HTTP $code")
        }
        return connection
    }

    private companion object {
        const val USER_AGENT = "Kumiho-Android"
        const val CONNECT_TIMEOUT_MS = 15_000
        const val READ_TIMEOUT_MS = 60_000
        const val MAX_TEXT_BYTES = 64 * 1024
        const val MAX_APK_BYTES = 200L * 1024 * 1024
    }
}
