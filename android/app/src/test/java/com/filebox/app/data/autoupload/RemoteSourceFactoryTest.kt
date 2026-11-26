package com.mounvix.app.data.autoupload

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import com.mounvix.app.data.feed.ProfileSecretStore
import com.mounvix.app.data.source.RemoteCredentialKeys
import com.mounvix.app.data.source.RemoteCredentialRepository
import com.mounvix.app.data.source.WebDavStorageSource
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

@RunWith(RobolectricTestRunner::class)
class RemoteSourceFactoryTest {
    private val context = ApplicationProvider.getApplicationContext<Context>()
    private val preferences = context.getSharedPreferences("remote-source-factory-test", Context.MODE_PRIVATE)
    private val secretStore = InMemorySecretStore()

    @Before
    fun setUp() {
        preferences.edit().clear().commit()
    }

    @After
    fun tearDown() {
        preferences.edit().clear().commit()
    }

    @Test
    fun sourceCreationMigratesLegacyWebDavSecretBeforeReturningSource() {
        val url = "https://nas.example/dav"
        val user = "alice"
        preferences.edit()
            .putString("webdav_url", url)
            .putString("webdav_user", user)
            .putString("webdav_pass", "legacy-secret")
            .commit()

        val source = RemoteSourceFactory.fromPrefs(
            preferences,
            RemoteCredentialRepository(preferences, secretStore),
        )

        assertTrue(source is WebDavStorageSource)
        assertFalse(preferences.contains("webdav_pass"))
        assertEquals("legacy-secret", secretStore.values[RemoteCredentialKeys.webDav(url, user)])
    }

    @Test
    fun legacyIdentityReconstructionNeverNeedsOrDeletesTheSecret() {
        preferences.edit()
            .putString("webdav_url", "https://nas.example/dav")
            .putString("webdav_user", "alice")
            .putString("webdav_pass", "legacy-secret")
            .commit()

        val identity = RemoteSourceFactory.legacyReceiverIdentity(preferences)

        assertNotNull(identity)
        assertEquals("legacy-secret", preferences.getString("webdav_pass", null))
        assertTrue(secretStore.values.isEmpty())
    }

    private class InMemorySecretStore : ProfileSecretStore {
        val values = mutableMapOf<String, String>()

        override fun write(profileKey: String, plaintext: String): Result<Unit> {
            values[profileKey] = plaintext
            return Result.success(Unit)
        }

        override fun read(profileKey: String): Result<String?> = Result.success(values[profileKey])

        override fun delete(profileKey: String): Result<Unit> {
            values.remove(profileKey)
            return Result.success(Unit)
        }
    }
}
