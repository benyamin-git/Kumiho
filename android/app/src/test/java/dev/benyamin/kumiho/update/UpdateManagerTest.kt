package dev.benyamin.kumiho.update

import dev.benyamin.kumiho.ui.settings.UpdateAsset
import dev.benyamin.kumiho.ui.settings.UpdateChecker
import dev.benyamin.kumiho.ui.settings.UpdateFailure
import dev.benyamin.kumiho.ui.settings.UpdateResult
import dev.benyamin.kumiho.ui.settings.UpdateState
import java.io.File
import java.security.MessageDigest
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

/**
 * The self-update state machine on the JVM: release feed, consent, download,
 * checksum and APK verification, and the launch prompt. Every Android edge is
 * a fake; nothing here touches the network, the installer or a package file.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class UpdateManagerTest {

    @get:Rule
    val temporary = TemporaryFolder()

    private val checker = FakeChecker()
    private val downloader = FakeDownloader()
    private val inspector = FakeInspector()
    private val preferences = FakePreferences()
    private val cacheDir by lazy { temporary.newFolder("updates") }
    private var now = 1_000_000L

    private fun TestScope.manager(
        selfUpdateAllowed: Boolean = true,
    ) = UpdateManager(
        checker = checker,
        downloader = downloader,
        inspector = inspector,
        preferences = preferences,
        cacheDir = cacheDir,
        currentVersion = CURRENT,
        currentVersionCode = CURRENT_CODE,
        packageName = PACKAGE,
        selfUpdateAllowed = selfUpdateAllowed,
        scope = backgroundScope,
        now = { now },
    )

    @Test
    fun `a newer release carries its installable assets`() = runTest {
        checker.result = release()
        val manager = manager()

        manager.check()
        runCurrent()

        assertEquals(
            UpdateState.Available(VERSION, RELEASE_URL, installable = true),
            manager.state.value,
        )
    }

    @Test
    fun `install downloads the checksum first and then the apk and becomes ready`() = runTest {
        checker.result = release()
        val manager = manager()
        manager.check()
        runCurrent()

        manager.install()
        assertTrue(manager.state.value is UpdateState.Downloading)
        runCurrent()

        assertEquals(UpdateState.ReadyToInstall(VERSION), manager.state.value)
        assertEquals(listOf(SUMS_URL), downloader.textCalls)
        assertEquals(listOf(APK_URL), downloader.downloadCalls)
        assertNotNull(manager.readyFile())
        assertTrue(manager.readyFile()!!.isFile)
    }

    @Test
    fun `a checksum mismatch never offers the apk to the installer`() = runTest {
        checker.result = release()
        downloader.checksumText = "${"0".repeat(64)}  $APK_NAME\n"
        val manager = manager()
        manager.check()
        runCurrent()

        manager.install()
        runCurrent()

        assertEquals(
            UpdateState.Available(
                VERSION,
                RELEASE_URL,
                installable = true,
                installError = UpdateFailure.Checksum,
            ),
            manager.state.value,
        )
        assertNull(manager.readyFile())
        assertFalse(File(cacheDir, "Kumiho-update.apk").exists())
    }

    @Test
    fun `an apk for another package is refused`() = runTest {
        checker.result = release()
        inspector.info = ApkInfo("com.example.other", VERSION, NEW_CODE)
        val manager = manager()
        manager.check()
        runCurrent()

        manager.install()
        runCurrent()

        assertEquals(
            UpdateState.Available(
                VERSION,
                RELEASE_URL,
                installable = true,
                installError = UpdateFailure.InvalidApk,
            ),
            manager.state.value,
        )
    }

    @Test
    fun `an apk that is not newer than the installed build is refused`() = runTest {
        checker.result = release()
        inspector.info = ApkInfo(PACKAGE, VERSION, CURRENT_CODE)
        val manager = manager()
        manager.check()
        runCurrent()

        manager.install()
        runCurrent()

        assertEquals(
            UpdateState.Available(
                VERSION,
                RELEASE_URL,
                installable = true,
                installError = UpdateFailure.InvalidApk,
            ),
            manager.state.value,
        )
    }

    @Test
    fun `a launch check prompts once for a newer release`() = runTest {
        checker.result = release()
        val manager = manager()

        manager.checkOnLaunch()
        runCurrent()

        assertTrue(manager.prompt.value)
        assertEquals(
            UpdateState.Available(VERSION, RELEASE_URL, installable = true),
            manager.state.value,
        )
        assertEquals(now, preferences.lastCheck)
    }

    @Test
    fun `a launch check inside the interval is skipped`() = runTest {
        preferences.lastCheck = now - 1_000
        val manager = manager()

        manager.checkOnLaunch()
        runCurrent()

        assertEquals(0, checker.calls)
        assertFalse(manager.prompt.value)
    }

    @Test
    fun `a failed launch check stays quiet`() = runTest {
        checker.result = UpdateResult.Failed(UpdateFailure.Network("offline"))
        val manager = manager()

        manager.checkOnLaunch()
        runCurrent()

        assertFalse(manager.prompt.value)
        assertEquals(
            UpdateState.Failed(UpdateFailure.Network("offline")),
            manager.state.value,
        )
    }

    @Test
    fun `a launch check without installable assets does not prompt`() = runTest {
        checker.result = UpdateResult.Available(VERSION, RELEASE_URL)
        val manager = manager()

        manager.checkOnLaunch()
        runCurrent()

        assertFalse(manager.prompt.value)
        assertEquals(
            UpdateState.Available(VERSION, RELEASE_URL, installable = false),
            manager.state.value,
        )
    }

    @Test
    fun `a dismissed version is checked but never prompted again`() = runTest {
        checker.result = release()
        preferences.dismissed = VERSION
        val manager = manager()

        manager.checkOnLaunch()
        runCurrent()

        assertEquals(1, checker.calls)
        assertFalse(manager.prompt.value)
        assertTrue(manager.state.value is UpdateState.Available)
    }

    @Test
    fun `dismiss remembers the version and hides the prompt`() = runTest {
        checker.result = release()
        val manager = manager()
        manager.check()
        runCurrent()

        manager.dismissPrompt()
        runCurrent()

        assertFalse(manager.prompt.value)
        assertEquals(VERSION, preferences.dismissed)
    }

    @Test
    fun `a manual check never opens the launch prompt`() = runTest {
        checker.result = release()
        val manager = manager()

        manager.check()
        runCurrent()

        assertFalse(manager.prompt.value)
    }

    @Test
    fun `an up-to-date launch never prompts`() = runTest {
        checker.result = UpdateResult.UpToDate
        val manager = manager()

        manager.checkOnLaunch()
        runCurrent()

        assertEquals(UpdateState.UpToDate, manager.state.value)
        assertFalse(manager.prompt.value)
    }

    @Test
    fun `a debug build never self-updates or prompts on launch`() = runTest {
        checker.result = release()
        val manager = manager(selfUpdateAllowed = false)

        manager.checkOnLaunch()
        runCurrent()
        assertEquals(0, checker.calls)

        manager.check()
        runCurrent()
        assertEquals(
            UpdateState.Available(VERSION, RELEASE_URL, installable = false),
            manager.state.value,
        )
        manager.install()
        runCurrent()
        assertTrue(downloader.downloadCalls.isEmpty())
    }

    @Test
    fun `a missing ready apk is downloaded again`() = runTest {
        checker.result = release()
        val manager = manager()
        manager.check()
        runCurrent()
        manager.install()
        runCurrent()

        assertTrue(manager.readyFile()!!.delete())
        manager.install()
        assertTrue(manager.state.value is UpdateState.Downloading)
        runCurrent()

        assertEquals(UpdateState.ReadyToInstall(VERSION), manager.state.value)
        assertEquals(2, downloader.downloadCalls.size)
    }

    @Test
    fun `the installer result refreshes an in-process install`() = runTest {
        checker.result = release()
        val manager = manager()
        manager.check()
        runCurrent()
        manager.install()
        runCurrent()
        manager.onInstallerLaunched()
        assertEquals(UpdateState.Installing(VERSION), manager.state.value)

        manager.onResume(VERSION)

        assertEquals(UpdateState.Installed(VERSION), manager.state.value)
    }

    @Test
    fun `closing the installing dialog returns to the ready state`() = runTest {
        checker.result = release()
        val manager = manager()
        manager.check()
        runCurrent()
        manager.install()
        runCurrent()
        manager.onInstallerLaunched()

        manager.dismissPrompt()

        assertFalse(manager.prompt.value)
        assertEquals(UpdateState.ReadyToInstall(VERSION), manager.state.value)
    }

    @Test
    fun `a cancelled installer returns to the ready state`() = runTest {
        checker.result = release()
        val manager = manager()
        manager.check()
        runCurrent()
        manager.install()
        runCurrent()
        manager.onInstallerLaunched()

        manager.onResume(CURRENT)

        assertEquals(UpdateState.ReadyToInstall(VERSION), manager.state.value)
    }

    private fun release() = UpdateResult.Available(
        version = VERSION,
        url = RELEASE_URL,
        apk = UpdateAsset(APK_NAME, APK_URL),
        checksum = UpdateAsset(SUMS_NAME, SUMS_URL),
    )

    private class FakeChecker : UpdateChecker {
        var result: UpdateResult = UpdateResult.UpToDate
        var calls = 0

        override suspend fun check(currentVersion: String): UpdateResult {
            calls += 1
            return result
        }
    }

    private class FakeDownloader : UpdateDownloader {
        var checksumText = "${sha256Of(APK_BYTES)}  $APK_NAME\n"
        val textCalls = mutableListOf<String>()
        val downloadCalls = mutableListOf<String>()

        override suspend fun text(url: String): String {
            textCalls += url
            return checksumText
        }

        override suspend fun download(
            url: String,
            destination: File,
            onProgress: (Int) -> Unit,
        ): File {
            downloadCalls += url
            destination.parentFile?.mkdirs()
            destination.writeBytes(APK_BYTES)
            onProgress(50)
            onProgress(100)
            return destination
        }
    }

    private class FakeInspector : ApkInspector {
        var info: ApkInfo? = ApkInfo(PACKAGE, VERSION, NEW_CODE)

        override fun inspect(file: File): ApkInfo? = info
    }

    private class FakePreferences : UpdatePreferences {
        var lastCheck = 0L
        var dismissed = ""

        override suspend fun lastUpdateCheck(): Long = lastCheck

        override suspend fun setLastUpdateCheck(value: Long) {
            lastCheck = value
        }

        override suspend fun dismissedUpdateVersion(): String = dismissed

        override suspend fun setDismissedUpdateVersion(value: String) {
            dismissed = value
        }
    }

    private companion object {
        const val PACKAGE = "dev.benyamin.kumiho"
        const val CURRENT = "0.13.0"
        const val CURRENT_CODE = 1300L
        const val VERSION = "0.14.0"
        const val NEW_CODE = 1400L
        const val APK_NAME = "Kumiho-v0.14.0-Android-arm64-v8a.apk"
        const val SUMS_NAME = "SHA256SUMS-v0.14.0"
        const val RELEASE_URL = "https://github.com/benyamin-git/Kumiho/releases/tag/v0.14.0"
        const val APK_URL =
            "https://github.com/benyamin-git/Kumiho/releases/download/v0.14.0/$APK_NAME"
        const val SUMS_URL =
            "https://github.com/benyamin-git/Kumiho/releases/download/v0.14.0/$SUMS_NAME"
        val APK_BYTES = "verified apk bytes".toByteArray()

        fun sha256Of(bytes: ByteArray): String =
            MessageDigest.getInstance("SHA-256").digest(bytes)
                .joinToString("") { "%02x".format(it.toInt() and 0xff) }
    }
}
