package dev.benyamin.kumiho.widget

import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.Context

/** The 2x1 state card. */
class KumihoStatusWidgetProvider : AppWidgetProvider() {
    override fun onUpdate(context: Context, appWidgetManager: AppWidgetManager, appWidgetIds: IntArray) {
        WidgetRefresh.render(context, appWidgetManager, appWidgetIds, WidgetSize.STATUS_2X1)
    }
}
