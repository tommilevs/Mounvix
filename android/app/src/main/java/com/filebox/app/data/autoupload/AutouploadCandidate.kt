package com.mounvix.app.data.autoupload

import com.mounvix.app.data.db.AutouploadAttempt
import com.mounvix.app.data.db.AutouploadReceipt

/** A validated SAF file snapshot; [sourceUri] is process-memory-only and never persisted. */
data class AutouploadCandidate(
    val treeKey: String,
    val documentKey: String,
    val destinationKey: String,
    val name: String,
    val relativePath: String,
    val sizeBytes: Long,
    val modifiedMs: Long,
    val sourceUri: String
) {
    init {
        require(treeKey.isNotBlank())
        require(documentKey.isNotBlank())
        require(destinationKey.isNotBlank())
        require(name.isNotBlank())
        require(sizeBytes >= 0L)
        require(modifiedMs >= 0L)
        require(sourceUri.startsWith("content://", ignoreCase = true))
        require(AutouploadIdentity.relativePath(relativePath.split('/')) == relativePath)
        require(relativePath.substringAfterLast('/') == name)
    }
}

/** Stored state used to decide whether a file version is already complete or delayed for retry. */
data class CandidateState(
    val receipt: AutouploadReceipt?,
    val attempt: AutouploadAttempt?
)

/** Bounded lookup over one SAF tree and one receiver identity. */
interface AutouploadStateLookup {
    fun loadBatch(
        treeKey: String,
        destinationKey: String,
        documentKeys: List<String>
    ): Map<String, CandidateState>
}
