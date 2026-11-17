package com.mounvix.app.data.model

/** Правила именования для новых файлов/папок (SAF и локальный источник). */
object FileNameRules {

    /** Возвращает текст ошибки или null, если имя допустимо. */
    fun validate(name: String): String? {
        if (name.isEmpty()) return "Введите имя"
        if ("/\\".any { name.contains(it) }) return "Символы / и \\ недопустимы в имени"
        if (name.length > 255) return "Имя слишком длинное"
        if (name == "." || name == "..") return "Недопустимое имя"
        return null
    }
}
