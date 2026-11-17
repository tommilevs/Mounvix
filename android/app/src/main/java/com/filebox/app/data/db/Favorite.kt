package com.mounvix.app.data.db

import androidx.room.Entity
import androidx.room.Index
import androidx.room.PrimaryKey

/**
 * Избранная запись. storageKey — стабильный ключ (SAF documentUri или абсолютный локальный путь).
 * treeId + relPath позволяют заново разрешить объект, если документ Uri сменился.
 */
@Entity(tableName = "favorites", indices = [Index(value = ["storageKey"], unique = true)])
data class Favorite(
    @PrimaryKey(autoGenerate = true) val id: Int = 0,
    val storageKey: String,
    val kind: String, // FILE | FOLDER
    val name: String,
    val treeId: String,
    val relPath: String,
    val size: Long = 0L,
    val modifiedAt: Long = 0L
) {
    companion object {
        const val KIND_FILE = "FILE"
        const val KIND_FOLDER = "FOLDER"
    }
}
