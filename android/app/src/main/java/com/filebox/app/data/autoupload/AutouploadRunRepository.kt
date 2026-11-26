package com.mounvix.app.data.autoupload

import androidx.room.withTransaction
import com.mounvix.app.data.db.AppDatabase
import com.mounvix.app.data.db.AutouploadAttempt
import com.mounvix.app.data.db.AutouploadReceipt
import com.mounvix.app.data.db.AutouploadRunState
import kotlinx.coroutines.flow.Flow

/** Transaction boundary for receipts, retry attempts, and durable terminal run summaries. */
class AutouploadRunRepository(
    private val database: AppDatabase,
    private val dao: com.mounvix.app.data.db.AutouploadDao = database.autouploadDao()
) {
    suspend fun recordSuccess(candidate: AutouploadCandidate, postUploadMetadata: SafEntry?): Boolean =
        database.withTransaction {
            if (!postUploadMetadata.matches(candidate)) return@withTransaction false

            dao.upsertReceipt(
                AutouploadReceipt(
                    treeKey = candidate.treeKey,
                    documentKey = candidate.documentKey,
                    destinationKey = candidate.destinationKey,
                    sizeBytes = candidate.sizeBytes,
                    modifiedMs = candidate.modifiedMs,
                    relativePath = candidate.relativePath,
                    uploadedAtMs = System.currentTimeMillis()
                )
            )
            dao.deleteAttempt(candidate.treeKey, candidate.documentKey, candidate.destinationKey)
            true
        }

    suspend fun recordFailure(
        candidate: AutouploadCandidate,
        safeErrorClass: String,
        nowMs: Long
    ): AutouploadAttempt = database.withTransaction {
        val previous = dao.attempt(candidate.treeKey, candidate.documentKey, candidate.destinationKey)
        val matchingPrevious = previous?.takeIf { it.matches(candidate) }
        val attemptCount = matchingPrevious?.attemptCount?.let { previousCount ->
            if (previousCount >= Int.MAX_VALUE) Int.MAX_VALUE else previousCount.coerceAtLeast(0) + 1
        } ?: 1
        val exponent = (attemptCount - 1).coerceAtMost(MAX_BACKOFF_EXPONENT)
        val delayMs = minOf(INITIAL_RETRY_MS * (1L shl exponent), MAX_RETRY_MS)
        val attempt = AutouploadAttempt(
            treeKey = candidate.treeKey,
            documentKey = candidate.documentKey,
            destinationKey = candidate.destinationKey,
            sizeBytes = candidate.sizeBytes,
            modifiedMs = candidate.modifiedMs,
            relativePath = candidate.relativePath,
            attemptCount = attemptCount,
            nextRetryMs = saturatingAdd(nowMs, delayMs),
            safeErrorClass = sanitizeErrorClass(safeErrorClass)
        )
        dao.upsertAttempt(attempt)
        attempt
    }

    suspend fun setRunState(
        treeKey: String,
        destinationKey: String,
        outcome: String,
        updatedAtMs: Long,
        uploadedCount: Int,
        errorCount: Int,
        safeErrorClass: String? = null
    ) {
        database.withTransaction {
            dao.upsertRunState(
                AutouploadRunState(
                    treeKey = treeKey,
                    destinationKey = destinationKey,
                    outcome = outcome,
                    updatedAtMs = updatedAtMs,
                    uploadedCount = uploadedCount.coerceAtLeast(0),
                    errorCount = errorCount.coerceAtLeast(0),
                    safeErrorClass = safeErrorClass?.let(::sanitizeErrorClass)
                )
            )
        }
    }

    fun observeRunState(treeKey: String, destinationKey: String): Flow<AutouploadRunState?> =
        dao.observeRunState(treeKey, destinationKey)

    private fun SafEntry?.matches(candidate: AutouploadCandidate): Boolean {
        if (this == null) return false
        val currentDocumentKey = runCatching { AutouploadIdentity.documentKey(documentUri) }.getOrNull()
        return currentDocumentKey == candidate.documentKey &&
            displayName == candidate.name &&
            relativePath == candidate.relativePath &&
            sizeBytes == candidate.sizeBytes &&
            modifiedMs == candidate.modifiedMs
    }

    private fun AutouploadAttempt.matches(candidate: AutouploadCandidate): Boolean =
        treeKey == candidate.treeKey &&
            documentKey == candidate.documentKey &&
            destinationKey == candidate.destinationKey &&
            sizeBytes == candidate.sizeBytes &&
            modifiedMs == candidate.modifiedMs &&
            relativePath == candidate.relativePath

    private fun saturatingAdd(left: Long, right: Long): Long =
        if (left > Long.MAX_VALUE - right) Long.MAX_VALUE else left + right

    private fun sanitizeErrorClass(value: String): String =
        value.filter { it.isLetterOrDigit() || it == '_' || it == '.' }
            .take(MAX_ERROR_CLASS_LENGTH)
            .ifBlank { "UnknownError" }

    private companion object {
        const val INITIAL_RETRY_MS = 60_000L
        const val MAX_RETRY_MS = 24L * 60L * 60L * 1000L
        const val MAX_BACKOFF_EXPONENT = 11
        const val MAX_ERROR_CLASS_LENGTH = 64
    }
}
