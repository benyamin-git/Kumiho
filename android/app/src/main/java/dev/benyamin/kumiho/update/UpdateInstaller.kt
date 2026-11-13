package dev.benyamin.kumiho.update

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.provider.Settings
import androidx.core.content.FileProvider
import java.io.File

/** The small Android surface around the system package installer. */
object UpdateInstaller {

    /** Android 8+ requires the per-app unknown-sources grant for sideloaded APKs. */
    fun canInstall(context: Context): Boolean =
        context.packageManager.canRequestPackageInstalls()

    fun permissionIntent(context: Context): Intent =
        Intent(
            Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,
            Uri.parse("package:${context.packageName}"),
        )

    /**
     * Hands the verified APK to Android's installer. The installer owns the
     * final confirmation; ACTION_INSTALL_PACKAGE is the explicit path, and
     * ACTION_VIEW is the fallback on devices that only register the MIME type.
     */
    @Suppress("DEPRECATION")
    fun install(context: Context, apk: File) {
        val uri = FileProvider.getUriForFile(
            context,
            "${context.packageName}.fileprovider",
            apk,
        )
        val flags = Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK
        val direct = Intent(Intent.ACTION_INSTALL_PACKAGE)
            .setData(uri)
            .addFlags(flags)
        try {
            context.startActivity(direct)
            return
        } catch (_: ActivityNotFoundException) {
            // Fall through to the MIME-type installer.
        }
        val view = Intent(Intent.ACTION_VIEW)
            .setDataAndType(uri, "application/vnd.android.package-archive")
            .addFlags(flags)
        try {
            context.startActivity(view)
        } catch (error: ActivityNotFoundException) {
            throw IllegalStateException("no package installer", error)
        }
    }
}
