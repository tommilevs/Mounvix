package com.mounvix.app.data.autoupload

import android.content.SharedPreferences
import com.mounvix.app.data.source.SftpServer
import com.mounvix.app.data.source.SftpStorageSource
import com.mounvix.app.data.source.SmbServer
import com.mounvix.app.data.source.SmbStorageSource
import com.mounvix.app.data.source.StorageSource
import com.mounvix.app.data.source.WebDavServer
import com.mounvix.app.data.source.WebDavStorageSource

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
}
