package com.mounvix.app.data.feed

import java.io.File
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

class NativeProfileSecretStoreTest {
    @get:Rule
    val temporaryFolder = TemporaryFolder()

    @Test
    fun writeReadAndDeleteRoundTripUsesOpaqueFileName() {
        val directory = temporaryFolder.newFolder("secrets")
        val store = NativeProfileSecretStore(directory, TestCipher())
        val profileKey = "https://nas.example/mounvix/\nalice"

        assertTrue(store.write(profileKey, "correct horse battery staple").isSuccess)
        assertEquals("correct horse battery staple", store.read(profileKey).getOrThrow())

        val files = directory.listFiles().orEmpty()
        assertEquals(1, files.size)
        assertFalse(files.single().name.contains("nas.example"))
        assertFalse(files.single().name.contains("alice"))
        assertFalse(String(files.single().readBytes()).contains("correct horse battery staple"))

        assertTrue(store.delete(profileKey).isSuccess)
        assertEquals(null, store.read(profileKey).getOrThrow())
    }

    @Test
    fun corruptedCiphertextReturnsFailureWithoutDeletingFile() {
        val directory = temporaryFolder.newFolder("corrupt")
        val store = NativeProfileSecretStore(directory, TestCipher())
        val profileKey = "profile-key"
        store.write(profileKey, "secret").getOrThrow()
        val file = directory.listFiles().orEmpty().single()
        file.writeBytes(byteArrayOf(1, 2, 3))

        val result = store.read(profileKey)

        assertTrue(result.isFailure)
        assertTrue(file.exists())
        assertArrayEquals(byteArrayOf(1, 2, 3), file.readBytes())
    }

    @Test
    fun blankProfileKeyIsRejected() {
        val store = NativeProfileSecretStore(temporaryFolder.newFolder("blank"), TestCipher())

        assertTrue(store.write(" ", "secret").isFailure)
        assertTrue(store.read("").isFailure)
        assertTrue(store.delete("\n").isFailure)
    }

    private class TestCipher : ProfileSecretCipher {
        private val marker = byteArrayOf(0x52, 0x13, 0x29)

        override fun encrypt(plaintext: ByteArray, associatedData: ByteArray): EncryptedProfileSecret {
            return EncryptedProfileSecret(
                iv = byteArrayOf(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12),
                ciphertext = plaintext.mapIndexed { index, byte -> (byte.toInt() xor marker[index % marker.size].toInt()).toByte() }.toByteArray(),
            )
        }

        override fun decrypt(secret: EncryptedProfileSecret, associatedData: ByteArray): ByteArray {
            if (!secret.iv.contentEquals(byteArrayOf(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12))) error("invalid IV")
            return secret.ciphertext.mapIndexed { index, byte -> (byte.toInt() xor marker[index % marker.size].toInt()).toByte() }.toByteArray()
        }
    }
}
