package com.mounvix.app.data.source

import android.os.CancellationSignal
import android.os.OperationCanceledException
import com.mounvix.app.data.model.FileEntry
import com.mounvix.app.data.model.FileType
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.SocketPolicy
import java.io.ByteArrayInputStream
import java.io.IOException

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [33])
class CancellableUploadTest {

    @Test
    fun cancellationAfterFirstChunkClosesSourceAndRemoteAndStopsFurtherWrites() {
        val input = CountingInputStream(ByteArray(128 * 1024 + 1))
        val signal = CancellationSignal()
        var remoteCloseCalls = 0
        var chunkWrites = 0
        val upload = CancellableUpload(input, signal) { remoteCloseCalls++ }

        try {
            assertThrows(OperationCanceledException::class.java) {
                upload.copyTo(
                    contentLength = input.available().toLong(),
                    onProgress = { transferred, _ -> if (transferred >= 64 * 1024L) signal.cancel() },
                    writeChunk = { _, _ -> chunkWrites++ }
                )
            }
        } finally {
            upload.close()
        }

        assertEquals(1, chunkWrites)
        assertEquals(1, input.readCalls)
        assertTrue(input.wasClosed)
        assertEquals(1, remoteCloseCalls)
    }

    @Test
    fun rejectsShortInputInsteadOfReportingAPartialTransferAsSuccess() {
        val input = CountingInputStream(byteArrayOf(1, 2))
        val upload = CancellableUpload(input, CancellationSignal()) { }

        try {
            assertThrows(IOException::class.java) {
                upload.copyTo(3L, { _, _ -> }, { _, _ -> })
            }
        } finally {
            upload.close()
        }
    }

    @Test
    fun rejectsInputLongerThanDeclaredWithoutWritingPastTheExpectedLength() {
        val input = CountingInputStream(byteArrayOf(1, 2, 3))
        val upload = CancellableUpload(input, CancellationSignal()) { }
        val writes = mutableListOf<Int>()

        try {
            assertThrows(IOException::class.java) {
                upload.copyTo(2L, { _, _ -> }, { _, count -> writes += count })
            }
        } finally {
            upload.close()
        }

        assertTrue(writes.isEmpty())
    }

    @Test
    fun webDavUploadReturnsAfterTheServerAcceptsTheCompleteBody() {
        val server = MockWebServer()
        server.start()
        server.enqueue(MockResponse().setResponseCode(201))
        val payload = "direct-stream".toByteArray()
        val input = CountingInputStream(payload)
        val progress = mutableListOf<Pair<Long, Long>>()
        val source = WebDavStorageSource(
            WebDavServer(baseUrl = server.url("/dav/").toString(), user = "user", password = "pass", trustAll = false)
        )

        try {
            source.uploadCancellable(input, payload.size.toLong(), "complete.jpg", CancellationSignal()) { done, total ->
                progress += done to total
            }

            val request = server.takeRequest()
            assertEquals("PUT", request.method)
            assertEquals("direct-stream", request.body.readUtf8())
            assertEquals(payload.size.toLong() to payload.size.toLong(), progress.last())
        } finally {
            server.shutdown()
        }
    }

    @Test
    fun webDavUploadRejectsUnsuccessfulServerResponse() {
        val server = MockWebServer()
        server.start()
        server.enqueue(MockResponse().setResponseCode(500))
        val payload = byteArrayOf(1, 2, 3)
        val source = WebDavStorageSource(
            WebDavServer(baseUrl = server.url("/dav/").toString(), user = "user", password = "pass", trustAll = false)
        )

        try {
            assertThrows(IOException::class.java) {
                source.uploadCancellable(
                    ByteArrayInputStream(payload),
                    payload.size.toLong(),
                    "failed.jpg",
                    CancellationSignal()
                )
            }
        } finally {
            server.shutdown()
        }
    }

    @Test
    fun webDavUploadCancellationStopsAfterTheFirstChunk() {
        val server = MockWebServer()
        server.start()
        server.enqueue(MockResponse().setSocketPolicy(SocketPolicy.NO_RESPONSE))
        val bytes = ByteArray(128 * 1024 + 1)
        val input = CountingInputStream(bytes)
        val signal = CancellationSignal()
        val source = WebDavStorageSource(
            WebDavServer(baseUrl = server.url("/dav/").toString(), user = "user", password = "pass", trustAll = false)
        )

        try {
            assertThrows(OperationCanceledException::class.java) {
                source.uploadCancellable(input, bytes.size.toLong(), "photo.jpg", signal) { transferred, _ ->
                    if (transferred >= 64 * 1024L) signal.cancel()
                }
            }
            assertEquals(1, input.readCalls)
            assertTrue(input.wasClosed)
        } finally {
            server.shutdown()
        }
    }

    @Test
    fun ensureParentDirectoriesCreatesMissingParentsAndIsIdempotent() {
        val source = FakeStorageSource()

        assertTrue(source.ensureParentDirectories("camera/2026/photo.jpg"))
        assertEquals(listOf("camera", "camera/2026"), source.createdDirectories)
        assertTrue(source.ensureParentDirectories("camera/2026/photo.jpg"))
        assertEquals(listOf("camera", "camera/2026"), source.createdDirectories)
    }

    @Test
    fun ensureParentDirectoriesRejectsAFileInTheParentPath() {
        val source = FakeStorageSource().apply { files += "blocked" }

        assertFalse(source.ensureParentDirectories("blocked/nested/photo.jpg"))
        assertTrue(source.createdDirectories.isEmpty())
    }

    private class FakeStorageSource : StorageSource {
        val directories = mutableSetOf<String>()
        val files = mutableSetOf<String>()
        val createdDirectories = mutableListOf<String>()

        override fun listDir(path: String): List<FileEntry> = emptyList()

        override fun createDir(path: String): Boolean {
            val normalized = path.trim('/')
            val parent = normalized.substringBeforeLast('/', "")
            if (parent.isNotEmpty() && parent !in directories) return false
            if (directories.add(normalized)) createdDirectories += normalized
            return true
        }

        override fun createFile(path: String): Boolean = false
        override fun delete(path: String): Boolean = false
        override fun rename(path: String, newName: String): Boolean = false
        override fun exists(path: String): Boolean = path.trim('/') in directories || path.trim('/') in files
        override fun stat(path: String): FileEntry? {
            val normalized = path.trim('/')
            val type = when (normalized) {
                in directories -> FileType.FOLDER
                in files -> FileType.FILE
                else -> return null
            }
            return FileEntry(
                name = normalized.substringAfterLast('/'),
                path = normalized,
                type = type,
                size = 0L,
                modified = 0L,
                docKey = normalized
            )
        }

        override fun copy(src: String, dst: String): Boolean = false
        override fun move(src: String, dst: String): Boolean = false
    }

    private class CountingInputStream(bytes: ByteArray) : ByteArrayInputStream(bytes) {
        var readCalls = 0
            private set
        var wasClosed = false
            private set

        override fun read(buffer: ByteArray, offset: Int, length: Int): Int {
            readCalls++
            return super.read(buffer, offset, length)
        }

        override fun close() {
            wasClosed = true
            super.close()
        }
    }
}
