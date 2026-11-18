package com.mounvix.app.ui

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Cloud
import androidx.compose.material.icons.filled.Folder
import androidx.compose.material.icons.filled.History
import androidx.compose.material.icons.filled.PhotoLibrary
import androidx.compose.material.icons.filled.Star
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.sp
import androidx.core.content.ContextCompat
import androidx.lifecycle.viewmodel.compose.viewModel
import com.mounvix.app.data.feed.FeedRepository

/** Корень приложения: файлы, избранное, недавние, серверы и воспоминания. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MounvixRoot(notificationOpenRequest: Int = 0) {
    val context = LocalContext.current
    val viewModel: MounvixViewModel = viewModel { MounvixViewModel(context.applicationContext) }
    val feedViewModel: FeedViewModel = viewModel {
        FeedViewModel(FeedRepository.from(context.applicationContext))
    }
    val notifPerm = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { }
    var tab by rememberSaveable { mutableStateOf("files") }

    LaunchedEffect(Unit) {
        if (Build.VERSION.SDK_INT >= 33 &&
            ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            notifPerm.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }
    LaunchedEffect(notificationOpenRequest) {
        if (notificationOpenRequest > 0) {
            tab = "memories"
            feedViewModel.load()
        }
    }

    Scaffold(
        bottomBar = {
            NavigationBar {
                NavigationBarItem(
                    selected = tab == "files",
                    onClick = { tab = "files" },
                    icon = { Icon(Icons.Filled.Folder, contentDescription = null) },
                    label = { Text("Файлы") },
                )
                NavigationBarItem(
                    selected = tab == "favorites",
                    onClick = { tab = "favorites"; viewModel.loadFavorites(force = true) },
                    icon = { Icon(Icons.Filled.Star, contentDescription = null) },
                    label = { Text("Избранное") },
                )
                NavigationBarItem(
                    selected = tab == "recents",
                    onClick = { tab = "recents"; viewModel.loadRecents(force = true) },
                    icon = { Icon(Icons.Filled.History, contentDescription = null) },
                    label = { Text("Недавние") },
                )
                NavigationBarItem(
                    selected = tab == "servers",
                    onClick = { tab = "servers" },
                    icon = { Icon(Icons.Filled.Cloud, contentDescription = null) },
                    label = { Text("Серверы") },
                )
                NavigationBarItem(
                    selected = tab == "memories",
                    onClick = { tab = "memories"; feedViewModel.load() },
                    icon = { Icon(Icons.Filled.PhotoLibrary, contentDescription = null) },
                    label = { Text("Воспоминания", fontSize = 10.sp, maxLines = 1, softWrap = false) },
                )
            }
        },
    ) { padding ->
        Box(Modifier.fillMaxSize().padding(padding)) {
            when (tab) {
                "files" -> ExplorerScreen(viewModel, onOpenGallery = { tab = "gallery"; viewModel.loadGallery() })
                "favorites" -> FavoritesScreen(
                    viewModel = viewModel,
                    onGoToFiles = { tab = "files" },
                )
                "recents" -> RecentsScreen(viewModel)
                "gallery" -> GalleryScreen(viewModel, onBack = { tab = "files" })
                "memories" -> FeedScreen(feedViewModel, onOpenSettings = { tab = "servers" })
                else -> WebDavScreen(viewModel, onConnected = { tab = "files" })
            }
        }
    }
}
