package com.mounvix.app.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.PlayCircle
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.unit.dp
import coil.compose.AsyncImage

/** Галерея текущей папки: сетка превью, видео-плейсхолдер, открытие по тапу. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun GalleryScreen(viewModel: MounvixViewModel, onBack: () -> Unit) {
    val ui by viewModel.galleryUi.collectAsState()
    val state by viewModel.state.collectAsState()
    LaunchedEffect(Unit) { viewModel.loadGallery() }
    Column(Modifier.fillMaxSize()) {
        TopAppBar(
            title = { Text("Галерея") },
            navigationIcon = {
                IconButton(onClick = onBack) {
                    Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Назад")
                }
            }
        )
        if (ui.items.isEmpty()) {
            val msg = when {
                ui.loading -> "Загрузка…"
                !state.hasSource -> "Сначала подключитесь к папке или серверу"
                else -> "В этой папке нет фотографий и видео"
            }
            Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                Text(msg, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        } else {
            LazyVerticalGrid(
                columns = GridCells.Fixed(3),
                modifier = Modifier.fillMaxSize(),
                contentPadding = PaddingValues(2.dp)
            ) {
                items(ui.items) { item ->
                    Box(
                        Modifier
                            .aspectRatio(1f)
                            .padding(2.dp)
                            .clip(RoundedCornerShape(6.dp))
                            .clickable { viewModel.openGalleryItem(item) }
                    ) {
                        AsyncImage(
                            model = item.file,
                            contentDescription = item.entry.name,
                            contentScale = ContentScale.Crop,
                            modifier = Modifier.fillMaxSize()
                        )
                        if (item.video) {
                            Icon(
                                Icons.Filled.PlayCircle,
                                contentDescription = "Видео",
                                modifier = Modifier.align(Alignment.Center).size(40.dp),
                                tint = Color.White
                            )
                        }
                    }
                }
            }
        }
    }
}
