package com.mounvix.app.data.autoupload

/** Текст итога автозагрузки (также используется в уведомлении). */
object AutouploadSummary {
    fun text(uploaded: Int, errors: Int): String = when {
        uploaded == 0 && errors == 0 -> "Новых файлов нет"
        errors == 0 -> "Загружено файлов: $uploaded"
        uploaded == 0 -> "Ошибок: $errors"
        else -> "Загружено: $uploaded, ошибок: $errors"
    }
}
