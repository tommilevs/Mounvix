package com.mounvix.app.data.feed

import android.content.Context
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import com.mounvix.app.data.db.AppDatabase
import java.util.UUID
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
class ServerEventRepositoryTest {
    private val context = ApplicationProvider.getApplicationContext<Context>()
    private lateinit var database: AppDatabase
    private lateinit var repository: ServerEventRepository
    private var nowMs = 100L

    @Before
    fun setUp() {
        database = Room.inMemoryDatabaseBuilder(context, AppDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        repository = ServerEventRepository(database.serverEventCursorDao()) { nowMs }
    }

    @After
    fun tearDown() {
        database.close()
    }

    @Test
    fun cursorIsScopedByNormalizedServerAndExactUsernameWithoutStoringSecrets() {
        val profile = FeedProfile(" HTTPS://nas.example:443/mounvix/// ", " alice ", "private-password")
        val differentServer = profile.copy(baseUrl = "https://nas.example/other")
        val differentAccount = profile.copy(username = "Alice")

        repository.commitPage(profile, nextCursor = 7L)

        assertEquals(7L, repository.loadCursor(profile))
        assertNull(repository.loadCursor(differentServer))
        assertNull(repository.loadCursor(differentAccount))

        val cursor = database.serverEventCursorDao().allCursors().single()
        assertEquals(64, cursor.serverKey.length)
        assertFalse(cursor.serverKey.contains("nas.example"))
        assertFalse(cursor.serverKey.contains("alice"))
        assertFalse(cursor.toString().contains("private-password"))
    }

    @Test
    fun baselineIsInsertedOnceAndDisableReenableKeepsCommittedCursor() {
        val profile = FeedProfile("https://nas.example/mounvix", "alice", "secret")

        repository.initializeBaseline(profile, latestSeq = 12L)
        assertEquals(12L, repository.loadCursor(profile))

        nowMs = 200L
        repository.commitPage(profile, nextCursor = 16L)
        nowMs = 300L
        repository.initializeBaseline(profile, latestSeq = 99L)

        assertEquals(16L, repository.loadCursor(profile))
        assertEquals(200L, database.serverEventCursorDao().allCursors().single().updatedAtMs)
    }

    @Test
    fun clearProfileRemovesOnlyThatProfileCursor() {
        val first = FeedProfile("https://one.example/mounvix", "alice", "secret")
        val second = first.copy(baseUrl = "https://two.example/mounvix")
        repository.commitPage(first, nextCursor = 3L)
        repository.commitPage(second, nextCursor = 8L)

        repository.clearProfile(first)

        assertNull(repository.loadCursor(first))
        assertEquals(8L, repository.loadCursor(second))
    }

    @Test
    fun negativeSequencesAreRejectedWithoutChangingTheCursor() {
        val profile = FeedProfile("https://nas.example/mounvix", "alice", "secret")
        repository.commitPage(profile, nextCursor = 5L)

        val error = runCatching { repository.commitPage(profile, nextCursor = -1L) }.exceptionOrNull()

        assertTrue(error is IllegalArgumentException)
        assertEquals(5L, repository.loadCursor(profile))
    }
}
