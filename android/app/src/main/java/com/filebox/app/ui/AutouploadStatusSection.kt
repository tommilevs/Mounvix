package com.mounvix.app.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.work.WorkInfo
import java.text.DateFormat
import java.util.Date

/** Live and durable autoupload state embedded in the existing server-settings screen. */
@Composable
fun AutouploadStatusSection(
    state: AutouploadUiState,
    modifier: Modifier = Modifier
) {
    Column(modifier = modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text("Работа: ${state.workState.label()}", style = MaterialTheme.typography.bodySmall)
        Text("Последний запуск: ${state.runOutcome.label()}", style = MaterialTheme.typography.bodySmall)
        if (state.progress) {
            LinearProgressIndicator(modifier = Modifier.fillMaxWidth())
        }
        if (state.shouldShowCounters()) {
            Text(
                "Загружено: ${state.uploadedCount}, ошибок: ${state.errorCount}",
                style = MaterialTheme.typography.bodySmall
            )
        }
        state.lastUpdatedMs?.let { updatedAt ->
            Text(
                "Обновлено: ${DateFormat.getDateTimeInstance(DateFormat.SHORT, DateFormat.SHORT).format(Date(updatedAt))}",
                style = MaterialTheme.typography.bodySmall
            )
        }
        state.missingAction?.let { action ->
            Text(action, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
        }
        if (state.legacyCutoffWarning) {
            Text(
                "Старый рубеж автозагрузки не удалось связать с выбранным сервером. Ранее существующие файлы могут быть загружены повторно.",
                color = MaterialTheme.colorScheme.error,
                style = MaterialTheme.typography.bodySmall
            )
        }
    }
}

private fun WorkInfo.State?.label(): String = when (this) {
    WorkInfo.State.ENQUEUED -> "В очереди"
    WorkInfo.State.RUNNING -> "Выполняется"
    WorkInfo.State.BLOCKED -> "Ожидает"
    WorkInfo.State.SUCCEEDED -> "Завершена"
    WorkInfo.State.FAILED -> "Завершилась с ошибкой"
    WorkInfo.State.CANCELLED -> "Отменена"
    null -> "Не запускалась"
}

private fun String?.label(): String = when (this) {
    "QUEUED" -> "В очереди"
    "RUNNING" -> "Выполняется"
    "WAITING_FOR_WIFI" -> "Ожидание Wi-Fi"
    "SUCCESS" -> "Успешно"
    "ERROR" -> "Ошибка"
    "CANCELLED" -> "Отменено"
    null -> "Нет результата"
    else -> "Состояние недоступно"
}

internal fun AutouploadUiState.shouldShowCounters(): Boolean =
    workState != null || runOutcome != null || uploadedCount > 0 || errorCount > 0
