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
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.collectAsState
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
import com.mounvix.app.data.source.WebDavServer
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun WebDavScreen(viewModel: MounvixViewModel, onConnected: () -> Unit) {
    val context = LocalContext.current
    val prefs = remember { context.getSharedPreferences("mounvix_prefs", Context.MODE_PRIVATE) }
    var url by rememberSaveable {
        mutableStateOf(prefs.getString("webdav_url", "http://192.168.160.1:48100/dav/share/") ?: "")
    }
    var user by rememberSaveable { mutableStateOf(prefs.getString("webdav_user", "admin") ?: "") }
    var pass by rememberSaveable { mutableStateOf(prefs.getString("webdav_pass", "") ?: "") }
    var trust by rememberSaveable { mutableStateOf(prefs.getBoolean("webdav_trust", true)) }
    var err by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    var smbHost by rememberSaveable { mutableStateOf(prefs.getString("smb_host", "192.168.160.2") ?: "") }
    var smbShare by rememberSaveable { mutableStateOf(prefs.getString("smb_share", "all") ?: "") }
    var smbDomain by rememberSaveable { mutableStateOf(prefs.getString("smb_domain", "") ?: "") }
    var smbUser by rememberSaveable { mutableStateOf(prefs.getString("smb_user", "admin") ?: "") }
    var smbPass by rememberSaveable { mutableStateOf(prefs.getString("smb_pass", "") ?: "") }
    var errSmb by remember { mutableStateOf<String?>(null) }
    var busySmb by remember { mutableStateOf(false) }
    var updateMsg by remember { mutableStateOf<String?>(null) }
    var busyUpdt by remember { mutableStateOf(false) }
    var updateApk by remember { mutableStateOf<String?>(null) }
    var sftpHost by rememberSaveable { mutableStateOf(prefs.getString("sftp_host", "192.168.160.1") ?: "") }
    var sftpPort by rememberSaveable { mutableStateOf(prefs.getInt("sftp_port", 222).toString()) }
    var sftpUser by rememberSaveable { mutableStateOf(prefs.getString("sftp_user", "root") ?: "") }
    var sftpPass by rememberSaveable { mutableStateOf(prefs.getString("sftp_pass", "") ?: "") }
    var sftpBase by rememberSaveable { mutableStateOf(prefs.getString("sftp_base", "/") ?: "/") }
    var errSftp by remember { mutableStateOf<String?>(null) }
    var busySftp by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val transfers by viewModel.transfers.collectAsState()
    var autoOn by rememberSaveable { mutableStateOf(prefs.getBoolean("autoupload_enabled", false)) }
    var autoDest by rememberSaveable { mutableStateOf(prefs.getString("autoupload_dest", "Mounvix") ?: "Mounvix") }
    var autoTreeName by rememberSaveable { mutableStateOf(prefs.getString("autoupload_tree_name", "") ?: "") }
    var autoStatus by remember { mutableStateOf(prefs.getString("autoupload_status", "—") ?: "—") }
    var aiBase by rememberSaveable { mutableStateOf(prefs.getString("ai_base", "") ?: "") }
    var aiModel by rememberSaveable { mutableStateOf(prefs.getString("ai_model", "") ?: "") }
    var aiKey by rememberSaveable { mutableStateOf(prefs.getString("ai_key", "") ?: "") }
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
                modifier = Modifier.fillMaxWidth()
            )
            Row(verticalAlignment = Alignment.CenterVertically) {
                Checkbox(checked = trust, onCheckedChange = { trust = it })
                Text("Принимать самоподписанный сертификат")
            }
            err?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            Button(
                enabled = !busy,
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
                            prefs.edit()
                                .putString("webdav_url", url.trim())
                                .putString("webdav_user", user.trim())
                                .putString("webdav_pass", pass)
                                .putBoolean("webdav_trust", trust)
                                .apply()
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
            OutlinedTextField(value = smbPass, onValueChange = { smbPass = it }, label = { Text("Пароль") }, singleLine = true, visualTransformation = PasswordVisualTransformation(), modifier = Modifier.fillMaxWidth())
            errSmb?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            Button(
                enabled = !busySmb,
                onClick = {
                    busySmb = true
                    errSmb = null
                    scope.launch {
                        val ok = withContext(Dispatchers.IO) {
                            viewModel.connectSmb(smbHost.trim(), smbShare.trim(), smbDomain.trim(), smbUser.trim(), smbPass)
                        }
                        busySmb = false
                        if (ok) {
                            prefs.edit()
                                .putString("smb_host", smbHost.trim())
                                .putString("smb_share", smbShare.trim())
                                .putString("smb_domain", smbDomain.trim())
                                .putString("smb_user", smbUser.trim())
                                .putString("smb_pass", smbPass)
                                .apply()
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
                        val url = updateApk ?: return@Button
                        scope.launch {
                            val ok = withContext(Dispatchers.IO) {
                                runCatching {
                                    val f = ApkDownloader().download(url, File(context.cacheDir, "mounvix-update.apk"))
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
            OutlinedTextField(value = sftpPass, onValueChange = { sftpPass = it }, label = { Text("Пароль") }, singleLine = true, visualTransformation = PasswordVisualTransformation(), modifier = Modifier.fillMaxWidth())
            OutlinedTextField(value = sftpBase, onValueChange = { sftpBase = it }, label = { Text("Базовый путь") }, singleLine = true, modifier = Modifier.fillMaxWidth())
            errSftp?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            Button(
                enabled = !busySftp,
                onClick = {
                    busySftp = true
                    errSftp = null
                    val port = sftpPort.trim().toIntOrNull() ?: 22
                    val base = sftpBase.trim().ifEmpty { "/" }
                    scope.launch {
                        val ok = withContext(Dispatchers.IO) {
                            viewModel.connectSftp(sftpHost.trim(), port, sftpUser.trim(), sftpPass, base)
                        }
                        busySftp = false
                        if (ok) {
                            prefs.edit()
                                .putString("sftp_host", sftpHost.trim())
                                .putInt("sftp_port", port)
                                .putString("sftp_user", sftpUser.trim())
                                .putString("sftp_pass", sftpPass)
                                .putString("sftp_base", base)
                                .apply()
                            onConnected()
                        } else {
                            errSftp = "Не удалось подключиться к SFTP"
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
            Text("Статус: $autoStatus", style = MaterialTheme.typography.bodySmall)
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
                modifier = Modifier.fillMaxWidth()
            )
            Button(onClick = {
                prefs.edit().putString("ai_base", aiBase.trim())
                    .putString("ai_model", aiModel.trim())
                    .putString("ai_key", aiKey).apply()
                Toast.makeText(context, "AI сохранён", Toast.LENGTH_SHORT).show()
            }, modifier = Modifier.fillMaxWidth()) { Text("Сохранить AI") }
            Text(
                "Для своего сервера: http://IP:48100/dav/<корень>/; " +
                    "Keenetic native WebDAV обычно …/webdav/. Простой режим не требует установленного Mounvix.",
                style = MaterialTheme.typography.bodySmall
            )
            Text("Доступы: Mounvix по умолчанию admin / 1234567890 (смените в панели сервера).",
                style = MaterialTheme.typography.bodySmall)

            FeedProfileSection()
        }
    }
}
