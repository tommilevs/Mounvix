package com.mounvix.app.data.db

import androidx.room.Dao
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

@Dao
abstract class AutouploadDao {

    @Query(
        """
        SELECT * FROM autoupload_receipts
        WHERE tree_key = :treeKey AND document_key = :documentKey AND destination_key = :destinationKey
        LIMIT 1
        """
    )
    abstract fun receipt(
        treeKey: String,
        documentKey: String,
        destinationKey: String
    ): AutouploadReceipt?

    @Query(
        """
        SELECT * FROM autoupload_attempts
        WHERE tree_key = :treeKey AND document_key = :documentKey AND destination_key = :destinationKey
        LIMIT 1
        """
    )
    abstract fun attempt(
        treeKey: String,
        documentKey: String,
        destinationKey: String
    ): AutouploadAttempt?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    abstract fun upsertReceipt(receipt: AutouploadReceipt)

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    abstract fun upsertAttempt(attempt: AutouploadAttempt)

    @Query(
        """
        DELETE FROM autoupload_attempts
        WHERE tree_key = :treeKey AND document_key = :documentKey AND destination_key = :destinationKey
        """
    )
    abstract fun deleteAttempt(treeKey: String, documentKey: String, destinationKey: String)

    @Query(
        """
        SELECT * FROM autoupload_legacy_cutoffs
        WHERE tree_key = :treeKey AND destination_key = :destinationKey
        LIMIT 1
        """
    )
    abstract fun legacyCutoff(treeKey: String, destinationKey: String): AutouploadLegacyCutoff?

    @Query("SELECT EXISTS(SELECT 1 FROM autoupload_legacy_cutoffs LIMIT 1)")
    abstract fun hasAnyLegacyCutoff(): Boolean

    @Query("SELECT COUNT(*) FROM autoupload_legacy_cutoffs")
    abstract fun legacyCutoffCount(): Int

    @Insert(onConflict = OnConflictStrategy.IGNORE)
    abstract fun insertCutoffIfAbsent(cutoff: AutouploadLegacyCutoff)

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    abstract fun upsertRunState(state: AutouploadRunState)

    @Query(
        """
        SELECT * FROM autoupload_run_state
        WHERE tree_key = :treeKey AND destination_key = :destinationKey
        LIMIT 1
        """
    )
    abstract fun observeRunState(
        treeKey: String,
        destinationKey: String
    ): Flow<AutouploadRunState?>

    @Query(
        """
        SELECT * FROM autoupload_receipts
        WHERE tree_key = :treeKey AND destination_key = :destinationKey
          AND document_key IN (:documentKeys)
        """
    )
    protected abstract fun queryReceiptsForKeys(
        treeKey: String,
        destinationKey: String,
        documentKeys: List<String>
    ): List<AutouploadReceipt>

    fun receiptsForKeys(
        treeKey: String,
        destinationKey: String,
        documentKeys: List<String>
    ): List<AutouploadReceipt> {
        require(documentKeys.size <= MAX_DOCUMENT_KEYS_PER_BATCH) {
            "Receipt lookup is limited to $MAX_DOCUMENT_KEYS_PER_BATCH document keys"
        }
        if (documentKeys.isEmpty()) return emptyList()
        return queryReceiptsForKeys(treeKey, destinationKey, documentKeys)
    }

    @Query(
        """
        SELECT * FROM autoupload_attempts
        WHERE tree_key = :treeKey AND destination_key = :destinationKey
          AND document_key IN (:documentKeys)
        """
    )
    protected abstract fun queryAttemptsForKeys(
        treeKey: String,
        destinationKey: String,
        documentKeys: List<String>
    ): List<AutouploadAttempt>

    fun attemptsForKeys(
        treeKey: String,
        destinationKey: String,
        documentKeys: List<String>
    ): List<AutouploadAttempt> {
        require(documentKeys.size <= MAX_DOCUMENT_KEYS_PER_BATCH) {
            "Attempt lookup is limited to $MAX_DOCUMENT_KEYS_PER_BATCH document keys"
        }
        if (documentKeys.isEmpty()) return emptyList()
        return queryAttemptsForKeys(treeKey, destinationKey, documentKeys)
    }

    companion object {
        const val MAX_DOCUMENT_KEYS_PER_BATCH = 500
    }
}
