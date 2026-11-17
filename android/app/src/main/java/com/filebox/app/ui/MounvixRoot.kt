package com.mounvix.app.ui

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Folder
import androidx.compose.material.icons.filled.Cloud
import androidx.compose.material.icons.filled.History
import androidx.compose.material.icons.filled.Star
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.viewmodel.compose.viewModel

/** Корень приложения: три вкладки Файлы / Избранное / Недавние. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MounvixRoot() {
    val context = LocalContext.current
    val viewModel: MounvixViewModel = viewModel { MounvixViewModel(context.applicationContext) }
    var tab by rememberSaveable { mutableStateOf("files") }

    Scaffold(
        bottomBar = {
            NavigationBar {
                NavigationBarItem(
                    selected = tab == "files",
                    onClick = { tab = "files" },
                    icon = { Icon(Icons.Filled.Folder, contentDescription = null) },
                    label = { Text("Файлы") }
                )
                NavigationBarItem(
                    selected = tab == "favorites",
                    onClick = { tab = "favorites"; viewModel.loadFavorites(force = true) },
                    icon = { Icon(Icons.Filled.Star, contentDescription = null) },
                    label = { Text("Избранное") }
                )
                NavigationBarItem(
                    selected = tab == "recents",
                    onClick = { tab = "recents"; viewModel.loadRecents(force = true) },
                    icon = { Icon(Icons.Filled.History, contentDescription = null) },
                    label = { Text("Недавние") }
                )
                NavigationBarItem(
                    selected = tab == "servers",
                    onClick = { tab = "servers" },
                    icon = { Icon(Icons.Filled.Cloud, contentDescription = null) },
                    label = { Text("Серверы") }
                )
            }
        }
    ) { padding ->
        Box(Modifier.fillMaxSize().padding(padding)) {
            when (tab) {
                "files" -> ExplorerScreen(viewModel)
                "favorites" -> FavoritesScreen(
                    viewModel = viewModel,
                    onGoToFiles = { tab = "files" }
                )
                "recents" -> RecentsScreen(viewModel)
                else -> WebDavScreen(viewModel, onConnected = { tab = "files" })
            }
        }
    }
}
