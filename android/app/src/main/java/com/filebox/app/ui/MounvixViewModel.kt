package com.mounvix.app.ui

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.util.Log
import androidx.lifecycle.ViewModel
import com.mounvix.app.data.db.AppDatabase
import com.mounvix.app.data.db.Favorite
import com.mounvix.app.data.db.Recent
import com.mounvix.app.data.model.FileEntry
import com.mounvix.app.data.model.FileNameRules
import com.mounvix.app.data.model.FileType
import com.mounvix.app.data.repository.FavoriteRepository
import com.mounvix.app.data.repository.RecentRepository
import com.mounvix.app.data.source.SafStorageSource
import com.mounvix.app.data.source.StorageSource
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import java.util.Locale

class MounvixViewModel(private val appContext: Context) : ViewModel() {

    private var source: StorageSource? = null
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    private val db by lazy { AppDatabase.get(appContext) }
    private val favoriteRepo by lazy { FavoriteRepository(db.favoritesDao()) }
    private val recentRepo by lazy { RecentRepository(db.recentsDao()) }

    @Volatile private var favoriteKeySet: Set<String> = emptySet()
    @Volatile private var favoritesLoaded = false
    @Volatile private var recentsLoading = false

    private val prefs by lazy { appContext.getSharedPreferences("mounvix_prefs", Context.MODE_PRIVATE) }

    /** Восстановить последнее дерево после перезапуска, если persistable-право SAF ещё действует. */
    private fun restoreLastTree() {
        val uriString = prefs.getString("last_tree_uri", null) ?: return
        val uri = try { Uri.parse(uriString) } catch (e: Exception) { return }
        val hasPerm = try {
            appContext.contentResolver.persistedUriPermissions
                .any { it.uri.toString() == uriString && it.isReadPermission }
        } catch (e: Exception) { false }
        if (!hasPerm) return
        try {
            source = SafStorageSource(appContext, uri)
            _state.update { it.copy(treeUri = uri, pathStack = emptyList()) }
            refresh()
        } catch (e: Exception) {
            Log.w("MounvixRestore", "restore last tree failed: ${e.message}")
        }
    }

    private val _state = MutableStateFlow(ExplorerState())
    val state: StateFlow<ExplorerState> = _state.asStateFlow()

    private val _favoritesUi = MutableStateFlow(FavoritesUi())
    val favoritesUi: StateFlow<FavoritesUi> = _favoritesUi.asStateFlow()

    private val _recentsUi = MutableStateFlow(RecentsUi())
    val recentsUi: StateFlow<RecentsUi> = _recentsUi.asStateFlow()

    // Инициализация ПОСЛЕ всех полей-состояний: Kotlin инициализирует поля в порядке объявления.
    init {
        restoreLastTree()
    }

    // ---------- Вкладка «Файлы» ----------

    fun onTreeGranted(treeUri: Uri) {
        source = SafStorageSource(appContext, treeUri)
        try { prefs.edit().putString("last_tree_uri", treeUri.toString()).apply() } catch (_: Exception) {}
        _state.update { it.copy(treeUri = treeUri, pathStack = emptyList()) }
        refresh()
    }

    /** Открыть сохранённое дерево (избранная папка из другого корня). */
    fun switchToSavedTree(treeId: String, relPath: String): Boolean {
        val uri = try { Uri.parse(treeId) } catch (e: Exception) { return false }
        val src = SafStorageSource(appContext, uri)
        val st = try { src.stat(relPath) } catch (e: Exception) { null } ?: return false
        if (st.type != FileType.FOLDER) return false
        source = src
        try { prefs.edit().putString("last_tree_uri", uri.toString()).apply() } catch (_: Exception) {}
        _state.update { s ->
            s.copy(
                treeUri = uri,
                pathStack = relPath.split("/").filter { it.isNotBlank() },
                selection = emptySet(),
                search = "",
                error = null,
                loading = true,
                favoriteKeys = favoriteKeySet
            )
        }
        refresh()
        return true
    }

    fun refresh() {
        val src = source ?: return
        val state = _state.value
        val path = state.currentPath
        _state.update { it.copy(loading = true, error = null) }
        val entries = try {
            src.listDir(path).sortedWith(sorter(state.sortBy))
        } catch (e: Exception) {
            _state.update { it.copy(loading = false, error = e.message ?: "Ошибка чтения") }
            return
        }
        _state.update {
            it.copy(
                loading = false,
                error = null,
                entries = if (state.search.isBlank()) entries
                    else entries.filter { en -> en.name.contains(state.search, ignoreCase = true) },
                selection = emptySet(),
                favoriteKeys = favoriteKeySet
            )
        }
    }

    private fun sorter(sortBy: SortBy): Comparator<FileEntry> = when (sortBy) {
        SortBy.NAME -> compareBy({ it.type != FileType.FOLDER }, { it.name.lowercase(Locale.ROOT) })
        SortBy.SIZE -> compareBy({ it.type != FileType.FOLDER }, { it.size })
        SortBy.DATE -> compareBy({ it.type != FileType.FOLDER }, { -it.modified })
        SortBy.TYPE -> compareBy(
            { it.type != FileType.FOLDER },
            { it.extName.lowercase(Locale.ROOT) },
            { it.name.lowercase(Locale.ROOT) }
        )
    }

    fun navigateInto(entry: FileEntry) {
        if (entry.type == FileType.FOLDER) {
            _state.update { s ->
                s.copy(pathStack = s.pathStack + entry.name, selection = emptySet(), search = "")
            }
            refresh()
        }
    }

    fun goUp() {
        _state.update { s ->
            s.copy(pathStack = s.pathStack.dropLast(1), selection = emptySet(), search = "")
        }
        refresh()
    }

    fun goToLevel(level: Int) {
        _state.update { s ->
            val n = level.coerceIn(0, s.pathStack.size)
            s.copy(pathStack = s.pathStack.take(n), selection = emptySet(), search = "")
        }
        refresh()
    }

    fun setSearch(query: String) {
        _state.update { it.copy(search = query) }
        refresh()
    }

    fun setSort(sortBy: SortBy) {
        _state.update { it.copy(sortBy = sortBy) }
        refresh()
    }

    fun toggleSelect(path: String) {
        _state.update { s ->
            val sel = s.selection.toMutableSet()
            if (!sel.add(path)) sel.remove(path)
            s.copy(selection = sel)
        }
    }

    fun clearSelection() {
        _state.update { it.copy(selection = emptySet()) }
    }

    private fun targetPathFor(name: String): Pair<String, StorageSource>? {
        val src = source ?: return null
        if (FileNameRules.validate(name) != null) return null
        val path = _state.value.currentPath
        val target = if (path.isBlank()) name else "$path/$name"
        return target to src
    }

    fun newFolder(name: String): Boolean {
        val t = targetPathFor(name.trim()) ?: run { showToast("Недопустимое имя"); return false }
        if (t.second.exists(t.first)) { showToast("«${name}» уже существует в этой папке"); return false }
        val ok = try { t.second.createDir(t.first) } catch (e: Exception) { false }
        if (!ok) showToast("Не удалось создать папку") else refresh()
        return ok
    }

    fun newFile(name: String): Boolean {
        val t = targetPathFor(name.trim()) ?: run { showToast("Недопустимое имя"); return false }
        if (t.second.exists(t.first)) { showToast("«${name}» уже существует в этой папке"); return false }
        val ok = try { t.second.createFile(t.first) } catch (e: Exception) { false }
        if (!ok) showToast("Не удалось создать файл") else refresh()
        return ok
    }

    /** Короткое сообщение об операции; баннер исчезает через 3 секунды. */
    private fun showToast(msg: String) {
        _state.update { it.copy(toast = msg, loading = false) }
        scope.launch {
            delay(3000)
            if (_state.value.toast == msg) _state.update { it.copy(toast = null) }
        }
    }

    fun renameEntry(path: String, newName: String): Boolean {
        val src = source ?: return false
        if (FileNameRules.validate(newName.trim()) != null) { showToast("Недопустимое имя"); return false }
        val ok = try { src.rename(path, newName.trim()) } catch (e: Exception) { false }
        if (ok) {
            _state.update { it.copy(selection = emptySet()) }
            refresh()
        } else showToast("Не удалось переименовать")
        return ok
    }

    fun deleteSelected(): Int {
        val src = source ?: return 0
        val sel = _state.value.selection.toList()
        var ok = 0
        for (p in sel.sortedByDescending { it.length }) {
            val r = try { src.delete(p) } catch (e: Exception) { false }
            if (r) ok++
        }
        _state.update { it.copy(selection = emptySet()) }
        if (ok > 0) refresh() else showToast("Не удалось удалить")
        return ok
    }

    fun copySelected() {
        val sel = _state.value.selection.toList()
        if (sel.isEmpty()) return
        _state.update { it.copy(clipboard = ClipboardInfo(sel, cut = false), selection = emptySet()) }
    }

    fun cutSelected() {
        val sel = _state.value.selection.toList()
        if (sel.isEmpty()) return
        _state.update { it.copy(clipboard = ClipboardInfo(sel, cut = true), selection = emptySet()) }
    }

    fun clearClipboard() {
        _state.update { it.copy(clipboard = null) }
    }

    /** Вставка в текущий каталог. Возвращает число перенесённых/скопированных объектов. */
    fun paste(): Int {
        val src = source ?: return 0
        val clip = _state.value.clipboard ?: return 0
        val dir = _state.value.currentPath
        var ok = 0
        for (p in clip.paths.sortedByDescending { it.length }) {
            val name = p.substringAfterLast('/')
            val dst = if (dir.isBlank()) name else "$dir/$name"
            if (p == dst) continue
            val done = try {
                if (clip.cut) src.move(p, dst) else src.copy(p, dst)
            } catch (e: Exception) { false }
            if (done) ok++
        }
        if (ok > 0) {
            if (clip.cut) _state.update { it.copy(clipboard = null) }
            refresh()
        } else showToast("Не удалось вставить (возможно, имена уже заняты)")
        return ok
    }

    /** Открыть файл другим приложением (chooser). Папка — навигация. Успех записи файла попадает в «Недавние». */
    fun openWith(entry: FileEntry, overrideSrc: StorageSource? = null): Boolean {
        val src = overrideSrc ?: source ?: return false
        if (entry.type == FileType.FOLDER) {
            navigateInto(entry)
            return true
        }
        val uri = src.uriOf(entry.path)
            ?: run { showToast("Не удалось получить доступ к «${entry.name}»"); return false }
        val mime = src.mimeTypeOf(entry.path) ?: "application/octet-stream"
        val intent = Intent(Intent.ACTION_VIEW).apply {
            setDataAndType(uri, mime)
            // appContext → NEW_TASK обязателен (иначе AndroidRuntimeException при startActivity)
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        val matches = appContext.packageManager.queryIntentActivities(intent, 0)
        Log.i("MounvixOpen", "openWith ${entry.name}: mime=$mime candidates=${matches.size}")
        if (matches.isEmpty()) {
            Log.w("MounvixOpen", "no activity found for $mime (${entry.name})")
            showToast("Нет приложения для открытия «${entry.name}»")
            return false
        }
        val launched = try {
            // NEW_TASK ставим на внешний chooser-intent: createChooser его не копирует из target,
            // а startActivity от Application-контекста без флага бросает AndroidRuntimeException.
            val chooser = Intent.createChooser(intent, "Открыть: ${entry.name}").apply {
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            }
            appContext.startActivity(chooser)
            true
        } catch (e: Exception) {
            Log.e("MounvixOpen", "startActivity failed for ${entry.name}", e)
            showToast(e.message ?: "Не удалось открыть файл")
            false
        }
        if (launched) recordRecent(entry, src)
        return launched
    }

    private fun recordRecent(entry: FileEntry, src: StorageSource) {
        val key = src.stableKey(entry.path) ?: return
        val treeId = src.rootId() ?: return
        scope.launch {
            try {
                recentRepo.record(
                    Recent(
                        storageKey = key,
                        kind = Favorite.KIND_FILE,
                        name = entry.name,
                        treeId = treeId,
                        relPath = entry.path,
                        size = entry.size,
                        modifiedAt = entry.modified,
                        openedAt = System.currentTimeMillis()
                    )
                )
                refreshRecents()
            } catch (e: Exception) {
                // История — некритичный путь; файл уже открыт.
            }
        }
    }

    // ---------- Избранное ----------

    fun loadFavorites(force: Boolean = false) {
        if (favoritesLoaded && !force) return
        favoritesLoaded = true
        scope.launch {
            _favoritesUi.update { FavoritesUi(loaded = false, loading = true) }
            try {
                val favs = favoriteRepo.all()
                favoriteKeySet = favs.mapTo(mutableSetOf()) { it.storageKey }
                _state.update { s -> s.copy(favoriteKeys = favoriteKeySet) }
                val cache = HashMap<String, StorageSource>()
                val items = favs.map { fav ->
                    FavoriteItem(fav, available = isSavedAvailable(fav.treeId, fav.relPath, wantFolder = fav.kind == Favorite.KIND_FOLDER, cache))
                }
                _favoritesUi.update { FavoritesUi(loaded = true, loading = false, error = null, items = items) }
            } catch (e: Exception) {
                _favoritesUi.update { FavoritesUi(loaded = false, loading = false, error = e.message ?: "Ошибка чтения") }
            }
        }
    }

    /** Честная проверка доступности сохранённого объекта; без падений. */
    private fun isSavedAvailable(treeId: String, relPath: String, wantFolder: Boolean, cache: HashMap<String, StorageSource>): Boolean {
        return try {
            val uri = Uri.parse(treeId)
            val src = cache.getOrPut(treeId) { SafStorageSource(appContext, uri) }
            val st = src.stat(relPath) ?: return false
            if (wantFolder) st.type == FileType.FOLDER else st.type == FileType.FILE
        } catch (e: Exception) {
            false
        }
    }

    fun toggleFavorite(entry: FileEntry) {
        val src = source ?: return
        applyFavoriteToggle(
            key = src.stableKey(entry.path),
            kind = if (entry.type == FileType.FOLDER) Favorite.KIND_FOLDER else Favorite.KIND_FILE,
            name = entry.name,
            treeId = src.rootId(),
            relPath = entry.path,
            size = entry.size,
            modifiedAt = entry.modified
        )
    }

    fun toggleSavedFavorite(item: FavoriteItem) {
        applyFavoriteToggle(
            key = item.fav.storageKey,
            kind = item.fav.kind,
            name = item.fav.name,
            treeId = item.fav.treeId,
            relPath = item.fav.relPath,
            size = item.fav.size,
            modifiedAt = item.fav.modifiedAt
        )
    }

    private fun applyFavoriteToggle(
        key: String?, kind: String, name: String, treeId: String?, relPath: String,
        size: Long, modifiedAt: Long
    ) {
        val k = key ?: return
        scope.launch {
            try {
                // Читаем актуальные ключи из состояния (все изменения на Main — single-threaded),
                // а не из застаревшего поля: иначе второе переключение «схлопывает» первое.
                favoriteKeySet = _state.value.favoriteKeys
                if (k in favoriteKeySet) {
                    favoriteRepo.remove(k)
                    val newKeys = favoriteKeySet - k
                    favoriteKeySet = newKeys
                    _state.update { s -> s.copy(favoriteKeys = newKeys) }
                    _favoritesUi.update { ui -> ui.copy(items = ui.items.filterNot { it.fav.storageKey == k }) }
                } else {
                    val fav = Favorite(
                        storageKey = k, kind = kind, name = name,
                        treeId = treeId ?: "unknown", relPath = relPath,
                        size = size, modifiedAt = if (modifiedAt > 0) modifiedAt else System.currentTimeMillis()
                    )
                    favoriteRepo.add(fav)
                    val newKeys = favoriteKeySet + k
                    favoriteKeySet = newKeys
                    _state.update { s -> s.copy(favoriteKeys = newKeys) }
                    _favoritesUi.update { ui ->
                        ui.copy(items = ui.items.filterNot { it.fav.storageKey == k } + FavoriteItem(fav, available = true))
                    }
                }
            } catch (e: Exception) {
                showToast("Не удалось обновить избранное")
            }
        }
    }

    fun removeSavedFavorite(item: FavoriteItem) {
        scope.launch {
            try {
                val key = item.fav.storageKey ?: return@launch
                favoriteRepo.remove(key)
                val newKeys = _state.value.favoriteKeys - key
                favoriteKeySet = newKeys
                _state.update { s -> s.copy(favoriteKeys = newKeys) }
                _favoritesUi.update { ui -> ui.copy(items = ui.items.filterNot { it.fav.id == item.fav.id }) }
            } catch (e: Exception) {
                showToast("Не удалось удалить из избранного")
            }
        }
    }

    /** Открыть сохранённый файл/папку из «Избранного» и «Недавних». */
    fun openSaved(treeId: String, relPath: String): Boolean {
        val uri = try { Uri.parse(treeId) } catch (e: Exception) { return false }
        val src = SafStorageSource(appContext, uri)
        val st = try { src.stat(relPath) } catch (e: Exception) { null }
            ?: run { showToast("Объект недоступен"); return false }
        if (st.type == FileType.FOLDER) {
            // Папка — переход внутрь неё в её дереве (для текущего дерева и того же пути refresh).
            switchToSavedTree(treeId, relPath)
            return true
        }
        return openWith(st, src)
    }

    // ---------- Недавние ----------

    private suspend fun refreshRecents() {
        try {
            val cache = HashMap<String, StorageSource>()
            val items = recentRepo.all().map { rec ->
                RecentItem(rec, available = isSavedAvailable(rec.treeId, rec.relPath, wantFolder = false, cache))
            }
            _recentsUi.update { RecentsUi(loaded = true, loading = false, error = null, items = items) }
        } catch (e: Exception) {
            _recentsUi.update { RecentsUi(loaded = false, loading = false, error = e.message ?: "Ошибка чтения") }
        } finally {
            recentsLoading = false
        }
    }

    fun loadRecents(force: Boolean = false) {
        if (recentsLoading && !force) return
        recentsLoading = true
        scope.launch {
            _recentsUi.update { RecentsUi(loaded = false, loading = true) }
            refreshRecents()
        }
    }

    fun clearRecents() {
        scope.launch {
            try {
                recentRepo.clear()
                _recentsUi.update { RecentsUi(loaded = true, loading = false, error = null, items = emptyList()) }
            } catch (e: Exception) {
                showToast("Не удалось очистить историю")
            } finally {
                recentsLoading = false
            }
        }
    }

    fun removeRecent(item: RecentItem) {
        scope.launch {
            try {
                recentRepo.remove(item.rec.storageKey)
                _recentsUi.update { ui -> ui.copy(items = ui.items.filterNot { it.rec.id == item.rec.id }) }
            } catch (e: Exception) {
                showToast("Не удалось удалить запись")
            } finally {
                recentsLoading = false
            }
        }
    }

    override fun onCleared() {
        scope.cancel()
        super.onCleared()
    }
}
