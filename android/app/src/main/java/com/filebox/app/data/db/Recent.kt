package com.mounvix.app.data.db

import androidx.room.Entity
import androidx.room.Index
import androidx.room.PrimaryKey

/** Недавно открытый файл (папки не записываются). Дедупликация по storageKey: повторное открытие обновляет openedAt. */
@Entity(tableName = "recents", indices = [Index(value = ["storageKey"], unique = true)])
data class Recent(
    @PrimaryKey(autoGenerate = true) val id: Int = 0,
    val storageKey: String,
    val kind: String, // FILE
    val name: String,
    val treeId: String,
    val relPath: String,
    val size: Long = 0L,
    val modifiedAt: Long = 0L,
    val openedAt: Long = 0L
)
