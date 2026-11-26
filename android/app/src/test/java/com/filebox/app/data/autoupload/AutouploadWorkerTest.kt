package com.mounvix.app.data.autoupload

import android.content.Context
import android.net.Uri
import android.os.CancellationSignal
import android.os.OperationCanceledException
import android.provider.DocumentsContract
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import androidx.work.Configuration
import androidx.work.Data
import androidx.work.ListenableWorker
import androidx.work.testing.TestListenableWorkerBuilder
import androidx.work.testing.WorkManagerTestInitHelper
import com.mounvix.app.data.db.AppDatabase
import com.mounvix.app.data.db.AutouploadAttempt
import com.mounvix.app.data.db.AutouploadReceipt
import com.mounvix.app.data.model.FileEntry
import com.mounvix.app.data.model.FileType
import com.mounvix.app.data.source.StorageSource
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.io.ByteArrayInputStream
import java.io.IOException
import java.io.InputStream
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [33], application = android.app.Application::class)
class AutouploadWorkerTest {
    private lateinit var context: Context
    private lateinit var prefs: android.content.SharedPreferences
    private lateinit var database: AppDatabase
    private lateinit var treeUri: Uri
    private lateinit var source: FakeStorageSource
    private lateinit var wifi: FakeWifiGuard
    private lateinit var reader: FakeMetadataReader
    private val openedStreams = mutableListOf<TrackingInputStream>()
    private val nowMs = 50_000_000L

    @Before
    fun setUp() {
        context = ApplicationProvider.getApplicationContext()
        prefs = context.getSharedPreferences("mounvix_prefs", Context.MODE_PRIVATE)
        prefs.edit().clear().commit()
        prefs.edit()
            .putBoolean("autoupload_enabled", true)
            .putString("autoupload_tree", DocumentsContract.buildTreeDocumentUri("provider", "root-id").toString())
            .putString("autoupload_dest", "Mounvix")
            .putString("webdav_url", "https://nas.example/dav/")
            .putString("webdav_user", "alice")
            .putString("webdav_pass", "secret")
            .commit()
        treeUri = DocumentsContract.buildTreeDocumentUri("provider", "root-id")
        database = Room.inMemoryDatabaseBuilder(context, AppDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        source = FakeStorageSource()
        wifi = FakeWifiGuard()
        reader = FakeMetadataReader { emptyList() }
        openedStreams.clear()
        WorkManagerTestInitHelper.initializeTestWorkManager(context, Configuration.Builder().build())
    }

    @After
    fun tearDown() {
        database.close()
        prefs.edit().clear().commit()
    }

    @Test
    fun periodicRunWithoutWifiStoresWaitingAndDoesNoRemoteOrContentWork() = runBlocking {
        reader = FakeMetadataReader { listOf(entry("one", "one.jpg")) }
        wifi.connected = false

        val (_, result) = runWorker()

        assertTrue(result is ListenableWorker.Result.Success)
        assertEquals("WAITING_FOR_WIFI", runState().outcome)
        assertTrue(source.uploadedPaths.isEmpty())
        assertTrue(source.parentPaths.isEmpty())
        assertTrue(openedStreams.isEmpty())
    }

    @Test
    fun runNowWithoutWifiStoresWaitingAndRetries() = runBlocking {
        reader = FakeMetadataReader { listOf(entry("one", "one.jpg")) }
        wifi.connected = false

        val (_, result) = runWorker(runNow = true)

        assertTrue(result is ListenableWorker.Result.Retry)
        assertEquals("WAITING_FOR_WIFI", runState().outcome)
        assertTrue(source.uploadedPaths.isEmpty())
        assertTrue(openedStreams.isEmpty())
    }

    @Test
    fun wifiLossDuringParentPreparationDoesNotCreateFailureAttempt() = runBlocking {
        val item = entry("one", "one.jpg")
        reader = FakeMetadataReader { listOf(item) }
        source.parentAction = {
            wifi.connected = false
            wifi.monitoredSignal?.cancel()
            false
        }

        runWorker()

        assertNull(attempt(item))
        assertTrue(source.uploadedPaths.isEmpty())
        assertTrue(openedStreams.isEmpty())
        assertEquals("WAITING_FOR_WIFI", runState().outcome)
    }

    @Test
    fun cellularTransitionBetweenFilesPreventsTheSecondTransfer() = runBlocking {
        val first = entry("first", "first.jpg", modifiedMs = 10L)
        val second = entry("second", "second.jpg", modifiedMs = 20L)
        reader = FakeMetadataReader { listOf(first, second) }
        source.uploadAction = { _, input, length, signal, progress ->
            consume(input, length, signal, progress)
            wifi.connected = false
        }

        runWorker()

        assertEquals(listOf("Mounvix/first.jpg"), source.uploadedPaths)
        assertNotNull(receipt(first))
        assertNull(receipt(second))
        assertNull(attempt(second))
        assertEquals("WAITING_FOR_WIFI", runState().outcome)
    }

    @Test
    fun wifiLossDuringTransferClosesActiveResourcesWithoutReceiptOrRetryAttempt() = runBlocking {
        val first = entry("first", "first.jpg", modifiedMs = 10L)
        val second = entry("second", "second.jpg", modifiedMs = 20L)
        reader = FakeMetadataReader { listOf(first, second) }
        source.uploadAction = { _, input, length, signal, progress ->
            val tracked = input as TrackingInputStream
            signal.setOnCancelListener {
                source.remoteCloseCalls++
                tracked.close()
            }
            val buffer = ByteArray(2)
            val count = input.read(buffer)
            if (count > 0) progress(count.toLong(), length)
            wifi.connected = false
            signal.cancel()
            signal.throwIfCanceled()
        }

        runWorker()

        assertEquals(listOf("Mounvix/first.jpg"), source.uploadedPaths)
        assertEquals(1, source.remoteCloseCalls)
        assertTrue(openedStreams.single().wasClosed)
        assertNull(receipt(first))
        assertNull(attempt(first))
        assertNull(receipt(second))
        assertEquals("WAITING_FOR_WIFI", runState().outcome)
    }

    @Test
    fun workManagerStopCancelsTransferAndLeavesCandidateWithoutReceiptOrAttempt() = runBlocking {
        val first = entry("first", "first.jpg", modifiedMs = 10L)
        val second = entry("second", "second.jpg", modifiedMs = 20L)
        reader = FakeMetadataReader { listOf(first, second) }
        val transferStarted = CountDownLatch(1)
        val transferCancelled = CountDownLatch(1)
        source.uploadAction = { _, input, _, signal, _ ->
            val tracked = input as TrackingInputStream
            signal.setOnCancelListener {
                source.remoteCloseCalls++
                tracked.close()
                transferCancelled.countDown()
            }
            transferStarted.countDown()
            if (!transferCancelled.await(5, TimeUnit.SECONDS)) throw AssertionError("stop did not close active transfer")
            signal.throwIfCanceled()
        }
        val worker = buildWorker()
        val job = async(Dispatchers.IO) { worker.doWork() }

        assertTrue(transferStarted.await(5, TimeUnit.SECONDS))
        worker.onStopped()
        // TestListenableWorkerBuilder calls doWork directly; cancel its coroutine job as
        // CoroutineWorker.onStopped does through the WorkManager future in production.
        job.cancel(CancellationException("WorkManager stopped"))
        assertTrue(transferCancelled.await(5, TimeUnit.SECONDS))
        try {
            job.await()
        } catch (_: CancellationException) {
            // WorkManager stop cancels the worker coroutine after its cleanup starts.
        }

        assertEquals(1, source.remoteCloseCalls)
        assertEquals(listOf("Mounvix/first.jpg"), source.uploadedPaths)
        assertNull(receipt(first))
        assertNull(attempt(first))
        assertNull(receipt(second))
        assertEquals("CANCELLED", runState().outcome)
    }

    @Test
    fun failureForOneFileIsPersistedAndDoesNotBlockTheNextFile() = runBlocking {
        val first = entry("first", "first.jpg", modifiedMs = 10L)
        val second = entry("second", "second.jpg", modifiedMs = 20L)
        reader = FakeMetadataReader { listOf(first, second) }
        source.failedUploads += "Mounvix/first.jpg"

        val (_, result) = runWorker()

        assertTrue(result is ListenableWorker.Result.Success)
        assertEquals(listOf("Mounvix/first.jpg", "Mounvix/second.jpg"), source.uploadedPaths)
        assertEquals(1, attempt(first)!!.attemptCount)
        assertEquals("IOException", attempt(first)!!.safeErrorClass)
        assertNotNull(receipt(second))
    }

    @Test
    fun postUploadMetadataChangePreventsReceiptCreation() = runBlocking {
        val original = entry("one", "one.jpg")
        reader = FakeMetadataReader { scan ->
            if (scan == 0) listOf(original) else listOf(original.copy(sizeBytes = original.sizeBytes!! + 1L))
        }

        runWorker()

        assertNull(receipt(original))
    }

    @Test
    fun repeatedIdenticalRunsDoNotTransferAReceiptMatchedVersionAgain() = runBlocking {
        val item = entry("one", "one.jpg")
        reader = FakeMetadataReader { listOf(item) }

        runWorker()
        runWorker()

        assertEquals(listOf("Mounvix/one.jpg"), source.uploadedPaths)
        assertNotNull(receipt(item))
    }

    @Test
    fun parentDirectoryFailureSkipsOnlyThatFileAndNestedPathIsPreparedBeforeUpload() = runBlocking {
        val nested = entry("nested", "photo.jpg", relativePath = "album/photo.jpg", modifiedMs = 10L)
        val other = entry("other", "other.jpg", modifiedMs = 20L)
        reader = FakeMetadataReader { listOf(nested, other) }
        source.failedDirectories += "Mounvix/album/photo.jpg"

        runWorker()

        assertEquals(listOf("Mounvix/album/photo.jpg", "Mounvix/other.jpg"), source.parentPaths)
        assertEquals(listOf("Mounvix/other.jpg"), source.uploadedPaths)
        assertNull(receipt(nested))
        assertNotNull(receipt(other))
    }

    @Test
    fun repositoryChecksMetadataBeforeAtomicallyRecordingReceiptAndDeletingAttempt() = runBlocking {
        val item = entry("one", "one.jpg")
        val candidate = candidate(item)
        val repository = AutouploadRunRepository(database)
        repository.recordFailure(candidate, "IOException", nowMs)

        assertFalse(repository.recordSuccess(candidate, item.copy(sizeBytes = item.sizeBytes!! + 1L)))
        assertNull(receipt(item))
        assertNotNull(attempt(item))

        assertTrue(repository.recordSuccess(candidate, item))
        assertNotNull(receipt(item))
        assertNull(attempt(item))
    }

    @Test
    fun repositoryUsesCappedExponentialBackoffAndResetsForChangedMetadata() = runBlocking {
        val item = entry("one", "one.jpg")
        val repository = AutouploadRunRepository(database)
        val candidate = candidate(item)

        for (attemptNumber in 1..14) {
            repository.recordFailure(candidate, "IOException", nowMs)
            val saved = requireNotNull(attempt(item))
            val uncappedDelay = 60_000L * (1L shl (attemptNumber - 1))
            assertEquals(attemptNumber, saved.attemptCount)
            assertEquals(nowMs + minOf(uncappedDelay, 24L * 60L * 60L * 1000L), saved.nextRetryMs)
        }

        repository.recordFailure(candidate.copy(sizeBytes = candidate.sizeBytes + 1L), "IOException", nowMs)
        val reset = requireNotNull(attempt(item))
        assertEquals(1, reset.attemptCount)
        assertEquals(nowMs + 60_000L, reset.nextRetryMs)
    }

    private suspend fun runWorker(runNow: Boolean = false): Pair<AutouploadWorker, ListenableWorker.Result> {
        val worker = buildWorker(runNow)
        return worker to worker.doWork()
    }

    private fun buildWorker(runNow: Boolean = false): AutouploadWorker =
        TestListenableWorkerBuilder<AutouploadWorker>(context)
            .setInputData(Data.Builder().putBoolean("run_now", runNow).build())
            .build()
            .also { it.dependencies = dependencies() }

    private fun dependencies() = AutouploadWorker.Dependencies(
        databaseProvider = { database },
        sourceFactory = { source },
        wifiGuardFactory = { wifi },
        readerFactory = { reader },
        inputStreamFactory = { _, _ ->
            TrackingInputStream("media".toByteArray()).also(openedStreams::add)
        },
        nowMs = { nowMs },
        notifier = { _, _, _ -> }
    )

    private fun entry(
        documentId: String,
        name: String,
        sizeBytes: Long = 5L,
        modifiedMs: Long = 100L,
        relativePath: String = name
    ) = SafEntry(
        documentUri = DocumentsContract.buildDocumentUriUsingTree(treeUri, documentId),
        displayName = name,
        mimeType = "image/jpeg",
        sizeBytes = sizeBytes,
        modifiedMs = modifiedMs,
        relativePath = relativePath
    )

    private fun candidate(entry: SafEntry): AutouploadCandidate {
        val identity = requireNotNull(RemoteSourceFactory.legacyReceiverIdentity(prefs))
        return AutouploadCandidate(
            treeKey = AutouploadIdentity.treeKey(treeUri),
            documentKey = AutouploadIdentity.documentKey(entry.documentUri),
            destinationKey = AutouploadIdentity.destinationKey(
                identity.adapter,
                identity.rootIdentity,
                identity.accountId,
                identity.destination
            ),
            name = requireNotNull(entry.displayName),
            relativePath = requireNotNull(entry.relativePath),
            sizeBytes = requireNotNull(entry.sizeBytes),
            modifiedMs = requireNotNull(entry.modifiedMs),
            sourceUri = entry.documentUri.toString()
        )
    }

    private suspend fun runState(): com.mounvix.app.data.db.AutouploadRunState {
        val (treeKey, destinationKey) = identityKeys()
        return requireNotNull(database.autouploadDao().observeRunState(treeKey, destinationKey).first())
    }

    private fun receipt(entry: SafEntry): AutouploadReceipt? {
        val (treeKey, destinationKey) = identityKeys()
        return database.autouploadDao().receipt(
            treeKey,
            AutouploadIdentity.documentKey(entry.documentUri),
            destinationKey
        )
    }

    private fun attempt(entry: SafEntry): AutouploadAttempt? {
        val (treeKey, destinationKey) = identityKeys()
        return database.autouploadDao().attempt(
            treeKey,
            AutouploadIdentity.documentKey(entry.documentUri),
            destinationKey
        )
    }

    private fun identityKeys(): Pair<String, String> {
        val identity = requireNotNull(RemoteSourceFactory.legacyReceiverIdentity(prefs))
        return AutouploadIdentity.treeKey(treeUri) to AutouploadIdentity.destinationKey(
            identity.adapter,
            identity.rootIdentity,
            identity.accountId,
            identity.destination
        )
    }

    private class FakeMetadataReader(
        private val snapshots: (Int) -> List<SafEntry>
    ) : AutouploadMetadataReader {
        private var scanCount = 0

        override fun entries(treeUri: Uri): Sequence<SafEntry> = snapshots(scanCount++).asSequence()
    }

    private class FakeWifiGuard(var connected: Boolean = true) : AutouploadNetworkGuard {
        var monitoredSignal: CancellationSignal? = null
        var closeCalls = 0

        override fun isWifiConnected(): Boolean = connected

        override fun monitorDisconnect(signal: CancellationSignal): AutoCloseable {
            monitoredSignal = signal
            return AutoCloseable { closeCalls++ }
        }
    }

    private class FakeStorageSource : StorageSource {
        val uploadedPaths = mutableListOf<String>()
        val parentPaths = mutableListOf<String>()
        val failedDirectories = mutableSetOf<String>()
        val failedUploads = mutableSetOf<String>()
        var remoteCloseCalls = 0
        var uploadAction: (String, InputStream, Long, CancellationSignal, (Long, Long) -> Unit) -> Unit =
            { _, input, length, signal, progress -> consume(input, length, signal, progress) }

        override fun listDir(path: String): List<FileEntry> = emptyList()
        override fun createDir(path: String): Boolean = true
        override fun createFile(path: String): Boolean = true
        override fun delete(path: String): Boolean = true
        override fun rename(path: String, newName: String): Boolean = true
        override fun exists(path: String): Boolean = false
        override fun stat(path: String): FileEntry? = null
        override fun copy(src: String, dst: String): Boolean = false
        override fun move(src: String, dst: String): Boolean = false
        override fun rootId(): String = "https://nas.example/dav"

        var parentAction: (String) -> Boolean = { path -> path !in failedDirectories }

        override fun ensureParentDirectories(path: String): Boolean {
            parentPaths += path
            return parentAction(path)
        }

        override fun uploadCancellable(
            input: InputStream,
            contentLength: Long,
            path: String,
            signal: CancellationSignal,
            onProgress: (Long, Long) -> Unit
        ) {
            uploadedPaths += path
            if (path in failedUploads) throw IOException("untrusted detail")
            uploadAction(path, input, contentLength, signal, onProgress)
        }
    }

    private class TrackingInputStream(bytes: ByteArray) : ByteArrayInputStream(bytes) {
        var wasClosed = false
            private set

        override fun close() {
            wasClosed = true
            super.close()
        }
    }

    private companion object {
        fun consume(
            input: InputStream,
            contentLength: Long,
            signal: CancellationSignal,
            onProgress: (Long, Long) -> Unit
        ) {
            var transferred = 0L
            val buffer = ByteArray(64)
            while (true) {
                signal.throwIfCanceled()
                val count = input.read(buffer)
                if (count < 0) break
                if (count == 0) continue
                transferred += count
                onProgress(transferred, contentLength)
            }
            signal.throwIfCanceled()
            if (transferred != contentLength) throw IOException("length mismatch")
        }
    }
}
