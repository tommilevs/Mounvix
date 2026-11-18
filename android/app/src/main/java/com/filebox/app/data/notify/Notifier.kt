package com.mounvix.app.data.notify

import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import com.mounvix.app.R

/** Локальные уведомления (передачи/автозагрузка). Безопасно, если разрешение не выдано. */
object Notifier {

    private const val CHANNEL = "mounvix_transfers"

    fun show(ctx: Context, title: String, text: String, id: Int = 1001) {
        if (Build.VERSION.SDK_INT >= 33 &&
            androidx.core.content.ContextCompat.checkSelfPermission(ctx, android.Manifest.permission.POST_NOTIFICATIONS) !=
                android.content.pm.PackageManager.PERMISSION_GRANTED
        ) return
        try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                val nm = ctx.getSystemService(NotificationManager::class.java)
                if (nm != null && nm.getNotificationChannel(CHANNEL) == null) {
                    nm.createNotificationChannel(
                        NotificationChannel(CHANNEL, "Передачи", NotificationManager.IMPORTANCE_DEFAULT)
                    )
                }
            }
            val n = NotificationCompat.Builder(ctx, CHANNEL)
                .setSmallIcon(R.drawable.ic_notify)
                .setContentTitle(title)
                .setContentText(text)
                .setAutoCancel(true)
                .setOnlyAlertOnce(true)
                .build()
            NotificationManagerCompat.from(ctx).notify(id, n)
        } catch (_: Exception) {
            // Нет разрешения POST_NOTIFICATIONS или уведомления отключены — молча игнорируем.
        }
    }
}
