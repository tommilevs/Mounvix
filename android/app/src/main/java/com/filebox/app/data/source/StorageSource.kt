package com.mounvix.app.data.source

import android.net.Uri
import com.mounvix.app.data.model.FileEntry

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
    fun uriOf(path: String): Uri? = null
    fun mimeTypeOf(path: String): String? = null

    /** Идентификатор корня этого источника (SAF tree Uri строкой или локальный путь). */
    fun rootId(): String? = null

    /** Стабильный ключ записи для избранных/недавних: SAF — documentUri, local — абсолютный путь. */
    fun stableKey(path: String): String? = null
}
