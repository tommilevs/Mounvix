package com.mounvix.app.ui

import android.content.Context
import android.content.ContentValues
import android.content.Intent
import android.net.Uri
import android.provider.MediaStore
import android.util.Log
import androidx.lifecycle.ViewModel
import androidx.core.content.FileProvider
import com.mounvix.app.data.db.AppDatabase
import com.mounvix.app.data.db.Favorite
import com.mounvix.app.data.db.Recent
import com.mounvix.app.data.model.FileEntry
import com.mounvix.app.data.ai.AiRecognizer
import com.mounvix.app.data.notify.Notifier
import com.mounvix.app.data.model.FileNameRules
import com.mounvix.app.data.model.FileType
import com.mounvix.app.data.repository.FavoriteRepository
import com.mounvix.app.data.repository.RecentRepository
import com.mounvix.app.data.source.SafStorageSource
import com.mounvix.app.data.source.SftpServer
import com.mounvix.app.data.source.SftpStorageSource
import com.mounvix.app.data.source.SmbServer
import com.mounvix.app.data.source.SmbStorageSource
import com.mounvix.app.data.source.StorageSource
import com.mounvix.app.data.source.WebDavServer
import com.mounvix.app.data.source.WebDavStorageSource
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
import kotlinx.coroutines.withContext
import java.io.File
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
            _state.update { it.copy(treeUri = uri, remoteMode = false, pathStack = emptyList()) }
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
    private val _galleryUi = MutableStateFlow(GalleryUi())
    private val _transfers = MutableStateFlow<List<TransferItem>>(emptyList())
    private val _aiState = MutableStateFlow<AiState?>(null)
    val aiState: StateFlow<AiState?> = _aiState.asStateFlow()
    val transfers: StateFlow<List<TransferItem>> = _transfers.asStateFlow()
    val galleryUi: StateFlow<GalleryUi> = _galleryUi.asStateFlow()
    val recentsUi: StateFlow<RecentsUi> = _recentsUi.asStateFlow()

    // Инициализация ПОСЛЕ всех полей-состояний: Kotlin инициализирует поля в порядке объявления.
    init {
        restoreLastTree()
        restoreLastWebDav()
        restoreLastSmb()
        restoreLastSftp()
    }

    // ---------- Вкладка «Файлы» ----------

    fun onTreeGranted(treeUri: Uri) {
        source = SafStorageSource(appContext, treeUri)
        try { prefs.edit().putString("last_tree_uri", treeUri.toString()).apply() } catch (_: Exception) {}
        _state.update { it.copy(treeUri = treeUri, remoteMode = false, pathStack = emptyList()) }
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
                remoteMode = false,
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

    /** Подключение к WebDAV (блокирующее; вызывать из IO). */
    fun connectWebDav(url: String, user: String, password: String, trustAll: Boolean): Boolean {
        if (url.isBlank()) return false
        return try {
            val src = WebDavStorageSource(WebDavServer(baseUrl = url, user = user, password = password, trustAll = trustAll))
            src.listDir("")
            source = src
            _state.update { it.copy(treeUri = null, remoteMode = true, pathStack = emptyList(), selection = emptySet(), search = "", error = null) }
            prefs.edit()
                .putString("webdav_url", url).putString("webdav_user", user)
                .putString("webdav_pass", password).putBoolean("webdav_trust", trustAll).apply()
            refresh()
            true
        } catch (e: Exception) {
            Log.w("MounvixWebDav", "connect failed: ${e.message}")
            false
        }
    }

    private fun restoreLastWebDav() {
        val url = prefs.getString("webdav_url", null) ?: return
        if (prefs.getString("last_tree_uri", null) != null) return
        val user = prefs.getString("webdav_user", "") ?: ""
        val pass = prefs.getString("webdav_pass", "") ?: ""
        val trust = prefs.getBoolean("webdav_trust", true)
        scope.launch { connectWebDav(url, user, pass, trust) }
    }

    /** Подключение к SMB (блокирующее; из IO). */
    fun connectSmb(host: String, share: String, domain: String, user: String, password: String): Boolean {
        if (host.isBlank() || share.isBlank()) return false
        return try {
            val src = SmbStorageSource(SmbServer(host = host, share = share, domain = domain, user = user, password = password))
            src.listDir("")
            source = src
            _state.update { it.copy(treeUri = null, remoteMode = true, pathStack = emptyList(), selection = emptySet(), search = "", error = null) }
            prefs.edit()
                .putString("smb_host", host).putString("smb_share", share).putString("smb_domain", domain)
                .putString("smb_user", user).putString("smb_pass", password).apply()
            refresh()
            true
        } catch (e: Exception) {
            Log.w("MounvixSmb", "connect failed: ${e.message}")
            false
        }
    }

    private fun restoreLastSmb() {
        val host = prefs.getString("smb_host", null) ?: return
        if (prefs.getString("last_tree_uri", null) != null) return
        if (prefs.getString("webdav_url", null) != null) return
        val share = prefs.getString("smb_share", "") ?: ""
        val domain = prefs.getString("smb_domain", "") ?: ""
        val user = prefs.getString("smb_user", "") ?: ""
        val pass = prefs.getString("smb_pass", "") ?: ""
        scope.launch { connectSmb(host, share, domain, user, pass) }
    }

    private fun smbSourceFor(treeId: String): SmbStorageSource? {
        val host = prefs.getString("smb_host", null) ?: return null
        val share = prefs.getString("smb_share", null) ?: return null
        val expected = "smb://$host/$share"
        if (treeId.trimEnd('/') != expected.trimEnd('/')) return null
        val user = prefs.getString("smb_user", "") ?: ""
        val pass = prefs.getString("smb_pass", "") ?: ""
        val domain = prefs.getString("smb_domain", "") ?: ""
        return SmbStorageSource(SmbServer(host = host, share = share, domain = domain, user = user, password = pass))
    }

    /** Подключение к SFTP (блокирующее; из IO). */
    fun connectSftp(host: String, port: Int, user: String, password: String, basePath: String): Boolean {
        if (host.isBlank()) return false
        return try {
            val src = SftpStorageSource(SftpServer(host = host, port = port, user = user, password = password, basePath = basePath))
            src.listDir("")
            source = src
            _state.update { it.copy(treeUri = null, remoteMode = true, pathStack = emptyList(), selection = emptySet(), search = "", error = null) }
            prefs.edit()
                .putString("sftp_host", host).putInt("sftp_port", port)
                .putString("sftp_user", user).putString("sftp_pass", password)
                .putString("sftp_base", basePath).apply()
            refresh()
            true
        } catch (e: Exception) {
            Log.w("MounvixSftp", "connect failed: ${e.message}")
            false
        }
    }

    private fun restoreLastSftp() {
        val host = prefs.getString("sftp_host", null) ?: return
        if (prefs.getString("last_tree_uri", null) != null) return
        if (prefs.getString("webdav_url", null) != null) return
        if (prefs.getString("smb_host", null) != null) return
        val port = prefs.getInt("sftp_port", 22)
        val user = prefs.getString("sftp_user", "") ?: ""
        val pass = prefs.getString("sftp_pass", "") ?: ""
        val base = prefs.getString("sftp_base", "/") ?: "/"
        scope.launch { connectSftp(host, port, user, pass, base) }
    }

    private fun sftpSourceFor(treeId: String): SftpStorageSource? {
        val host = prefs.getString("sftp_host", null) ?: return null
        val port = prefs.getInt("sftp_port", 22)
        val base = prefs.getString("sftp_base", "/") ?: "/"
        val expected = "sftp://$host:$port" + base.trimEnd('/')
        if (treeId.trimEnd('/') != expected.trimEnd('/')) return null
        val user = prefs.getString("sftp_user", "") ?: ""
        val pass = prefs.getString("sftp_pass", "") ?: ""
        return SftpStorageSource(SftpServer(host = host, port = port, user = user, password = pass, basePath = base))
    }

    fun refresh() {
        val src = source ?: return
        val state = _state.value
        val path = state.currentPath
        _state.update { it.copy(loading = true, error = null) }
        scope.launch {
            try {
                val entries = src.listDir(path).sortedWith(sorter(state.sortBy))
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
            } catch (e: Exception) {
                _state.update { it.copy(loading = false, error = e.message ?: "Ошибка чтения") }
            }
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

    /** Скачать выбранный удалённый файл в системные «Загрузки» (MediaStore). */
    fun saveSelectedToDownloads(): Boolean {
        val src = source ?: return false
        val state = _state.value
        val path = state.selection.firstOrNull() ?: return false
        val entry = state.entries.firstOrNull { it.path == path } ?: return false
        if (entry.type != FileType.FILE) return false
        scope.launch {
            try {
                val tmp = File(appContext.cacheDir, "dl_" + System.currentTimeMillis() + "_" + entry.name)
                if (!src.downloadTo(entry.path, tmp)) {
                    withContext(Dispatchers.Main) { showToast("Источник не поддерживает скачивание") }
                    return@launch
                }
                val values = ContentValues().apply {
                    put(MediaStore.Downloads.DISPLAY_NAME, entry.name)
                    put(MediaStore.Downloads.MIME_TYPE, src.mimeTypeOf(entry.path) ?: "application/octet-stream")
                    put(MediaStore.Downloads.RELATIVE_PATH, "Download/Mounvix")
                    put(MediaStore.Downloads.IS_PENDING, 1)
                }
                val resolver = appContext.contentResolver
                val uri = resolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values)
                    ?: throw IllegalStateException("MediaStore insert failed")
                resolver.openOutputStream(uri)?.use { out ->
                    tmp.inputStream().use { input -> input.copyTo(out) }
                }
                values.clear()
                values.put(MediaStore.Downloads.IS_PENDING, 0)
                resolver.update(uri, values, null, null)
                tmp.delete()
                withContext(Dispatchers.Main) {
                    showToast("Скачано в Загрузки: " + entry.name)
                    clearSelection()
                }
            } catch (e: Exception) {
                Log.w("MounvixDownload", "save failed: ${e.message}", e)
                withContext(Dispatchers.Main) { showToast("Не удалось скачать «${entry.name}»: ${e.message}") }
            }
        }
        return true
    }

    /** Открыть файл другим приложением (chooser). Папка — навигация. Успех записи файла попадает в «Недавние». */
    fun openWith(entry: FileEntry, overrideSrc: StorageSource? = null): Boolean {
        val src = overrideSrc ?: source ?: return false
        if (entry.type == FileType.FOLDER) {
            navigateInto(entry)
            return true
        }
        val uri = src.uriOf(entry.path)
        if (uri == null) {
            if (src is WebDavStorageSource || src is SmbStorageSource || src is SftpStorageSource) return openRemoteFile(entry, src)
            showToast("Не удалось получить доступ к «${entry.name}»")
            return false
        }
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

    /** Скачать WebDAV-файл в кэш и открыть через FileProvider. */
    private fun openRemoteFile(entry: FileEntry, src: StorageSource): Boolean {
        scope.launch {
            try {
                val dir = File(appContext.cacheDir, "webdav").apply { mkdirs() }
                val out = File(dir, entry.name)
                if (!src.downloadTo(entry.path, out)) throw IllegalStateException("download failed")
                val mime = src.mimeTypeOf(entry.path) ?: "application/octet-stream"
                withContext(Dispatchers.Main) {
                    val uri = FileProvider.getUriForFile(appContext, appContext.packageName + ".fileprovider", out)
                    val intent = Intent(Intent.ACTION_VIEW).apply {
                        setDataAndType(uri, mime)
                        addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
                    }
                    val matches = appContext.packageManager.queryIntentActivities(intent, 0)
                    if (matches.isEmpty()) { showToast("Нет приложения для открытия «${entry.name}»"); return@withContext }
                    appContext.startActivity(Intent.createChooser(intent, "Открыть: ${entry.name}").apply { addFlags(Intent.FLAG_ACTIVITY_NEW_TASK) })
                    recordRecent(entry, src)
                }
            } catch (e: Exception) {
                Log.w("MounvixOpen", "remote open failed ${entry.name}: ${e.message}", e)
                withContext(Dispatchers.Main) { showToast("Не удалось скачать «${entry.name}»: ${e.message}") }
            }
        }
        return true
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
    /** Если treeId — WebDAV-корень из prefs, вернуть источник; иначе null. */
    private fun webDavSourceFor(treeId: String): WebDavStorageSource? {
        val url = prefs.getString("webdav_url", null) ?: return null
        if (url.trimEnd('/') != treeId.trimEnd('/')) return null
        val user = prefs.getString("webdav_user", "") ?: ""
        val pass = prefs.getString("webdav_pass", "") ?: ""
        val trust = prefs.getBoolean("webdav_trust", true)
        return WebDavStorageSource(WebDavServer(baseUrl = treeId, user = user, password = pass, trustAll = trust))
    }

    private fun isSavedAvailable(treeId: String, relPath: String, wantFolder: Boolean, cache: HashMap<String, StorageSource>): Boolean {
        webDavSourceFor(treeId)?.let { remote ->
            return try {
                val st = remote.stat(relPath) ?: return false
                if (wantFolder) st.type == FileType.FOLDER else st.type == FileType.FILE
            } catch (e: Exception) {
                false
            }
        }
        smbSourceFor(treeId)?.let { remote ->
            return try {
                val st = remote.stat(relPath) ?: return false
                if (wantFolder) st.type == FileType.FOLDER else st.type == FileType.FILE
            } catch (e: Exception) {
                false
            }
        }
        sftpSourceFor(treeId)?.let { remote ->
            return try {
                val st = remote.stat(relPath) ?: return false
                if (wantFolder) st.type == FileType.FOLDER else st.type == FileType.FILE
            } catch (e: Exception) {
                false
            }
        }
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
        val remote = webDavSourceFor(treeId)
        if (remote != null) {
            val st = try { remote.stat(relPath) } catch (e: Exception) { null }
                ?: run { showToast("Объект недоступен"); return false }
            if (st.type == FileType.FOLDER) {
                source = remote
                _state.update { s ->
                    s.copy(
                        treeUri = null,
                        remoteMode = true,
                        pathStack = relPath.split("/").filter { it.isNotBlank() },
                        selection = emptySet(),
                        search = "",
                        error = null
                    )
                }
                refresh()
                return true
            }
            return openRemoteFile(st, remote)
        }
        val smb = smbSourceFor(treeId)
        if (smb != null) {
            val st = try { smb.stat(relPath) } catch (e: Exception) { null }
                ?: run { showToast("Объект недоступен"); return false }
            if (st.type == FileType.FOLDER) {
                source = smb
                _state.update { s ->
                    s.copy(
                        treeUri = null,
                        remoteMode = true,
                        pathStack = relPath.split("/").filter { it.isNotBlank() },
                        selection = emptySet(),
                        search = "",
                        error = null
                    )
                }
                refresh()
                return true
            }
            return openRemoteFile(st, smb)
        }
        val sftp = sftpSourceFor(treeId)
        if (sftp != null) {
            val st = try { sftp.stat(relPath) } catch (e: Exception) { null }
                ?: run { showToast("Объект недоступен"); return false }
            if (st.type == FileType.FOLDER) {
                source = sftp
                _state.update { s ->
                    s.copy(
                        treeUri = null,
                        remoteMode = true,
                        pathStack = relPath.split("/").filter { it.isNotBlank() },
                        selection = emptySet(),
                        search = "",
                        error = null
                    )
                }
                refresh()
                return true
            }
            return openRemoteFile(st, sftp)
        }
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

    // ---------- Галерея ----------

    private fun isVideoPath(path: String): Boolean = when (path.substringAfterLast('.', "").lowercase(Locale.US)) {
        "mp4", "mkv", "webm", "mov", "m4v" -> true
        else -> false
    }

    private fun isImagePath(path: String): Boolean = when (path.substringAfterLast('.', "").lowercase(Locale.US)) {
        "jpg", "jpeg", "png", "gif", "webp", "bmp" -> true
        else -> false
    }

    fun loadGallery(force: Boolean = false) {
        if (_galleryUi.value.loading) return
        val src = source ?: run {
            _galleryUi.update { it.copy(loaded = true, loading = false, items = emptyList()) }
            return
        }
        scope.launch {
            _galleryUi.update { it.copy(loading = true, error = null) }
            val items = ArrayList<GalleryItem>()
            try {
                for (e in _state.value.entries) {
                    if (e.type != FileType.FILE) continue
                    val video = isVideoPath(e.path)
                    if (!video && !isImagePath(e.path)) continue
                    val f = cacheMediaFile(e.path, src)
                    items.add(GalleryItem(e, f, video))
                    _galleryUi.update { it.copy(loading = false, loaded = true, items = items.toList()) }
                }
                _galleryUi.update { it.copy(loading = false, loaded = true, items = items) }
            } catch (e: Exception) {
                _galleryUi.update { it.copy(loading = false, loaded = true, error = e.message ?: "Ошибка галереи") }
            }
        }
    }

    private fun cacheMediaFile(path: String, src: StorageSource): File? {
        val dir = File(appContext.cacheDir, "media").apply { mkdirs() }
        val out = File(dir, path.replace('/', '_'))
        if (out.exists() && out.length() > 0) return out
        return try {
            if (src.downloadTo(path, out)) {
                out
            } else {
                val u = src.uriOf(path) ?: return null
                appContext.contentResolver.openInputStream(u)?.use { input ->
                    out.outputStream().use { o -> input.copyTo(o) }
                }
                if (out.length() > 0) out else null
            }
        } catch (e: Exception) {
            null
        }
    }

    fun openGalleryItem(item: GalleryItem): Boolean {
        val f = item.file ?: run { showToast("Файл ещё не загружен"); return false }
        val mime = when (item.entry.path.substringAfterLast('.', "").lowercase(Locale.US)) {
            "jpg", "jpeg" -> "image/jpeg"
            "png" -> "image/png"
            "gif" -> "image/gif"
            "webp" -> "image/webp"
            "mp4" -> "video/mp4"
            "webm" -> "video/webm"
            "mkv" -> "video/x-matroska"
            else -> "application/octet-stream"
        }
        return try {
            val uri = FileProvider.getUriForFile(appContext, appContext.packageName + ".fileprovider", f)
            val intent = Intent(Intent.ACTION_VIEW).apply {
                setDataAndType(uri, mime)
                addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
            }
            val matches = appContext.packageManager.queryIntentActivities(intent, 0)
            if (matches.isEmpty()) {
                showToast("Нет приложения для просмотра")
                false
            } else {
                appContext.startActivity(Intent.createChooser(intent, "Открыть: " + item.entry.name).apply {
                    addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                })
                true
            }
        } catch (e: Exception) {
            showToast(e.message ?: "Не удалось открыть")
            false
        }
    }
    // ---------- Передачи ----------

    /** Тестовая загрузка в текущую папку (для проверки transfer-пайплайна). */
    fun uploadTestFile(): Boolean {
        val src = source ?: return false
        scope.launch {
            val stamp = System.currentTimeMillis()
            val name = "mounvix-test-" + stamp + ".txt"
            val tmp = File(appContext.cacheDir, name)
            tmp.writeText("Mounvix upload test " + stamp)
            val id = stamp
            _transfers.update { it + TransferItem(id, name, tmp.length(), 0, "идёт") }
            val ok = try {
                src.upload(tmp, name) { done, total ->
                    _transfers.update { list ->
                        list.map { if (it.id == id) it.copy(done = done, total = total) else it }
                    }
                }
            } catch (e: Exception) {
                Log.w("MounvixUpload", "upload failed: ${e.message}", e)
                false
            }
            _transfers.update { list -> list.map { if (it.id == id) it.copy(state = if (ok) "готово" else "ошибка") else it } }
            tmp.delete()
            if (ok) refresh()
            Notifier.show(appContext, "Mounvix", if (ok) "Загружено: " + name else "Ошибка загрузки", 1002)
            withContext(Dispatchers.Main) { showToast(if (ok) "Загружено: " + name else "Ошибка загрузки") }
        }
        return true
    }
    // ---------- AI ----------

    fun clearAiState() {
        _aiState.value = null
    }

    /** Распознать выбранное изображение через настроенный vision-бэкенд. */
    fun describeSelectedImage(): Boolean {
        val st = _state.value
        val path = st.selection.firstOrNull() ?: return false
        val entry = st.entries.firstOrNull { it.path == path } ?: return false
        if (entry.type != FileType.FILE) return false
        val src = source ?: return false
        val base = prefs.getString("ai_base", "") ?: ""
        val model = prefs.getString("ai_model", "") ?: ""
        if (base.isBlank() || model.isBlank()) {
            showToast("Настройте AI в «Серверы»")
            return false
        }
        scope.launch {
            _aiState.value = AiState.Loading(entry.name)
            try {
                val f = cacheMediaFile(entry.path, src) ?: throw IllegalStateException("не удалось прочитать файл")
                val text = AiRecognizer().describe(
                    base, model, prefs.getString("ai_key", "") ?: "",
                    f.readBytes(), src.mimeTypeOf(entry.path) ?: "image/jpeg", ""
                )
                _aiState.value = AiState.Done(entry.name, text)
            } catch (e: Exception) {
                _aiState.value = AiState.Error(e.message ?: "Ошибка AI")
            }
        }
        return true
    }
    override fun onCleared() {
        scope.cancel()
        super.onCleared()
    }
}
