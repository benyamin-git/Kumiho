package dev.benyamin.kumiho.tv

import android.content.Context
import android.content.pm.PackageManager
import android.content.res.Configuration

/**
 * Answers the one question the UI root needs: is this device a television?
 *
 * The check is read once per activity start. A device that advertises the
 * Leanback feature is treated as a TV even when its current UI mode is not
 * television, because Android TV launchers require that feature.
 */
object TvEnvironment {
    fun isTelevision(context: Context): Boolean = isTelevision(
        uiMode = context.resources.configuration.uiMode,
        hasLeanback = context.packageManager.hasSystemFeature(PackageManager.FEATURE_LEANBACK),
    )

    fun isTelevision(uiMode: Int, hasLeanback: Boolean): Boolean {
        val type = uiMode and Configuration.UI_MODE_TYPE_MASK
        return type == Configuration.UI_MODE_TYPE_TELEVISION || hasLeanback
    }
}
