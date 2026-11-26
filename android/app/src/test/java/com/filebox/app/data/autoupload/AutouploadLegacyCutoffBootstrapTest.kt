package com.mounvix.app.data.autoupload

import android.content.Context
import android.content.SharedPreferences
import android.provider.DocumentsContract
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import com.mounvix.app.data.db.AppDatabase
import com.mounvix.app.data.db.AutouploadLegacyCutoff
import com.mounvix.app.data.feed.ProfileSecretStore
import com.mounvix.app.data.source.RemoteCredentialRepository
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [33])
class AutouploadLegacyCutoffBootstrapTest {
    private lateinit var context: Context
    private lateinit var prefs: SharedPreferences
    private lateinit var database: AppDatabase

    @Before
    fun setUp() {
        context = ApplicationProvider.getApplicationContext()
        prefs = context.getSharedPreferences("autoupload-bootstrap-test", Context.MODE_PRIVATE)
        prefs.edit().clear().commit()
        database = Room.inMemoryDatabaseBuilder(context, AppDatabase::class.java)
            .allowMainThreadQueries()
            .build()
    }

    @After
    fun tearDown() {
        database.close()
        prefs.edit().clear().commit()
    }

    @Test
    fun positiveWatermarkIsScopedToTheExactTreeAndReceiverOnce() = runBlocking {
        configureCompleteIdentity("https://nas-a.example/dav/")
        val identity = requireNotNull(RemoteSourceFactory.legacyReceiverIdentity(prefs))
        val treeKey = AutouploadIdentity.treeKey(DocumentsContract.buildTreeDocumentUri("provider", "root-id"))
        val destinationKey = AutouploadIdentity.destinationKey(
            identity.adapter,
            identity.rootIdentity,
            identity.accountId,
            identity.destination
        )

        val first = AutouploadLegacyCutoffBootstrap(database).migrateOnce(prefs, identity)
        val second = AutouploadLegacyCutoffBootstrap(database).migrateOnce(prefs, identity)

        assertNull(first.warningCode)
        assertNull(second.warningCode)
        assertEquals(1, database.autouploadDao().legacyCutoffCount())
        assertEquals(
            AutouploadLegacyCutoff(treeKey, destinationKey, 1234L),
            database.autouploadDao().legacyCutoff(treeKey, destinationKey)
        )
        assertTrue(prefs.getBoolean(AutouploadLegacyCutoffBootstrap.BOOTSTRAPPED_KEY, false))
        assertFalse(prefs.getBoolean(AutouploadLegacyCutoffBootstrap.WARNING_KEY, false))
    }

    @Test
    fun positiveWatermarkWithoutTreeDestinationOrReceiverPersistsUnscopedWarning() = runBlocking {
        configureCompleteIdentity("https://nas-a.example/dav/")
        val bootstrap = AutouploadLegacyCutoffBootstrap(database)
        val missingTree = DocumentsContract.buildTreeDocumentUri("provider", "root-id")
        val cases = listOf(
            { prefs.edit().remove("autoupload_tree").commit(); RemoteSourceFactory.legacyReceiverIdentity(prefs) },
            { prefs.edit().remove("autoupload_dest").commit(); RemoteSourceFactory.legacyReceiverIdentity(prefs) },
            {
                prefs.edit().remove("webdav_url").commit()
                RemoteSourceFactory.legacyReceiverIdentity(prefs)
            }
        )

        cases.forEach { identityFactory ->
            prefs.edit().clear()
                .putLong("autoupload_last_ms", 1234L)
                .putString("autoupload_tree", missingTree.toString())
                .putString("autoupload_dest", "Mounvix")
                .putString("webdav_url", "https://nas-a.example/dav/")
                .putString("webdav_user", "alice")
                .commit()
            val identity = identityFactory()

            val result = bootstrap.migrateOnce(prefs, identity)

            assertEquals("LEGACY_CUTOFF_UNSCOPED", result.warningCode)
            assertTrue(prefs.getBoolean(AutouploadLegacyCutoffBootstrap.BOOTSTRAPPED_KEY, false))
            assertTrue(prefs.getBoolean(AutouploadLegacyCutoffBootstrap.WARNING_KEY, false))
            assertEquals(0, database.autouploadDao().legacyCutoffCount())
            database.clearAllTables()
        }
    }

    @Test
    fun zeroOrNegativeWatermarkDoesNotCreateCutoffOrWarning() = runBlocking {
        val identity = configureCompleteIdentity("https://nas-a.example/dav/")
        val bootstrap = AutouploadLegacyCutoffBootstrap(database)

        listOf(0L, -10L).forEach { watermark ->
            prefs.edit().putLong("autoupload_last_ms", watermark).remove(
                AutouploadLegacyCutoffBootstrap.BOOTSTRAPPED_KEY
            ).commit()

            val result = bootstrap.migrateOnce(prefs, RemoteSourceFactory.legacyReceiverIdentity(prefs))

            assertNull(result.warningCode)
            assertTrue(prefs.getBoolean(AutouploadLegacyCutoffBootstrap.BOOTSTRAPPED_KEY, false))
            assertFalse(prefs.getBoolean(AutouploadLegacyCutoffBootstrap.WARNING_KEY, false))
            assertEquals(0, database.autouploadDao().legacyCutoffCount())
            database.clearAllTables()
        }
        assertTrue(identity)
    }

    @Test
    fun recoveryAfterRoomCommitDoesNotMoveCutoffToAChangedReceiver() = runBlocking {
        configureCompleteIdentity("https://nas-a.example/dav/")
        val originalIdentity = requireNotNull(RemoteSourceFactory.legacyReceiverIdentity(prefs))
        val treeUri = DocumentsContract.buildTreeDocumentUri("provider", "root-id")
        val originalCutoff = AutouploadLegacyCutoff(
            AutouploadIdentity.treeKey(treeUri),
            AutouploadIdentity.destinationKey(
                originalIdentity.adapter,
                originalIdentity.rootIdentity,
                originalIdentity.accountId,
                originalIdentity.destination
            ),
            1234L
        )
        database.autouploadDao().insertCutoffIfAbsent(originalCutoff)
        prefs.edit().remove(AutouploadLegacyCutoffBootstrap.BOOTSTRAPPED_KEY)
            .putString("webdav_url", "https://nas-b.example/dav/")
            .commit()

        val currentIdentity = requireNotNull(RemoteSourceFactory.legacyReceiverIdentity(prefs))
        val result = AutouploadLegacyCutoffBootstrap(database).migrateOnce(prefs, currentIdentity)
        val newDestinationKey = AutouploadIdentity.destinationKey(
            currentIdentity.adapter,
            currentIdentity.rootIdentity,
            currentIdentity.accountId,
            currentIdentity.destination
        )

        assertNull(result.warningCode)
        assertEquals(1, database.autouploadDao().legacyCutoffCount())
        assertEquals(originalCutoff, database.autouploadDao().legacyCutoff(originalCutoff.treeKey, originalCutoff.destinationKey))
        assertNull(database.autouploadDao().legacyCutoff(originalCutoff.treeKey, newDestinationKey))
        assertTrue(prefs.getBoolean(AutouploadLegacyCutoffBootstrap.BOOTSTRAPPED_KEY, false))
    }

    @Test
    fun completedPreferenceMarkerPreventsLaterWatermarkOrReceiverOverwrite() = runBlocking {
        configureCompleteIdentity("https://nas-a.example/dav/")
        val bootstrap = AutouploadLegacyCutoffBootstrap(database)
        val firstIdentity = requireNotNull(RemoteSourceFactory.legacyReceiverIdentity(prefs))
        bootstrap.migrateOnce(prefs, firstIdentity)
        val firstCutoff = database.autouploadDao().legacyCutoff(
            AutouploadIdentity.treeKey(DocumentsContract.buildTreeDocumentUri("provider", "root-id")),
            AutouploadIdentity.destinationKey(
                firstIdentity.adapter,
                firstIdentity.rootIdentity,
                firstIdentity.accountId,
                firstIdentity.destination
            )
        )
        prefs.edit().putLong("autoupload_last_ms", 9999L).putString("webdav_url", "https://nas-b.example/dav/").commit()

        bootstrap.migrateOnce(prefs, RemoteSourceFactory.legacyReceiverIdentity(prefs))

        assertEquals(1, database.autouploadDao().legacyCutoffCount())
        assertEquals(firstCutoff, database.autouploadDao().legacyCutoff(firstCutoff!!.treeKey, firstCutoff.destinationKey))
    }

    @Test
    fun legacyIdentityUsesTheSameWebDavThenSmbThenSftpPriorityAsSourceFactory() {
        configureCompleteIdentity("https://nas-a.example/dav/")
        prefs.edit()
            .putString("smb_host", "smb.example")
            .putString("smb_share", "photos")
            .putString("sftp_host", "sftp.example")
            .commit()

        assertEquals("webdav", requireNotNull(RemoteSourceFactory.legacyReceiverIdentity(prefs)).adapter)
        assertEquals("webdav", adapterChosenBySourceFactory())

        prefs.edit().remove("webdav_url").commit()
        assertEquals("smb", requireNotNull(RemoteSourceFactory.legacyReceiverIdentity(prefs)).adapter)
        assertEquals("smb", adapterChosenBySourceFactory())

        prefs.edit().remove("smb_host").commit()
        assertEquals("sftp", requireNotNull(RemoteSourceFactory.legacyReceiverIdentity(prefs)).adapter)
        assertEquals("sftp", adapterChosenBySourceFactory())
    }

    private val sourceSecrets = InMemoryProfileSecretStore()

    private fun adapterChosenBySourceFactory(): String = when (
        RemoteSourceFactory.fromPrefs(prefs, RemoteCredentialRepository(prefs, sourceSecrets))
    ) {
        is com.mounvix.app.data.source.WebDavStorageSource -> "webdav"
        is com.mounvix.app.data.source.SmbStorageSource -> "smb"
        is com.mounvix.app.data.source.SftpStorageSource -> "sftp"
        else -> error("No source configured")
    }

    private fun configureCompleteIdentity(url: String): Boolean {
        val treeUri = DocumentsContract.buildTreeDocumentUri("provider", "root-id")
        return prefs.edit()
            .putLong("autoupload_last_ms", 1234L)
            .putString("autoupload_tree", treeUri.toString())
            .putString("autoupload_dest", "Mounvix")
            .putString("webdav_url", url)
            .putString("webdav_user", "alice")
            .putString("webdav_pass", "not-persisted-in-identity")
            .commit()
    }
}

private class InMemoryProfileSecretStore : ProfileSecretStore {
    private val values = mutableMapOf<String, String>()

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
