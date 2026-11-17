package com.mounvix.app.ui

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.InsertDriveFile
import androidx.compose.material.icons.filled.Audiotrack
import androidx.compose.material.icons.filled.Description
import androidx.compose.material.icons.filled.Folder
import androidx.compose.material.icons.filled.Image
import androidx.compose.material.icons.filled.Videocam
import androidx.compose.ui.graphics.vector.ImageVector
import com.mounvix.app.data.model.FileType

/** Иконка по расширению имени (общая для вкладок Файлы/Избранное/Недавние). */
internal fun entryIcon(name: String, type: FileType): ImageVector {
    if (type == FileType.FOLDER) return Icons.Filled.Folder
    val ext = name.substringAfterLast('.', "").lowercase()
    return when {
        ext in setOf("jpg", "jpeg", "png", "webp", "gif", "bmp") -> Icons.Filled.Image
        ext in setOf("mp4", "mkv", "avi", "mov", "webm") -> Icons.Filled.Videocam
        ext in setOf("mp3", "wav", "ogg", "flac", "m4a") -> Icons.Filled.Audiotrack
        ext in setOf("txt", "md", "log", "json", "xml", "yml", "yaml", "csv", "ini", "conf") -> Icons.Filled.Description
        else -> Icons.AutoMirrored.Filled.InsertDriveFile
    }
}
