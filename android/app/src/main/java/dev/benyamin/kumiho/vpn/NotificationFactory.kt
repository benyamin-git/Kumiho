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

/**
 * The basic foreground and sign-in notifications. T16 replaces the body with
 * location/uptime/traffic and adds the tile; T9 only needs a truthful state
 * word plus Disconnect and Open.
 */
class NotificationFactory(private val context: Context) {
    /** Creates the low-importance status channel; safe to call repeatedly. */
    fun ensureChannel() {
        val manager = context.getSystemService(NotificationManager::class.java) ?: return
        if (manager.getNotificationChannel(CHANNEL_ID) != null) return
        val channel = NotificationChannel(
            CHANNEL_ID,
            context.getString(R.string.notification_channel_status),
            NotificationManager.IMPORTANCE_LOW,
        ).apply {
            description = context.getString(R.string.notification_channel_status_description)
            setShowBadge(false)
        }
        manager.createNotificationChannel(channel)
    }

    /** The persistent foreground notification for [state]. */
    fun build(state: VpnUiState): Notification {
        ensureChannel()
        return NotificationCompat.Builder(context, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_power)
            .setContentTitle(context.getString(R.string.notification_title))
            .setContentText(context.getString(stateText(state)))
            .setContentIntent(openIntent())
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setShowWhen(false)
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
    fun update(id: Int, state: VpnUiState) {
        try {
            NotificationManagerCompat.from(context).notify(id, build(state))
        } catch (_: SecurityException) {
            // POST_NOTIFICATIONS was revoked; the service still runs.
        }
    }

    /** The one-shot "Sign in to Kumiho" notification for the signed-out path. */
    fun postSignIn() {
        ensureChannel()
        val notification = NotificationCompat.Builder(context, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_power)
            .setContentTitle(context.getString(R.string.notification_sign_in_title))
            .setContentText(context.getString(R.string.notification_sign_in_text))
            .setContentIntent(openIntent())
            .setAutoCancel(true)
            .setOnlyAlertOnce(true)
            .setCategory(NotificationCompat.CATEGORY_STATUS)
            .setPriority(NotificationCompat.PRIORITY_DEFAULT)
            .build()
        try {
            NotificationManagerCompat.from(context).notify(SIGN_IN_ID, notification)
        } catch (_: SecurityException) {
            // Denied notifications are surfaced by setup/diagnostics, not here.
        }
    }

    /** Removes the sign-in prompt after a successful sign-in/connect. */
    fun cancelSignIn() {
        NotificationManagerCompat.from(context).cancel(SIGN_IN_ID)
    }

    private fun stateText(state: VpnUiState): Int = when (state) {
        VpnUiState.OFF -> R.string.notification_state_off
        VpnUiState.CONNECTED -> R.string.notification_state_connected
        VpnUiState.PROXY_ONLY -> R.string.notification_state_proxy_only
        VpnUiState.CONNECTING -> R.string.notification_state_connecting
        VpnUiState.RECONNECTING -> R.string.notification_state_reconnecting
        VpnUiState.WAITING -> R.string.notification_state_waiting
        VpnUiState.NOT_SIGNED_IN -> R.string.notification_state_not_signed_in
        VpnUiState.ERROR -> R.string.notification_state_error
        VpnUiState.SERVICE_UNAVAILABLE -> R.string.notification_state_unavailable
        VpnUiState.PERMISSION_NEEDED -> R.string.notification_state_permission_needed
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

        const val CHANNEL_ID = "connection"

        private const val REQUEST_OPEN = 10
        private const val REQUEST_DISCONNECT = 11
    }
}
