package com.mounvix.app.data.source

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File

class LocalStorageSourceStableKeyTest {

    @get:Rule
    val tmp = TemporaryFolder()

    private fun src(dir: File) = LocalStorageSource(dir)

    @Test
    fun rootIdIsLocalAbsoluteRoot() {
        val dir = tmp.newFolder("root")
        assertEquals("local:" + dir.absolutePath, src(dir).rootId())
    }

    @Test
    fun stableKeyOfRootIsAbsolutePath() {
        val dir = tmp.newFolder("root2")
        val s = src(dir)
        assertEquals(dir.absolutePath, s.stableKey(""))
        assertFalse(s.rootId().isNullOrBlank())
    }

    @Test
    fun stableKeyOfFileResolvesWhenExists() {
        val dir = tmp.newFolder("root3")
        File(dir, "a.txt").createNewFile()
        File(dir, "sub").mkdirs()
        File(dir, "sub/b.bin").createNewFile()
        val s = src(dir)
        assertEquals(File(dir, "a.txt").absolutePath, s.stableKey("a.txt"))
        assertEquals(File(File(dir, "sub"), "b.bin").absolutePath, s.stableKey("sub/b.bin"))
    }

    @Test
    fun stableKeyOfFileMissingIsNull() {
        val dir = tmp.newFolder("root4")
        assertNull(src(dir).stableKey("nope.txt"))
        assertNull(src(dir).stableKey("deep/missing/x.dat"))
    }

    @Test
    fun listDirCarriesDocKeys() {
        val dir = tmp.newFolder("root5")
        File(dir, "one.txt").createNewFile()
        val entries = src(dir).listDir("")
        assertEquals(1, entries.size)
        assertEquals(File(dir, "one.txt").absolutePath, entries[0].docKey)
    }
}
