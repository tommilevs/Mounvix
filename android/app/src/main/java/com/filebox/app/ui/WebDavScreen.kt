package com.mounvix.app.ui

import android.content.Context
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
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
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
import com.mounvix.app.data.net.UpdateChecker
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
    val scope = rememberCoroutineScope()

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
                                if (u.available) "Доступна версия " + u.tag + ": " + u.url
                                else "У вас последняя версия (" + BuildConfig.VERSION_NAME + ")"
                            },
                            { e -> "Ошибка проверки: " + e.message }
                        )
                    }
                },
                modifier = Modifier.fillMaxWidth()
            ) { Text(if (busyUpdt) "Проверка…" else "Проверить обновления") }
            Text(
                "Для своего сервера: http://IP:48100/dav/<корень>/; " +
                    "Keenetic native WebDAV обычно …/webdav/. Простой режим не требует установленного Mounvix.",
                style = MaterialTheme.typography.bodySmall
            )
            Text("Доступы: Mounvix по умолчанию admin / 1234567890 (смените в панели сервера).",
                style = MaterialTheme.typography.bodySmall)
        }
    }
}
