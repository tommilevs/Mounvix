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
import androidx.compose.runtime.remember
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
fun MounvixRoot(
    notificationOpenRequest: Int = 0,
    serverEventOpenRequest: Int = 0,
    serverEventTargetRoute: String? = null,
) {
    val context = LocalContext.current
    val viewModel: MounvixViewModel = viewModel { MounvixViewModel(context.applicationContext) }
    val profileRepository = remember { FeedRepository.from(context.applicationContext) }
    val feedViewModel: FeedViewModel = viewModel { FeedViewModel(profileRepository) }
    val preferences = remember { context.getSharedPreferences("mounvix_prefs", android.content.Context.MODE_PRIVATE) }
    val initialProfile = remember { profileRepository.loadProfile() }
    var activeProfile by remember { mutableStateOf(initialProfile) }
    val notifPerm = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { }
    var tab by rememberSaveable { mutableStateOf("files") }
    var advancedMode by rememberSaveable {
        mutableStateOf(AdvancedModePreferences.isEnabled(preferences) && initialProfile != null)
    }
    var advancedTargetUrl by rememberSaveable { mutableStateOf<String?>(null) }

    fun exitAdvancedMode() {
        AdvancedModePreferences.setEnabled(preferences, false)
        advancedMode = false
        advancedTargetUrl = null
        tab = "servers"
    }

    fun openAdvancedMode(profile: com.mounvix.app.data.feed.FeedProfile, requestedTarget: String? = null) {
        val currentProfile = profileRepository.loadProfile() ?: run {
            activeProfile = null
            tab = "servers"
            return
        }
        if (currentProfile.baseUrl != profile.baseUrl || currentProfile.username != profile.username) {
            tab = "servers"
            return
        }
        val resolvedTarget = when {
            requestedTarget.isNullOrBlank() -> currentProfile.baseUrl
            requestedTarget.startsWith('/') -> AdvancedModeUrlPolicy.resolveInternalRoute(currentProfile.baseUrl, requestedTarget)
            AdvancedModeUrlPolicy.decide(currentProfile.baseUrl, requestedTarget, false, false) == NavigationDecision.ALLOW_INTERNAL -> requestedTarget
            else -> null
        }
        if (resolvedTarget == null) {
            tab = "servers"
            return
        }
        activeProfile = currentProfile
        advancedTargetUrl = resolvedTarget
        AdvancedModePreferences.setEnabled(preferences, true)
        advancedMode = true
    }

    LaunchedEffect(Unit) {
        if (Build.VERSION.SDK_INT >= 33 &&
            ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            notifPerm.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }
    LaunchedEffect(initialProfile) {
        if (initialProfile == null && AdvancedModePreferences.isEnabled(preferences)) {
            AdvancedModePreferences.setEnabled(preferences, false)
            tab = "servers"
        }
    }
    LaunchedEffect(notificationOpenRequest) {
        if (notificationOpenRequest > 0) {
            exitAdvancedMode()
            tab = "memories"
            feedViewModel.load()
        }
    }
    LaunchedEffect(serverEventOpenRequest, serverEventTargetRoute) {
        if (serverEventOpenRequest > 0 && !serverEventTargetRoute.isNullOrBlank()) {
            val profile = profileRepository.loadProfile()
            if (profile != null) openAdvancedMode(profile, serverEventTargetRoute)
            else tab = "servers"
        }
    }

    Scaffold(
        bottomBar = {
            if (!advancedMode) NavigationBar {
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
            if (advancedMode) {
                val profile = activeProfile
                if (profile != null) {
                    AdvancedModeScreen(
                        profile = profile,
                        targetUrl = advancedTargetUrl ?: profile.baseUrl,
                        onExit = ::exitAdvancedMode,
                        onTargetUrlChanged = { currentUrl ->
                            if (AdvancedModeUrlPolicy.decide(
                                    profile.baseUrl,
                                    currentUrl,
                                    isRedirect = false,
                                    hasUserGesture = false,
                                ) == NavigationDecision.ALLOW_INTERNAL
                            ) {
                                advancedTargetUrl = currentUrl
                            }
                        },
                    )
                } else {
                    WebDavScreen(
                        viewModel = viewModel,
                        onConnected = { tab = "files" },
                        onOpenAdvancedMode = { savedProfile -> openAdvancedMode(savedProfile) },
                        onProfileDisabled = { activeProfile = null; exitAdvancedMode() },
                    )
                }
            } else {
                when (tab) {
                    "files" -> ExplorerScreen(viewModel, onOpenGallery = { tab = "gallery"; viewModel.loadGallery() })
                    "favorites" -> FavoritesScreen(
                        viewModel = viewModel,
                        onGoToFiles = { tab = "files" },
                    )
                    "recents" -> RecentsScreen(viewModel)
                    "gallery" -> GalleryScreen(viewModel, onBack = { tab = "files" })
                    "memories" -> FeedScreen(feedViewModel, onOpenSettings = { tab = "servers" })
                    "servers" -> WebDavScreen(
                        viewModel = viewModel,
                        onConnected = { tab = "files" },
                        onOpenAdvancedMode = { savedProfile -> openAdvancedMode(savedProfile) },
                        onProfileDisabled = { activeProfile = null; exitAdvancedMode() },
                    )
                    else -> WebDavScreen(
                        viewModel = viewModel,
                        onConnected = { tab = "files" },
                        onOpenAdvancedMode = { savedProfile -> openAdvancedMode(savedProfile) },
                        onProfileDisabled = { activeProfile = null; exitAdvancedMode() },
                    )
                }
            }
        }
    }
}
