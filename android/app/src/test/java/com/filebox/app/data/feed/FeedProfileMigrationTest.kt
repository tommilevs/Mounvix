package com.mounvix.app.data.feed

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import okhttp3.OkHttpClient
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

@RunWith(RobolectricTestRunner::class)
class FeedProfileMigrationTest {
    private val context = ApplicationProvider.getApplicationContext<Context>()
    private val preferences = context.getSharedPreferences("feed-profile-migration-test", Context.MODE_PRIVATE)
    private lateinit var secretStore: InMemoryProfileSecretStore
    private lateinit var repository: FeedRepository

    @Before
    fun setUp() {
        preferences.edit().clear().commit()
        secretStore = InMemoryProfileSecretStore()
        repository = FeedRepository(
            api = FeedApi(OkHttpClient.Builder().cookieJar(FeedCookieJar()).build()),
            preferences = preferences,
            secretStore = secretStore,
        )
    }

    @After
    fun tearDown() {
        preferences.edit().clear().commit()
    }

    @Test
    fun migratesLegacyPasswordAfterSecureReadBackAndIsIdempotent() {
        preferences.edit()
            .putString("feed_profile_base_url", " HTTPS://nas.example:443/mounvix/// ")
            .putString("feed_profile_username", " alice ")
            .putString("feed_profile_password", "legacy-secret")
            .commit()

        val first = repository.loadProfile()
        val second = repository.loadProfile()

        assertEquals(FeedProfile("https://nas.example/mounvix/", "alice", "legacy-secret"), first)
        assertEquals(first, second)
        assertFalse(preferences.contains("feed_profile_password"))
        assertEquals(1, secretStore.writeCount)
        assertEquals(1, secretStore.values.size)
        assertFalse(secretStore.values.keys.single().contains("nas.example"))
        assertFalse(secretStore.values.keys.single().contains("alice"))
    }

    @Test
    fun migrationWriteFailureKeepsLegacySecretAndDisablesProfile() {
        preferences.edit()
            .putString("feed_profile_base_url", "https://nas.example/mounvix")
            .putString("feed_profile_username", "alice")
            .putString("feed_profile_password", "legacy-secret")
            .commit()
        secretStore.failWrite = true

        assertEquals(null, repository.loadProfile())

        assertTrue(preferences.contains("feed_profile_password"))
        assertTrue(repository.hasStoredProfileIdentity())
    }

    @Test
    fun secureReadBackMismatchKeepsLegacySecretAndDoesNotReturnProfile() {
        preferences.edit()
            .putString("feed_profile_base_url", "https://nas.example/mounvix")
            .putString("feed_profile_username", "alice")
            .putString("feed_profile_password", "legacy-secret")
            .commit()
        secretStore.returnWrongValueAfterWrite = true

        assertEquals(null, repository.loadProfile())

        assertEquals("legacy-secret", preferences.getString("feed_profile_password", null))
        assertTrue(repository.hasStoredProfileIdentity())
    }

    @Test
    fun secureSaveNeverStoresPlaintextAndPreservesBasePathPrefix() {
        val result = repository.saveProfile(
            FeedProfile(" HTTPS://NAS.EXAMPLE:443/mounvix/// ", " alice ", "new-secret")
        )

        assertTrue(result.isSuccess)
        assertEquals("https://nas.example/mounvix/", preferences.getString("feed_profile_base_url", null))
        assertEquals("alice", preferences.getString("feed_profile_username", null))
        assertFalse(preferences.contains("feed_profile_password"))
        assertFalse(preferences.getAll().values.contains("new-secret"))
        assertEquals("new-secret", repository.loadProfile()?.password)
    }

    @Test
    fun unsafeProfileUrlsAreRejected() {
        val invalid = listOf(
            "ftp://nas.example/mounvix",
            "https://alice:secret@nas.example/mounvix",
            "https://nas.example/mounvix?token=x",
            "https://nas.example/mounvix#fragment",
        )

        for (url in invalid) {
            assertTrue("expected rejection for $url", repository.saveProfile(FeedProfile(url, "alice", "secret")).isFailure)
        }
    }
}

internal class InMemoryProfileSecretStore : ProfileSecretStore {
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
