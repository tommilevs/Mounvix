package com.mounvix.app.data.autoupload

import com.mounvix.app.data.db.AutouploadLegacyCutoff
import com.mounvix.app.data.db.AutouploadReceipt
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AutouploadPlannerTest {
    private data class Key(val tree: String, val destination: String, val document: String)

    private class MemoryLookup : AutouploadStateLookup {
        val receipts = mutableMapOf<Key, AutouploadReceipt>()
        val batchSizes = mutableListOf<Int>()

        override fun loadBatch(
            treeKey: String,
            destinationKey: String,
            documentKeys: List<String>
        ): Map<String, CandidateState> {
            batchSizes += documentKeys.size
            return documentKeys.distinct().associateWith { documentKey ->
                CandidateState(
                    receipt = receipts[Key(treeKey, destinationKey, documentKey)],
                    attempt = null
                )
            }
        }
    }

    private fun candidate(
        documentKey: String,
        treeKey: String = "tree",
        destinationKey: String = "destination",
        name: String? = null,
        relativePath: String? = null,
        sizeBytes: Long = 1L,
        modifiedMs: Long = 1L
    ): AutouploadCandidate {
        val path = relativePath ?: name ?: "file-$documentKey.jpg"
        val displayName = name ?: path.substringAfterLast('/')
        return AutouploadCandidate(
            treeKey = treeKey,
            documentKey = documentKey,
            destinationKey = destinationKey,
            name = displayName,
            relativePath = path,
            sizeBytes = sizeBytes,
            modifiedMs = modifiedMs,
            sourceUri = "content://provider/document/$documentKey"
        )
    }

    private fun receipt(candidate: AutouploadCandidate) = AutouploadReceipt(
        treeKey = candidate.treeKey,
        documentKey = candidate.documentKey,
        destinationKey = candidate.destinationKey,
        sizeBytes = candidate.sizeBytes,
        modifiedMs = candidate.modifiedMs,
        relativePath = candidate.relativePath,
        uploadedAtMs = 10L
    )

    @Test
    fun skipsOnlyExactReceiptAndKeepsCandidatesFromOtherIdentityOrFileVersion() {
        val exact = candidate("same", relativePath = "camera/same.jpg", sizeBytes = 20, modifiedMs = 30)
        val lookup = MemoryLookup().apply {
            receipts[Key(exact.treeKey, exact.destinationKey, exact.documentKey)] = receipt(exact)
        }
        val changedSize = exact.copy(sizeBytes = 21)
        val changedTime = exact.copy(modifiedMs = 31)
        val changedPath = exact.copy(relativePath = "archive/same.jpg")
        val changedTree = exact.copy(treeKey = "other-tree")
        val changedDestination = exact.copy(destinationKey = "other-destination")

        val selected = AutouploadPlanner.selectBatch(
            entries = sequenceOf(exact, changedSize, changedTime, changedPath, changedTree, changedDestination),
            stateLookup = lookup,
            legacyCutoff = null,
            nowMs = 100L
        )

        assertEquals(
            setOf(
                "tree/destination/21/30/camera/same.jpg",
                "tree/destination/20/31/camera/same.jpg",
                "tree/destination/20/30/archive/same.jpg",
                "other-tree/destination/20/30/camera/same.jpg",
                "tree/other-destination/20/30/camera/same.jpg"
            ),
            selected.map { "${it.treeKey}/${it.destinationKey}/${it.sizeBytes}/${it.modifiedMs}/${it.relativePath}" }.toSet()
        )
    }

    @Test
    fun legacyCutoffAppliesOnlyToItsTreeAndDestinationPair() {
        val atCutoff = candidate("at", modifiedMs = 100L)
        val oldSamePair = candidate("old", modifiedMs = 99L)
        val oldOtherTree = candidate("other-tree", treeKey = "other-tree", modifiedMs = 1L)
        val oldOtherDestination = candidate("other-destination", destinationKey = "other-destination", modifiedMs = 1L)
        val afterCutoff = candidate("new", modifiedMs = 101L)

        val selected = AutouploadPlanner.selectBatch(
            entries = sequenceOf(atCutoff, oldSamePair, oldOtherTree, oldOtherDestination, afterCutoff),
            stateLookup = MemoryLookup(),
            legacyCutoff = AutouploadLegacyCutoff("tree", "destination", 100L),
            nowMs = 1_000L
        )

        assertEquals(listOf("other-destination", "other-tree", "new"), selected.map { it.documentKey })
    }

    @Test
    fun filtersNonMediaAndSortsByModifiedTimeThenDocumentKey() {
        val candidates = sequenceOf(
            candidate("z", name = "z.jpg", modifiedMs = 7L),
            candidate("b", name = "notes.pdf", modifiedMs = 1L),
            candidate("c", name = "c.mp4", modifiedMs = 5L),
            candidate("a", name = "a.png", modifiedMs = 5L)
        )

        val selected = AutouploadPlanner.selectBatch(
            entries = candidates,
            stateLookup = MemoryLookup(),
            legacyCutoff = null,
            nowMs = 1_000L,
            limit = 2
        )

        assertEquals(listOf("a", "c"), selected.map { it.documentKey })
    }

    @Test
    fun selectsFiveHundredEligibleItemsEvenWhenFirstRawEntriesHaveReceipts() {
        val lookup = MemoryLookup()
        val candidates = (0 until 600).map { index ->
            candidate("doc-${index.toString().padStart(3, '0')}", modifiedMs = index.toLong())
        }
        candidates.take(100).forEach { item ->
            lookup.receipts[Key(item.treeKey, item.destinationKey, item.documentKey)] = receipt(item)
        }

        val selected = AutouploadPlanner.selectBatch(
            entries = candidates.asSequence(),
            stateLookup = lookup,
            legacyCutoff = null,
            nowMs = 1_000L,
            limit = 500
        )

        assertEquals(500, selected.size)
        assertEquals("doc-100", selected.first().documentKey)
        assertEquals("doc-599", selected.last().documentKey)
        assertEquals((100 until 600).map { "doc-${it.toString().padStart(3, '0')}" }, selected.map { it.documentKey })
        assertTrue(lookup.batchSizes.isNotEmpty())
        assertTrue(lookup.batchSizes.all { it in 1..500 })
    }

    @Test
    fun rescanningAfterFirstBatchReceiptMovesOnToRemainingFiles() {
        val lookup = MemoryLookup()
        val candidates = (0 until 6).map { index ->
            candidate("doc-$index", modifiedMs = index.toLong())
        }

        val firstBatch = AutouploadPlanner.selectBatch(
            candidates.asSequence(), lookup, legacyCutoff = null, nowMs = 100L, limit = 3
        )
        firstBatch.forEach { item ->
            lookup.receipts[Key(item.treeKey, item.destinationKey, item.documentKey)] = receipt(item)
        }
        val secondBatch = AutouploadPlanner.selectBatch(
            candidates.asSequence(), lookup, legacyCutoff = null, nowMs = 100L, limit = 3
        )

        assertEquals(listOf("doc-0", "doc-1", "doc-2"), firstBatch.map { it.documentKey })
        assertEquals(listOf("doc-3", "doc-4", "doc-5"), secondBatch.map { it.documentKey })
    }
}
