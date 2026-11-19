package com.mounvix.app.data.source

import android.net.Uri
import android.os.CancellationSignal
import android.os.OperationCanceledException
import com.mounvix.app.data.model.FileEntry
import com.mounvix.app.data.model.FileType
import java.io.IOException
import java.io.InputStream
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Абстракция источника файлов.
 * Реализации: SAF (DocumentFile/Uri) — основная, далее MediaStore, прямой File.
 * Путь — строка относительно корня: "Images/photo.jpg"; пустая строка — корень.
 */
interface StorageSource {
    fun listDir(path: String): List<FileEntry>
    fun createDir(path: String): Boolean
    fun createFile(path: String): Boolean
    fun delete(path: String): Boolean
    fun rename(path: String, newName: String): Boolean
    fun exists(path: String): Boolean
    fun stat(path: String): FileEntry?
    fun copy(src: String, dst: String): Boolean
    fun move(src: String, dst: String): Boolean
    /** Скачать файл в target (для удалённых источников); false — не поддержано. */
    fun downloadTo(path: String, target: java.io.File): Boolean = false
    /** Загрузить локальный файл в path (для удалённых источников); false — не поддержано. */
    fun upload(local: java.io.File, path: String, onProgress: (Long, Long) -> Unit = { _, _ -> }): Boolean = false

    /** Создать каждый отсутствующий каталог, необходимый родительскому пути файла. */
    fun ensureParentDirectories(path: String): Boolean {
        val normalized = path.trim('/')
        if (normalized.isEmpty()) return true
        val segments = normalized.split('/')
        if (segments.any { it.isBlank() || it == "." || it == ".." || '\\' in it }) return false

        var current = ""
        for (segment in segments.dropLast(1)) {
            current = if (current.isEmpty()) segment else "$current/$segment"
            val existing = try {
                stat(current)
            } catch (_: Exception) {
                null
            }
            if (existing?.type == FileType.FOLDER) continue
            if (existing != null) return false
            if (!createDir(current)) {
                val afterCreate = try {
                    stat(current)
                } catch (_: Exception) {
                    null
                }
                if (afterCreate?.type != FileType.FOLDER) return false
            }
        }
        return true
    }

    /** Stream content directly to a remote path; cancellation and I/O failures are thrown. */
    @Throws(IOException::class)
    fun uploadCancellable(
        input: InputStream,
        contentLength: Long,
        path: String,
        signal: CancellationSignal,
        onProgress: (Long, Long) -> Unit = { _, _ -> }
    ) {
        throw IOException("Cancellable upload is not supported by this source")
    }

    fun uriOf(path: String): Uri? = null
    fun mimeTypeOf(path: String): String? = null

    /** Идентификатор корня этого источника (SAF tree Uri строкой или локальный путь). */
    fun rootId(): String? = null

    /** Стабильный ключ записи для избранных/недавних: SAF — documentUri, local — абсолютный путь. */
    fun stableKey(path: String): String? = null
}

/** Owns the input stream and active remote close action for one cancellable transfer. */
internal class CancellableUpload(
    private val input: InputStream,
    private val signal: CancellationSignal,
    private val closeRemote: () -> Unit
) : AutoCloseable {

    private val resourcesClosed = AtomicBoolean()

    init {
        signal.setOnCancelListener { closeActiveResources() }
        if (signal.isCanceled) closeActiveResources()
    }

    fun copyTo(
        contentLength: Long,
        onProgress: (Long, Long) -> Unit,
        writeChunk: (ByteArray, Int) -> Unit
    ) {
        require(contentLength >= 0L)
        var transferred = 0L
        val buffer = ByteArray(CHUNK_SIZE)
        try {
            signal.throwIfCanceled()
            while (true) {
                signal.throwIfCanceled()
                val remaining = contentLength - transferred
                val readLimit = if (remaining >= buffer.size) buffer.size else remaining.toInt() + 1
                val count = input.read(buffer, 0, readLimit)
                if (count < 0) break
                if (count == 0) continue
                signal.throwIfCanceled()
                if (count.toLong() > remaining) throw IOException("Input exceeds declared content length")
                writeChunk(buffer, count)
                transferred += count
                onProgress(transferred, contentLength)
            }
            signal.throwIfCanceled()
            if (transferred != contentLength) throw IOException("Input is shorter than declared content length")
        } catch (cancelled: OperationCanceledException) {
            throw cancelled
        } catch (error: IOException) {
            if (signal.isCanceled) throw OperationCanceledException("Upload cancelled")
            throw error
        } catch (error: Exception) {
            if (signal.isCanceled) throw OperationCanceledException("Upload cancelled")
            throw IOException("Upload failed", error)
        }
    }

    private fun closeActiveResources() {
        if (!resourcesClosed.compareAndSet(false, true)) return
        try {
            input.close()
        } catch (_: Exception) {
        }
        try {
            closeRemote()
        } catch (_: Exception) {
        }
    }

    override fun close() {
        signal.setOnCancelListener(null)
    }

    private companion object {
        const val CHUNK_SIZE = 64 * 1024
    }
}
