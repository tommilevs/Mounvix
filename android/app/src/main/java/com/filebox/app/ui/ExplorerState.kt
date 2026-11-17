package com.mounvix.app.ui

import android.net.Uri
import com.mounvix.app.data.db.Favorite
import com.mounvix.app.data.db.Recent
import com.mounvix.app.data.model.FileEntry

enum class SortBy { NAME, SIZE, DATE, TYPE }

data class ClipboardInfo(val paths: List<String>, val cut: Boolean)

data class ExplorerState(
    val treeUri: Uri? = null,
    /** true — источник WebDAV (без SAF-дерева). */
    val remoteMode: Boolean = false,
    val pathStack: List<String> = emptyList(),
    val entries: List<FileEntry> = emptyList(),
    val selection: Set<String> = emptySet(),
    val clipboard: ClipboardInfo? = null,
    val sortBy: SortBy = SortBy.NAME,
    val search: String = "",
    val loading: Boolean = false,
    /** Жёсткая ошибка чтения (нет доступа к дереву) — заменяет контент. */
    val error: String? = null,
    /** Короткое сообщение об операции — показывается баннером и само исчезает. */
    val toast: String? = null,
    /** Стабильные ключи избранных объектов для пометки строк звёздочкой. */
    val favoriteKeys: Set<String> = emptySet()
) {
    val currentPath: String get() = pathStack.joinToString("/")
    val canGoBack: Boolean get() = pathStack.isNotEmpty()
    val hasSource: Boolean get() = treeUri != null || remoteMode
    val isSelecting: Boolean get() = selection.isNotEmpty()
}

data class FavoriteItem(val fav: Favorite, val available: Boolean = true)
data class RecentItem(val rec: Recent, val available: Boolean = true)
data class FavoritesUi(
    val loaded: Boolean = false,
    val loading: Boolean = false,
    val error: String? = null,
    val items: List<FavoriteItem> = emptyList()
)
data class RecentsUi(
    val loaded: Boolean = false,
    val loading: Boolean = false,
    val error: String? = null,
    val items: List<RecentItem> = emptyList()
)
data class GalleryItem(val entry: FileEntry, val file: java.io.File?, val video: Boolean)

data class GalleryUi(
    val loaded: Boolean = false,
    val loading: Boolean = false,
    val error: String? = null,
    val items: List<GalleryItem> = emptyList()
)
