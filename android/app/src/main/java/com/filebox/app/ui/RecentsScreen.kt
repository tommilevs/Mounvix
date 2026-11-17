package com.mounvix.app.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
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
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.mounvix.app.data.model.formatDate

/** Вкладка «Недавние»: история открытых файлов (Room, лимит ~100). */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun RecentsScreen(viewModel: MounvixViewModel) {
    val ui by viewModel.recentsUi.collectAsState()
    var confirmClear by remember { mutableStateOf(false) }
    var itemToRemove by remember { mutableStateOf<RecentItem?>(null) }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Недавние") },
                actions = {
                    if (ui.items.isNotEmpty()) {
                        IconButton(onClick = { confirmClear = true }) {
                            Icon(Icons.Filled.Delete, contentDescription = "Очистить историю")
                        }
                    }
                }
            )
        }
    ) { padding ->
        when {
            ui.loading && !ui.loaded -> LoadingBox(Modifier.fillMaxSize().padding(padding))
            ui.error != null -> ErrorBox(
                text = ui.error.orEmpty(),
                onRetry = { viewModel.loadRecents(force = true) },
                modifier = Modifier.fillMaxSize().padding(padding)
            )
            ui.items.isEmpty() -> EmptyBox(
                text = "Здесь появятся недавно открытые файлы.",
                modifier = Modifier.fillMaxSize().padding(padding)
            )
            else -> LazyColumn(
                modifier = Modifier.fillMaxSize().padding(padding),
                contentPadding = PaddingValues(vertical = 8.dp)
            ) {
                items(ui.items, key = { it.rec.id }) { item ->
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable { viewModel.openSaved(item.rec.treeId, item.rec.relPath) }
                            .padding(horizontal = 12.dp, vertical = 8.dp),
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Icon(
                            imageVector = entryIcon(item.rec.name, com.mounvix.app.data.model.FileType.FILE),
                            contentDescription = null,
                            tint = MaterialTheme.colorScheme.primary
                        )
                        Column(Modifier.padding(start = 12.dp).weight(1f)) {
                            Text(
                                text = item.rec.name,
                                style = MaterialTheme.typography.bodyLarge,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis
                            )
                            if (item.available) {
                                Text(
                                    text = "открыт: ${formatDate(item.rec.openedAt)}",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis
                                )
                            } else {
                                Text(
                                    text = "недоступно (файл удалён или не открыт)",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.error,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis
                                )
                            }
                        }
                        IconButton(onClick = { itemToRemove = item }) {
                            Icon(
                                Icons.Filled.Delete,
                                contentDescription = "Удалить «${item.rec.name}» из истории",
                                tint = MaterialTheme.colorScheme.onSurfaceVariant
                            )
                        }
                    }
                }
            }
        }
    }

    if (confirmClear) {
        AlertDialog(
            onDismissRequest = { confirmClear = false },
            title = { Text("Очистить историю") },
            text = { Text("Удалить все недавние файлы? Файлы на диске не удаляются.") },
            confirmButton = {
                TextButton(onClick = { viewModel.clearRecents(); confirmClear = false }) { Text("Очистить") }
            },
            dismissButton = {
                TextButton(onClick = { confirmClear = false }) { Text("Отмена") }
            }
        )
    }

    itemToRemove?.let { item ->
        AlertDialog(
            onDismissRequest = { itemToRemove = null },
            title = { Text("Удалить запись") },
            text = { Text("Убрать «${item.rec.name}» из недавних? Файл на диске остаётся.") },
            confirmButton = {
                TextButton(onClick = { viewModel.removeRecent(item); itemToRemove = null }) { Text("Удалить") }
            },
            dismissButton = {
                TextButton(onClick = { itemToRemove = null }) { Text("Отмена") }
            }
        )
    }
}
