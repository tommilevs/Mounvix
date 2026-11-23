package com.mounvix.app.data.feed

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.util.UUID
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class AndroidKeystoreSecretStoreTest {
    @Test
    fun encryptDecryptAndDeleteUsesPlatformAndroidKeystore() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val store = NativeProfileSecretStore(context)
        val profileKey = "instrumentation-${UUID.randomUUID()}"
        val plaintext = "keystore-test-secret-${UUID.randomUUID()}"
        val secretDirectory = File(context.noBackupFilesDir, "mounvix-profile-secrets")
        val filesBefore = secretDirectory.listFiles()?.toSet().orEmpty()
        var createdFile: File? = null

        try {
            store.write(profileKey, plaintext).getOrThrow()
            assertEquals(plaintext, store.read(profileKey).getOrThrow())

            val createdFiles = secretDirectory.listFiles().orEmpty().filterNot { it in filesBefore }
            assertEquals(1, createdFiles.size)
            createdFile = createdFiles.single()
            assertFalse(createdFile.name.contains(profileKey))
            assertFalse(String(createdFile.readBytes(), Charsets.UTF_8).contains(plaintext))
        } finally {
            store.delete(profileKey).getOrThrow()
        }

        assertEquals(null, store.read(profileKey).getOrThrow())
        assertTrue(createdFile?.exists() == false)
        assertEquals(filesBefore, secretDirectory.listFiles()?.toSet().orEmpty())
    }
}
