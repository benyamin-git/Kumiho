package dev.benyamin.kumiho.update

import dev.benyamin.kumiho.ui.settings.UpdateChecker
import dev.benyamin.kumiho.ui.settings.UpdateFailure
import dev.benyamin.kumiho.ui.settings.UpdateResult
import dev.benyamin.kumiho.ui.settings.UpdateState
import java.io.File
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

/**
 * App-scoped self-update orchestration: one launch check at most every 24 h,
 * the Settings check, the consented download, checksum and APK verification,
 * and the hand-off to Android's installer. The system installer owns the final
 * confirmation; Kumiho never installs silently.
 */
class UpdateManager(
    private val checker: UpdateChecker,
    private val downloader: UpdateDownloader,
    private val inspector: ApkInspector,
    private val preferences: UpdatePreferences,
    private val cacheDir: File,
    private val currentVersion: String,
    private val currentVersionCode: Long,
    private val packageName: String,
    private val selfUpdateAllowed: Boolean,
    private val scope: CoroutineScope,
    private val now: () -> Long = System::currentTimeMillis,
) {
    private val mutableState = MutableStateFlow<UpdateState>(UpdateState.Idle)
    val state: StateFlow<UpdateState> = mutableState.asStateFlow()

    private val mutablePrompt = MutableStateFlow(false)
    val prompt: StateFlow<Boolean> = mutablePrompt.asStateFlow()

    @Volatile
    private var job: Job? = null

    @Volatile
    private var release: UpdateResult.Available? = null

    @Volatile
    private var downloaded: File? = null

    /** A launch check is skipped for debug builds, which cannot install the release APK. */
    fun checkOnLaunch() {
        if (!selfUpdateAllowed || job?.isActive == true) return
        job = scope.launch {
            if (mutableState.value !is UpdateState.Idle) return@launch
            val at = now()
            val last = preferences.lastUpdateCheck()
            if (last > 0 && at - last < CHECK_INTERVAL_MS) return@launch
            mutableState.value = UpdateState.Checking
            performCheck(manual = false, at = at)
        }
    }

    /** The Settings tap. It never opens the launch prompt by itself. */
    fun check() {
        if (job?.isActive == true || mutableState.value is UpdateState.Installing) return
        mutableState.value = UpdateState.Checking
        mutablePrompt.value = false
        job = scope.launch { performCheck(manual = true, at = now()) }
    }

    private suspend fun performCheck(manual: Boolean, at: Long) {
        val result = try {
            checker.check(currentVersion)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            UpdateResult.Failed(UpdateFailure.Network(error.message))
        }
        try {
            preferences.setLastUpdateCheck(at)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (_: Exception) {
            // The check itself succeeded; a failed timestamp only means the
            // next launch may check again sooner.
        }
        when (result) {
            UpdateResult.UpToDate -> {
                release = null
                mutableState.value = UpdateState.UpToDate
            }
            is UpdateResult.Available -> {
                release = result
                val installable = selfUpdateAllowed &&
                    result.apk != null && result.checksum != null
                mutableState.value = UpdateState.Available(
                    version = result.version,
                    url = result.url,
                    installable = installable,
                )
                if (!manual &&
                    installable &&
                    preferences.dismissedUpdateVersion() != result.version
                ) {
                    mutablePrompt.value = true
                }
            }
            is UpdateResult.Failed -> {
                release = null
                mutableState.value = UpdateState.Failed(result.failure)
            }
        }
    }

    /**
     * Downloads and verifies the selected APK. The system installer is opened
     * later by the UI, after the unknown-sources permission is present.
     */
    fun install() {
        val available = release ?: return
        if (!selfUpdateAllowed || available.apk == null || available.checksum == null) return
        val current = mutableState.value
        if (current is UpdateState.Installing) {
            mutablePrompt.value = true
            return
        }
        if (current is UpdateState.ReadyToInstall && readyFile() != null) {
            mutablePrompt.value = true
            return
        }
        if (job?.isActive == true) {
            mutablePrompt.value = true
            return
        }
        mutablePrompt.value = true
        mutableState.value = UpdateState.Downloading(available.version, 0)
        job = scope.launch {
            val apk = available.apk
            val checksum = available.checksum
            try {
                val sums = downloader.text(checksum.url)
                val expected = checksumFor(sums, apk.name)
                if (expected == null) {
                    installFailed(UpdateFailure.Checksum)
                    return@launch
                }
                val file = updateFile()
                downloader.download(apk.url, file) { progress ->
                    mutableState.value = UpdateState.Downloading(available.version, progress)
                }
                if (!sha256(file).equals(expected, ignoreCase = true)) {
                    file.delete()
                    installFailed(UpdateFailure.Checksum)
                    return@launch
                }
                if (!validApk(inspector.inspect(file), available.version)) {
                    file.delete()
                    installFailed(UpdateFailure.InvalidApk)
                    return@launch
                }
                downloaded = file
                mutableState.value = UpdateState.ReadyToInstall(available.version)
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                deleteDownload()
                installFailed(UpdateFailure.Download(error.message))
            }
        }
    }

    private fun installFailed(failure: UpdateFailure) {
        val available = release ?: return
        downloaded = null
        mutableState.value = UpdateState.Available(
            version = available.version,
            url = available.url,
            installable = true,
            installError = failure,
        )
    }

    private fun validApk(info: ApkInfo?, version: String): Boolean =
        info != null &&
            info.packageName == packageName &&
            info.versionName.removePrefix("v").removePrefix("V") == version &&
            info.versionCode > currentVersionCode

    /** The verified file, once [state] is [UpdateState.ReadyToInstall]. */
    fun readyFile(): File? = downloaded?.takeIf { it.isFile }

    /** Called after Android's installer activity was started. */
    fun onInstallerLaunched() {
        val current = mutableState.value
        if (current is UpdateState.ReadyToInstall) {
            mutableState.value = UpdateState.Installing(current.version)
        }
    }

    /**
     * Reads the installed version after the installer or settings page returns.
     * The package manager may have killed the process on success; this only
     * settles a still-running process.
     */
    fun onResume(installedVersion: String) {
        val current = mutableState.value
        if (current !is UpdateState.Installing) return
        mutableState.value = if (sameVersion(installedVersion, current.version)) {
            UpdateState.Installed(current.version)
        } else {
            UpdateState.ReadyToInstall(current.version)
        }
        mutablePrompt.value = true
    }

    /**
     * "Later" persists the version so a launch check does not ask again. During
     * a download it also cancels the transfer and drops the partial file.
     * Closing the installing dialog keeps the verified file so Settings can
     * offer the install again.
     */
    fun dismissPrompt() {
        val current = mutableState.value
        when (current) {
            is UpdateState.Available -> rememberDismissed(current.version)
            is UpdateState.ReadyToInstall -> rememberDismissed(current.version)
            is UpdateState.Downloading -> {
                job?.cancel()
                deleteDownload()
                rememberDismissed(current.version)
                mutableState.value = UpdateState.Available(
                    version = current.version,
                    url = release?.url ?: "",
                    installable = true,
                )
            }
            is UpdateState.Installing ->
                mutableState.value = UpdateState.ReadyToInstall(current.version)
            is UpdateState.Installed -> {
                mutableState.value = UpdateState.Idle
                downloaded = null
            }
            else -> Unit
        }
        mutablePrompt.value = false
    }

    private fun rememberDismissed(version: String) {
        scope.launch { preferences.setDismissedUpdateVersion(version) }
    }

    private fun updateFile(): File = File(cacheDir, UPDATE_APK_NAME)

    private fun deleteDownload() {
        updateFile().delete()
        downloaded = null
    }

    private fun sameVersion(a: String, b: String): Boolean =
        a.trim().removePrefix("v").removePrefix("V") ==
            b.trim().removePrefix("v").removePrefix("V")

    private companion object {
        const val CHECK_INTERVAL_MS = 24L * 60 * 60 * 1000
        const val UPDATE_APK_NAME = "Kumiho-update.apk"
    }
}
