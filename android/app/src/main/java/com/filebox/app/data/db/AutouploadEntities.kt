package com.mounvix.app.data.db

import androidx.room.ColumnInfo
import androidx.room.Entity
import androidx.room.Index

@Entity(
    tableName = "autoupload_receipts",
    primaryKeys = ["tree_key", "document_key", "destination_key"],
    indices = [Index(value = ["tree_key", "destination_key", "document_key"])]
)
data class AutouploadReceipt(
    @ColumnInfo(name = "tree_key") val treeKey: String,
    @ColumnInfo(name = "document_key") val documentKey: String,
    @ColumnInfo(name = "destination_key") val destinationKey: String,
    @ColumnInfo(name = "size_bytes") val sizeBytes: Long,
    @ColumnInfo(name = "modified_ms") val modifiedMs: Long,
    @ColumnInfo(name = "relative_path") val relativePath: String,
    @ColumnInfo(name = "uploaded_at_ms") val uploadedAtMs: Long
)

@Entity(
    tableName = "autoupload_attempts",
    primaryKeys = ["tree_key", "document_key", "destination_key"],
    indices = [Index(value = ["tree_key", "destination_key", "document_key"])]
)
data class AutouploadAttempt(
    @ColumnInfo(name = "tree_key") val treeKey: String,
    @ColumnInfo(name = "document_key") val documentKey: String,
    @ColumnInfo(name = "destination_key") val destinationKey: String,
    @ColumnInfo(name = "size_bytes") val sizeBytes: Long,
    @ColumnInfo(name = "modified_ms") val modifiedMs: Long,
    @ColumnInfo(name = "relative_path") val relativePath: String,
    @ColumnInfo(name = "attempt_count") val attemptCount: Int,
    @ColumnInfo(name = "next_retry_ms") val nextRetryMs: Long,
    @ColumnInfo(name = "safe_error_class") val safeErrorClass: String?
)

@Entity(
    tableName = "autoupload_legacy_cutoffs",
    primaryKeys = ["tree_key", "destination_key"]
)
data class AutouploadLegacyCutoff(
    @ColumnInfo(name = "tree_key") val treeKey: String,
    @ColumnInfo(name = "destination_key") val destinationKey: String,
    @ColumnInfo(name = "cutoff_ms") val cutoffMs: Long
)

@Entity(
    tableName = "autoupload_run_state",
    primaryKeys = ["tree_key", "destination_key"]
)
data class AutouploadRunState(
    @ColumnInfo(name = "tree_key") val treeKey: String,
    @ColumnInfo(name = "destination_key") val destinationKey: String,
    @ColumnInfo(name = "outcome") val outcome: String,
    @ColumnInfo(name = "updated_at_ms") val updatedAtMs: Long,
    @ColumnInfo(name = "uploaded_count") val uploadedCount: Int,
    @ColumnInfo(name = "error_count") val errorCount: Int,
    @ColumnInfo(name = "safe_error_class") val safeErrorClass: String?
)
