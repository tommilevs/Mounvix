package com.mounvix.app.data.autoupload

import android.content.SharedPreferences
import android.net.Uri
import com.mounvix.app.data.source.RemoteCredentialKeys
import com.mounvix.app.data.source.RemoteCredentialRepository
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

    fun fromPrefs(prefs: SharedPreferences, credentials: RemoteCredentialRepository): StorageSource? {
        prefs.getString("webdav_url", null)?.let { url ->
            val user = prefs.getString("webdav_user", "") ?: ""
            val password = credentials.read(RemoteCredentialKeys.webDav(url, user), "webdav_pass").getOrThrow()
            return WebDavStorageSource(
                WebDavServer(
                    baseUrl = url,
                    user = user,
                    password = password,
                    trustAll = prefs.getBoolean("webdav_trust", true),
                )
            )
        }
        prefs.getString("smb_host", null)?.let { host ->
            val share = prefs.getString("smb_share", "all") ?: "all"
            val domain = prefs.getString("smb_domain", "") ?: ""
            val user = prefs.getString("smb_user", "") ?: ""
            val password = credentials.read(
                RemoteCredentialKeys.smb(host, share, domain, user),
                "smb_pass",
            ).getOrThrow()
            return SmbStorageSource(
                SmbServer(
                    host = host,
                    share = share,
                    domain = domain,
                    user = user,
                    password = password,
                )
            )
        }
        prefs.getString("sftp_host", null)?.let { host ->
            val port = prefs.getInt("sftp_port", 22)
            val basePath = prefs.getString("sftp_base", "/") ?: "/"
            val user = prefs.getString("sftp_user", "") ?: ""
            val password = credentials.read(
                RemoteCredentialKeys.sftp(host, port, basePath, user),
                "sftp_pass",
            ).getOrThrow()
            return SftpStorageSource(
                SftpServer(
                    host = host,
                    port = port,
                    user = user,
                    password = password,
                    basePath = basePath,
                    hostKeyFingerprint = prefs.getString("sftp_host_key_fingerprint", "") ?: "",
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
        val (source, adapter, accountId) = when (val url = prefs.getString("webdav_url", null)) {
            null -> when (val host = prefs.getString("smb_host", null)) {
                null -> {
                    val sftpHost = prefs.getString("sftp_host", null) ?: return null
                    val port = prefs.getInt("sftp_port", 22)
                    val user = prefs.getString("sftp_user", "") ?: ""
                    val source = SftpStorageSource(
                        SftpServer(
                            host = sftpHost,
                            port = port,
                            user = user,
                            basePath = prefs.getString("sftp_base", "/") ?: "/",
                            hostKeyFingerprint = prefs.getString("sftp_host_key_fingerprint", "") ?: "",
                        )
                    )
                    Triple(source, "sftp", user)
                }
                else -> {
                    val share = prefs.getString("smb_share", "all") ?: "all"
                    val domain = prefs.getString("smb_domain", "") ?: ""
                    val user = prefs.getString("smb_user", "") ?: ""
                    val source = SmbStorageSource(
                        SmbServer(host = host, share = share, domain = domain, user = user, password = "")
                    )
                    Triple(source, "smb", "$domain\\$user")
                }
            }
            else -> {
                val user = prefs.getString("webdav_user", "") ?: ""
                val source = WebDavStorageSource(
                    WebDavServer(
                        baseUrl = url,
                        user = user,
                        password = "",
                        trustAll = prefs.getBoolean("webdav_trust", true),
                    )
                )
                Triple(source, "webdav", user)
            }
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
