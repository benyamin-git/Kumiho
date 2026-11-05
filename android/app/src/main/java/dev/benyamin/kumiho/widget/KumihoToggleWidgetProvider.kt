package dev.benyamin.kumiho.widget

import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.Context

/** The 1x1 icon-only toggle. */
class KumihoToggleWidgetProvider : AppWidgetProvider() {
    override fun onUpdate(context: Context, appWidgetManager: AppWidgetManager, appWidgetIds: IntArray) {
        WidgetRefresh.render(context, appWidgetManager, appWidgetIds, WidgetSize.TOGGLE_1X1)
    }
}
