package com.mounvix.app.ui

import android.content.Intent
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Download
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.InsertDriveFile
import androidx.compose.material.icons.automirrored.filled.Sort
import androidx.compose.material.icons.filled.Clear
import androidx.compose.material.icons.filled.ContentCopy
import androidx.compose.material.icons.filled.ContentCut
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.FolderOff
import androidx.compose.material.icons.filled.FolderOpen
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.OpenInNew
import androidx.compose.material.icons.filled.PhotoLibrary
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.outlined.StarBorder
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.mounvix.app.data.model.FileEntry
import com.mounvix.app.data.model.FileType
import com.mounvix.app.data.model.formatDate
import com.mounvix.app.data.model.formatSize

private sealed class ExplorerDialog {
    object NewFolder : ExplorerDialog()
    object NewFile : ExplorerDialog()
    data class Rename(val entry: FileEntry) : ExplorerDialog()
    data class Delete(val count: Int) : ExplorerDialog()
    data class Info(val entry: FileEntry, val isFavorite: Boolean) : ExplorerDialog()
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ExplorerScreen(viewModel: MounvixViewModel, onOpenGallery: () -> Unit = {}) {
    val context = LocalContext.current
    val state by viewModel.state.collectAsState()
    var dialog by remember { mutableStateOf<ExplorerDialog?>(null) }
    var sortMenu by remember { mutableStateOf(false) }
    var searchField by remember { mutableStateOf("") }
    var showSearch by remember { mutableStateOf(false) }

    val pickTree = rememberLauncherForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        val uri = result.data?.data ?: return@rememberLauncherForActivityResult
        val flags = Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            try {
                context.contentResolver.takePersistableUriPermission(uri, flags)
            } catch (_: Exception) {
                // Не критично для v1
            }
        }
        viewModel.onTreeGranted(uri)
    }

    val onPick: () -> Unit = { pickTree.launch(Intent(Intent.ACTION_OPEN_DOCUMENT_TREE)) }

    fun closeSearch() {
        showSearch = false
        searchField = ""
        viewModel.setSearch("")
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Text(
                        text = if (state.pathStack.isEmpty()) "Mounvix" else state.pathStack.last(),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis
                    )
                },
                navigationIcon = {
                    IconButton(enabled = state.canGoBack, onClick = { viewModel.goUp() }) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Назад")
                    }
                },
                actions = {
                    if (showSearch) {
                        OutlinedTextField(
                            value = searchField,
                            onValueChange = {
                                searchField = it
                                viewModel.setSearch(it)
                            },
                            modifier = Modifier.width(240.dp),
                            placeholder = { Text("Поиск") },
                            singleLine = true,
                            trailingIcon = {
                                IconButton(onClick = { closeSearch() }) {
                                    Icon(Icons.Filled.Clear, contentDescription = "Очистить поиск")
                                }
                            }
                        )
                    } else {
                        IconButton(onClick = { showSearch = true; searchField = "" }) {
                            Icon(Icons.Filled.Search, contentDescription = "Поиск")
                        }
                    }
                    IconButton(onClick = onOpenGallery) {
                        Icon(Icons.Filled.PhotoLibrary, contentDescription = "Галерея")
                    }
                    Box {
                        IconButton(onClick = { sortMenu = true }) {
                            Icon(Icons.AutoMirrored.Filled.Sort, contentDescription = "Сортировка")
                        }
                        DropdownMenu(expanded = sortMenu, onDismissRequest = { sortMenu = false }) {
                            DropdownMenuItem(
                                text = { Text("По имени") },
                                onClick = { sortMenu = false; viewModel.setSort(SortBy.NAME) }
                            )
                            DropdownMenuItem(
                                text = { Text("По размеру") },
                                onClick = { sortMenu = false; viewModel.setSort(SortBy.SIZE) }
                            )
                            DropdownMenuItem(
                                text = { Text("По дате") },
                                onClick = { sortMenu = false; viewModel.setSort(SortBy.DATE) }
                            )
                            DropdownMenuItem(
                                text = { Text("По типу") },
                                onClick = { sortMenu = false; viewModel.setSort(SortBy.TYPE) }
                            )
                        }
                    }
                    IconButton(onClick = onPick) {
                        Icon(Icons.Filled.FolderOpen, contentDescription = "Сменить папку")
                    }
                }
            )
        },
        bottomBar = {
            if (state.hasSource) {
                Surface(tonalElevation = 4.dp) {
                    when {
                        state.isSelecting -> SelectionBar(viewModel = viewModel, state = state, onDialog = { dialog = it })
                        state.clipboard != null -> ClipboardBar(
                            viewModel = viewModel,
                            count = state.clipboard!!.paths.size,
                            cut = state.clipboard!!.cut
                        )
                        else -> CreateBar(
                            onNewFolder = { dialog = ExplorerDialog.NewFolder },
                            onNewFile = { dialog = ExplorerDialog.NewFile }
                        )
                    }
                }
            }
        }
    ) { padding ->
        if (!state.hasSource) {
            NoRootView(onPick = onPick, modifier = Modifier.padding(padding))
        } else {
            Column(modifier = Modifier.fillMaxSize().padding(padding)) {
                Breadcrumbs(state.pathStack) { viewModel.goToLevel(it) }
                state.toast?.let { toast ->
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .background(MaterialTheme.colorScheme.errorContainer)
                            .padding(horizontal = 16.dp, vertical = 4.dp),
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Text(
                            text = toast,
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onErrorContainer,
                            modifier = Modifier.weight(1f)
                        )
                    }
                }
                when {
                    state.loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                        CircularProgressIndicator()
                    }
                    state.error != null -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                        Text(
                            text = state.error.orEmpty(),
                            color = MaterialTheme.colorScheme.error,
                            modifier = Modifier.padding(24.dp),
                            textAlign = TextAlign.Center
                        )
                    }
                    state.entries.isEmpty() -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                        Text(
                            text = if (state.search.isBlank()) "Папка пуста" else "Ничего не найдено",
                            style = MaterialTheme.typography.bodyLarge,
                            color = MaterialTheme.colorScheme.onSurfaceVariant
                        )
                    }
                    else -> LazyColumn(Modifier.fillMaxSize()) {
                        items(state.entries, key = { it.path }) { entry ->
                            EntryRow(
                                entry = entry,
                                selected = entry.path in state.selection,
                                isFavorite = entry.docKey != null && entry.docKey in state.favoriteKeys,
                                onClick = {
                                    if (state.isSelecting) viewModel.toggleSelect(entry.path)
                                    else if (entry.type == FileType.FOLDER) viewModel.navigateInto(entry)
                                    else viewModel.openWith(entry)
                                },
                                onLongClick = { viewModel.toggleSelect(entry.path) },
                                onToggleFavorite = { viewModel.toggleFavorite(entry) }
                            )
                        }
                    }
                }
            }
        }
    }

    when (val d = dialog) {
        null -> Unit
        is ExplorerDialog.NewFolder -> CreateNameDialog(
            title = "Новая папка", label = "Имя папки", confirmLabel = "Создать",
            onDismiss = { dialog = null },
            onCreate = { name -> viewModel.newFolder(name); dialog = null }
        )
        is ExplorerDialog.NewFile -> CreateNameDialog(
            title = "Новый файл", label = "Имя файла (с расширением, напр. notes.txt)", confirmLabel = "Создать",
            onDismiss = { dialog = null },
            onCreate = { name -> viewModel.newFile(name); dialog = null }
        )
        is ExplorerDialog.Rename -> RenameDialog(
            entry = d.entry,
            onDismiss = { dialog = null },
            onRename = { newName -> viewModel.renameEntry(d.entry.path, newName); dialog = null }
        )
        is ExplorerDialog.Delete -> AlertDialog(
            onDismissRequest = { dialog = null },
            title = { Text("Удалить") },
            text = { Text("Удалить выбранные объекты (${d.count}) без восстановления?") },
            confirmButton = {
                TextButton(onClick = { viewModel.deleteSelected(); dialog = null }) { Text("Удалить") }
            },
            dismissButton = { TextButton(onClick = { dialog = null }) { Text("Отмена") } }
        )
        is ExplorerDialog.Info -> AlertDialog(
            onDismissRequest = { dialog = null },
            title = { Text(d.entry.name) },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    Text(
                        "Тип: " + if (d.entry.type == FileType.FOLDER) "папка" else "файл" +
                            "\nРазмер: " + formatSize(d.entry.size) +
                            "\nИзменён: " + formatDate(d.entry.modified) +
                            "\nВ избранном: " + (if (d.isFavorite) "да" else "нет")
                    )
                }
            },
            confirmButton = { TextButton(onClick = { dialog = null }) { Text("Закрыть") } }
        )
    }
}

@Composable
private fun SelectionBar(viewModel: MounvixViewModel, state: ExplorerState, onDialog: (ExplorerDialog) -> Unit) {
    val single = state.selection.size == 1
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 12.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically
    ) {
        Text(
            text = "Выбрано: ${state.selection.size}",
            style = MaterialTheme.typography.bodyMedium,
            modifier = Modifier.weight(1f)
        )
        IconButton(onClick = { viewModel.copySelected() }) {
            Icon(Icons.Filled.ContentCopy, contentDescription = "Копировать")
        }
        IconButton(onClick = { viewModel.cutSelected() }) {
            Icon(Icons.Filled.ContentCut, contentDescription = "Вырезать")
        }
        if (state.clipboard != null) {
            IconButton(onClick = { viewModel.paste() }) {
                Icon(Icons.AutoMirrored.Filled.InsertDriveFile, contentDescription = "Вставить в эту папку")
            }
        }
        if (single) {
            val selected = state.entries.firstOrNull { it.path in state.selection }
            if (selected != null) {
                val isFav = selected.docKey != null && selected.docKey in state.favoriteKeys
                IconButton(onClick = { onDialog(ExplorerDialog.Info(selected, isFav)) }) {
                    Icon(Icons.Filled.Info, contentDescription = "Информация")
                }
                IconButton(onClick = { onDialog(ExplorerDialog.Rename(selected)) }) {
                    Icon(Icons.Filled.Edit, contentDescription = "Переименовать")
                }
            }
        }
        if (single) {
            val sel = state.entries.firstOrNull { it.path in state.selection }
            if (sel != null && sel.type == FileType.FILE) {
                if (state.remoteMode) {
                    IconButton(onClick = { viewModel.saveSelectedToDownloads() }) {
                        Icon(Icons.Filled.Download, contentDescription = "Скачать в Загрузки")
                    }
                }
                IconButton(onClick = { if (viewModel.openWith(sel)) viewModel.clearSelection() }) {
                    Icon(Icons.Filled.OpenInNew, contentDescription = "Открыть")
                }
            }
        }
        IconButton(onClick = { onDialog(ExplorerDialog.Delete(state.selection.size)) }) {
            Icon(Icons.Filled.Delete, contentDescription = "Удалить выбранное")
        }
        TextButton(onClick = { viewModel.clearSelection() }) { Text("Готово") }
    }
}

@Composable
private fun ClipboardBar(viewModel: MounvixViewModel, count: Int, cut: Boolean) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 12.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically
    ) {
        Text(
            text = "В буфере: $count (${if (cut) "вырезано" else "скопировано"})",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.weight(1f)
        )
        IconButton(onClick = { viewModel.paste() }) {
            Icon(Icons.AutoMirrored.Filled.InsertDriveFile, contentDescription = "Вставить в эту папку")
        }
        IconButton(onClick = { viewModel.clearClipboard() }) {
            Icon(Icons.Filled.Clear, contentDescription = "Очистить буфер")
        }
    }
}

@Composable
private fun CreateBar(onNewFolder: () -> Unit, onNewFile: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 12.dp, vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically
    ) {
        Spacer(Modifier.weight(1f))
        TextButton(onClick = onNewFolder) { Text("Новая папка") }
        TextButton(onClick = onNewFile) { Text("Новый файл") }
    }
}

@Composable
private fun NoRootView(onPick: () -> Unit, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier.fillMaxSize().padding(24.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally
    ) {
        Icon(
            imageVector = Icons.Filled.FolderOff,
            contentDescription = null,
            modifier = Modifier.size(72.dp),
            tint = MaterialTheme.colorScheme.primary
        )
        Text(
            text = "Выберите папку",
            style = MaterialTheme.typography.headlineSmall,
            modifier = Modifier.padding(top = 16.dp)
        )
        Text(
            text = "Файловый менеджер откроет выбранную папку через систему доступа Android (SAF).",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(top = 8.dp),
            textAlign = TextAlign.Center
        )
        OutlinedButton(onClick = onPick, modifier = Modifier.padding(top = 24.dp)) {
            Text("Выбрать папку")
        }
    }
}

@Composable
private fun Breadcrumbs(pathStack: List<String>, onNavigate: (Int) -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState(0))
            .padding(horizontal = 16.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically
    ) {
        Crumb("Mounvix", current = pathStack.isEmpty(), onClick = { onNavigate(0) })
        pathStack.forEachIndexed { i, name ->
            Text(
                text = " / ",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )
            Crumb(name, current = i == pathStack.size - 1, onClick = { onNavigate(i + 1) })
        }
    }
}

@Composable
private fun Crumb(text: String, current: Boolean, onClick: () -> Unit) {
    Text(
        text = text,
        style = MaterialTheme.typography.bodySmall,
        color = if (current) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.primary,
        maxLines = 1,
        modifier = if (current) Modifier else Modifier.clickable(onClick = onClick)
    )
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun EntryRow(
    entry: FileEntry,
    selected: Boolean,
    isFavorite: Boolean,
    onClick: () -> Unit,
    onLongClick: () -> Unit,
    onToggleFavorite: () -> Unit
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 12.dp)
            .combinedClickable(onClick = onClick, onLongClick = onLongClick)
            .background(if (selected) MaterialTheme.colorScheme.primaryContainer else Color.Transparent)
            .padding(vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically
    ) {
        Icon(
            imageVector = entryIcon(entry.name, entry.type),
            contentDescription = null,
            tint = MaterialTheme.colorScheme.primary
        )
        Column(Modifier.padding(start = 12.dp).weight(1f)) {
            Text(
                text = entry.name,
                style = MaterialTheme.typography.bodyLarge,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis
            )
            if (entry.type == FileType.FILE) {
                Text(
                    text = "${formatSize(entry.size)}  •  ${formatDate(entry.modified)}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
            }
        }
        IconButton(onClick = onToggleFavorite) {
            Icon(
                imageVector = if (isFavorite) Icons.Filled.Star else Icons.Outlined.StarBorder,
                contentDescription = if (isFavorite) "Убрать из избранного: ${entry.name}" else "Добавить в избранное: ${entry.name}",
                tint = if (isFavorite) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant
            )
        }
    }
}

@Composable
private fun CreateNameDialog(
    title: String, label: String, confirmLabel: String,
    onDismiss: () -> Unit, onCreate: (String) -> Unit
) {
    var name by remember { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = {
            OutlinedTextField(
                value = name,
                onValueChange = { name = it },
                singleLine = true,
                label = { Text(label) }
            )
        },
        confirmButton = {
            TextButton(onClick = { onCreate(name.trim()) }, enabled = name.isNotBlank()) {
                Text(confirmLabel)
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Отмена") } }
    )
}

@Composable
private fun RenameDialog(entry: FileEntry, onDismiss: () -> Unit, onRename: (String) -> Unit) {
    var name by remember { mutableStateOf(entry.name) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Переименовать") },
        text = {
            OutlinedTextField(
                value = name,
                onValueChange = { name = it },
                singleLine = true,
                label = { Text("Новое имя") }
            )
        },
        confirmButton = {
            TextButton(onClick = { onRename(name.trim()) }, enabled = name.isNotBlank() && name != entry.name) {
                Text("Переименовать")
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Отмена") } }
    )
}
