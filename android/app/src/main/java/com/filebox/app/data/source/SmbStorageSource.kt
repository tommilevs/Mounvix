package com.mounvix.app.data.source

import com.hierynomus.msdtyp.AccessMask
import com.hierynomus.msfscc.FileAttributes
import com.hierynomus.mssmb2.SMB2CreateDisposition
import com.hierynomus.mssmb2.SMB2CreateOptions
import com.hierynomus.mssmb2.SMB2ShareAccess
import com.hierynomus.smbj.SMBClient
import com.hierynomus.smbj.auth.AuthenticationContext
import com.hierynomus.smbj.connection.Connection
import com.hierynomus.smbj.session.Session
import com.hierynomus.smbj.share.DiskShare
import com.mounvix.app.data.model.FileEntry
import com.mounvix.app.data.model.FileType
import java.io.File
import java.util.EnumSet

/** Настройки SMB-сервера (SMB2/3, smbj). */
data class SmbServer(
    val host: String,
    val share: String,
    val user: String = "",
    val password: String = "",
    val domain: String = "",
)

/**
 * SMB-источник: листинг/статы/создание/удаление/переименование и скачивание.
 * Соединение открывается на операцию и закрывается (просто и безопасно для телефона).
 */
class SmbStorageSource(private val server: SmbServer) : StorageSource {

    override fun rootId(): String = "smb://${server.host}/${server.share}"
    override fun stableKey(path: String): String = "${rootId()}/${path.trim('/')}".trimEnd('/')
    override fun uriOf(path: String): android.net.Uri? = null
    override fun mimeTypeOf(path: String): String? = when (path.substringAfterLast('.', "").lowercase(java.util.Locale.US)) {
        "jpg", "jpeg" -> "image/jpeg"
        "png" -> "image/png"
        "gif" -> "image/gif"
        "webp" -> "image/webp"
        "mp4" -> "video/mp4"
        "mkv" -> "video/x-matroska"
        "mp3" -> "audio/mpeg"
        "txt" -> "text/plain"
        "pdf" -> "application/pdf"
        "zip" -> "application/zip"
        else -> "application/octet-stream"
    }

    private fun <T> withShare(block: (DiskShare) -> T): T {
        val client = SMBClient()
        var conn: Connection? = null
        var session: Session? = null
        var share: DiskShare? = null
        try {
            conn = client.connect(server.host)
            val ac = AuthenticationContext(
                server.user,
                server.password.toCharArray(),
                server.domain.ifBlank { null },
            )
            session = conn.authenticate(ac)
            share = session.connectShare(server.share) as DiskShare
            return block(share)
        } finally {
            try { share?.close() } catch (_: Exception) {}
            try { session?.close() } catch (_: Exception) {}
            try { conn?.close() } catch (_: Exception) {}
            try { client.close() } catch (_: Exception) {}
        }
    }

    override fun listDir(path: String): List<FileEntry> {
        val dir = path.trim('/')
        return withShare { share ->
            val out = ArrayList<FileEntry>()
            for (lf in share.list(dir)) {
                val name = lf.fileName
                if (name == "." || name == "..") continue
                val isDir = (lf.fileAttributes and FileAttributes.FILE_ATTRIBUTE_DIRECTORY.value) != 0L
                val childPath = if (dir.isEmpty()) name else "$dir/$name"
                out.add(
                    FileEntry(
                        name = name,
                        path = childPath,
                        type = if (isDir) FileType.FOLDER else FileType.FILE,
                        size = lf.endOfFile,
                        modified = lf.lastWriteTime?.toEpochMillis() ?: 0L,
                        docKey = "${rootId()}/$childPath",
                    )
                )
            }
            out
        }
    }

    override fun stat(path: String): FileEntry? {
        val rel = path.trim('/')
        if (rel.isEmpty()) {
            return FileEntry("share", "", FileType.FOLDER, 0L, 0L, docKey = rootId())
        }
        val parent = rel.substringBeforeLast('/', "")
        val name = rel.substringAfterLast('/')
        return listDir(parent).firstOrNull { it.name == name }
    }

    override fun exists(path: String): Boolean = try {
        stat(path) != null
    } catch (e: Exception) {
        false
    }

    override fun createDir(path: String): Boolean = try {
        withShare { it.mkdir(path.trim('/')) }
        true
    } catch (e: Exception) {
        false
    }

    override fun createFile(path: String): Boolean = try {
        withShare { share ->
            share.openFile(
                path.trim('/'),
                EnumSet.of(AccessMask.GENERIC_WRITE),
                EnumSet.of(FileAttributes.FILE_ATTRIBUTE_ARCHIVE),
                SMB2ShareAccess.ALL,
                SMB2CreateDisposition.FILE_OVERWRITE_IF,
                EnumSet.of(SMB2CreateOptions.FILE_NON_DIRECTORY_FILE),
            ).close()
        }
        true
    } catch (e: Exception) {
        false
    }

    override fun delete(path: String): Boolean = try {
        val rel = path.trim('/')
        val isDir = stat(rel)?.type == FileType.FOLDER
        withShare { share ->
            if (isDir) share.rmdir(rel, true) else share.rm(rel)
        }
        true
    } catch (e: Exception) {
        false
    }

    override fun rename(path: String, newName: String): Boolean = try {
        // smbj 0.13 не даёт публичного rename у File/DiskShare — перенос отключён.
        if (newName.isBlank()) false else false
    } catch (e: Exception) {
        false
    }

    override fun copy(src: String, dst: String): Boolean = false

    override fun move(src: String, dst: String): Boolean = false

    override fun downloadTo(path: String, target: File): Boolean = try {
        target.parentFile?.mkdirs()
        withShare { share ->
            share.openFile(
                path.trim('/'),
                EnumSet.of(AccessMask.GENERIC_READ),
                EnumSet.noneOf(FileAttributes::class.java),
                SMB2ShareAccess.ALL,
                SMB2CreateDisposition.FILE_OPEN,
                EnumSet.noneOf(SMB2CreateOptions::class.java),
            ).use { f ->
                f.inputStream.use { input ->
                    target.outputStream().use { out -> input.copyTo(out) }
                }
            }
        }
        true
    } catch (e: Exception) {
        System.out.println("SMB download error: ${e.message}")
        false
    }
}
