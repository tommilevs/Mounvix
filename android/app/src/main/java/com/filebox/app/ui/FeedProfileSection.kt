package com.mounvix.app.ui

import android.content.Context
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
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
import com.mounvix.app.data.feed.FeedApi
import com.mounvix.app.data.feed.FeedProfile
import com.mounvix.app.data.feed.FeedRepository
import com.mounvix.app.data.feed.FeedScheduler
import com.mounvix.app.data.feed.FeedWorker
import java.time.LocalTime
import kotlinx.coroutines.launch

/** Mounvix API profile and local notification preferences, appended to Servers settings. */
@Composable
fun FeedProfileSection(modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val preferences = remember {
        context.getSharedPreferences("mounvix_prefs", Context.MODE_PRIVATE)
    }
    val repository = remember { FeedRepository.from(context.applicationContext) }
    val savedProfile = remember { repository.loadProfile() }
    val scope = rememberCoroutineScope()

    var baseUrl by rememberSaveable { mutableStateOf(savedProfile?.baseUrl.orEmpty()) }
    var username by rememberSaveable { mutableStateOf(savedProfile?.username.orEmpty()) }
    var password by rememberSaveable { mutableStateOf(savedProfile?.password.orEmpty()) }
    var hasProfile by rememberSaveable { mutableStateOf(savedProfile != null) }
    var notificationsEnabled by rememberSaveable {
        mutableStateOf(preferences.getBoolean(FeedWorker.NOTIFICATIONS_ENABLED, false))
    }
    var quietStart by rememberSaveable {
        mutableStateOf(preferences.getString(FeedWorker.QUIET_START, "22:00") ?: "22:00")
    }
    var quietEnd by rememberSaveable {
        mutableStateOf(preferences.getString(FeedWorker.QUIET_END, "07:00") ?: "07:00")
    }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var message by remember { mutableStateOf<String?>(null) }

    fun updateSchedule(enabled: Boolean) {
        if (enabled && repository.loadProfile() != null) {
            FeedScheduler.schedule(context.applicationContext)
        } else {
            FeedScheduler.cancel(context.applicationContext)
        }
    }

    fun saveQuietHours(showMessage: Boolean = true): Boolean {
        val start = runCatching { LocalTime.parse(quietStart.trim()) }.getOrNull()
        val end = runCatching { LocalTime.parse(quietEnd.trim()) }.getOrNull()
        if (start == null || end == null) {
            error = "Введите тихие часы в формате HH:mm"
            message = null
            return false
        }
        preferences.edit()
            .putString(FeedWorker.QUIET_START, start.toString())
            .putString(FeedWorker.QUIET_END, end.toString())
            .apply()
        quietStart = start.toString()
        quietEnd = end.toString()
        error = null
        if (showMessage) message = "Тихие часы сохранены"
        return true
    }

    Column(
        modifier = modifier.fillMaxWidth(),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text("Воспоминания на сервере", style = MaterialTheme.typography.headlineSmall)
        Text(
            "Подключите один профиль Mounvix API. Эти данные не меняют WebDAV, SMB или SFTP.",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        OutlinedTextField(
            value = baseUrl,
            onValueChange = { baseUrl = it },
            label = { Text("Адрес сервера, например http://192.168.1.10:48100") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        OutlinedTextField(
            value = username,
            onValueChange = { username = it },
            label = { Text("Логин Mounvix") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        OutlinedTextField(
            value = password,
            onValueChange = { password = it },
            label = { Text("Пароль Mounvix") },
            singleLine = true,
            visualTransformation = PasswordVisualTransformation(),
            modifier = Modifier.fillMaxWidth(),
        )
        Button(
            enabled = !busy,
            onClick = {
                error = null
                message = null
                if (baseUrl.isBlank() || username.isBlank() || password.isBlank()) {
                    error = "Заполните адрес сервера, логин и пароль"
                    return@Button
                }
                if (!saveQuietHours(showMessage = false)) return@Button
                val profile = FeedProfile(baseUrl.trim(), username.trim(), password)
                busy = true
                scope.launch {
                    val result = FeedApi.shared().login(profile)
                    busy = false
                    result.onSuccess {
                        repository.saveProfile(profile)
                        hasProfile = true
                        updateSchedule(notificationsEnabled)
                        error = null
                        message = "Профиль Mounvix сохранён"
                    }.onFailure { failure ->
                        message = null
                        error = failure.message?.takeIf(String::isNotBlank) ?: "Не удалось подключиться к Mounvix"
                    }
                }
            },
            modifier = Modifier.fillMaxWidth(),
        ) {
            if (busy) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    CircularProgressIndicator(modifier = Modifier.size(18.dp), strokeWidth = 2.dp)
                    Text("Проверяем подключение…")
                }
            } else {
                Text(if (hasProfile) "Проверить и сохранить" else "Подключить профиль")
            }
        }

        Text("Локальные уведомления", style = MaterialTheme.typography.titleMedium)
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            Column(Modifier.weight(1f).padding(end = 12.dp)) {
                Text("Новые воспоминания")
                Text("Проверка выполняется не чаще раза в 15 минут", style = MaterialTheme.typography.bodySmall)
            }
            Switch(
                checked = notificationsEnabled,
                onCheckedChange = { enabled ->
                    if (enabled && !hasProfile) {
                        error = "Сначала подключите профиль Mounvix"
                        message = null
                    } else {
                        notificationsEnabled = enabled
                        preferences.edit().putBoolean(FeedWorker.NOTIFICATIONS_ENABLED, enabled).apply()
                        updateSchedule(enabled)
                        error = null
                        message = if (enabled) "Уведомления включены" else "Уведомления выключены"
                    }
                },
            )
        }
        OutlinedTextField(
            value = quietStart,
            onValueChange = { quietStart = it },
            label = { Text("Тихие часы: начало (HH:mm)") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        OutlinedTextField(
            value = quietEnd,
            onValueChange = { quietEnd = it },
            label = { Text("Тихие часы: конец (HH:mm)") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        Button(onClick = { saveQuietHours() }, modifier = Modifier.fillMaxWidth()) {
            Text("Сохранить тихие часы")
        }
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        message?.let { Text(it, color = MaterialTheme.colorScheme.primary) }
    }
}
