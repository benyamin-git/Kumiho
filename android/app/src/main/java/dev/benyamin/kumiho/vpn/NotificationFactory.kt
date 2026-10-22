package dev.benyamin.kumiho.vpn

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import dev.benyamin.kumiho.MainActivity
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.engine.EngineStatus

/**
 * The foreground status notification (T9/T16) and the separate alert channel.
 *
 * The status channel is low-importance, silent, ongoing and updated in place:
 * state, location, uptime and up/down totals, with Disconnect and Open and
 * deliberately no Connect action (D30). Sign-in prompts and background failures
 * use the default-importance alert channel so they are noticeable once, without
 * turning the persistent status into an alert.
 */
class NotificationFactory(private val context: Context) {
    /** Creates both channels; safe to call repeatedly. */
    fun ensureChannels() {
        ensureStatusChannel()
        ensureAlertChannel()
    }

    private fun ensureStatusChannel() {
        val manager = context.getSystemService(NotificationManager::class.java) ?: return
        if (manager.getNotificationChannel(CHANNEL_ID) != null) return
        val channel = NotificationChannel(
            CHANNEL_ID,
            context.getString(R.string.notification_channel_status),
            STATUS_IMPORTANCE,
        ).apply {
            description = context.getString(R.string.notification_channel_status_description)
            setShowBadge(false)
            setSound(null, null)
            enableVibration(false)
        }
        manager.createNotificationChannel(channel)
    }

    private fun ensureAlertChannel() {
        val manager = context.getSystemService(NotificationManager::class.java) ?: return
        if (manager.getNotificationChannel(ALERT_CHANNEL_ID) != null) return
        val channel = NotificationChannel(
            ALERT_CHANNEL_ID,
            context.getString(R.string.notification_channel_alerts),
            ALERT_IMPORTANCE,
        ).apply {
            description = context.getString(R.string.notification_channel_alerts_description)
            setShowBadge(false)
        }
        manager.createNotificationChannel(channel)
    }

    /** The persistent foreground notification for [state]. */
    fun build(
        state: VpnUiState,
        status: EngineStatus? = null,
        nowSeconds: Long = System.currentTimeMillis() / 1000L,
    ): Notification {
        ensureChannels()
        val content = NotificationText.content(state, status, nowSeconds)
        val stateWord = context.getString(content.stateRes)
        val headline = content.location?.let {
            context.getString(R.string.notification_with_location, stateWord, it)
        } ?: stateWord
        val details = buildList {
            content.uptime?.let { add(context.getString(R.string.notification_uptime, it)) }
            if (content.up != null && content.down != null) {
                add(context.getString(R.string.notification_traffic, content.up, content.down))
            }
        }
        val expanded = (listOf(headline) + details).joinToString("\n")
        return NotificationCompat.Builder(context, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_power)
            .setContentTitle(context.getString(R.string.notification_title))
            .setContentText(headline)
            .setSubText(details.firstOrNull())
            .setStyle(NotificationCompat.BigTextStyle().bigText(expanded))
            .setContentIntent(openIntent())
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setShowWhen(false)
            .setSilent(true)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .addAction(
                R.drawable.ic_power,
                context.getString(R.string.notification_action_disconnect),
                disconnectIntent(),
            )
            .addAction(
                R.drawable.ic_power,
                context.getString(R.string.notification_action_open),
                openIntent(),
            )
            .build()
    }

    /** Updates the foreground notification; harmless when notifications are off. */
    fun update(id: Int, state: VpnUiState, status: EngineStatus?) {
        try {
            NotificationManagerCompat.from(context).notify(id, build(state, status))
        } catch (_: SecurityException) {
            // POST_NOTIFICATIONS was revoked; the service still runs.
        }
    }

    /** The one-shot "Sign in to Kumiho" alert for the signed-out path. */
    fun postSignIn() {
        ensureChannels()
        val notification = NotificationCompat.Builder(context, ALERT_CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_power)
            .setContentTitle(context.getString(R.string.notification_sign_in_title))
            .setContentText(context.getString(R.string.notification_sign_in_text))
            .setContentIntent(openIntent())
            .setAutoCancel(true)
            .setOnlyAlertOnce(true)
            .setCategory(NotificationCompat.CATEGORY_STATUS)
            .setPriority(NotificationCompat.PRIORITY_DEFAULT)
            .build()
        notify(SIGN_IN_ID, notification)
    }

    /**
     * One alert for a background failure the service itself detected (a failed
     * connect or engine start). It never replaces the status notification and
     * never repeats for the same failure because it is posted once per event.
     */
    fun postError(detail: String? = null) {
        ensureChannels()
        val notification = NotificationCompat.Builder(context, ALERT_CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_power)
            .setContentTitle(context.getString(R.string.notification_error_title))
            .setContentText(context.getString(R.string.notification_error_text))
            .apply {
                val reason = detail?.trim().orEmpty()
                if (reason.isNotEmpty()) {
                    setStyle(NotificationCompat.BigTextStyle().bigText(reason))
                    setSubText(reason)
                }
            }
            .setContentIntent(openIntent())
            .setAutoCancel(true)
            .setOnlyAlertOnce(true)
            .setCategory(NotificationCompat.CATEGORY_ERROR)
            .setPriority(NotificationCompat.PRIORITY_DEFAULT)
            .build()
        notify(ERROR_ID, notification)
    }

    /** Removes the sign-in prompt after a successful sign-in/connect. */
    fun cancelSignIn() {
        NotificationManagerCompat.from(context).cancel(SIGN_IN_ID)
    }

    /** Removes a stale failure alert after a successful connect. */
    fun cancelError() {
        NotificationManagerCompat.from(context).cancel(ERROR_ID)
    }

    private fun notify(id: Int, notification: Notification) {
        try {
            NotificationManagerCompat.from(context).notify(id, notification)
        } catch (_: SecurityException) {
            // Denied notifications are surfaced by setup/diagnostics, not here.
        }
    }

    private fun openIntent(): PendingIntent = PendingIntent.getActivity(
        context,
        REQUEST_OPEN,
        Intent(context, MainActivity::class.java).apply {
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)
        },
        PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
    )

    private fun disconnectIntent(): PendingIntent = PendingIntent.getService(
        context,
        REQUEST_DISCONNECT,
        Intent(context, KumihoVpnService::class.java)
            .setAction(KumihoVpnService.ACTION_DISCONNECT),
        PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
    )

    companion object {
        /** The foreground notification id; T16 keeps this id stable. */
        const val FOREGROUND_ID = 1

        /** The separate sign-in prompt, never a foreground notification. */
        const val SIGN_IN_ID = 2

        /** A background connect/start failure, never a foreground notification. */
        const val ERROR_ID = 3

        /** The persistent status channel; the id is stable across releases. */
        const val CHANNEL_ID = "connection"

        /** Sign-in and background-error alerts, default importance. */
        const val ALERT_CHANNEL_ID = "alerts"

        const val STATUS_IMPORTANCE = NotificationManager.IMPORTANCE_LOW
        const val ALERT_IMPORTANCE = NotificationManager.IMPORTANCE_DEFAULT

        private const val REQUEST_OPEN = 10
        private const val REQUEST_DISCONNECT = 11
    }
}
