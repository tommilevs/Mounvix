package com.mounvix.app.data.autoupload

/** Кандидат на автозагрузку (локальный SAF-файл). */
data class AutoFile(
    val name: String,
    val size: Long,
    val modified: Long,
    val uri: String,
)

/** Чистая логика выбора файлов для автозагрузки. */
object AutouploadPlanner {

    private val imageExt = setOf("jpg", "jpeg", "png", "gif", "webp", "heic", "heif", "bmp")
    private val videoExt = setOf("mp4", "mkv", "webm", "mov", "3gp", "m4v", "avi")

    fun isMedia(name: String): Boolean {
        val ext = name.substringAfterLast('.', "").lowercase()
        return ext in imageExt || ext in videoExt
    }

    /** Новые файлы: только медиа и строго новее lastMillis (0 — все). */
    fun plan(files: List<AutoFile>, lastMillis: Long): List<AutoFile> =
        files.filter { isMedia(it.name) && it.modified > lastMillis }
            .sortedBy { it.modified }
}
