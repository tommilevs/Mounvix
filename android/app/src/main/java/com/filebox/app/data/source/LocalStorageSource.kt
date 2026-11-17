package com.mounvix.app.data.source

import com.mounvix.app.data.model.FileEntry
import com.mounvix.app.data.model.FileType
import java.io.File

/**
 * Прямой источник на java.io.File.
 * Используется в юнит-тестах и (в будущем) для режима с ROOT/доступом к корню.
 */
class LocalStorageSource(private val rootDir: File) : StorageSource {

    private fun resolve(path: String): File =
        if (path.isBlank()) rootDir else File(rootDir, path)

    override fun listDir(path: String): List<FileEntry> {
        val dir = resolve(path)
        if (!dir.isDirectory) return emptyList()
        return dir.listFiles()?.map { f ->
            FileEntry(
                name = f.name,
                path = if (path.isBlank()) f.name else "$path/${f.name}",
                type = if (f.isDirectory) FileType.FOLDER else FileType.FILE,
                size = if (f.isFile) f.length() else 0L,
                modified = f.lastModified(),
                docKey = f.absolutePath
            )
        } ?: emptyList()
    }

    override fun createDir(path: String): Boolean = resolve(path).mkdirs()

    override fun createFile(path: String): Boolean {
        val f = resolve(path)
        f.parentFile?.mkdirs()
        return f.createNewFile()
    }

    override fun delete(path: String): Boolean {
        val f = resolve(path)
        return if (f.isDirectory) f.deleteRecursively() else f.delete()
    }

    override fun rename(path: String, newName: String): Boolean {
        if (newName.isBlank()) return false
        val f = resolve(path)
        val parent = f.parentFile ?: return false
        return f.renameTo(File(parent, newName))
    }

    override fun exists(path: String): Boolean = resolve(path).exists()

    override fun stat(path: String): FileEntry? {
        val f = resolve(path)
        if (!f.exists()) return null
        return FileEntry(
            name = f.name,
            path = path,
            type = if (f.isDirectory) FileType.FOLDER else FileType.FILE,
            size = if (f.isFile) f.length() else 0L,
            modified = f.lastModified()
        )
    }

    override fun copy(src: String, dst: String): Boolean {
        val s = resolve(src)
        val d = resolve(dst)
        if (!s.exists() || d.exists()) return false
        val parent = d.parentFile ?: return false
        if (!parent.exists() && !parent.mkdirs()) return false
        try {
            return copyRec(s, d)
        } catch (e: Exception) {
            cleanupPartial(d)
            return false
        }
    }

    private fun copyRec(srcFile: File, dstFile: File): Boolean {
        if (!srcFile.isDirectory) {
            val dest = srcFile.copyTo(dstFile, overwrite = false)
            // copyTo возвращает File; успех — файл создан и не пустой при совпадении размера.
            return dest.exists() && (srcFile.length() == 0L || dest.length() > 0L)
        }
        if (!dstFile.mkdir()) return false
        for (child in srcFile.listFiles().orEmpty()) {
            if (!copyRec(child, File(dstFile, child.name))) return false
        }
        return true
    }

    private fun cleanupPartial(d: File) {
        try {
            if (d.isDirectory) d.deleteRecursively() else d.delete()
        } catch (_: Exception) {
            // Частичный артефакт оставляем как есть; операция уже вернула false.
        }
    }

    override fun move(src: String, dst: String): Boolean {
        if (dst == src || dst.startsWith(src + "/")) return false
        if (!copy(src, dst)) return false
        return delete(src)
    }

    override fun rootId(): String = "local:" + rootDir.absolutePath

    override fun stableKey(path: String): String? {
        val f = resolve(path)
        if (path.isBlank()) return f.absolutePath
        return if (f.exists()) f.absolutePath else null
    }
}
