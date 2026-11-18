package com.mounvix.app.data.autoupload

import org.junit.Assert.assertEquals
import org.junit.Test

class AutouploadSummaryTest {

    @Test
    fun summaryVariants() {
        assertEquals("Новых файлов нет", AutouploadSummary.text(0, 0))
        assertEquals("Загружено файлов: 3", AutouploadSummary.text(3, 0))
        assertEquals("Ошибок: 2", AutouploadSummary.text(0, 2))
        assertEquals("Загружено: 5, ошибок: 1", AutouploadSummary.text(5, 1))
    }
}
