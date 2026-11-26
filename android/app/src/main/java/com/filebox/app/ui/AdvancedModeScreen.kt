package com.mounvix.app.ui

import android.content.Intent
import android.net.Uri
import android.webkit.WebView
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import com.mounvix.app.data.feed.FeedProfile
import com.mounvix.app.data.feed.ServerEventForegroundState

/** Full server UI in a same-origin WebView; native profile credentials are never injected. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AdvancedModeScreen(
    profile: FeedProfile,
    onExit: () -> Unit,
    modifier: Modifier = Modifier,
    targetUrl: String = profile.baseUrl,
    onTargetUrlChanged: (String) -> Unit = {},
) {
    val context = LocalContext.current
    val lifecycleOwner = LocalLifecycleOwner.current
    val currentExit = rememberUpdatedState(onExit)
    var webView by remember { mutableStateOf<WebView?>(null) }
    var loadError by rememberSaveable(profile.baseUrl) { mutableStateOf<String?>(null) }
    var webViewGeneration by rememberSaveable(profile.baseUrl) { mutableIntStateOf(0) }
    var pendingDocumentResult by remember { mutableStateOf<((Uri?) -> Unit)?>(null) }

    val documentPicker = rememberLauncherForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
        val complete = pendingDocumentResult
        pendingDocumentResult = null
        complete?.invoke(if (result.resultCode == android.app.Activity.RESULT_OK) result.data?.data else null)
    }

    DisposableEffect(lifecycleOwner) {
        val observer = LifecycleEventObserver { _, event ->
            when (event) {
                Lifecycle.Event.ON_RESUME -> ServerEventForegroundState.setAdvancedModeForeground(true)
                Lifecycle.Event.ON_PAUSE,
                Lifecycle.Event.ON_STOP -> ServerEventForegroundState.setAdvancedModeForeground(false)
                else -> Unit
            }
        }
        lifecycleOwner.lifecycle.addObserver(observer)
        if (lifecycleOwner.lifecycle.currentState.isAtLeast(Lifecycle.State.RESUMED)) {
            ServerEventForegroundState.setAdvancedModeForeground(true)
        }
        onDispose {
            lifecycleOwner.lifecycle.removeObserver(observer)
            ServerEventForegroundState.setAdvancedModeForeground(false)
            pendingDocumentResult?.invoke(null)
            pendingDocumentResult = null
        }
    }

    val exitOrBack = {
        AdvancedModeBackNavigation.handle(
            canGoBack = webView?.canGoBack() == true,
            goBack = { webView?.goBack() },
            exitAdvancedMode = { currentExit.value() },
        )
    }
    BackHandler(onBack = exitOrBack)

    Scaffold(
        modifier = modifier,
        topBar = {
            Column {
                TopAppBar(
                    title = { Text("Advanced Mode") },
                    navigationIcon = {
                        TextButton(onClick = exitOrBack) { Text("Назад") }
                    },
                )
                if (profile.baseUrl.startsWith("http://", ignoreCase = true)) {
                    Text(
                        "Незашифрованное HTTP-соединение: пароль и сессия передаются без шифрования. Не используйте в публичной сети.",
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 4.dp),
                        color = MaterialTheme.colorScheme.error,
                        style = MaterialTheme.typography.bodySmall,
                    )
                }
                Text(
                    "Панель сервера Mounvix. Войдите на странице сервера; пароль нативного профиля сюда не передаётся.",
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 4.dp),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                loadError?.let { errorText ->
                    Text(
                        "Не удалось загрузить панель: $errorText",
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 4.dp),
                        color = MaterialTheme.colorScheme.error,
                    )
                    Button(
                        onClick = {
                            loadError = null
                            webViewGeneration += 1
                        },
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp),
                    ) { Text("Повторить") }
                }
            }
        },
    ) { padding ->
        Column(modifier = Modifier.fillMaxSize().padding(padding)) {
            key(webViewGeneration) {
                AdvancedModeWebView(
                    baseUrl = profile.baseUrl,
                    targetUrl = targetUrl,
                    onOpenExternal = { url ->
                        runCatching {
                            context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
                        }.onFailure {
                            loadError = "не удалось открыть внешнюю ссылку"
                        }
                    },
                    onChooseDocument = { intent, complete ->
                        pendingDocumentResult?.invoke(null)
                        pendingDocumentResult = complete
                        runCatching { documentPicker.launch(intent) }.onFailure {
                            pendingDocumentResult = null
                            complete(null)
                            loadError = "не удалось открыть выбор файла"
                        }
                    },
                    onWebViewChanged = { webView = it },
                    onLoadError = { loadError = it },
                    onLoadFinished = { loadError = null },
                    onRendererGone = {
                        webView = null
                        loadError = "процесс WebView остановился. Повторите загрузку или вернитесь в нативный режим."
                    },
                    onMainFrameNavigation = onTargetUrlChanged,
                    modifier = Modifier.weight(1f).fillMaxWidth(),
                )
            }
        }
    }
}
