package dev.benyamin.kumiho.ui.settings

import kotlinx.coroutines.runBlocking
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The manual GitHub update check, over a fake HTTP seam: the exact endpoint and
 * User-Agent, numeric semver comparison, the prerelease list and every honest
 * failure. Nothing here touches the network.
 */
class UpdateCheckerTest {

    private val fetcher = FakeFetcher()

    private fun checker() = GitHubUpdateChecker(fetcher)

    // ---- the happy paths -------------------------------------------------

    @Test
    fun `a newer release is offered with its release page`() = runBlocking {
        fetcher.response = UpdateHttpResponse(
            code = 200,
            body = releases(release(tag = "v0.6.0", htmlUrl = RELEASE_URL)),
        )

        assertEquals(
            UpdateResult.Available(version = "0.6.0", url = RELEASE_URL),
            checker().check("0.5.0"),
        )
        assertEquals(
            listOf(GitHubUpdateChecker.RELEASES_URL to GitHubUpdateChecker.USER_AGENT),
            fetcher.requests,
        )
    }

    @Test
    fun `an equal version is up to date`() = runBlocking {
        fetcher.response = UpdateHttpResponse(200, releases(release("v0.5.0")))
        assertEquals(UpdateResult.UpToDate, checker().check("0.5.0"))
    }

    @Test
    fun `an older release is up to date`() = runBlocking {
        fetcher.response = UpdateHttpResponse(200, releases(release("v0.4.9")))
        assertEquals(UpdateResult.UpToDate, checker().check("0.5.0"))
    }

    @Test
    fun `semver compares numerically not lexically`() = runBlocking {
        fetcher.response = UpdateHttpResponse(200, releases(release("v0.10.0")))
        assertEquals(
            UpdateResult.Available(version = "0.10.0", url = DEFAULT_URL),
            checker().check("0.9.0"),
        )
    }

    @Test
    fun `a debug version suffix is ignored when comparing`() = runBlocking {
        fetcher.response = UpdateHttpResponse(200, releases(release("v0.5.0")))
        assertEquals(UpdateResult.UpToDate, checker().check("0.5.0-debug"))
    }

    @Test
    fun `the preferred abi apk and versioned checksum are selected`() = runBlocking {
        fetcher.response = UpdateHttpResponse(
            200,
            releases(releaseWithAssets("v0.6.0", "0.6.0")),
        )

        assertEquals(
            UpdateResult.Available(
                version = "0.6.0",
                url = DEFAULT_URL,
                apk = UpdateAsset(
                    "Kumiho-v0.6.0-Android-arm64-v8a.apk",
                    "$ASSET_BASE/Kumiho-v0.6.0-Android-arm64-v8a.apk",
                ),
                checksum = UpdateAsset(
                    "SHA256SUMS-v0.6.0",
                    "$ASSET_BASE/SHA256SUMS-v0.6.0",
                ),
            ),
            GitHubUpdateChecker(fetcher, listOf("arm64-v8a", "armeabi-v7a"))
                .check("0.5.0"),
        )
    }

    @Test
    fun `the universal apk is the fallback when no abi matches`() = runBlocking {
        fetcher.response = UpdateHttpResponse(
            200,
            releases(releaseWithAssets("v0.6.0", "0.6.0")),
        )

        val result = GitHubUpdateChecker(fetcher, listOf("x86_64")).check("0.5.0")
        assertEquals(
            "Kumiho-v0.6.0-Android-universal.apk",
            (result as UpdateResult.Available).apk?.name,
        )
    }

    @Test
    fun `stable asset aliases are accepted when versioned ones are absent`() = runBlocking {
        fetcher.response = UpdateHttpResponse(
            200,
            releases(
                release("v0.6.0").put(
                    "assets",
                    JSONArray()
                        .put(asset("Kumiho-Android-armeabi-v7a.apk", "$ASSET_BASE/armv7"))
                        .put(asset("SHA256SUMS", "$ASSET_BASE/SHA256SUMS")),
                ),
            ),
        )

        val result = GitHubUpdateChecker(fetcher, listOf("armeabi-v7a")).check("0.5.0")
            as UpdateResult.Available
        assertEquals("Kumiho-Android-armeabi-v7a.apk", result.apk?.name)
        assertEquals("SHA256SUMS", result.checksum?.name)
    }

    @Test
    fun `an asset outside the release download path is ignored`() = runBlocking {
        fetcher.response = UpdateHttpResponse(
            200,
            releases(
                release("v0.6.0").put(
                    "assets",
                    JSONArray()
                        .put(asset("Kumiho-v0.6.0-Android-arm64-v8a.apk", "https://evil.test/a.apk"))
                        .put(asset("SHA256SUMS-v0.6.0", "https://evil.test/SHA256SUMS")),
                ),
            ),
        )

        val result = GitHubUpdateChecker(fetcher, listOf("arm64-v8a")).check("0.5.0")
            as UpdateResult.Available
        assertNull(result.apk)
        assertNull(result.checksum)
    }

    @Test
    fun `an asset from another repository is ignored`() = runBlocking {
        fetcher.response = UpdateHttpResponse(
            200,
            releases(
                release("v0.6.0").put(
                    "assets",
                    JSONArray()
                        .put(
                            asset(
                                "Kumiho-v0.6.0-Android-arm64-v8a.apk",
                                "https://github.com/other/repo/releases/download/v0.6.0/a.apk",
                            ),
                        )
                        .put(
                            asset(
                                "SHA256SUMS-v0.6.0",
                                "https://github.com/other/repo/releases/download/v0.6.0/SHA256SUMS",
                            ),
                        ),
                ),
            ),
        )

        assertEquals(
            UpdateResult.Available(version = "0.6.0", url = DEFAULT_URL),
            checker().check("0.5.0"),
        )
    }

    @Test
    fun `a missing html url falls back to the release tag page`() = runBlocking {
        fetcher.response = UpdateHttpResponse(
            200,
            releases(JSONObject().put("tag_name", "v0.6.0")),
        )
        assertEquals(
            UpdateResult.Available(
                version = "0.6.0",
                url = "${GitHubUpdateChecker.RELEASES_PAGE}/tag/v0.6.0",
            ),
            checker().check("0.5.0"),
        )
    }

    // ---- prereleases and drafts -----------------------------------------

    @Test
    fun `a release marked prerelease is offered`() = runBlocking {
        fetcher.response = UpdateHttpResponse(
            200,
            releases(release("v0.9.0", prerelease = true)),
        )
        assertEquals(
            UpdateResult.Available(version = "0.9.0", url = DEFAULT_URL),
            checker().check("0.5.0"),
        )
    }

    @Test
    fun `a draft is skipped`() = runBlocking {
        fetcher.response = UpdateHttpResponse(
            200,
            releases(
                release("v0.9.0", draft = true),
                release("v0.6.0"),
            ),
        )
        assertEquals(
            UpdateResult.Available(version = "0.6.0", url = DEFAULT_URL),
            checker().check("0.5.0"),
        )
    }

    @Test
    fun `a hyphen tag is a pre-release and is ignored`() = runBlocking {
        fetcher.response = UpdateHttpResponse(200, releases(release("v0.6.0-rc.1")))
        assertEquals(UpdateResult.UpToDate, checker().check("0.5.0"))
    }

    // ---- failures --------------------------------------------------------

    @Test
    fun `a transport failure is reported and never invented`() = runBlocking {
        fetcher.error = IllegalStateException("no route to host")
        assertEquals(
            UpdateResult.Failed(UpdateFailure.Network("no route to host")),
            checker().check("0.5.0"),
        )
    }

    @Test
    fun `a non-success status is a failure with the code`() = runBlocking {
        fetcher.response = UpdateHttpResponse(503, "")
        val result = checker().check("0.5.0")
        assertTrue(result is UpdateResult.Failed)
        assertEquals(UpdateFailure.Http(503), (result as UpdateResult.Failed).failure)
    }

    @Test
    fun `malformed JSON is a failure`() = runBlocking {
        fetcher.response = UpdateHttpResponse(200, "not json")
        assertTrue(checker().check("0.5.0") is UpdateResult.Failed)
    }

    @Test
    fun `an empty release list is a failure`() = runBlocking {
        fetcher.response = UpdateHttpResponse(200, "[]")
        assertTrue(checker().check("0.5.0") is UpdateResult.Failed)
    }

    @Test
    fun `a release without a tag is a failure`() = runBlocking {
        fetcher.response = UpdateHttpResponse(200, releases(JSONObject()))
        assertTrue(checker().check("0.5.0") is UpdateResult.Failed)
    }

    @Test
    fun `a tag that is not a version is a failure`() = runBlocking {
        fetcher.response = UpdateHttpResponse(200, releases(release("nightly")))
        assertTrue(checker().check("0.5.0") is UpdateResult.Failed)
    }

    @Test
    fun `an unreadable installed version fails before any request`() = runBlocking {
        assertTrue(checker().check("development") is UpdateResult.Failed)
        assertTrue(fetcher.requests.isEmpty())
    }

    // ---- helpers ---------------------------------------------------------

    private fun release(
        tag: String,
        htmlUrl: String? = DEFAULT_URL,
        prerelease: Boolean = false,
        draft: Boolean = false,
    ): JSONObject {
        val release = JSONObject()
            .put("tag_name", tag)
            .put("prerelease", prerelease)
            .put("draft", draft)
        htmlUrl?.let { release.put("html_url", it) }
        return release
    }

    private fun releaseWithAssets(tag: String, version: String): JSONObject = release(tag)
        .put(
            "assets",
            JSONArray()
                .put(
                    asset(
                        "Kumiho-v$version-Android-arm64-v8a.apk",
                        "$ASSET_BASE/Kumiho-v$version-Android-arm64-v8a.apk",
                    ),
                )
                .put(
                    asset(
                        "Kumiho-v$version-Android-armeabi-v7a.apk",
                        "$ASSET_BASE/Kumiho-v$version-Android-armeabi-v7a.apk",
                    ),
                )
                .put(
                    asset(
                        "Kumiho-v$version-Android-universal.apk",
                        "$ASSET_BASE/Kumiho-v$version-Android-universal.apk",
                    ),
                )
                .put(asset("SHA256SUMS-v$version", "$ASSET_BASE/SHA256SUMS-v$version"))
                .put(asset("SHA256SUMS", "$ASSET_BASE/SHA256SUMS")),
        )

    private fun asset(name: String, url: String): JSONObject =
        JSONObject().put("name", name).put("browser_download_url", url)

    private fun releases(vararg releases: JSONObject): String {
        val array = JSONArray()
        releases.forEach { array.put(it) }
        return array.toString()
    }

    private class FakeFetcher : UpdateFetcher {
        var response = UpdateHttpResponse(200, "[]")
        var error: Exception? = null
        val requests = mutableListOf<Pair<String, String>>()

        override fun get(url: String, userAgent: String): UpdateHttpResponse {
            requests += url to userAgent
            error?.let { throw it }
            return response
        }
    }

    private companion object {
        const val RELEASE_URL = "https://github.com/benyamin-git/Kumiho/releases/tag/v0.6.0"
        const val DEFAULT_URL = RELEASE_URL
        const val ASSET_BASE =
            "https://github.com/benyamin-git/Kumiho/releases/download/v0.6.0"
    }
}
