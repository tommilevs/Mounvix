package com.mounvix.app.ui

import android.content.Context
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.TextButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
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
import com.mounvix.app.data.db.AppDatabase
import com.mounvix.app.data.feed.ServerEventAccessStatus
import com.mounvix.app.data.feed.ServerEventCapabilityProbe
import com.mounvix.app.data.feed.ServerEventCapabilityProbeResult
import com.mounvix.app.data.feed.ServerEventForegroundState
import com.mounvix.app.data.feed.ServerEventRepository
import com.mounvix.app.data.feed.ServerEventScheduler
import com.mounvix.app.data.feed.ServerEventStatusStore
import com.mounvix.app.data.feed.normalizeMounvixBaseUrl
import java.time.LocalTime
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Mounvix API profile and local notification preferences, appended to Servers settings. */
@Composable
fun FeedProfileSection(
    modifier: Modifier = Modifier,
    onOpenAdvancedMode: (FeedProfile) -> Unit = {},
    onProfileDisabled: () -> Unit = {},
) {
    val context = LocalContext.current
    val preferences = remember {
        context.getSharedPreferences("mounvix_prefs", Context.MODE_PRIVATE)
    }
    val repository = remember { FeedRepository.from(context.applicationContext) }
    val initialProfile = remember { repository.loadProfile() }
    val savedIdentity = initialProfile?.let { it.baseUrl to it.username }
    val serverEventStatusStore = remember { ServerEventStatusStore(preferences) }
    val scope = rememberCoroutineScope()

    var baseUrl by rememberSaveable { mutableStateOf(savedIdentity?.first.orEmpty()) }
    var username by rememberSaveable { mutableStateOf(savedIdentity?.second.orEmpty()) }
    var password by remember { mutableStateOf("") }
    var hasProfile by rememberSaveable { mutableStateOf(savedIdentity != null) }
    var needsReauthentication by rememberSaveable {
        mutableStateOf(savedIdentity == null && repository.hasStoredProfileIdentity())
    }
    var notificationsEnabled by rememberSaveable {
        mutableStateOf(preferences.getBoolean(FeedWorker.NOTIFICATIONS_ENABLED, false))
    }
    var serverEventsEnabled by rememberSaveable {
        mutableStateOf(initialProfile?.let(serverEventStatusStore::isEnabled) ?: false)
    }
    var serverEventAccess by rememberSaveable {
        mutableStateOf(initialProfile?.let { serverEventStatusStore.status(it).access } ?: ServerEventAccessStatus.UNVERIFIED)
    }
    var serverHistoryGap by rememberSaveable {
        mutableStateOf(initialProfile?.let { serverEventStatusStore.status(it).historyGap } ?: false)
    }
    var serverProbeBusy by remember { mutableStateOf(false) }
    var quietStart by rememberSaveable {
        mutableStateOf(preferences.getString(FeedWorker.QUIET_START, "22:00") ?: "22:00")
    }
    var quietEnd by rememberSaveable {
        mutableStateOf(preferences.getString(FeedWorker.QUIET_END, "07:00") ?: "07:00")
    }
    var busy by remember { mutableStateOf(false) }
    var confirmDisableProfile by rememberSaveable { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var message by remember { mutableStateOf<String?>(null) }

    DisposableEffect(Unit) {
        onDispose { password = "" }
    }

    fun updateSchedule(enabled: Boolean) {
        if (enabled && repository.loadProfile() != null) {
            FeedScheduler.schedule(context.applicationContext)
        } else {
            FeedScheduler.cancel(context.applicationContext)
        }
    }

    fun updateServerEventSchedule(profile: FeedProfile?) {
        if (profile == null) {
            ServerEventScheduler.cancel(context.applicationContext)
        } else {
            ServerEventScheduler.schedule(context.applicationContext, profile)
        }
    }

    fun applyServerEventProbeResult(
        profile: FeedProfile,
        result: ServerEventCapabilityProbeResult,
        enableAfterProbe: Boolean,
    ) {
        val state = serverEventStatusStore.status(profile)
        serverEventAccess = state.access
        serverHistoryGap = state.historyGap
        when (result) {
            is ServerEventCapabilityProbeResult.Supported -> {
                if (enableAfterProbe) serverEventStatusStore.setEnabled(profile, true)
                serverEventsEnabled = serverEventStatusStore.isEnabled(profile)
                error = null
                message = if (enableAfterProbe) "Задания сервера включены" else "M7 API доступен"
                if (serverEventStatusStore.canPoll(profile)) {
                    ServerEventScheduler.schedule(context.applicationContext, profile)
                }
            }
            is ServerEventCapabilityProbeResult.Unsupported -> {
                serverEventsEnabled = serverEventStatusStore.isEnabled(profile)
                ServerEventScheduler.cancel(context.applicationContext)
                message = null
                error = "Сервер не поддерживает inbox заданий. Обновите Mounvix и нажмите «Проверить сервер»."
            }
            ServerEventCapabilityProbeResult.NeedsLogin -> {
                serverEventsEnabled = serverEventStatusStore.isEnabled(profile)
                ServerEventScheduler.cancel(context.applicationContext)
                needsReauthentication = true
                message = null
                error = "Требуется повторный вход в Mounvix. Проверьте пароль профиля."
            }
            ServerEventCapabilityProbeResult.Forbidden -> {
                serverEventsEnabled = serverEventStatusStore.isEnabled(profile)
                ServerEventScheduler.cancel(context.applicationContext)
                message = null
                error = "Inbox заданий доступен только администратору сервера."
            }
            is ServerEventCapabilityProbeResult.Unavailable -> {
                serverEventsEnabled = serverEventStatusStore.isEnabled(profile)
                message = null
                error = "Не удалось проверить M7 API: ${result.message}"
            }
        }
    }

    suspend fun probeServer(profile: FeedProfile, enableAfterProbe: Boolean) {
        serverProbeBusy = true
        error = null
        message = null
        try {
            val result = ServerEventCapabilityProbe(FeedApi.shared(), serverEventStatusStore).probe(profile)
            applyServerEventProbeResult(profile, result, enableAfterProbe)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (failure: Exception) {
            error = "Не удалось проверить сервер: ${failure.message ?: "ошибка соединения"}"
        } finally {
            serverProbeBusy = false
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

    fun disableProfile() {
        val profile = repository.loadProfile()
        if (profile == null) {
            error = "Нет проверенного профиля Mounvix для отключения"
            confirmDisableProfile = false
            return
        }
        confirmDisableProfile = false
        busy = true
        error = null
        message = null
        scope.launch {
            try {
                AdvancedModePreferences.setEnabled(preferences, false)
                FeedScheduler.cancel(context.applicationContext)
                ServerEventScheduler.cancel(context.applicationContext)
                ServerEventForegroundState.setAdvancedModeForeground(false)
                val cookieExpired = AdvancedModeSessionCookieCleaner.expire(profile.baseUrl)
                withContext(Dispatchers.IO) {
                    ServerEventRepository(AppDatabase.get(context.applicationContext).serverEventCursorDao())
                        .clearProfile(profile)
                    serverEventStatusStore.clearProfile(profile)
                    repository.clearProfile(profile).getOrThrow()
                }
                baseUrl = ""
                username = ""
                password = ""
                hasProfile = false
                needsReauthentication = false
                serverEventsEnabled = false
                serverEventAccess = ServerEventAccessStatus.UNVERIFIED
                serverHistoryGap = false
                onProfileDisabled()
                message = if (cookieExpired) {
                    "Профиль Mounvix, его секрет и курсоры отключены"
                } else {
                    "Профиль отключён; удаление веб-сессии не удалось подтвердить"
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (failure: Exception) {
                error = "Не удалось отключить профиль Mounvix: ${failure.message ?: "ошибка хранения"}"
            } finally {
                busy = false
            }
        }
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
        if (hasProfile) {
            Text("Сохранённый пароль не отображается; оставьте поле пустым для проверки.", style = MaterialTheme.typography.bodySmall)
        }
        if (needsReauthentication) {
            Text("Пароль не удалось безопасно восстановить. Введите его повторно.", color = MaterialTheme.colorScheme.error)
        }
        Button(
            enabled = !busy,
            onClick = {
                error = null
                message = null
                if (baseUrl.isBlank() || username.isBlank()) {
                    error = "Заполните адрес сервера и логин"
                    return@Button
                }
                val normalizedUrl = runCatching { normalizeMounvixBaseUrl(baseUrl) }.getOrNull()
                if (normalizedUrl == null) {
                    error = "Введите корректный HTTP или HTTPS адрес без логина, query и fragment"
                    return@Button
                }
                if (!saveQuietHours(showMessage = false)) return@Button

                val normalizedUsername = username.trim()
                val existingProfile = repository.loadProfile()
                val reusableProfile = existingProfile?.takeIf {
                    it.baseUrl == normalizedUrl && it.username == normalizedUsername
                }
                val passwordForLogin = password.takeIf(String::isNotBlank) ?: reusableProfile?.password
                if (passwordForLogin.isNullOrBlank()) {
                    error = "Введите пароль для этого профиля"
                    needsReauthentication = repository.hasStoredProfileIdentity()
                    return@Button
                }
                val profile = FeedProfile(normalizedUrl, normalizedUsername, passwordForLogin)
                busy = true
                scope.launch {
                    val result = FeedApi.shared().login(profile)
                    if (result.isSuccess) {
                        val saveResult = repository.saveProfile(profile)
                        if (saveResult.isSuccess) {
                            password = ""
                            hasProfile = true
                            needsReauthentication = false
                            serverEventsEnabled = serverEventStatusStore.isEnabled(profile)
                            serverEventAccess = serverEventStatusStore.status(profile).access
                            serverHistoryGap = serverEventStatusStore.status(profile).historyGap
                            updateSchedule(notificationsEnabled)
                            updateServerEventSchedule(profile)
                            error = null
                            message = "Профиль Mounvix сохранён"
                        } else {
                            repository.loadProfile()
                            message = null
                            error = "Не удалось безопасно сохранить пароль. Повторите ввод и проверку."
                        }
                    } else {
                        repository.loadProfile()
                        message = null
                        error = "Не удалось подключиться к Mounvix. Проверьте адрес и пароль."
                    }
                    busy = false
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

        Text(
            "Нативный режим остаётся режимом по умолчанию. Advanced Mode открывает панель сервера без передачи пароля из профиля.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Button(
            enabled = hasProfile && !busy,
            onClick = {
                val profile = repository.loadProfile()
                if (profile == null) {
                    error = "Сначала подключите профиль Mounvix"
                } else {
                    onOpenAdvancedMode(profile)
                }
            },
            modifier = Modifier.fillMaxWidth(),
        ) { Text("Advanced Mode / Открыть панель сервера") }
        OutlinedButton(
            enabled = hasProfile && !busy,
            onClick = { confirmDisableProfile = true },
            modifier = Modifier.fillMaxWidth(),
        ) { Text("Отключить профиль Mounvix") }

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

        Text("Задания сервера", style = MaterialTheme.typography.titleMedium)
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            Column(Modifier.weight(1f).padding(end = 12.dp)) {
                Text("Уведомления о заданиях сервера")
                Text("Проверка раз в 15 минут, до 500 событий за запуск", style = MaterialTheme.typography.bodySmall)
            }
            Switch(
                checked = serverEventsEnabled,
                enabled = !busy && !serverProbeBusy,
                onCheckedChange = { enabled ->
                    error = null
                    message = null
                    val profile = repository.loadProfile()
                    if (profile == null) {
                        error = if (repository.hasStoredProfileIdentity()) {
                            "Введите пароль профиля для повторной авторизации"
                        } else {
                            "Сначала подключите профиль Mounvix"
                        }
                    } else if (!enabled) {
                        serverEventStatusStore.setEnabled(profile, false)
                        serverEventsEnabled = false
                        updateServerEventSchedule(profile)
                        message = "Уведомления о заданиях выключены; курсор сохранён"
                    } else {
                        scope.launch { probeServer(profile, enableAfterProbe = true) }
                    }
                },
            )
        }
        Button(
            enabled = !busy && !serverProbeBusy,
            onClick = {
                error = null
                message = null
                val profile = repository.loadProfile()
                if (profile == null) {
                    error = if (repository.hasStoredProfileIdentity()) {
                        "Введите пароль профиля для повторной авторизации"
                    } else {
                        "Сначала подключите профиль Mounvix"
                    }
                } else {
                    scope.launch { probeServer(profile, enableAfterProbe = false) }
                }
            },
            modifier = Modifier.fillMaxWidth(),
        ) {
            if (serverProbeBusy) {
                CircularProgressIndicator(modifier = Modifier.size(18.dp), strokeWidth = 2.dp)
            } else {
                Text("Проверить сервер")
            }
        }
        Text(
            text = when (serverEventAccess) {
                ServerEventAccessStatus.UNVERIFIED -> "Серверный inbox ещё не проверен."
                ServerEventAccessStatus.READY -> "Доступ к серверному inbox проверен."
                ServerEventAccessStatus.NEEDS_LOGIN -> "Требуется повторный вход в Mounvix."
                ServerEventAccessStatus.FORBIDDEN -> "Серверный inbox доступен только администратору."
                ServerEventAccessStatus.UNSUPPORTED -> "Эта версия сервера не поддерживает inbox заданий."
            },
            style = MaterialTheme.typography.bodySmall,
            color = if (serverEventAccess == ServerEventAccessStatus.READY) {
                MaterialTheme.colorScheme.onSurfaceVariant
            } else {
                MaterialTheme.colorScheme.error
            },
        )
        if (serverHistoryGap) {
            Text(
                "Сервер удалил часть старой истории. Откройте inbox, чтобы проверить пропуск.",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error,
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
    if (confirmDisableProfile) {
        AlertDialog(
            onDismissRequest = { if (!busy) confirmDisableProfile = false },
            title = { Text("Отключить профиль Mounvix?") },
            text = { Text("Будут удалены сохранённый секрет и курсоры только этого профиля; WebView-сессия будет завершена для настроенного пути сервера.") },
            confirmButton = {
                TextButton(enabled = !busy, onClick = ::disableProfile) { Text("Отключить") }
            },
            dismissButton = {
                TextButton(enabled = !busy, onClick = { confirmDisableProfile = false }) { Text("Отмена") }
            },
        )
    }
}
