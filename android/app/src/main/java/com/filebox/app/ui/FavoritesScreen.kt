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
import androidx.compose.material.icons.filled.Star
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.mounvix.app.data.db.Favorite
import com.mounvix.app.data.model.FileType
import com.mounvix.app.data.model.formatDate
import com.mounvix.app.data.model.formatSize

/** Вкладка «Избранное»: сохранённые файлы и папки (Room, переживают перезапуск). */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FavoritesScreen(viewModel: MounvixViewModel, onGoToFiles: () -> Unit = {}) {
    val ui by viewModel.favoritesUi.collectAsState()

    Scaffold(topBar = { TopAppBar(title = { Text("Избранное") }) }) { padding ->
        when {
            ui.loading && !ui.loaded -> LoadingBox(Modifier.fillMaxSize().padding(padding))
            ui.error != null -> ErrorBox(
                text = ui.error.orEmpty(),
                onRetry = { viewModel.loadFavorites(force = true) },
                modifier = Modifier.fillMaxSize().padding(padding)
            )
            ui.items.isEmpty() -> EmptyBox(
                text = "В избранном пока пусто.\nДобавьте файл или папку звёздочкой во вкладке «Файлы».",
                modifier = Modifier.fillMaxSize().padding(padding)
            )
            else -> LazyColumn(
                modifier = Modifier.fillMaxSize().padding(padding),
                contentPadding = PaddingValues(vertical = 8.dp)
            ) {
                items(ui.items, key = { it.fav.id }) { item ->
                    FavoriteRow(
                        item = item,
                        onOpen = {
                            val ok = viewModel.openSaved(item.fav.treeId, item.fav.relPath)
                            if (ok && item.fav.kind == Favorite.KIND_FOLDER) onGoToFiles()
                        },
                        onRemove = { viewModel.removeSavedFavorite(item) }
                    )
                }
            }
        }
    }
}

@Composable
private fun FavoriteRow(item: FavoriteItem, onOpen: () -> Unit, onRemove: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onOpen)
            .padding(horizontal = 12.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically
    ) {
        Icon(
            imageVector = entryIcon(
                item.fav.name,
                if (item.fav.kind == Favorite.KIND_FOLDER) FileType.FOLDER else FileType.FILE
            ),
            contentDescription = null,
            tint = MaterialTheme.colorScheme.primary
        )
        Column(Modifier.padding(start = 12.dp).weight(1f)) {
            Text(
                text = item.fav.name,
                style = MaterialTheme.typography.bodyLarge,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis
            )
            if (item.available) {
                Text(
                    text = favoriteSubtitle(item),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis
                )
            } else {
                Text(
                    text = "недоступно (объект удалён или не открыт)",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis
                )
            }
        }
        IconButton(onClick = onRemove) {
            Icon(
                Icons.Filled.Star,
                contentDescription = "Убрать «${item.fav.name}» из избранного",
                tint = MaterialTheme.colorScheme.onSurfaceVariant
            )
        }
    }
}

private fun favoriteSubtitle(item: FavoriteItem): String =
    if (item.fav.kind == Favorite.KIND_FOLDER) {
        "папка" + (if (item.fav.relPath.isNotBlank()) ": ${item.fav.relPath}" else "")
    } else {
        "${formatSize(item.fav.size)}  •  ${formatDate(item.fav.modifiedAt)}"
    }
