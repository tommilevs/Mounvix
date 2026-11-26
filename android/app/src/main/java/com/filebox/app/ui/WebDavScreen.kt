package com.mounvix.app.ui

import android.content.Context
import android.content.Intent
import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.Switch
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.collectAsState
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import com.mounvix.app.BuildConfig
import com.mounvix.app.data.autoupload.AutouploadScheduler
import com.mounvix.app.data.net.UpdateChecker
import com.mounvix.app.data.update.ApkDownloader
import com.mounvix.app.data.update.ApkInstaller
import java.io.File
import com.mounvix.app.data.feed.FeedProfile
import com.mounvix.app.data.source.SftpHostKeyFingerprint
import com.mounvix.app.data.source.WebDavServer
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun WebDavScreen(
    viewModel: MounvixViewModel,
    onConnected: () -> Unit,
    onOpenAdvancedMode: (FeedProfile) -> Unit = {},
    onProfileDisabled: () -> Unit = {},
) {
    val context = LocalContext.current
    val prefs = remember { context.getSharedPreferences("mounvix_prefs", Context.MODE_PRIVATE) }
    var url by rememberSaveable {
        mutableStateOf(prefs.getString("webdav_url", "http://192.168.160.1:48100/dav/share/") ?: "")
    }
    var user by rememberSaveable { mutableStateOf(prefs.getString("webdav_user", "admin") ?: "") }
    var pass by remember { mutableStateOf("") }
    var trust by rememberSaveable { mutableStateOf(prefs.getBoolean("webdav_trust", true)) }
    var err by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    var smbHost by rememberSaveable { mutableStateOf(prefs.getString("smb_host", "192.168.160.2") ?: "") }
    var smbShare by rememberSaveable { mutableStateOf(prefs.getString("smb_share", "all") ?: "") }
    var smbDomain by rememberSaveable { mutableStateOf(prefs.getString("smb_domain", "") ?: "") }
    var smbUser by rememberSaveable { mutableStateOf(prefs.getString("smb_user", "admin") ?: "") }
    var smbPass by remember { mutableStateOf("") }
    var errSmb by remember { mutableStateOf<String?>(null) }
    var busySmb by remember { mutableStateOf(false) }
    var updateMsg by remember { mutableStateOf<String?>(null) }
    var busyUpdt by remember { mutableStateOf(false) }
    var updateApk by remember { mutableStateOf<String?>(null) }
    var sftpHost by rememberSaveable { mutableStateOf(prefs.getString("sftp_host", "192.168.160.1") ?: "") }
    var sftpPort by rememberSaveable { mutableStateOf(prefs.getInt("sftp_port", 222).toString()) }
    var sftpUser by rememberSaveable { mutableStateOf(prefs.getString("sftp_user", "root") ?: "") }
    var sftpPass by remember { mutableStateOf("") }
    var sftpBase by rememberSaveable { mutableStateOf(prefs.getString("sftp_base", "/") ?: "/") }
    var sftpHostKeyFingerprint by rememberSaveable {
        mutableStateOf(prefs.getString("sftp_host_key_fingerprint", "") ?: "")
    }
    var errSftp by remember { mutableStateOf<String?>(null) }
    var busySftp by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val transfers by viewModel.transfers.collectAsState()
    val autouploadStatusViewModel: AutouploadStatusViewModel = viewModel(
        factory = AutouploadStatusViewModel.Factory(context)
    )
    val autouploadStatus by autouploadStatusViewModel.uiState.collectAsStateWithLifecycle()
    var autoOn by rememberSaveable { mutableStateOf(prefs.getBoolean("autoupload_enabled", false)) }
    var autoDest by rememberSaveable { mutableStateOf(prefs.getString("autoupload_dest", "Mounvix") ?: "Mounvix") }
    var autoTreeName by rememberSaveable { mutableStateOf(prefs.getString("autoupload_tree_name", "") ?: "") }
    var aiBase by rememberSaveable { mutableStateOf(prefs.getString("ai_base", "") ?: "") }
    var aiModel by rememberSaveable { mutableStateOf(prefs.getString("ai_model", "") ?: "") }
    var aiKey by remember { mutableStateOf("") }
    var credentialWarning by remember { mutableStateOf<String?>(null) }
    var credentialsLoaded by remember { mutableStateOf(false) }
    LaunchedEffect(viewModel) {
        val stored = withContext(Dispatchers.IO) {
            listOf(
                viewModel.loadWebDavPassword(),
                viewModel.loadSmbPassword(),
                viewModel.loadSftpPassword(),
                viewModel.loadAiKey(),
            )
        }
        pass = stored[0].getOrDefault("")
        smbPass = stored[1].getOrDefault("")
        sftpPass = stored[2].getOrDefault("")
        aiKey = stored[3].getOrDefault("")
        if (stored.any { it.isFailure }) {
            credentialWarning = "Некоторые сохранённые секреты не удалось прочитать безопасно. Введите их заново."
        }
        credentialsLoaded = true
    }
    val pickAutoTree = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocumentTree()) { uri ->
        if (uri != null) {
            try {
                context.contentResolver.takePersistableUriPermission(uri, Intent.FLAG_GRANT_READ_URI_PERMISSION)
            } catch (_: Exception) {
            }
            val nm = uri.lastPathSegment ?: "папка"
            prefs.edit().putString("autoupload_tree", uri.toString())
                .putString("autoupload_tree_name", nm).apply()
            autoTreeName = nm
        }
    }

    Scaffold(topBar = { TopAppBar(title = { Text("Серверы") }) }) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .padding(16.dp)
                .verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(10.dp)
        ) {
            if (!credentialsLoaded) {
                Text("Загрузка защищённых учётных данных…", style = MaterialTheme.typography.bodySmall)
            }
            credentialWarning?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            Text("Подключение к WebDAV", style = MaterialTheme.typography.headlineSmall)
            OutlinedTextField(
                value = url, onValueChange = { url = it },
                label = { Text("URL (WebDAV)") }, singleLine = true,
                modifier = Modifier.fillMaxWidth()
            )
            OutlinedTextField(
                value = user, onValueChange = { user = it },
                label = { Text("Логин") }, singleLine = true,
                modifier = Modifier.fillMaxWidth()
            )
            OutlinedTextField(
                value = pass, onValueChange = { pass = it },
                label = { Text("Пароль") }, singleLine = true,
                visualTransformation = PasswordVisualTransformation(),
                enabled = credentialsLoaded,
                modifier = Modifier.fillMaxWidth()
            )
            Row(verticalAlignment = Alignment.CenterVertically) {
                Checkbox(checked = trust, onCheckedChange = { trust = it })
                Text("Принимать самоподписанный сертификат")
            }
            err?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            Button(
                enabled = !busy && credentialsLoaded,
                onClick = {
                    busy = true
                    err = null
                    val server = WebDavServer(
                        baseUrl = url.trim(), user = user.trim(), password = pass, trustAll = true
                    )
                    scope.launch {
                        val ok = withContext(Dispatchers.IO) {
                            viewModel.connectWebDav(server.baseUrl, server.user, server.password, trust)
                        }
                        busy = false
                        if (ok) {
                            onConnected()
                        } else {
                            err = "Не удалось подключиться к WebDAV"
                        }
                    }
                },
                modifier = Modifier.fillMaxWidth()
            ) { Text(if (busy) "Подключение…" else "Подключиться") }
            Text("Подключение к SMB", style = MaterialTheme.typography.headlineSmall)
            OutlinedTextField(value = smbHost, onValueChange = { smbHost = it }, label = { Text("SMB хост") }, singleLine = true, modifier = Modifier.fillMaxWidth())
            OutlinedTextField(value = smbShare, onValueChange = { smbShare = it }, label = { Text("SMB шара") }, singleLine = true, modifier = Modifier.fillMaxWidth())
            OutlinedTextField(value = smbDomain, onValueChange = { smbDomain = it }, label = { Text("Домен (пусто = WORKGROUP)") }, singleLine = true, modifier = Modifier.fillMaxWidth())
            OutlinedTextField(value = smbUser, onValueChange = { smbUser = it }, label = { Text("Логин") }, singleLine = true, modifier = Modifier.fillMaxWidth())
            OutlinedTextField(value = smbPass, onValueChange = { smbPass = it }, label = { Text("Пароль") }, singleLine = true, visualTransformation = PasswordVisualTransformation(), enabled = credentialsLoaded, modifier = Modifier.fillMaxWidth())
            errSmb?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            Button(
                enabled = !busySmb && credentialsLoaded,
                onClick = {
                    busySmb = true
                    errSmb = null
                    scope.launch {
                        val ok = withContext(Dispatchers.IO) {
                            viewModel.connectSmb(smbHost.trim(), smbShare.trim(), smbDomain.trim(), smbUser.trim(), smbPass)
                        }
                        busySmb = false
                        if (ok) {
                            onConnected()
                        } else {
                            errSmb = "Не удалось подключиться к SMB"
                        }
                    }
                },
                modifier = Modifier.fillMaxWidth()
            ) { Text(if (busySmb) "Подключение…" else "Подключиться к SMB") }
            Text("Приложение", style = MaterialTheme.typography.headlineSmall)
            updateMsg?.let { Text(it, style = MaterialTheme.typography.bodySmall) }
            Button(
                enabled = !busyUpdt,
                onClick = {
                    busyUpdt = true
                    updateMsg = null
                    scope.launch {
                        val res = withContext(Dispatchers.IO) {
                            runCatching { UpdateChecker().check(BuildConfig.VERSION_NAME) }
                        }
                        busyUpdt = false
                        updateMsg = res.fold(
                            { u ->
                                if (u.available) {
                                    updateApk = u.apkUrl.takeIf { it.isNotBlank() }
                                    "Доступна версия " + u.tag + ": " + u.url
                                } else {
                                    updateApk = null
                                    "У вас последняя версия (" + BuildConfig.VERSION_NAME + ")"
                                }
                            },
                            { e ->
                                updateApk = null
                                "Ошибка проверки: " + e.message
                            }
                        )
                    }
                },
                modifier = Modifier.fillMaxWidth()
            ) { Text(if (busyUpdt) "Проверка…" else "Проверить обновления") }
            if (updateApk != null) {
                Button(
                    onClick = {
                        val updateUrl = updateApk ?: return@Button
                        scope.launch {
                            val ok = withContext(Dispatchers.IO) {
                                runCatching {
                                    val f = ApkDownloader().download(updateUrl, File(context.cacheDir, "mounvix-update.apk"))
                                    ApkInstaller().install(context, f)
                                }.getOrDefault(false)
                            }
                            Toast.makeText(
                                context,
                                if (ok) "Запуск установки…" else "Не удалось скачать обновление",
                                Toast.LENGTH_SHORT
                            ).show()
                        }
                    },
                    modifier = Modifier.fillMaxWidth()
                ) { Text("Скачать и установить обновление") }
            }
            Text("Подключение к SFTP", style = MaterialTheme.typography.headlineSmall)
            OutlinedTextField(value = sftpHost, onValueChange = { sftpHost = it }, label = { Text("SFTP хост") }, singleLine = true, modifier = Modifier.fillMaxWidth())
            OutlinedTextField(value = sftpPort, onValueChange = { sftpPort = it }, label = { Text("Порт") }, singleLine = true, modifier = Modifier.fillMaxWidth())
            OutlinedTextField(value = sftpUser, onValueChange = { sftpUser = it }, label = { Text("Логин") }, singleLine = true, modifier = Modifier.fillMaxWidth())
            OutlinedTextField(
                value = sftpHostKeyFingerprint,
                onValueChange = { sftpHostKeyFingerprint = it },
                label = { Text("Отпечаток ключа сервера (SHA-256)") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Text(
                "Введите SHA-256 отпечаток открытого SSH-ключа, сверенный с доверенным источником. Без закрепления ключа подключение запрещено.",
                style = MaterialTheme.typography.bodySmall,
            )
            OutlinedTextField(value = sftpPass, onValueChange = { sftpPass = it }, label = { Text("Пароль") }, singleLine = true, visualTransformation = PasswordVisualTransformation(), enabled = credentialsLoaded, modifier = Modifier.fillMaxWidth())
            OutlinedTextField(value = sftpBase, onValueChange = { sftpBase = it }, label = { Text("Базовый путь") }, singleLine = true, modifier = Modifier.fillMaxWidth())
            errSftp?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            Button(
                enabled = !busySftp && credentialsLoaded,
                onClick = {
                    val fingerprint = sftpHostKeyFingerprint.trim()
                    if (!SftpHostKeyFingerprint.isValid(fingerprint)) {
                        errSftp = "Укажите проверенный SHA-256 отпечаток ключа SFTP-сервера"
                    } else {
                        busySftp = true
                        errSftp = null
                        val port = sftpPort.trim().toIntOrNull() ?: 22
                        val base = sftpBase.trim().ifEmpty { "/" }
                        scope.launch {
                            val ok = withContext(Dispatchers.IO) {
                                viewModel.connectSftp(
                                    sftpHost.trim(), port, sftpUser.trim(), sftpPass, base, fingerprint
                                )
                            }
                            busySftp = false
                            if (ok) {
                                onConnected()
                            } else {
                                errSftp = "Не удалось подключиться к SFTP; проверьте адрес, учётные данные и закреплённый ключ"
                            }
                        }
                    }
                },
                modifier = Modifier.fillMaxWidth()
            ) { Text(if (busySftp) "Подключение…" else "Подключиться к SFTP") }
            Text("Передачи", style = MaterialTheme.typography.headlineSmall)
            if (transfers.isEmpty()) {
                Text("Пока нет передач", style = MaterialTheme.typography.bodySmall)
            }
            transfers.takeLast(5).forEach { t ->
                Column(modifier = Modifier.fillMaxWidth()) {
                    Text(t.name + " — " + t.state, style = MaterialTheme.typography.bodySmall)
                    val p = if (t.total > 0) (t.done.toFloat() / t.total.toFloat()).coerceIn(0f, 1f) else 0f
                    LinearProgressIndicator(progress = { p }, modifier = Modifier.fillMaxWidth())
                }
            }
            Button(onClick = { viewModel.uploadTestFile() }, modifier = Modifier.fillMaxWidth()) {
                Text("Тест загрузки в текущую папку")
            }
            Text("Автозагрузка фото и видео", style = MaterialTheme.typography.headlineSmall)
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("Включена")
                Switch(
                    checked = autoOn,
                    onCheckedChange = { v ->
                        autoOn = v
                        prefs.edit().putBoolean("autoupload_enabled", v).apply()
                        if (v) AutouploadScheduler.schedule(context) else AutouploadScheduler.cancel(context)
                    }
                )
            }
            Text(if (autoTreeName.isBlank()) "Папка не выбрана" else "Папка: $autoTreeName",
                style = MaterialTheme.typography.bodySmall)
            Button(onClick = { pickAutoTree.launch(null) }, modifier = Modifier.fillMaxWidth()) {
                Text("Выбрать папку для автозагрузки")
            }
            OutlinedTextField(
                value = autoDest, onValueChange = { autoDest = it },
                label = { Text("Папка назначения на сервере") }, singleLine = true,
                modifier = Modifier.fillMaxWidth()
            )
            Button(onClick = {
                prefs.edit().putString("autoupload_dest", autoDest).apply()
                AutouploadScheduler.runNow(context)
                Toast.makeText(context, "Автозагрузка запущена", Toast.LENGTH_SHORT).show()
            }, modifier = Modifier.fillMaxWidth()) { Text("Запустить автозагрузку сейчас") }
            AutouploadStatusSection(autouploadStatus)
            Text("AI-распознавание", style = MaterialTheme.typography.headlineSmall)
            OutlinedTextField(
                value = aiBase, onValueChange = { aiBase = it },
                label = { Text("Base URL (OpenAI-совместимый)") }, singleLine = true,
                modifier = Modifier.fillMaxWidth()
            )
            OutlinedTextField(
                value = aiModel, onValueChange = { aiModel = it },
                label = { Text("Модель") }, singleLine = true,
                modifier = Modifier.fillMaxWidth()
            )
            OutlinedTextField(
                value = aiKey, onValueChange = { aiKey = it },
                label = { Text("API key (необяз.)") }, singleLine = true,
                visualTransformation = PasswordVisualTransformation(),
                enabled = credentialsLoaded,
                modifier = Modifier.fillMaxWidth()
            )
            Button(enabled = credentialsLoaded, onClick = {
                scope.launch {
                    val saved = withContext(Dispatchers.IO) {
                        viewModel.saveAiSettings(aiBase, aiModel, aiKey)
                    }
                    if (saved) {
                        Toast.makeText(context, "AI сохранён", Toast.LENGTH_SHORT).show()
                    } else {
                        credentialWarning = "Не удалось безопасно сохранить ключ AI; прежние настройки оставлены."
                    }
                }
            }, modifier = Modifier.fillMaxWidth()) { Text("Сохранить AI") }
            Text(
                "Для своего сервера: http://IP:48100/dav/<корень>/; " +
                    "Keenetic native WebDAV обычно …/webdav/. Простой режим не требует установленного Mounvix.",
                style = MaterialTheme.typography.bodySmall
            )
            Text("Доступы: Mounvix по умолчанию admin / 1234567890 (смените в панели сервера).",
                style = MaterialTheme.typography.bodySmall)

            FeedProfileSection(
                onOpenAdvancedMode = onOpenAdvancedMode,
                onProfileDisabled = onProfileDisabled,
            )
        }
    }
}
