package com.mounvix.app.data.feed

import android.Manifest
import android.annotation.SuppressLint
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import com.mounvix.app.R

/** Posts only the event kind and safe summary; route data is carried only in the tap intent. */
object ServerEventNotifier {
    const val CHANNEL_ID = "mounvix_server_events"
    const val NOTIFICATION_ID = 0
    private const val CHANNEL_NAME = "Задания сервера"

    @SuppressLint("MissingPermission")
    fun show(context: Context, alert: ServerEventAlert): Boolean {
        if (!areNotificationsAllowed(context)) return false
        val appContext = context.applicationContext
        createChannel(appContext)
        val open = ServerEventNotificationIntentRouter.createIntent(
            appContext,
            alert.eventId,
            alert.targetRoute,
        )
        val pending = PendingIntent.getActivity(
            appContext,
            alert.eventId.hashCode(),
            open,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        return try {
            NotificationManagerCompat.from(appContext).notify(
                notificationTag(alert.eventId),
                NOTIFICATION_ID,
                NotificationCompat.Builder(appContext, CHANNEL_ID)
                    .setSmallIcon(R.drawable.ic_notify)
                    .setContentTitle(CHANNEL_NAME)
                    .setContentText(alert.summary)
                    .setSubText(alert.kind)
                    .setContentIntent(pending)
                    .setAutoCancel(true)
                    .setOnlyAlertOnce(true)
                    .build(),
            )
            true
        } catch (_: SecurityException) {
            false
        }
    }

    fun createChannel(context: Context) {
        context.applicationContext.getSystemService(NotificationManager::class.java).createNotificationChannel(
            NotificationChannel(CHANNEL_ID, CHANNEL_NAME, NotificationManager.IMPORTANCE_DEFAULT),
        )
    }

    fun areNotificationsAllowed(context: Context): Boolean {
        if (Build.VERSION.SDK_INT >= 33 && ContextCompat.checkSelfPermission(
                context,
                Manifest.permission.POST_NOTIFICATIONS,
            ) != PackageManager.PERMISSION_GRANTED
        ) return false
        return NotificationManagerCompat.from(context).areNotificationsEnabled()
    }

    fun notificationTag(eventId: String): String = "server-event:$eventId"
}
