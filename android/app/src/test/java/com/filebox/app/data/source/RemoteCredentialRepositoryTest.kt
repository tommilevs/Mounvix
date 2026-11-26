package com.mounvix.app.data.source

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import com.mounvix.app.data.feed.ProfileSecretStore
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

@RunWith(RobolectricTestRunner::class)
class RemoteCredentialRepositoryTest {
    private val context = ApplicationProvider.getApplicationContext<Context>()
    private val preferences = context.getSharedPreferences("remote-credential-test", Context.MODE_PRIVATE)
    private lateinit var secretStore: InMemorySecretStore
    private lateinit var repository: RemoteCredentialRepository

    @Before
    fun setUp() {
        preferences.edit().clear().commit()
        secretStore = InMemorySecretStore()
        repository = RemoteCredentialRepository(preferences, secretStore)
    }

    @After
    fun tearDown() {
        preferences.edit().clear().commit()
    }

    @Test
    fun migratesLegacySecretOnlyAfterSecureReadBackAndIsIdempotent() {
        preferences.edit().putString("sftp_pass", "legacy-secret").commit()
        val key = RemoteCredentialKeys.sftp("nas.example", 22, "/files", "alice")

        assertEquals("legacy-secret", repository.read(key, "sftp_pass").getOrThrow())
        assertEquals("legacy-secret", repository.read(key, "sftp_pass").getOrThrow())

        assertFalse(preferences.contains("sftp_pass"))
        assertEquals(1, secretStore.writeCount)
        assertEquals("legacy-secret", secretStore.values[key])
    }

    @Test
    fun secureWriteFailurePreservesLegacySecretAndReturnsFailure() {
        preferences.edit().putString("webdav_pass", "legacy-secret").commit()
        secretStore.failWrite = true
        val key = RemoteCredentialKeys.webDav("https://nas.example/dav", "alice")

        assertTrue(repository.read(key, "webdav_pass").isFailure)

        assertEquals("legacy-secret", preferences.getString("webdav_pass", null))
        assertTrue(secretStore.values.isEmpty())
    }

    @Test
    fun readBackMismatchPreservesLegacySecretAndDoesNotReturnIt() {
        preferences.edit().putString("ai_key", "legacy-secret").commit()
        secretStore.returnWrongValueAfterWrite = true
        val key = RemoteCredentialKeys.ai("https://ai.example/v1")

        assertTrue(repository.read(key, "ai_key").isFailure)

        assertEquals("legacy-secret", preferences.getString("ai_key", null))
    }

    @Test
    fun savingSecretStoresItOnlyInSecureStoreAndSupportsEmptyPassword() {
        val key = RemoteCredentialKeys.smb("nas.example", "media", "workgroup", "alice")

        assertTrue(repository.save(key, "smb_pass", "new-secret") { editor ->
            editor.putString("smb_host", "nas.example")
        }.isSuccess)
        assertFalse(preferences.contains("smb_pass"))
        assertFalse(preferences.all.values.contains("new-secret"))
        assertEquals("nas.example", preferences.getString("smb_host", null))
        assertEquals("new-secret", repository.read(key, "smb_pass").getOrThrow())

        assertTrue(repository.save(key, "smb_pass", "").isSuccess)
        assertEquals("", secretStore.values[key])
        assertEquals("", repository.read(key, "smb_pass").getOrThrow())
    }

    @Test
    fun failedSecureSaveDoesNotCommitProfileSettings() {
        secretStore.failWrite = true
        val key = RemoteCredentialKeys.smb("nas.example", "media", "workgroup", "alice")

        assertTrue(repository.save(key, "smb_pass", "new-secret") { editor ->
            editor.putString("smb_host", "nas.example")
        }.isFailure)

        assertFalse(preferences.contains("smb_host"))
    }

    @Test
    fun conflictingSecureAndLegacyValuesFailClosedAndPreserveLegacyCopy() {
        val key = RemoteCredentialKeys.webDav("https://nas.example/dav", "alice")
        secretStore.values[key] = "secure-secret"
        preferences.edit().putString("webdav_pass", "newer-legacy-secret").commit()

        assertTrue(repository.read(key, "webdav_pass").isFailure)

        assertEquals("newer-legacy-secret", preferences.getString("webdav_pass", null))
        assertEquals("secure-secret", secretStore.values[key])
    }

    @Test
    fun profileKeysAreStableAndDoNotExposeEndpointOrUsername() {
        val first = RemoteCredentialKeys.webDav("HTTPS://NAS.EXAMPLE:443/dav/", "alice")
        val normalized = RemoteCredentialKeys.webDav("https://nas.example/dav", "alice")
        val otherUser = RemoteCredentialKeys.webDav("https://nas.example/dav", "bob")

        assertEquals(first, normalized)
        assertFalse(first == otherUser)
        assertFalse(first.contains("nas.example"))
        assertFalse(first.contains("alice"))
    }
}

private class InMemorySecretStore : ProfileSecretStore {
    val values = mutableMapOf<String, String>()
    var writeCount = 0
    var failWrite = false
    var returnWrongValueAfterWrite = false

    override fun write(profileKey: String, plaintext: String): Result<Unit> {
        writeCount++
        if (failWrite) return Result.failure(IllegalStateException("injected write failure"))
        values[profileKey] = plaintext
        return Result.success(Unit)
    }

    override fun read(profileKey: String): Result<String?> {
        val value = values[profileKey] ?: return Result.success(null)
        return Result.success(if (returnWrongValueAfterWrite) "wrong-secret" else value)
    }

    override fun delete(profileKey: String): Result<Unit> {
        values.remove(profileKey)
        return Result.success(Unit)
    }
}
