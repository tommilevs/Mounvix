package com.mounvix.app.data.db

import android.content.Context
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [33])
class AutouploadDaoTest {
    private lateinit var database: AppDatabase
    private lateinit var dao: AutouploadDao

    @Before
    fun setUp() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        database = Room.inMemoryDatabaseBuilder(context, AppDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        dao = database.autouploadDao()
    }

    @After
    fun tearDown() {
        database.close()
    }

    @Test
    fun receiptAndAttemptLookupsUseTheFullCompositeKey() {
        val receipt = AutouploadReceipt(
            treeKey = "tree",
            documentKey = "document",
            destinationKey = "destination-a",
            sizeBytes = 12L,
            modifiedMs = 34L,
            relativePath = "nested/file.txt",
            uploadedAtMs = 56L
        )
        val attempt = AutouploadAttempt(
            treeKey = "tree",
            documentKey = "document",
            destinationKey = "destination-a",
            sizeBytes = 12L,
            modifiedMs = 34L,
            relativePath = "nested/file.txt",
            attemptCount = 2,
            nextRetryMs = 78L,
            safeErrorClass = "NetworkUnavailable"
        )

        dao.upsertReceipt(receipt)
        dao.upsertAttempt(attempt)

        assertEquals(receipt, dao.receipt("tree", "document", "destination-a"))
        assertNull(dao.receipt("tree", "document", "destination-b"))
        assertEquals(attempt, dao.attempt("tree", "document", "destination-a"))
        dao.deleteAttempt("tree", "document", "destination-a")
        assertNull(dao.attempt("tree", "document", "destination-a"))
    }

    @Test
    fun cutoffInsertIfAbsentPreservesTheOriginalCutoff() {
        dao.insertCutoffIfAbsent(AutouploadLegacyCutoff("tree", "destination", 10L))
        dao.insertCutoffIfAbsent(AutouploadLegacyCutoff("tree", "destination", 20L))

        assertEquals(
            AutouploadLegacyCutoff("tree", "destination", 10L),
            dao.legacyCutoff("tree", "destination")
        )
    }

    @Test
    fun batchLookupsRejectMoreThanFiveHundredDocumentKeys() {
        val documentKeys = (0..AutouploadDao.MAX_DOCUMENT_KEYS_PER_BATCH).map { "document-$it" }

        assertThrows(IllegalArgumentException::class.java) {
            dao.receiptsForKeys("tree", "destination", documentKeys)
        }
        assertThrows(IllegalArgumentException::class.java) {
            dao.attemptsForKeys("tree", "destination", documentKeys)
        }
    }

    @Test
    fun runStateCanBeWrittenAndObservedByTreeAndDestination() = runBlocking {
        val state = AutouploadRunState(
            treeKey = "tree",
            destinationKey = "destination",
            outcome = "SUCCESS",
            updatedAtMs = 123L,
            uploadedCount = 4,
            errorCount = 0,
            safeErrorClass = null
        )

        assertNull(dao.observeRunState("tree", "destination").first())
        dao.upsertRunState(state)

        assertEquals(state, dao.observeRunState("tree", "destination").first())
        assertNull(dao.observeRunState("other-tree", "destination").first())
    }
}
