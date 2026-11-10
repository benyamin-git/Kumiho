package dev.benyamin.kumiho.widget

import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.view.View
import android.widget.RemoteViews
import androidx.compose.ui.graphics.toArgb
import dev.benyamin.kumiho.KumihoApplication
import dev.benyamin.kumiho.MainActivity
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.data.PaletteState
import dev.benyamin.kumiho.permissions.PermissionController
import dev.benyamin.kumiho.ui.i18n.PersianFormat
import dev.benyamin.kumiho.ui.theme.KumihoColors
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.vpn.VpnStatusCache

/**
 * Paints both home-screen widgets from the same process-local snapshot the
 * Quick Settings tile reads. Nothing renders before the palette is ready, so a
 * widget never flashes a guessed theme.
 */
object WidgetRefresh {
    private const val TOGGLE_REQUEST = 100

    /** Re-renders every placed widget; a no-op when none exist. */
    fun request(context: Context) {
        val app = context.applicationContext
        val manager = AppWidgetManager.getInstance(app)
        render(app, manager, toggleIds(app, manager), WidgetSize.TOGGLE_1X1)
        render(app, manager, statusIds(app, manager), WidgetSize.STATUS_2X1)
    }

    /** Provider entry point for one widget shape. */
    fun render(context: Context, manager: AppWidgetManager, ids: IntArray, size: WidgetSize) {
        if (ids.isEmpty()) return
        val app = context.applicationContext
        val container = (app as? KumihoApplication)?.container ?: return
        val colors = (container.themeRepository.state.value as? PaletteState.Ready)?.colors ?: return
        val localized = container.languageRepository.localizedContext(context)
        val view = WidgetState.reduce(
            VpnStatusCache.current(),
            consentGranted = PermissionController.vpnPrepareIntent(app) == null,
        )
        for (id in ids) {
            val remote = when (size) {
                WidgetSize.TOGGLE_1X1 -> toggle(localized, view, colors)
                WidgetSize.STATUS_2X1 -> status(localized, view, colors)
            }
            manager.updateAppWidget(id, remote)
        }
    }

    /** OLED fills with the ladder's true black; Light and Dark keep the card step. */
    private fun widgetBackground(colors: KumihoColors): Int =
        (if (colors.theme == ThemeId.OLED) colors.surfaceContainerLowest else colors.surfaceContainer)
            .toArgb()

    private fun toggleIds(context: Context, manager: AppWidgetManager): IntArray =
        manager.getAppWidgetIds(ComponentName(context, KumihoToggleWidgetProvider::class.java))

    private fun statusIds(context: Context, manager: AppWidgetManager): IntArray =
        manager.getAppWidgetIds(ComponentName(context, KumihoStatusWidgetProvider::class.java))

    private fun toggle(
        context: Context,
        view: WidgetPresentation,
        colors: KumihoColors,
    ): RemoteViews {
        val remote = RemoteViews(context.packageName, R.layout.widget_toggle)
        remote.setInt(R.id.widget_root, "setBackgroundColor", widgetBackground(colors))
        remote.setImageViewResource(
            R.id.widget_icon,
            if (view.active) R.drawable.ic_widget_on else R.drawable.ic_widget_off,
        )
        remote.setInt(
            R.id.widget_icon,
            "setColorFilter",
            (if (view.active) colors.primary else colors.onSurface).toArgb(),
        )
        remote.setContentDescription(R.id.widget_root, description(context, view))
        remote.setOnClickPendingIntent(R.id.widget_root, toggleIntent(context))
        return remote
    }

    private fun status(
        context: Context,
        view: WidgetPresentation,
        colors: KumihoColors,
    ): RemoteViews {
        val remote = RemoteViews(context.packageName, R.layout.widget_status)
        remote.setInt(R.id.widget_root, "setBackgroundColor", widgetBackground(colors))
        remote.setImageViewResource(
            R.id.widget_icon,
            if (view.active) R.drawable.ic_widget_on else R.drawable.ic_widget_off,
        )
        remote.setInt(
            R.id.widget_icon,
            "setColorFilter",
            (if (view.active) colors.primary else colors.onSurface).toArgb(),
        )
        remote.setContentDescription(R.id.widget_toggle_target, description(context, view))
        remote.setTextColor(R.id.widget_line1, colors.onSurface.toArgb())
        remote.setTextColor(R.id.widget_line2, colors.onSurface.toArgb())
        remote.setTextViewText(R.id.widget_line1, stateWord(context, view))
        val detail = detailLine(context, view)
        remote.setTextViewText(R.id.widget_line2, detail)
        remote.setViewVisibility(R.id.widget_line2, if (detail.isEmpty()) View.GONE else View.VISIBLE)
        remote.setOnClickPendingIntent(R.id.widget_toggle_target, toggleIntent(context))
        remote.setOnClickPendingIntent(
            R.id.widget_text,
            PendingIntent.getActivity(
                context,
                TOGGLE_REQUEST + 1,
                Intent(context, MainActivity::class.java),
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
            ),
        )
        return remote
    }

    private fun toggleIntent(context: Context): PendingIntent = PendingIntent.getActivity(
        context,
        TOGGLE_REQUEST,
        Intent(context, WidgetConnectActivity::class.java),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    )

    private fun stateWord(context: Context, view: WidgetPresentation): String =
        context.getString(
            when (view.label) {
                WidgetLabel.NOT_CONNECTED -> R.string.tile_subtitle_off
                WidgetLabel.CONNECTED -> R.string.tile_subtitle_connected
                WidgetLabel.CONNECTING -> R.string.tile_subtitle_connecting
                WidgetLabel.PROXY_ONLY -> R.string.tile_subtitle_proxy_only
                WidgetLabel.RECONNECTING -> R.string.tile_subtitle_reconnecting
                WidgetLabel.WAITING -> R.string.tile_subtitle_waiting
                WidgetLabel.SIGN_IN_REQUIRED -> R.string.tile_subtitle_sign_in
                WidgetLabel.PERMISSION_NEEDED -> R.string.tile_subtitle_permission
                WidgetLabel.UNAVAILABLE -> R.string.tile_subtitle_unavailable
                WidgetLabel.ERROR -> R.string.tile_subtitle_error
            },
        )

    private fun description(context: Context, view: WidgetPresentation): String =
        if (view.label == WidgetLabel.CONNECTED && view.location != null) {
            context.getString(R.string.tile_subtitle_connected_location, view.location)
        } else {
            stateWord(context, view)
        }

    private fun detailLine(context: Context, view: WidgetPresentation): String {
        val parts = mutableListOf<String>()
        view.location?.let(parts::add)
        when (val quota = view.quota) {
            is WidgetQuota.Percent -> {
                val percent = if (context.resources.configuration.locales[0].language == "fa") {
                    PersianFormat.formatNumber(quota.value.toLong())
                } else {
                    quota.value.toString()
                }
                parts.add(context.getString(R.string.account_row_quota_left, percent))
            }
            WidgetQuota.Unknown -> parts.add(context.getString(R.string.account_row_quota_unknown))
            WidgetQuota.Hidden -> Unit
        }
        return parts.joinToString(" · ")
    }
}
