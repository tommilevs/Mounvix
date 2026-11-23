package com.mounvix.app.data.db

import android.content.Context
import androidx.room.Database
import androidx.room.Room
import androidx.room.RoomDatabase

@Database(
    entities = [
        Favorite::class,
        Recent::class,
        AutouploadReceipt::class,
        AutouploadAttempt::class,
        AutouploadLegacyCutoff::class,
        AutouploadRunState::class,
        ServerEventCursor::class,
    ],
    version = 2
)
abstract class AppDatabase : RoomDatabase() {

    abstract fun favoritesDao(): FavoritesDao
    abstract fun recentsDao(): RecentsDao
    abstract fun autouploadDao(): AutouploadDao
    abstract fun serverEventCursorDao(): ServerEventCursorDao

    companion object {
        @Volatile
        private var instance: AppDatabase? = null

        fun get(context: Context): AppDatabase =
            instance ?: synchronized(this) {
                instance ?: Room.databaseBuilder(
                    context.applicationContext,
                    AppDatabase::class.java,
                    "mounvix.db"
                ).addMigrations(AutouploadMigrations.MIGRATION_1_2).build().also { instance = it }
            }
    }
}
