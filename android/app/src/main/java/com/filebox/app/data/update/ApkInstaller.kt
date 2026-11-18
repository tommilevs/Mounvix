package com.mounvix.app.data.update

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import android.widget.Toast
import java.io.File

/** Установка APK через PackageInstaller (с системным подтверждением). */
class ApkInstaller {

    fun install(ctx: Context, apk: File): Boolean {
        if (!apk.exists() || apk.length() <= 0) return false
        val installer = ctx.packageManager.packageInstaller
        val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL)
        val sessionId = installer.createSession(params)
        installer.openSession(sessionId).use { session ->
            apk.inputStream().use { input ->
                session.openWrite("mounvix-update.apk", 0, apk.length()).use { output ->
                    input.copyTo(output)
                }
            }
            val status = Intent(ctx, InstallResultReceiver::class.java)
                .putExtra(PackageInstaller.EXTRA_SESSION_ID, sessionId)
            val pi = PendingIntent.getBroadcast(
                ctx, sessionId, status,
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_MUTABLE
            )
            session.commit(pi.intentSender)
        }
        return true
    }
}

/** Получает результат установки и запускает системный диалог подтверждения. */
class InstallResultReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, intent: Intent) {
        val status = intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)
        when (status) {
            PackageInstaller.STATUS_PENDING_USER_ACTION -> {
                @Suppress("DEPRECATION")
                val confirm = intent.getParcelableExtra<Intent>(Intent.EXTRA_INTENT)
                if (confirm != null) {
                    confirm.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                    ctx.startActivity(confirm)
                }
            }
            PackageInstaller.STATUS_SUCCESS ->
                Toast.makeText(ctx, "Mounvix обновлён", Toast.LENGTH_LONG).show()
            else -> {
                val msg = intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE)
                    ?: "код $status"
                Toast.makeText(ctx, "Обновление не установлено: $msg", Toast.LENGTH_LONG).show()
            }
        }
    }
}
