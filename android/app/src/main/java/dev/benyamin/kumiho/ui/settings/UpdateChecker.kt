package dev.benyamin.kumiho.ui.settings

import java.net.HttpURLConnection
import java.net.URL
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONException

/** One HTTP response; the seam exists so tests never touch the network. */
data class UpdateHttpResponse(val code: Int, val body: String)

/**
 * The blocking GET the checker needs. Production uses [HttpUrlConnectionFetcher];
 * every test injects a fake that records the request and returns canned text.
 */
interface UpdateFetcher {
    fun get(url: String, userAgent: String): UpdateHttpResponse
}

/** The result of one manual check. Only [Available] carries a release to open. */
sealed interface UpdateResult {
    data object UpToDate : UpdateResult

    data class Available(val version: String, val url: String) : UpdateResult

    data class Failed(val message: String) : UpdateResult
}

/**
 * The update check. It is called only from the Settings tap: there is no
 * background job, no periodic poll and no download. Prereleases are included:
 * every v0.x release is published as a GitHub prerelease, so the latest-release
 * endpoint answered 404 for the whole series.
 */
interface UpdateChecker {
    suspend fun check(currentVersion: String): UpdateResult
}

/**
 * Reads GitHub's release list for `benyamin-git/Kumiho`, takes the newest
 * published release, compares its `tag_name` to the installed app version with
 * a numeric semver comparison and returns the release page URL. Nothing is
 * downloaded or installed.
 */
class GitHubUpdateChecker(
    private val fetcher: UpdateFetcher = HttpUrlConnectionFetcher(),
) : UpdateChecker {

    override suspend fun check(currentVersion: String): UpdateResult = withContext(Dispatchers.IO) {
        val current = parseSemver(currentVersion)
            ?: return@withContext UpdateResult.Failed("The installed version is not a version number.")
        val response = try {
            fetcher.get(RELEASES_URL, USER_AGENT)
        } catch (error: Exception) {
            return@withContext UpdateResult.Failed(
                error.message?.takeIf { it.isNotBlank() } ?: "The update check could not reach GitHub.",
            )
        }
        if (response.code != HttpURLConnection.HTTP_OK) {
            return@withContext UpdateResult.Failed("GitHub answered HTTP ${response.code}.")
        }
        val releases = try {
            JSONArray(response.body)
        } catch (error: JSONException) {
            return@withContext UpdateResult.Failed("GitHub sent a release that could not be read.")
        }
        val release = (0 until releases.length())
            .mapNotNull { index -> releases.optJSONObject(index) }
            .firstOrNull { !it.optBoolean("draft", false) }
            ?: return@withContext UpdateResult.Failed("GitHub has no published release.")
        val tag = release.optString("tag_name").trim()
        if (tag.isEmpty()) {
            return@withContext UpdateResult.Failed("GitHub's latest release has no version tag.")
        }
        val normalized = tag.removePrefix("v").removePrefix("V")
        if (isPrereleaseTag(normalized)) return@withContext UpdateResult.UpToDate
        val latest = parseSemver(normalized)
            ?: return@withContext UpdateResult.Failed("GitHub's latest release tag is not a version number.")
        if (latest <= current) return@withContext UpdateResult.UpToDate
        val url = release.optString("html_url").trim().ifEmpty { "$RELEASES_PAGE/tag/$tag" }
        UpdateResult.Available(version = normalized, url = url)
    }

    /** `0.10.0` is newer than `0.9.0`; leading `v` and build suffixes are tolerated. */
    private fun parseSemver(raw: String): Semver? {
        var text = raw.trim().removePrefix("v").removePrefix("V")
        val cut = text.indexOfFirst { it == '-' || it == '+' }
        if (cut >= 0) text = text.substring(0, cut)
        val parts = text.split('.')
        if (parts.size < 3) return null
        val numbers = parts.take(3).map { part ->
            if (part.isEmpty() || part.any { !it.isDigit() }) return null
            part.toIntOrNull() ?: return null
        }
        return Semver(numbers[0], numbers[1], numbers[2])
    }

    /** A tag with a hyphen suffix (`0.6.0-rc.1`) is a pre-release and is ignored. */
    private fun isPrereleaseTag(version: String): Boolean = version.contains('-')

    private data class Semver(val major: Int, val minor: Int, val patch: Int) : Comparable<Semver> {
        override fun compareTo(other: Semver): Int = when {
            major != other.major -> major.compareTo(other.major)
            minor != other.minor -> minor.compareTo(other.minor)
            else -> patch.compareTo(other.patch)
        }
    }

    companion object {
        /** The token-free GitHub endpoint; A11 fixes the repository and shape. */
        const val RELEASES_URL = "https://api.github.com/repos/benyamin-git/Kumiho/releases?per_page=10"
        const val RELEASES_PAGE = "https://github.com/benyamin-git/Kumiho/releases"
        const val USER_AGENT = "Kumiho-Android"
        const val CONNECT_TIMEOUT_MS = 10_000
        const val READ_TIMEOUT_MS = 10_000
    }
}

/**
 * The production HTTP client. It performs one GET, sends the required
 * User-Agent, and reads a bounded body; there is no asset or APK request.
 */
class HttpUrlConnectionFetcher : UpdateFetcher {
    override fun get(url: String, userAgent: String): UpdateHttpResponse {
        val connection = URL(url).openConnection() as HttpURLConnection
        return try {
            connection.requestMethod = "GET"
            connection.connectTimeout = GitHubUpdateChecker.CONNECT_TIMEOUT_MS
            connection.readTimeout = GitHubUpdateChecker.READ_TIMEOUT_MS
            connection.instanceFollowRedirects = true
            connection.setRequestProperty("User-Agent", userAgent)
            connection.setRequestProperty("Accept", "application/vnd.github+json")
            val code = connection.responseCode
            val stream = if (code in 200..299) connection.inputStream else connection.errorStream
            val body = stream?.bufferedReader()?.use { it.readText() }.orEmpty()
            UpdateHttpResponse(code, body)
        } finally {
            connection.disconnect()
        }
    }
}
