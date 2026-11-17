package com.mounvix.app.data.model

import java.text.DateFormat
import java.util.Date

enum class FileType { FOLDER, FILE }

data class FileEntry(
    val name: String,
    val path: String,
    val type: FileType,
    val size: Long = 0L,
    val modified: Long = 0L,
    /** Стабильный ключ записи (SAF documentUri или абсолютный локальный путь), ставится при перечислении. */
    val docKey: String? = null,
    val extension: String = ""
) {
    val extName: String get() = name.substringAfterLast('.', "")
}

fun formatSize(bytes: Long): String = when {
    bytes <= 0 -> "—"
    bytes < 1024 -> "$bytes B"
    bytes < 1024 * 1024 -> String.format("%.1f KB", bytes / 1024.0)
    bytes < 1024 * 1024 * 1024 -> String.format("%.1f MB", bytes / (1024.0 * 1024))
    else -> String.format("%.2f GB", bytes / (1024.0 * 1024 * 1024))
}

fun formatDate(millis: Long): String {
    if (millis <= 0) return "—"
    return DateFormat.getDateTimeInstance(DateFormat.MEDIUM, DateFormat.SHORT).format(Date(millis))
}
