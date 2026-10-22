package dev.benyamin.kumiho.ui.settings

import kotlinx.coroutines.runBlocking
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The manual GitHub update check, over a fake HTTP seam: the exact endpoint and
 * User-Agent, numeric semver comparison, pre-release exclusion and every honest
 * failure. Nothing here touches the network.
 */
class UpdateCheckerTest {

    private val fetcher = FakeFetcher()

    private fun checker() = GitHubUpdateChecker(fetcher)

    // ---- the happy paths -------------------------------------------------

    @Test
    fun `a newer stable release is offered with its release page`() = runBlocking {
        fetcher.response = UpdateHttpResponse(
            code = 200,
            body = release(tag = "v0.6.0", htmlUrl = RELEASE_URL),
        )

        assertEquals(
            UpdateResult.Available(version = "0.6.0", url = RELEASE_URL),
            checker().check("0.5.0"),
        )
        assertEquals(
            listOf(GitHubUpdateChecker.LATEST_URL to GitHubUpdateChecker.USER_AGENT),
            fetcher.requests,
        )
    }

    @Test
    fun `an equal version is up to date`() = runBlocking {
        fetcher.response = UpdateHttpResponse(200, release("v0.5.0"))
        assertEquals(UpdateResult.UpToDate, checker().check("0.5.0"))
    }

    @Test
    fun `an older release is up to date`() = runBlocking {
        fetcher.response = UpdateHttpResponse(200, release("v0.4.9"))
        assertEquals(UpdateResult.UpToDate, checker().check("0.5.0"))
    }

    @Test
    fun `semver compares numerically not lexically`() = runBlocking {
        fetcher.response = UpdateHttpResponse(200, release("v0.10.0"))
        assertEquals(
            UpdateResult.Available(version = "0.10.0", url = DEFAULT_URL),
            checker().check("0.9.0"),
        )
    }

    @Test
    fun `a debug version suffix is ignored when comparing`() = runBlocking {
        fetcher.response = UpdateHttpResponse(200, release("v0.5.0"))
        assertEquals(UpdateResult.UpToDate, checker().check("0.5.0-debug"))
    }

    @Test
    fun `a missing html url falls back to the release tag page`() = runBlocking {
        fetcher.response = UpdateHttpResponse(
            200,
            JSONObject().put("tag_name", "v0.6.0").toString(),
        )
        assertEquals(
            UpdateResult.Available(
                version = "0.6.0",
                url = "${GitHubUpdateChecker.RELEASES_PAGE}/tag/v0.6.0",
            ),
            checker().check("0.5.0"),
        )
    }

    // ---- pre-releases ----------------------------------------------------

    @Test
    fun `a payload marked prerelease is ignored`() = runBlocking {
        fetcher.response = UpdateHttpResponse(
            200,
            release("v0.9.0", prerelease = true),
        )
        assertEquals(UpdateResult.UpToDate, checker().check("0.5.0"))
    }

    @Test
    fun `a hyphen tag is a pre-release and is ignored`() = runBlocking {
        fetcher.response = UpdateHttpResponse(200, release("v0.6.0-rc.1"))
        assertEquals(UpdateResult.UpToDate, checker().check("0.5.0"))
    }

    // ---- failures --------------------------------------------------------

    @Test
    fun `a transport failure is reported and never invented`() = runBlocking {
        fetcher.error = IllegalStateException("no route to host")
        assertEquals(
            UpdateResult.Failed("no route to host"),
            checker().check("0.5.0"),
        )
    }

    @Test
    fun `a non-success status is a failure with the code`() = runBlocking {
        fetcher.response = UpdateHttpResponse(503, "")
        val result = checker().check("0.5.0")
        assertTrue(result is UpdateResult.Failed)
        assertTrue((result as UpdateResult.Failed).message.contains("503"))
    }

    @Test
    fun `malformed JSON is a failure`() = runBlocking {
        fetcher.response = UpdateHttpResponse(200, "not json")
        assertTrue(checker().check("0.5.0") is UpdateResult.Failed)
    }

    @Test
    fun `a release without a tag is a failure`() = runBlocking {
        fetcher.response = UpdateHttpResponse(200, "{}")
        assertTrue(checker().check("0.5.0") is UpdateResult.Failed)
    }

    @Test
    fun `a tag that is not a version is a failure`() = runBlocking {
        fetcher.response = UpdateHttpResponse(200, release("nightly"))
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
    ): String {
        val release = JSONObject()
            .put("tag_name", tag)
            .put("prerelease", prerelease)
        htmlUrl?.let { release.put("html_url", it) }
        return release.toString()
    }

    private class FakeFetcher : UpdateFetcher {
        var response = UpdateHttpResponse(200, "{}")
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
    }
}
