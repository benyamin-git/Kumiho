package dev.benyamin.kumiho.data

import android.content.Context
import android.content.pm.ApplicationInfo
import android.content.pm.PackageManager
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * One installed application the split-tunnel picker can show.
 *
 * [isSelf] marks Kumiho itself: the picker omits it from Include and shows it
 * locked in Exclude/Off, because Kumiho must always bypass its own tunnel.
 * [launchable] is whether the app has a launcher entry; the default picker
 * list is launchable apps and the "System apps" toggle reveals the rest.
 */
data class InstalledApp(
    val packageName: String,
    val label: String,
    val isSystem: Boolean,
    val launchable: Boolean,
    val isSelf: Boolean = false,
)

/** The app-list seam, so the split view model is testable without Android. */
interface InstalledAppsSource {
    suspend fun load(): List<InstalledApp>
}

/**
 * Reads the installed-app list off the main thread. The manifest declares
 * `QUERY_ALL_PACKAGES`, so this sees every app, including packages with no
 * intent filters; the app is sideload-only, so Play policy is not a constraint.
 */
class InstalledAppsRepository(context: Context) : InstalledAppsSource {
    private val appContext = context.applicationContext
    private val ownPackage: String = appContext.packageName

    override suspend fun load(): List<InstalledApp> = withContext(Dispatchers.IO) {
        val manager = appContext.packageManager
        installedPackages(manager).mapNotNull { info ->
            val application = info.applicationInfo ?: return@mapNotNull null
            val packageName = info.packageName
            val label = runCatching {
                manager.getApplicationLabel(application).toString()
            }.getOrDefault(packageName)
            InstalledApp(
                packageName = packageName,
                label = label.ifBlank { packageName },
                isSystem = application.flags and ApplicationInfo.FLAG_SYSTEM != 0,
                launchable = runCatching {
                    manager.getLaunchIntentForPackage(packageName) != null
                }.getOrDefault(false),
                isSelf = packageName == ownPackage,
            )
        }.sortedWith(compareBy({ it.label.lowercase() }, { it.packageName }))
    }

    /** The installed package names, for dropping uninstalled split selections. */
    suspend fun installedPackageNames(): Set<String> = withContext(Dispatchers.IO) {
        installedPackages(appContext.packageManager).mapTo(HashSet()) { it.packageName }
    }

    @Suppress("DEPRECATION") // getInstalledPackages(0) is the API 26-compatible form.
    private fun installedPackages(manager: PackageManager) = try {
        manager.getInstalledPackages(0)
    } catch (_: RuntimeException) {
        emptyList()
    }
}

/**
 * The pure picker filter: search by label or package (case-insensitive), the
 * default launchable list, the "System apps" reveal and a deterministic
 * alphabetical order. Kumiho itself is never part of the selectable list in
 * any mode; the screen shows it as its own locked row (Off/Exclude) or omits
 * it (Include), so a duplicate selectable Kumiho row can never appear.
 */
object InstalledAppsFilter {
    fun filter(
        apps: List<InstalledApp>,
        query: String,
        showSystem: Boolean,
    ): List<InstalledApp> {
        val needle = query.trim().lowercase()
        return apps.asSequence()
            .filterNot { it.isSelf }
            .filter { showSystem || it.launchable }
            .filter { app ->
                needle.isEmpty() ||
                    app.label.lowercase().contains(needle) ||
                    app.packageName.lowercase().contains(needle)
            }
            .sortedWith(compareBy({ it.label.lowercase() }, { it.packageName }))
            .toList()
    }
}
