package com.mounvix.app.data.autoupload

import com.mounvix.app.data.db.AutouploadAttempt
import com.mounvix.app.data.db.AutouploadReceipt
import org.junit.Assert.assertEquals
import org.junit.Test

class AutouploadReceiptMatchingTest {
    private data class Key(val tree: String, val destination: String, val document: String)

    private class MemoryLookup : AutouploadStateLookup {
        val receipts = mutableMapOf<Key, AutouploadReceipt>()
        val attempts = mutableMapOf<Key, AutouploadAttempt>()

        override fun loadBatch(
            treeKey: String,
            destinationKey: String,
            documentKeys: List<String>
        ): Map<String, CandidateState> = documentKeys.distinct().associateWith { documentKey ->
            val key = Key(treeKey, destinationKey, documentKey)
            CandidateState(receipts[key], attempts[key])
        }
    }

    private fun candidate(
        treeKey: String = "tree",
        destinationKey: String = "destination",
        documentKey: String = "document",
        sizeBytes: Long = 10L,
        modifiedMs: Long = 20L,
        relativePath: String = "camera/photo.jpg"
    ) = AutouploadCandidate(
        treeKey = treeKey,
        documentKey = documentKey,
        destinationKey = destinationKey,
        name = relativePath.substringAfterLast('/'),
        relativePath = relativePath,
        sizeBytes = sizeBytes,
        modifiedMs = modifiedMs,
        sourceUri = "content://provider/document/$documentKey"
    )

    private fun receipt(item: AutouploadCandidate) = AutouploadReceipt(
        treeKey = item.treeKey,
        documentKey = item.documentKey,
        destinationKey = item.destinationKey,
        sizeBytes = item.sizeBytes,
        modifiedMs = item.modifiedMs,
        relativePath = item.relativePath,
        uploadedAtMs = 1L
    )

    @Test
    fun receiptSkipsOnlyTheUploadedSizeTimeAndPathVersion() {
        val uploaded = candidate()
        val lookup = MemoryLookup().apply {
            receipts[Key(uploaded.treeKey, uploaded.destinationKey, uploaded.documentKey)] = receipt(uploaded)
        }
        val modifiedSize = uploaded.copy(sizeBytes = 11L)
        val modifiedTime = uploaded.copy(modifiedMs = 21L)
        val movedPath = uploaded.copy(relativePath = "archive/photo.jpg")

        val selected = AutouploadPlanner.selectBatch(
            sequenceOf(uploaded, modifiedSize, modifiedTime, movedPath),
            lookup,
            legacyCutoff = null,
            nowMs = 100L
        )

        assertEquals(
            setOf("11:20:camera/photo.jpg", "10:21:camera/photo.jpg", "10:20:archive/photo.jpg"),
            selected.map { "${it.sizeBytes}:${it.modifiedMs}:${it.relativePath}" }.toSet()
        )
    }

    @Test
    fun duplicateFilenamesInDifferentFoldersRemainDistinctCandidates() {
        val cameraCopy = candidate(documentKey = "camera-copy", relativePath = "camera/photo.jpg")
        val archiveCopy = candidate(documentKey = "archive-copy", relativePath = "archive/photo.jpg")

        val selected = AutouploadPlanner.selectBatch(
            sequenceOf(cameraCopy, archiveCopy),
            MemoryLookup(),
            legacyCutoff = null,
            nowMs = 100L
        )

        assertEquals(2, selected.size)
        assertEquals(setOf("camera/photo.jpg", "archive/photo.jpg"), selected.map { it.relativePath }.toSet())
    }

    @Test
    fun retryBackoffAppliesOnlyToMatchingVersionAndExpiresAtRetryTime() {
        val item = candidate()
        val key = Key(item.treeKey, item.destinationKey, item.documentKey)
        val lookup = MemoryLookup().apply {
            attempts[key] = AutouploadAttempt(
                treeKey = item.treeKey,
                documentKey = item.documentKey,
                destinationKey = item.destinationKey,
                sizeBytes = item.sizeBytes,
                modifiedMs = item.modifiedMs,
                relativePath = item.relativePath,
                attemptCount = 2,
                nextRetryMs = 100L,
                safeErrorClass = "NetworkUnavailable"
            )
        }

        val beforeRetry = AutouploadPlanner.selectBatch(
            sequenceOf(item), lookup, legacyCutoff = null, nowMs = 99L
        )
        val atRetryTime = AutouploadPlanner.selectBatch(
            sequenceOf(item), lookup, legacyCutoff = null, nowMs = 100L
        )
        val changedSize = item.copy(sizeBytes = item.sizeBytes + 1L)
        val changedTime = item.copy(modifiedMs = item.modifiedMs + 1L)
        val changedPath = item.copy(relativePath = "new-folder/photo.jpg")
        val changedVersions = AutouploadPlanner.selectBatch(
            sequenceOf(changedSize, changedTime, changedPath), lookup, legacyCutoff = null, nowMs = 99L
        )

        assertEquals(emptyList<AutouploadCandidate>(), beforeRetry)
        assertEquals(listOf(item), atRetryTime)
        assertEquals(setOf(changedSize, changedTime, changedPath), changedVersions.toSet())
    }
}
