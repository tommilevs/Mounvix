package com.mounvix.app.ui

import android.content.Context
import android.content.Intent
import android.widget.Toast
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PlayCircle
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.core.content.FileProvider
import coil.ImageLoader
import coil.compose.AsyncImage
import com.mounvix.app.data.feed.FeedApi
import com.mounvix.app.data.feed.FeedCard
import com.mounvix.app.data.feed.FeedMedia
import com.mounvix.app.data.feed.FeedRepository
import java.io.File
import java.net.URI
import java.time.LocalDate
import java.time.format.DateTimeFormatter
import java.util.Locale
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Memories feed with explicit loading, empty, offline, and content states. */
@Composable
fun FeedScreen(viewModel: FeedViewModel, onOpenSettings: () -> Unit) {
    val state by viewModel.uiState.collectAsState()
    val context = LocalContext.current
    val baseUrl = remember(context.applicationContext) {
        FeedRepository.from(context.applicationContext).loadProfile()?.baseUrl
    }
    val imageLoader = remember(context.applicationContext) { FeedImageLoader.get(context) }
    val scope = rememberCoroutineScope()
    var selectedPhoto by remember { mutableStateOf<FeedMedia?>(null) }

    Column(Modifier.fillMaxSize()) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(start = 20.dp, top = 16.dp, end = 12.dp, bottom = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            Text("Воспоминания", style = MaterialTheme.typography.headlineSmall)
            IconButton(onClick = onOpenSettings) {
                Icon(Icons.Filled.Settings, contentDescription = "Настройки серверов")
            }
        }
        when (val current = state) {
            FeedUiState.Loading -> CenterState {
                CircularProgressIndicator()
                Text("Загружаем воспоминания…")
            }
            FeedUiState.ProfileNotConfigured -> CenterState {
                Text("Подключите профиль Mounvix, чтобы открыть воспоминания.")
                Button(onClick = onOpenSettings) { Text("Настроить сервер") }
            }
            FeedUiState.Empty -> CenterState {
                Text("Пока нет воспоминаний")
                Text("Новые подборки появятся после синхронизации с сервером.", color = MaterialTheme.colorScheme.onSurfaceVariant)
                TextButton(onClick = viewModel::retry) {
                    Icon(Icons.Filled.Refresh, contentDescription = null)
                    Text(" Обновить")
                }
            }
            is FeedUiState.Error -> CenterState {
                Text("Не удалось загрузить воспоминания", style = MaterialTheme.typography.titleMedium)
                Text(current.message, color = MaterialTheme.colorScheme.error)
                Button(onClick = viewModel::retry) { Text("Повторить") }
            }
            is FeedUiState.Content -> {
                current.message?.let { message ->
                    Text(
                        message,
                        color = MaterialTheme.colorScheme.error,
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
                    )
                }
                val groups = current.cards.sortedByDescending(FeedCard::generatedAt)
                    .groupBy { it.generatedAt.substringBefore('T') }
                LazyColumn(
                    modifier = Modifier.fillMaxSize(),
                    contentPadding = PaddingValues(bottom = 12.dp),
                ) {
                    groups.forEach { (day, cards) ->
                        item(key = "day-$day") {
                            Text(
                                text = formatFeedDate(day),
                                style = MaterialTheme.typography.titleMedium,
                                fontWeight = FontWeight.SemiBold,
                                modifier = Modifier.fillMaxWidth().padding(start = 20.dp, top = 12.dp, bottom = 4.dp),
                            )
                        }
                        items(cards, key = FeedCard::id) { card ->
                            FeedCardItem(
                                card = card,
                                opened = current.openedCardId == card.id,
                                baseUrl = baseUrl,
                                imageLoader = imageLoader,
                                onOpenCard = { viewModel.openCard(card.id) },
                                onCloseCard = viewModel::closeCard,
                                onOpenMedia = { media ->
                                    viewModel.openCard(card.id)
                                    if (media.kind.equals("video", ignoreCase = true)) {
                                        scope.launch { openFeedVideo(context, media) }
                                    } else {
                                        selectedPhoto = media
                                    }
                                },
                            )
                        }
                    }
                }
            }
        }
    }

    selectedPhoto?.let { media ->
        val originalUrl = resolveMediaUrl(baseUrl, media.fileUrl)
        Dialog(onDismissRequest = { selectedPhoto = null }) {
            Surface(
                shape = RoundedCornerShape(20.dp),
                color = Color.Black,
                modifier = Modifier.fillMaxWidth().heightIn(max = 720.dp),
            ) {
                Box(Modifier.fillMaxWidth()) {
                    if (originalUrl != null) {
                        AsyncImage(
                            model = originalUrl,
                            imageLoader = imageLoader,
                            contentDescription = media.path.substringAfterLast('/'),
                            contentScale = ContentScale.Fit,
                            modifier = Modifier.fillMaxWidth().heightIn(min = 240.dp, max = 680.dp),
                        )
                    }
                    TextButton(
                        onClick = { selectedPhoto = null },
                        modifier = Modifier.align(Alignment.TopEnd).padding(8.dp),
                    ) { Text("Закрыть", color = Color.White) }
                }
            }
        }
    }
}

@Composable
private fun CenterState(content: @Composable ColumnScope.() -> Unit) {
    Column(
        modifier = Modifier.fillMaxSize().padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp, Alignment.CenterVertically),
        horizontalAlignment = Alignment.CenterHorizontally,
        content = content,
    )
}

@Composable
private fun FeedCardItem(
    card: FeedCard,
    opened: Boolean,
    baseUrl: String?,
    imageLoader: ImageLoader,
    onOpenCard: () -> Unit,
    onCloseCard: () -> Unit,
    onOpenMedia: (FeedMedia) -> Unit,
) {
    Card(modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)) {
        Column(Modifier.fillMaxWidth()) {
            Row(
                modifier = Modifier.fillMaxWidth().clickable(onClick = onOpenCard).padding(14.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(Modifier.weight(1f)) {
                    Text(card.title, style = MaterialTheme.typography.titleMedium)
                    Text(
                        text = card.generatedAt.substringAfter('T').take(5),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Text(
                    text = if (card.isRead) "Прочитано" else "Новое",
                    style = MaterialTheme.typography.labelMedium,
                    color = if (card.isRead) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.primary,
                )
            }
            if (card.media.isNotEmpty()) {
                LazyRow(
                    contentPadding = PaddingValues(start = 12.dp, end = 12.dp, bottom = 12.dp),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    items(card.media, key = { "${card.id}:${it.path}" }) { media ->
                        FeedMediaThumbnail(
                            media = media,
                            baseUrl = baseUrl,
                            imageLoader = imageLoader,
                            onClick = { onOpenMedia(media) },
                        )
                    }
                }
            }
            if (opened) {
                TextButton(onClick = onCloseCard, modifier = Modifier.align(Alignment.End)) {
                    Text("Свернуть")
                }
            }
        }
    }
}

@Composable
private fun FeedMediaThumbnail(
    media: FeedMedia,
    baseUrl: String?,
    imageLoader: ImageLoader,
    onClick: () -> Unit,
) {
    val thumbnailUrl = resolveMediaUrl(baseUrl, media.thumbnailUrl)
    Box(
        modifier = Modifier.size(104.dp).clip(RoundedCornerShape(10.dp))
            .background(MaterialTheme.colorScheme.surfaceVariant)
            .clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        if (thumbnailUrl != null) {
            AsyncImage(
                model = thumbnailUrl,
                imageLoader = imageLoader,
                contentDescription = media.path.substringAfterLast('/'),
                contentScale = ContentScale.Crop,
                modifier = Modifier.fillMaxSize(),
            )
        }
        if (media.kind.equals("video", ignoreCase = true)) {
            Icon(
                Icons.Filled.PlayCircle,
                contentDescription = "Видео",
                tint = Color.White,
                modifier = Modifier.size(40.dp),
            )
        }
    }
}

private fun resolveMediaUrl(baseUrl: String?, relativeUrl: String?): String? {
    if (baseUrl.isNullOrBlank() || relativeUrl.isNullOrBlank()) return null
    return runCatching { URI(baseUrl).resolve(relativeUrl).toString() }.getOrNull()
}

private fun formatFeedDate(value: String): String = runCatching {
    val date = LocalDate.parse(value)
    date.format(DateTimeFormatter.ofPattern("d MMMM yyyy", Locale.getDefault()))
}.getOrDefault(value)

private suspend fun openFeedVideo(context: Context, media: FeedMedia) {
    try {
        val bytes = FeedApi.shared().fetchMedia(media.fileUrl)
        val extension = media.path.substringAfterLast('.', "").lowercase(Locale.ROOT)
            .filter { it.isLetterOrDigit() }.take(8)
        val fileName = media.path.substringAfterLast('/').substringAfterLast('\\')
            .replace(Regex("[^A-Za-z0-9._-]"), "_")
            .ifBlank { "memory-video" }
        val directory = File(context.cacheDir, "memories").apply { mkdirs() }
        val file = File(directory, if (extension.isBlank()) fileName else "$fileName.$extension")
        withContext(Dispatchers.IO) { file.writeBytes(bytes) }

        val appContext = context.applicationContext
        val uri = FileProvider.getUriForFile(appContext, "${appContext.packageName}.fileprovider", file)
        val intent = Intent(Intent.ACTION_VIEW).apply {
            setDataAndType(uri, videoMimeType(extension))
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        appContext.startActivity(Intent.createChooser(intent, "Открыть видео").apply {
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        })
    } catch (error: Exception) {
        Toast.makeText(
            context,
            error.message?.takeIf(String::isNotBlank) ?: "Не удалось открыть видео",
            Toast.LENGTH_LONG,
        ).show()
    }
}

private fun videoMimeType(extension: String): String = when (extension) {
    "mp4", "m4v" -> "video/mp4"
    "webm" -> "video/webm"
    "mkv" -> "video/x-matroska"
    "mov" -> "video/quicktime"
    "avi" -> "video/x-msvideo"
    else -> "video/*"
}

private object FeedImageLoader {
    @Volatile
    private var instance: ImageLoader? = null

    fun get(context: Context): ImageLoader = instance ?: synchronized(this) {
        instance ?: ImageLoader.Builder(context.applicationContext)
            .okHttpClient { FeedApi.shared().client }
            .build()
            .also { instance = it }
    }
}
