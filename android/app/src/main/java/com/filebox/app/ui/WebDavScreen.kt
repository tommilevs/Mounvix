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
