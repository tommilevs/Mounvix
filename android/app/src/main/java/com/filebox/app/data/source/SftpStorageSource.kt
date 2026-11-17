package com.mounvix.app.data.source

import com.mounvix.app.data.model.FileEntry
import com.mounvix.app.data.model.FileType
import net.schmizz.sshj.SSHClient
import net.schmizz.sshj.sftp.FileMode
import net.schmizz.sshj.sftp.OpenMode
import net.schmizz.sshj.sftp.SFTPClient
import net.schmizz.sshj.transport.verification.PromiscuousVerifier
import java.io.File
import java.util.EnumSet
import java.util.Locale

/** Настройки SFTP-сервера. */
data class SftpServer(
    val host: String,
    val port: Int = 22,
    val user: String = "",
    val password: String = "",
    val basePath: String = "/",
)

/**
 * SFTP-источник (sshj): листинг/stat/mkdir/rm/rename и скачивание.
 * Соединение на операцию (просто и безопасно для мобильного клиента).
 */
class SftpStorageSource(private val server: SftpServer) : StorageSource {

    override fun rootId(): String {
        val base = server.basePath.trimEnd('/')
        return "sftp://${server.host}:${server.port}$base"
    }

    override fun stableKey(path: String): String = "${rootId()}/${path.trim('/')}".trimEnd('/')
    override fun uriOf(path: String): android.net.Uri? = null
    override fun mimeTypeOf(path: String): String? = mimeOf(path)

    private fun remotePath(path: String): String {
        val base = server.basePath.trimEnd('/')
        val rel = path.trim('/')
        return if (rel.isEmpty()) (if (base.isEmpty()) "/" else base + "/") else "$base/$rel"
    }

    private fun <T> withSftp(block: (SFTPClient) -> T): T {
        val client = SSHClient()
        var sftp: SFTPClient? = null
        try {
            client.addHostKeyVerifier(PromiscuousVerifier())
            client.connect(server.host, server.port)
            client.authPassword(server.user, server.password.toCharArray())
            sftp = client.newSFTPClient()
            return block(sftp)
        } finally {
            try { sftp?.close() } catch (_: Exception) {}
            try { client.disconnect() } catch (_: Exception) {}
        }
    }

    override fun listDir(path: String): List<FileEntry> {
        val dir = path.trim('/')
        return withSftp { sftp ->
            val out = ArrayList<FileEntry>()
            for (info in sftp.ls(remotePath(dir))) {
                val name = info.name
                if (name == "." || name == "..") continue
                val childPath = if (dir.isEmpty()) name else "$dir/$name"
                val attrs = info.attributes
                out.add(
                    FileEntry(
                        name = name,
                        path = childPath,
                        type = if (info.isDirectory) FileType.FOLDER else FileType.FILE,
                        size = attrs.size,
                        modified = attrs.mtime * 1000L,
                        docKey = "${rootId()}/$childPath",
                    )
                )
            }
            out
        }
    }

    override fun stat(path: String): FileEntry? {
        val dir = path.trim('/')
        return withSftp { sftp ->
            val attrs = try { sftp.stat(remotePath(dir)) } catch (e: Exception) { null } ?: return@withSftp null
            val isDir = attrs.type == FileMode.Type.DIRECTORY
            FileEntry(
                name = dir.substringAfterLast('/').ifEmpty { "root" },
                path = dir,
                type = if (isDir) FileType.FOLDER else FileType.FILE,
                size = attrs.size,
                modified = attrs.mtime * 1000L,
                docKey = "${rootId()}/$dir",
            )
        }
    }

    override fun exists(path: String): Boolean = try {
        stat(path) != null
    } catch (e: Exception) {
        false
    }

    override fun createDir(path: String): Boolean = try {
        withSftp { it.mkdir(remotePath(path.trim('/'))) }
        true
    } catch (e: Exception) {
        false
    }

    override fun createFile(path: String): Boolean = try {
        withSftp { sftp ->
            sftp.open(remotePath(path.trim('/')), EnumSet.of(OpenMode.WRITE, OpenMode.CREAT, OpenMode.TRUNC)).close()
        }
        true
    } catch (e: Exception) {
        false
    }

    override fun delete(path: String): Boolean = try {
        val rel = path.trim('/')
        val isDir = stat(rel)?.type == FileType.FOLDER
        withSftp { sftp ->
            if (isDir) sftp.rmdir(remotePath(rel)) else sftp.rm(remotePath(rel))
        }
        true
    } catch (e: Exception) {
        false
    }

    override fun rename(path: String, newName: String): Boolean = try {
        val rel = path.trim('/')
        val parent = rel.substringBeforeLast('/', "")
        val destRel = if (parent.isEmpty()) newName else "$parent/$newName"
        withSftp { sftp -> sftp.rename(remotePath(rel), remotePath(destRel)) }
        true
    } catch (e: Exception) {
        false
    }

    override fun copy(src: String, dst: String): Boolean = false
    override fun move(src: String, dst: String): Boolean = false

    override fun downloadTo(path: String, target: File): Boolean = try {
        target.parentFile?.mkdirs()
        withSftp { sftp ->
            sftp.open(remotePath(path.trim('/')), EnumSet.of(OpenMode.READ)).use { f ->
                target.outputStream().use { out ->
                    val buf = ByteArray(64 * 1024)
                    var offset = 0L
                    while (true) {
                        val n = f.read(offset, buf, 0, buf.size)
                        if (n <= 0) break
                        out.write(buf, 0, n)
                        offset += n
                    }
                }
            }
        }
        true
    } catch (e: Exception) {
        System.out.println("SFTP download error: ${e.message}")
        false
    }

    private fun mimeOf(path: String): String? = when (path.substringAfterLast('.', "").lowercase(Locale.US)) {
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
}
