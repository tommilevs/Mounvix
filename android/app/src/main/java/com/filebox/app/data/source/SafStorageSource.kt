package com.mounvix.app.data.source

import android.content.Context
import android.net.Uri
import android.util.Log
import androidx.documentfile.provider.DocumentFile
import com.mounvix.app.data.model.FileEntry
import com.mounvix.app.data.model.FileType

/**
 * Основной источник на SAF (Storage Access Framework).
 * Пользователь выбирает папку через ACTION_OPEN_DOCUMENT_TREE;
 * далее доступ работает по сохранённому tree Uri.
 */
class SafStorageSource(
    private val context: Context,
    private val rootUri: Uri
) : StorageSource {

    companion object {
        private const val TAG = "MounvixSAF"
    }

    private fun root(): DocumentFile? = DocumentFile.fromTreeUri(context, rootUri)

    /** Родительский путь: "" для корня дерева (substringBeforeLast без разделителя вернул бы всю строку). */
    private fun parentOf(path: String): String {
        val i = path.lastIndexOf('/')
        return if (i < 0) "" else path.substring(0, i)
    }


    private fun resolve(path: String): DocumentFile? {
        var current = root() ?: return null
        if (path.isBlank()) return current
        for (seg in path.split('/').filter { it.isNotBlank() }) {
            current = current.findFile(seg) ?: return null
        }
        return current
    }

    override fun listDir(path: String): List<FileEntry> {
        val dir = resolve(path) ?: return emptyList()
        if (!dir.isDirectory) return emptyList()
        return dir.listFiles().map { entry ->
            FileEntry(
                name = entry.name ?: "?",
                path = if (path.isBlank()) entry.name ?: "?" else "$path/${entry.name}",
                type = if (entry.isDirectory) FileType.FOLDER else FileType.FILE,
                size = entry.length(),
                modified = entry.lastModified(),
                docKey = entry.uri?.toString()
            )
        }
    }

    override fun createDir(path: String): Boolean {
        val name = path.substringAfterLast('/')
        if (name.isBlank()) return false
        val parent = parentOf(path)
        val parentDoc = resolve(parent) ?: run { Log.w(TAG, "createDir: parent not resolved for $path"); return false }
        return try {
            val doc = parentDoc.createDirectory(name)
            if (doc == null) Log.w(TAG, "createDir: createDirectory returned null for $name")
            doc != null
        } catch (e: Exception) {
            Log.e(TAG, "createDir failed for $name", e)
            false
        }
    }

    override fun createFile(path: String): Boolean {
        val name = path.substringAfterLast('/')
        if (name.isBlank()) return false
        val parent = parentOf(path)
        val parentDoc = resolve(parent) ?: run { Log.w(TAG, "createFile: parent not resolved for $path"); return false }
        return try {
            val doc = parentDoc.createFile("text/plain", name)
            if (doc == null) Log.w(TAG, "createFile: createFile returned null for $name")
            doc != null
        } catch (e: Exception) {
            Log.e(TAG, "createFile failed for $name", e)
            false
        }
    }

    override fun delete(path: String): Boolean {
        val doc = resolve(path) ?: run { Log.w(TAG, "delete: not resolved $path"); return false }
        return try {
            val ok = doc.delete()
            if (!ok) Log.w(TAG, "delete: returned false for $path")
            ok
        } catch (e: Exception) {
            Log.e(TAG, "delete failed for $path", e)
            false
        }
    }

    override fun rename(path: String, newName: String): Boolean {
        if (newName.isBlank()) return false
        val doc = resolve(path) ?: run { Log.w(TAG, "rename: not resolved $path"); return false }
        return try {
            val ok = doc.renameTo(newName)
            if (!ok) Log.w(TAG, "rename: returned false for $path -> $newName")
            ok
        } catch (e: Exception) {
            Log.e(TAG, "rename failed for $path -> $newName", e)
            false
        }
    }

    override fun exists(path: String): Boolean = resolve(path) != null

    override fun stat(path: String): FileEntry? {
        val doc = resolve(path) ?: return null
        return FileEntry(
            name = doc.name ?: "?",
            path = path,
            type = if (doc.isDirectory) FileType.FOLDER else FileType.FILE,
            size = doc.length(),
            modified = doc.lastModified()
        )
    }

    override fun copy(src: String, dst: String): Boolean {
        val s = resolve(src) ?: return false
        val dstName = dst.substringAfterLast('/')
        if (dstName.isBlank()) return false
        val dstParent = resolve(parentOf(dst)) ?: return false
        if (dstParent.findFile(dstName) != null) return false
        return if (s.isDirectory) {
            val newDir = dstParent.createDirectory(dstName) ?: return false
            copyDirContents(s, newDir)
        } else {
            val outDoc = dstParent.createFile(s.type ?: "application/octet-stream", dstName) ?: return false
            copyStream(s.uri, outDoc.uri)
        }
    }

    private fun copyDirContents(srcDir: DocumentFile, dstDir: DocumentFile): Boolean {
        for (child in srcDir.listFiles()) {
            val name = child.name ?: return false
            if (child.isDirectory) {
                val newSub = dstDir.createDirectory(name) ?: return false
                if (!copyDirContents(child, newSub)) return false
            } else {
                val outDoc = dstDir.createFile(child.type ?: "application/octet-stream", name) ?: return false
                if (!copyStream(child.uri, outDoc.uri)) return false
            }
        }
        return true
    }

    private fun copyStream(srcUri: Uri, dstUri: Uri): Boolean {
        val inStream = context.contentResolver.openInputStream(srcUri) ?: return false
        val outStream = context.contentResolver.openOutputStream(dstUri) ?: return false
        return try {
            inStream.copyTo(outStream)
            true
        } catch (e: Exception) {
            false
        } finally {
            inStream.close()
            outStream.close()
        }
    }

    override fun move(src: String, dst: String): Boolean {
        if (dst == src || dst.startsWith(src + "/")) return false
        val s = resolve(src) ?: return false
        val srcParent = parentOf(src)
        val dstParent = parentOf(dst)
        if (srcParent == dstParent) return s.renameTo(dst.substringAfterLast('/'))
        if (!copy(src, dst)) return false
        return delete(src)
    }

    override fun rootId(): String = rootUri.toString()

    override fun stableKey(path: String): String? = resolve(path)?.uri?.toString()

    override fun uriOf(path: String): Uri? = resolve(path)?.uri

    override fun mimeTypeOf(path: String): String? {
        val doc = resolve(path) ?: return null
        if (doc.isDirectory) return null
        return doc.type ?: "application/octet-stream"
    }
}
