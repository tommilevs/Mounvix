package com.mounvix.app.data.source

import com.mounvix.app.data.model.FileType
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

class LocalStorageSourceTest {

    @get:Rule
    val tmp = TemporaryFolder()

    private fun source(): LocalStorageSource {
        val root = tmp.newFolder("root")
        return LocalStorageSource(root)
    }

    @Test
    fun listDirEmpty() {
        val s = source()
        assertTrue(s.listDir("").isEmpty())
    }

    @Test
    fun createAndList() {
        val s = source()
        assertTrue(s.createDir("Docs"))
        assertTrue(s.createFile("Docs/note.txt"))
        val entries = s.listDir("")
        assertEquals(1, entries.size)
        assertEquals("Docs", entries[0].name)
        assertEquals(FileType.FOLDER, entries[0].type)
        val sub = s.listDir("Docs")
        assertEquals(1, sub.size)
        assertEquals("note.txt", sub[0].name)
        assertEquals(FileType.FILE, sub[0].type)
        assertEquals("Docs/note.txt", sub[0].path)
    }

    @Test
    fun renameAndDelete() {
        val s = source()
        assertTrue(s.createDir("A"))
        assertTrue(s.rename("A", "B"))
        assertFalse(s.exists("A"))
        assertTrue(s.exists("B"))
        assertTrue(s.delete("B"))
        assertFalse(s.exists("B"))
    }

    @Test
    fun deleteFile() {
        val s = source()
        assertTrue(s.createFile("f.txt"))
        assertTrue(s.delete("f.txt"))
        assertFalse(s.exists("f.txt"))
    }

    @Test
    fun stat() {
        val s = source()
        assertTrue(s.createFile("f.txt"))
        val st = s.stat("f.txt")
        assertNotNull(st)
        assertEquals("f.txt", st?.name)
        assertEquals(FileType.FILE, st?.type)
        assertNull(s.stat("missing.txt"))
    }

    @Test
    fun listDirMissing() {
        val s = source()
        assertTrue(s.listDir("no/such").isEmpty())
        assertNull(s.stat("no/such"))
    }

    @Test
    fun copyFileAndFolder() {
        val s = source()
        s.createFile("a.txt")
        s.createDir("dir")
        s.createFile("dir/inner.txt")
        assertTrue(s.copy("a.txt", "b.txt"))
        assertTrue(s.exists("b.txt"))
        assertTrue(s.copy("dir", "dir2"))
        assertTrue(s.exists("dir2/inner.txt"))
        // копирование поверх существующего — отказ
        assertFalse(s.copy("a.txt", "b.txt"))
    }

    @Test
    fun moveFileAcrossDirs() {
        val s = source()
        s.createDir("src")
        s.createDir("dst")
        s.createFile("src/f.txt")
        assertTrue(s.move("src/f.txt", "dst/f.txt"))
        assertFalse(s.exists("src/f.txt"))
        assertTrue(s.exists("dst/f.txt"))
    }

    @Test
    fun moveFolderIntoItselfRejected() {
        val s = source()
        s.createDir("a")
        s.createFile("a/x.txt")
        assertFalse(s.move("a", "a/inner"))
        assertTrue(s.exists("a/x.txt"))
    }

    @Test
    fun moveSameNameNoopRejected() {
        val s = source()
        s.createFile("f.txt")
        assertFalse(s.move("f.txt", "f.txt"))
        assertTrue(s.exists("f.txt"))
    }
}
