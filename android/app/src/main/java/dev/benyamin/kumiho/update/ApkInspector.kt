package dev.benyamin.kumiho.update

import android.content.Context
import android.os.Build
import java.io.File

/** The three APK facts the updater checks before handing the file to Android. */
data class ApkInfo(
    val packageName: String,
    val versionName: String,
    val versionCode: Long,
)

/** Reads an APK's manifest without installing it. */
interface ApkInspector {
    fun inspect(file: File): ApkInfo?
}

/** Production inspector over [android.content.pm.PackageManager]. */
class AndroidApkInspector(private val context: Context) : ApkInspector {
    @Suppress("DEPRECATION")
    override fun inspect(file: File): ApkInfo? {
        val info = context.packageManager.getPackageArchiveInfo(file.path, 0) ?: return null
        val code = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            info.longVersionCode
        } else {
            info.versionCode.toLong()
        }
        return ApkInfo(
            packageName = info.packageName,
            versionName = info.versionName.orEmpty(),
            versionCode = code,
        )
    }
}
