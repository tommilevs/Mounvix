package com.mounvix.app.data.feed

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import java.io.DataInputStream
import java.io.DataOutputStream
import java.io.File
import java.io.FileInputStream
import java.io.FileOutputStream
import java.nio.charset.StandardCharsets
import java.nio.file.AtomicMoveNotSupportedException
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import java.security.GeneralSecurityException
import java.security.KeyStore
import java.security.MessageDigest
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/** Storage boundary used by the native API profile repository. */
interface ProfileSecretStore {
    fun write(profileKey: String, plaintext: String): Result<Unit>
    fun read(profileKey: String): Result<String?>
    fun delete(profileKey: String): Result<Unit>
}

internal data class EncryptedProfileSecret(val iv: ByteArray, val ciphertext: ByteArray)

internal interface ProfileSecretCipher {
    fun encrypt(plaintext: ByteArray, associatedData: ByteArray): EncryptedProfileSecret
    fun decrypt(secret: EncryptedProfileSecret, associatedData: ByteArray): ByteArray
}

/** AES-GCM encrypted profile secrets stored only in the app's no-backup directory. */
class NativeProfileSecretStore internal constructor(
    private val directory: File,
    private val cipher: ProfileSecretCipher,
) : ProfileSecretStore {
    constructor(context: Context) : this(
        directory = File(context.applicationContext.noBackupFilesDir, DIRECTORY_NAME),
        cipher = AndroidKeystoreProfileSecretCipher(),
    )

    override fun write(profileKey: String, plaintext: String): Result<Unit> = capture {
        require(profileKey.isNotBlank()) { "profile key is required" }
        ensureDirectory()

        val keyBytes = profileKey.toByteArray(StandardCharsets.UTF_8)
        val encrypted = cipher.encrypt(plaintext.toByteArray(StandardCharsets.UTF_8), keyBytes)
        require(encrypted.iv.size == GCM_IV_BYTES) { "invalid encrypted secret IV" }
        require(encrypted.ciphertext.isNotEmpty() && encrypted.ciphertext.size <= MAX_CIPHERTEXT_BYTES) {
            "invalid encrypted secret length"
        }

        val target = fileFor(profileKey)
        val temporary = File.createTempFile("profile-secret-", ".tmp", directory)
        try {
            val output = FileOutputStream(temporary)
            try {
                val data = DataOutputStream(output)
                data.writeInt(FILE_MAGIC)
                data.writeByte(FILE_VERSION)
                data.writeByte(encrypted.iv.size)
                data.writeInt(encrypted.ciphertext.size)
                data.write(encrypted.iv)
                data.write(encrypted.ciphertext)
                data.flush()
                output.fd.sync()
            } finally {
                output.close()
            }
            replaceAtomically(temporary, target)
        } finally {
            if (temporary.exists()) temporary.delete()
        }
    }

    override fun read(profileKey: String): Result<String?> = capture {
        require(profileKey.isNotBlank()) { "profile key is required" }
        val file = fileFor(profileKey)
        if (!file.exists()) return@capture null
        if (!file.isFile || file.length() !in MIN_FILE_BYTES..MAX_FILE_BYTES) {
            throw GeneralSecurityException("invalid encrypted secret file")
        }

        val encrypted = DataInputStream(FileInputStream(file)).use { input ->
            if (input.readInt() != FILE_MAGIC) throw GeneralSecurityException("invalid encrypted secret format")
            if (input.readUnsignedByte() != FILE_VERSION) throw GeneralSecurityException("unsupported encrypted secret version")
            val ivSize = input.readUnsignedByte()
            val ciphertextSize = input.readInt()
            if (ivSize != GCM_IV_BYTES || ciphertextSize !in 1..MAX_CIPHERTEXT_BYTES) {
                throw GeneralSecurityException("invalid encrypted secret length")
            }
            val iv = ByteArray(ivSize)
            input.readFully(iv)
            val ciphertext = ByteArray(ciphertextSize)
            input.readFully(ciphertext)
            if (input.read() != -1) throw GeneralSecurityException("unexpected encrypted secret data")
            EncryptedProfileSecret(iv, ciphertext)
        }

        String(
            cipher.decrypt(encrypted, profileKey.toByteArray(StandardCharsets.UTF_8)),
            StandardCharsets.UTF_8,
        )
    }

    override fun delete(profileKey: String): Result<Unit> = capture {
        require(profileKey.isNotBlank()) { "profile key is required" }
        val file = fileFor(profileKey)
        if (file.exists() && !file.delete()) throw IllegalStateException("could not delete encrypted profile secret")
    }

    private fun ensureDirectory() {
        if (!directory.exists() && !directory.mkdirs() && !directory.isDirectory) {
            throw IllegalStateException("could not create encrypted profile secret directory")
        }
        if (!directory.isDirectory) throw IllegalStateException("encrypted profile secret path is not a directory")
    }

    private fun fileFor(profileKey: String): File {
        val digest = MessageDigest.getInstance("SHA-256")
            .digest(profileKey.toByteArray(StandardCharsets.UTF_8))
            .joinToString(separator = "") { byte -> "%02x".format(byte.toInt() and 0xff) }
        return File(directory, "$digest.bin")
    }

    private fun replaceAtomically(source: File, target: File) {
        try {
            Files.move(
                source.toPath(),
                target.toPath(),
                StandardCopyOption.ATOMIC_MOVE,
                StandardCopyOption.REPLACE_EXISTING,
            )
        } catch (_: AtomicMoveNotSupportedException) {
            Files.move(source.toPath(), target.toPath(), StandardCopyOption.REPLACE_EXISTING)
        }
    }

    private inline fun <T> capture(block: () -> T): Result<T> =
        try {
            Result.success(block())
        } catch (error: Exception) {
            Result.failure(error)
        }

    companion object {
        private const val DIRECTORY_NAME = "mounvix-profile-secrets"
        private const val FILE_MAGIC = 0x4d565053 // MVPS
        private const val FILE_VERSION = 1
        private const val GCM_IV_BYTES = 12
        private const val MAX_CIPHERTEXT_BYTES = 64 * 1024
        private const val HEADER_BYTES = 10
        private const val MIN_FILE_BYTES = HEADER_BYTES + GCM_IV_BYTES + 1L
        private const val MAX_FILE_BYTES = HEADER_BYTES + GCM_IV_BYTES + MAX_CIPHERTEXT_BYTES.toLong()
    }
}

private class AndroidKeystoreProfileSecretCipher : ProfileSecretCipher {
    override fun encrypt(plaintext: ByteArray, associatedData: ByteArray): EncryptedProfileSecret {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, getOrCreateKey())
        cipher.updateAAD(associatedData)
        return EncryptedProfileSecret(cipher.iv, cipher.doFinal(plaintext))
    }

    override fun decrypt(secret: EncryptedProfileSecret, associatedData: ByteArray): ByteArray {
        val key = loadKeyStore().getKey(KEY_ALIAS, null) as? SecretKey
            ?: throw GeneralSecurityException("native profile encryption key is unavailable")
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.DECRYPT_MODE, key, GCMParameterSpec(GCM_TAG_BITS, secret.iv))
        cipher.updateAAD(associatedData)
        return cipher.doFinal(secret.ciphertext)
    }

    private fun getOrCreateKey(): SecretKey {
        val existing = loadKeyStore().getKey(KEY_ALIAS, null) as? SecretKey
        if (existing != null) return existing

        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, ANDROID_KEYSTORE)
        generator.init(
            KeyGenParameterSpec.Builder(
                KEY_ALIAS,
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
            )
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setRandomizedEncryptionRequired(true)
                .build(),
        )
        return generator.generateKey()
    }

    private fun loadKeyStore(): KeyStore = KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }

    private companion object {
        const val ANDROID_KEYSTORE = "AndroidKeyStore"
        const val KEY_ALIAS = "mounvix-native-profile-aes-gcm-v1"
        const val TRANSFORMATION = "AES/GCM/NoPadding"
        const val GCM_TAG_BITS = 128
    }
}
