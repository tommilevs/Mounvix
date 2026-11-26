package com.mounvix.app.data.db

import androidx.room.migration.Migration
import androidx.sqlite.db.SupportSQLiteDatabase

object AutouploadMigrations {
    val MIGRATION_1_2: Migration = object : Migration(1, 2) {
        override fun migrate(db: SupportSQLiteDatabase) {
            db.execSQL(
                """
                CREATE TABLE IF NOT EXISTS `autoupload_receipts` (
                    `tree_key` TEXT NOT NULL,
                    `document_key` TEXT NOT NULL,
                    `destination_key` TEXT NOT NULL,
                    `size_bytes` INTEGER NOT NULL,
                    `modified_ms` INTEGER NOT NULL,
                    `relative_path` TEXT NOT NULL,
                    `uploaded_at_ms` INTEGER NOT NULL,
                    PRIMARY KEY(`tree_key`, `document_key`, `destination_key`)
                )
                """.trimIndent()
            )
            db.execSQL(
                """
                CREATE INDEX IF NOT EXISTS `index_autoupload_receipts_tree_key_destination_key_document_key`
                ON `autoupload_receipts` (`tree_key`, `destination_key`, `document_key`)
                """.trimIndent()
            )
            db.execSQL(
                """
                CREATE TABLE IF NOT EXISTS `autoupload_attempts` (
                    `tree_key` TEXT NOT NULL,
                    `document_key` TEXT NOT NULL,
                    `destination_key` TEXT NOT NULL,
                    `size_bytes` INTEGER NOT NULL,
                    `modified_ms` INTEGER NOT NULL,
                    `relative_path` TEXT NOT NULL,
                    `attempt_count` INTEGER NOT NULL,
                    `next_retry_ms` INTEGER NOT NULL,
                    `safe_error_class` TEXT,
                    PRIMARY KEY(`tree_key`, `document_key`, `destination_key`)
                )
                """.trimIndent()
            )
            db.execSQL(
                """
                CREATE INDEX IF NOT EXISTS `index_autoupload_attempts_tree_key_destination_key_document_key`
                ON `autoupload_attempts` (`tree_key`, `destination_key`, `document_key`)
                """.trimIndent()
            )
            db.execSQL(
                """
                CREATE TABLE IF NOT EXISTS `autoupload_legacy_cutoffs` (
                    `tree_key` TEXT NOT NULL,
                    `destination_key` TEXT NOT NULL,
                    `cutoff_ms` INTEGER NOT NULL,
                    PRIMARY KEY(`tree_key`, `destination_key`)
                )
                """.trimIndent()
            )
            db.execSQL(
                """
                CREATE TABLE IF NOT EXISTS `autoupload_run_state` (
                    `tree_key` TEXT NOT NULL,
                    `destination_key` TEXT NOT NULL,
                    `outcome` TEXT NOT NULL,
                    `updated_at_ms` INTEGER NOT NULL,
                    `uploaded_count` INTEGER NOT NULL,
                    `error_count` INTEGER NOT NULL,
                    `safe_error_class` TEXT,
                    PRIMARY KEY(`tree_key`, `destination_key`)
                )
                """.trimIndent()
            )
            db.execSQL(
                """
                CREATE TABLE IF NOT EXISTS `server_event_cursors` (
                    `server_key` TEXT NOT NULL,
                    `last_seq` INTEGER NOT NULL,
                    `updated_at_ms` INTEGER NOT NULL,
                    PRIMARY KEY(`server_key`)
                )
                """.trimIndent()
            )
        }
    }
}
