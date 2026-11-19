package com.mounvix.app.data.autoupload

import android.content.SharedPreferences
import android.net.Uri
import com.mounvix.app.data.source.SftpServer
import com.mounvix.app.data.source.SftpStorageSource
import com.mounvix.app.data.source.SmbServer
import com.mounvix.app.data.source.SmbStorageSource
import com.mounvix.app.data.source.StorageSource
import com.mounvix.app.data.source.WebDavServer
import com.mounvix.app.data.source.WebDavStorageSource
import java.util.Locale

data class LegacyReceiverIdentity(
    val adapter: String,
    val rootIdentity: String,
    val accountId: String,
    val destination: String
) {
    val destinationKey: String
        get() = AutouploadIdentity.destinationKey(adapter, rootIdentity, accountId, destination)
}

/** Пересоздаёт удалённый источник по сохранённым настройкам (для фонового воркера). */
object RemoteSourceFactory {

    fun fromPrefs(prefs: SharedPreferences): StorageSource? {
        prefs.getString("webdav_url", null)?.let { url ->
            return WebDavStorageSource(
                WebDavServer(
                    baseUrl = url,
                    user = prefs.getString("webdav_user", "") ?: "",
                    password = prefs.getString("webdav_pass", "") ?: "",
                    trustAll = prefs.getBoolean("webdav_trust", true),
                )
            )
        }
        prefs.getString("smb_host", null)?.let { host ->
            return SmbStorageSource(
                SmbServer(
                    host = host,
                    share = prefs.getString("smb_share", "all") ?: "all",
                    domain = prefs.getString("smb_domain", "") ?: "",
                    user = prefs.getString("smb_user", "") ?: "",
                    password = prefs.getString("smb_pass", "") ?: "",
                )
            )
        }
        prefs.getString("sftp_host", null)?.let { host ->
            return SftpStorageSource(
                SftpServer(
                    host = host,
                    port = prefs.getInt("sftp_port", 22),
                    user = prefs.getString("sftp_user", "") ?: "",
                    password = prefs.getString("sftp_pass", "") ?: "",
                    basePath = prefs.getString("sftp_base", "/") ?: "/",
                )
            )
        }
        return null
    }

    /** Reconstructs only non-secret destination identity; construction performs no network I/O. */
    fun legacyReceiverIdentity(prefs: SharedPreferences): LegacyReceiverIdentity? {
        val rawDestination = prefs.getString("autoupload_dest", "Mounvix") ?: "Mounvix"
        val destination = runCatching { AutouploadIdentity.normalizeDestination(rawDestination) }
            .getOrNull() ?: return null
        val source = fromPrefs(prefs) ?: return null
        val (adapter, accountId) = when (source) {
            is WebDavStorageSource -> "webdav" to (prefs.getString("webdav_user", "") ?: "")
            is SmbStorageSource -> {
                val domain = prefs.getString("smb_domain", "") ?: ""
                val user = prefs.getString("smb_user", "") ?: ""
                "smb" to "$domain\\$user"
            }
            is SftpStorageSource -> "sftp" to (prefs.getString("sftp_user", "") ?: "")
            else -> return null
        }
        val rootIdentity = normalizeRootIdentity(source.rootId()) ?: return null
        return LegacyReceiverIdentity(adapter, rootIdentity, accountId, destination)
    }

    private fun normalizeRootIdentity(rawRoot: String?): String? {
        if (rawRoot.isNullOrBlank()) return null
        val uri = runCatching { Uri.parse(rawRoot) }.getOrNull() ?: return null
        val scheme = uri.scheme?.lowercase(Locale.ROOT) ?: return null
        val rawHost = uri.host?.lowercase(Locale.ROOT) ?: return null
        val host = rawHost.removePrefix("[").removeSuffix("]")
        val authorityHost = if (':' in host) "[$host]" else host
        val port = if (uri.port >= 0) ":${uri.port}" else ""
        val path = uri.encodedPath.orEmpty().trimEnd('/')
        return "$scheme://$authorityHost$port$path"
    }
}
