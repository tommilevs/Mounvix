package com.mounvix.app.data.autoupload

import com.mounvix.app.data.db.AutouploadAttempt
import com.mounvix.app.data.db.AutouploadLegacyCutoff
import com.mounvix.app.data.db.AutouploadReceipt
import java.util.PriorityQueue

/** Bounded, metadata-only selection for one or more SAF tree/destination pairs. */
object AutouploadPlanner {
    const val MAX_CANDIDATE_BATCH_SIZE = 500

    private val imageExt = setOf("jpg", "jpeg", "png", "gif", "webp", "heic", "heif", "bmp")
    private val videoExt = setOf("mp4", "mkv", "webm", "mov", "3gp", "m4v", "avi")

    private val candidateOrder = compareBy<AutouploadCandidate>(
        AutouploadCandidate::modifiedMs,
        AutouploadCandidate::documentKey
    )

    fun isMedia(name: String): Boolean {
        val extension = name.substringAfterLast('.', "").lowercase()
        return extension in imageExt || extension in videoExt
    }

    /**
     * Consumes candidates in bounded chunks, consults state in ≤500-key lookups, and retains only
     * the best [limit] eligible items. The SAF tree itself and all receipt state are never collected.
     */
    fun selectBatch(
        entries: Sequence<AutouploadCandidate>,
        stateLookup: AutouploadStateLookup,
        legacyCutoff: AutouploadLegacyCutoff?,
        nowMs: Long,
        limit: Int = MAX_CANDIDATE_BATCH_SIZE
    ): List<AutouploadCandidate> {
        require(limit in 1..MAX_CANDIDATE_BATCH_SIZE) { "Candidate batch limit must be between 1 and $MAX_CANDIDATE_BATCH_SIZE" }

        val best = PriorityQueue(limit, candidateOrder.reversed())
        val pending = ArrayList<AutouploadCandidate>(MAX_CANDIDATE_BATCH_SIZE)

        fun keepIfEligible(candidate: AutouploadCandidate, state: CandidateState?) {
            if (legacyCutoff != null &&
                legacyCutoff.treeKey == candidate.treeKey &&
                legacyCutoff.destinationKey == candidate.destinationKey &&
                candidate.modifiedMs <= legacyCutoff.cutoffMs
            ) return

            if (state?.receipt?.matches(candidate) == true) return
            val attempt = state?.attempt
            if (attempt != null && attempt.matches(candidate) && nowMs < attempt.nextRetryMs) return

            if (best.size < limit) {
                best.add(candidate)
            } else if (candidateOrder.compare(candidate, best.peek()) < 0) {
                best.remove()
                best.add(candidate)
            }
        }

        fun flushPending() {
            if (pending.isEmpty()) return
            pending.groupBy { it.treeKey to it.destinationKey }.forEach { (pair, pairEntries) ->
                val documentKeys = pairEntries.asSequence().map { it.documentKey }.distinct().toList()
                require(documentKeys.size <= MAX_CANDIDATE_BATCH_SIZE) { "State lookup exceeds the $MAX_CANDIDATE_BATCH_SIZE-key bound" }
                val states = stateLookup.loadBatch(pair.first, pair.second, documentKeys)
                pairEntries.forEach { candidate -> keepIfEligible(candidate, states[candidate.documentKey]) }
            }
            pending.clear()
        }

        entries.forEach { candidate ->
            if (!isMedia(candidate.name)) return@forEach
            pending.add(candidate)
            if (pending.size == MAX_CANDIDATE_BATCH_SIZE) flushPending()
        }
        flushPending()

        return best.toList().sortedWith(candidateOrder)
    }

    private fun AutouploadReceipt.matches(candidate: AutouploadCandidate): Boolean =
        treeKey == candidate.treeKey &&
            documentKey == candidate.documentKey &&
            destinationKey == candidate.destinationKey &&
            sizeBytes == candidate.sizeBytes &&
            modifiedMs == candidate.modifiedMs &&
            relativePath == candidate.relativePath

    private fun AutouploadAttempt.matches(candidate: AutouploadCandidate): Boolean =
        treeKey == candidate.treeKey &&
            documentKey == candidate.documentKey &&
            destinationKey == candidate.destinationKey &&
            sizeBytes == candidate.sizeBytes &&
            modifiedMs == candidate.modifiedMs &&
            relativePath == candidate.relativePath
}
