package dev.benyamin.kumiho.ui.settings

import java.net.HttpURLConnection
import java.net.URI
import java.net.URL
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONException
import org.json.JSONObject

/** One HTTP response; the seam exists so tests never touch the network. */
data class UpdateHttpResponse(val code: Int, val body: String)

/**
 * The blocking GET the checker needs. Production uses [HttpUrlConnectionFetcher];
 * every test injects a fake that records the request and returns canned text.
 */
interface UpdateFetcher {
    fun get(url: String, userAgent: String): UpdateHttpResponse
}

/** The structured reason an update check or install step failed. */
sealed interface UpdateFailure {
    data object InstalledVersionInvalid : UpdateFailure

    data class Network(val detail: String?) : UpdateFailure

    data class Http(val code: Int) : UpdateFailure

    data object Malformed : UpdateFailure

    data object NoRelease : UpdateFailure

    data object MissingTag : UpdateFailure

    data object InvalidTag : UpdateFailure

    data class Download(val detail: String?) : UpdateFailure

    data object Checksum : UpdateFailure

    data object InvalidApk : UpdateFailure

    data class Install(val detail: String?) : UpdateFailure
}

/** One downloadable release asset; the URL is checked before it is offered. */
data class UpdateAsset(val name: String, val url: String)

/** The result of one check. [Available] carries the page and, when published, the assets. */
sealed interface UpdateResult {
    data object UpToDate : UpdateResult

    data class Available(
        val version: String,
        val url: String,
        val apk: UpdateAsset? = null,
        val checksum: UpdateAsset? = null,
    ) : UpdateResult

    data class Failed(val failure: UpdateFailure) : UpdateResult
}

/**
 * The release feed. The app-scoped `UpdateManager` calls it on launch (throttled
 * to once every 24 hours) and from Settings; this interface downloads nothing.
 * Prereleases are included: every v0.x release is published as a GitHub
 * prerelease, so the latest-release endpoint answered 404 for the whole series.
 */
interface UpdateChecker {
    suspend fun check(currentVersion: String): UpdateResult
}

/**
 * Reads GitHub's release list for `benyamin-git/Kumiho`, takes the newest
 * published release, compares its `tag_name` to the installed app version with
 * a numeric semver comparison and returns the release page plus the matching
 * ABI APK and checksum asset. Nothing is downloaded or installed here.
 */
class GitHubUpdateChecker(
    private val fetcher: UpdateFetcher = HttpUrlConnectionFetcher(),
    private val supportedAbis: List<String> = emptyList(),
) : UpdateChecker {

    override suspend fun check(currentVersion: String): UpdateResult = withContext(Dispatchers.IO) {
        val current = parseSemver(currentVersion)
            ?: return@withContext UpdateResult.Failed(UpdateFailure.InstalledVersionInvalid)
        val response = try {
            fetcher.get(RELEASES_URL, USER_AGENT)
        } catch (error: Exception) {
            return@withContext UpdateResult.Failed(
                UpdateFailure.Network(error.message?.takeIf { it.isNotBlank() }),
            )
        }
        if (response.code != HttpURLConnection.HTTP_OK) {
            return@withContext UpdateResult.Failed(UpdateFailure.Http(response.code))
        }
        val releases = try {
            JSONArray(response.body)
        } catch (error: JSONException) {
            return@withContext UpdateResult.Failed(UpdateFailure.Malformed)
        }
        val release = (0 until releases.length())
            .mapNotNull { index -> releases.optJSONObject(index) }
            .firstOrNull { !it.optBoolean("draft", false) }
            ?: return@withContext UpdateResult.Failed(UpdateFailure.NoRelease)
        val tag = release.optString("tag_name").trim()
        if (tag.isEmpty()) {
            return@withContext UpdateResult.Failed(UpdateFailure.MissingTag)
        }
        val normalized = tag.removePrefix("v").removePrefix("V")
        if (isPrereleaseTag(normalized)) return@withContext UpdateResult.UpToDate
        val latest = parseSemver(normalized)
            ?: return@withContext UpdateResult.Failed(UpdateFailure.InvalidTag)
        if (latest <= current) return@withContext UpdateResult.UpToDate
        val url = release.optString("html_url").trim().ifEmpty { "$RELEASES_PAGE/tag/$tag" }
        val assets = parseAssets(release)
        UpdateResult.Available(
            version = normalized,
            url = url,
            apk = selectApk(assets, normalized),
            checksum = selectChecksum(assets, normalized),
        )
    }

    /** Keeps only assets whose URL is an HTTPS Kumiho release download. */
    private fun parseAssets(release: JSONObject): List<UpdateAsset> {
        val assets = release.optJSONArray("assets") ?: return emptyList()
        return (0 until assets.length()).mapNotNull { index ->
            val item = assets.optJSONObject(index) ?: return@mapNotNull null
            val name = item.optString("name").trim()
            val url = item.optString("browser_download_url").trim()
            if (name.isEmpty() || !validAssetURL(url)) null else UpdateAsset(name, url)
        }
    }

    /**
     * The versioned asset wins over the stable alias, and the phone's preferred
     * ABI wins over the next supported one. The universal APK is the fallback.
     */
    private fun selectApk(assets: List<UpdateAsset>, version: String): UpdateAsset? {
        for (abi in supportedAbis) {
            assets.firstOrNull { it.name == "Kumiho-v$version-Android-$abi.apk" }
                ?.let { return it }
            assets.firstOrNull { it.name == "Kumiho-Android-$abi.apk" }
                ?.let { return it }
        }
        return assets.firstOrNull { it.name == "Kumiho-v$version-Android-universal.apk" }
            ?: assets.firstOrNull { it.name == "Kumiho-Android-universal.apk" }
    }

    private fun selectChecksum(assets: List<UpdateAsset>, version: String): UpdateAsset? =
        assets.firstOrNull { it.name == "SHA256SUMS-v$version" }
            ?: assets.firstOrNull { it.name == "SHA256SUMS" }

    /** Only HTTPS assets on this project's release download path are accepted. */
    private fun validAssetURL(raw: String): Boolean = try {
        val uri = URI(raw)
        uri.scheme.equals("https", ignoreCase = true) &&
            uri.host.equals("github.com", ignoreCase = true) &&
            uri.path?.startsWith(
                "/benyamin-git/Kumiho/releases/download/",
                ignoreCase = true,
            ) == true
    } catch (_: Exception) {
        false
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
 * The production release-feed client. It performs one GET, sends the required
 * User-Agent, and reads a bounded body; asset and APK downloads use
 * `HttpUpdateDownloader`.
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
