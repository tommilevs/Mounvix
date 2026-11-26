package com.mounvix.app.data.db

import android.content.ContentValues
import android.content.Context
import android.database.sqlite.SQLiteDatabase
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import com.mounvix.app.data.feed.FeedProfile
import com.mounvix.app.data.feed.ServerEventRepository
import java.util.UUID
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [33])
class ServerEventCursorMigrationTest {
    @Test
    fun v1MigrationPreservesDataAndAddsServerEventCursor() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val databaseName = "cursor-${UUID.randomUUID()}.db"
        context.deleteDatabase(databaseName)
        createVersionOneDatabase(context, databaseName)

        val migrated = Room.databaseBuilder(context, AppDatabase::class.java, databaseName)
            .addMigrations(AutouploadMigrations.MIGRATION_1_2)
            .allowMainThreadQueries()
            .build()

        try {
            val sqlite = migrated.openHelper.writableDatabase
            assertEquals(2, sqlite.version)
            assertEquals("favorite.jpg", migrated.favoritesDao().getByKey("favorite-key")?.name)
            assertEquals("recent.jpg", migrated.recentsDao().getAllOrdered().single().name)
            assertTrue(migrated.serverEventCursorDao().allCursors().isEmpty())

            val repository = ServerEventRepository(migrated.serverEventCursorDao()) { 123L }
            val profile = FeedProfile("https://nas.example/mounvix", "alice", "secret")
            repository.initializeBaseline(profile, latestSeq = 9L)
            assertEquals(9L, repository.loadCursor(profile))

            val tables = mutableSetOf<String>()
            sqlite.query("SELECT name FROM sqlite_master WHERE type='table'").use { cursor ->
                while (cursor.moveToNext()) tables += cursor.getString(0)
            }
            assertTrue(tables.containsAll(setOf(
                "autoupload_receipts",
                "autoupload_attempts",
                "autoupload_legacy_cutoffs",
                "autoupload_run_state",
                "server_event_cursors",
            )))
        } finally {
            migrated.close()
            context.deleteDatabase(databaseName)
        }
    }

    private fun createVersionOneDatabase(context: Context, databaseName: String) {
        val file = context.getDatabasePath(databaseName)
        file.parentFile?.mkdirs()
        val raw = SQLiteDatabase.openOrCreateDatabase(file, null)
        try {
            raw.execSQL(
                """
                CREATE TABLE `favorites` (
                    `id` INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
                    `storageKey` TEXT NOT NULL,
                    `kind` TEXT NOT NULL,
                    `name` TEXT NOT NULL,
                    `treeId` TEXT NOT NULL,
                    `relPath` TEXT NOT NULL,
                    `size` INTEGER NOT NULL,
                    `modifiedAt` INTEGER NOT NULL
                )
                """.trimIndent(),
            )
            raw.execSQL("CREATE UNIQUE INDEX `index_favorites_storageKey` ON `favorites` (`storageKey`)")
            raw.execSQL(
                """
                CREATE TABLE `recents` (
                    `id` INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
                    `storageKey` TEXT NOT NULL,
                    `kind` TEXT NOT NULL,
                    `name` TEXT NOT NULL,
                    `treeId` TEXT NOT NULL,
                    `relPath` TEXT NOT NULL,
                    `size` INTEGER NOT NULL,
                    `modifiedAt` INTEGER NOT NULL,
                    `openedAt` INTEGER NOT NULL
                )
                """.trimIndent(),
            )
            raw.execSQL("CREATE UNIQUE INDEX `index_recents_storageKey` ON `recents` (`storageKey`)")
            raw.insertOrThrow(
                "favorites",
                null,
                ContentValues().apply {
                    put("storageKey", "favorite-key")
                    put("kind", "FILE")
                    put("name", "favorite.jpg")
                    put("treeId", "tree-favorite")
                    put("relPath", "favorite.jpg")
                    put("size", 123L)
                    put("modifiedAt", 456L)
                },
            )
            raw.insertOrThrow(
                "recents",
                null,
                ContentValues().apply {
                    put("storageKey", "recent-key")
                    put("kind", "FILE")
                    put("name", "recent.jpg")
                    put("treeId", "tree-recent")
                    put("relPath", "recent.jpg")
                    put("size", 789L)
                    put("modifiedAt", 101L)
                    put("openedAt", 112L)
                },
            )
            raw.version = 1
        } finally {
            raw.close()
        }
    }
}
