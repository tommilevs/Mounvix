package com.mounvix.app.data.model

import org.junit.Assert.assertNull
import org.junit.Assert.assertNotNull
import org.junit.Test

class FileNameRulesTest {

    @Test
    fun validNamesPass() {
        assertNull(FileNameRules.validate("notes.txt"))
        assertNull(FileNameRules.validate("Фото 2024 (1).JPG"))
        assertNull(FileNameRules.validate(".hidden")) // допустимо: не "." и не ".."
    }

    @Test
    fun emptyRejected() {
        assertNotNull(FileNameRules.validate(""))
    }

    @Test
    fun slashesRejected() {
        assertNotNull(FileNameRules.validate("a/b"))
        assertNotNull(FileNameRules.validate("a\\b"))
    }

    @Test
    fun tooLongRejected() {
        assertNotNull(FileNameRules.validate("x".repeat(256)))
        assertNull(FileNameRules.validate("x".repeat(255)))
    }

    @Test
    fun dotsRejected() {
        assertNotNull(FileNameRules.validate("."))
        assertNotNull(FileNameRules.validate(".."))
    }
}
