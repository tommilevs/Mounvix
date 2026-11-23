package com.mounvix.app.data.db

import androidx.room.Dao
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query

@Dao
abstract class ServerEventCursorDao {
    @Query("SELECT * FROM server_event_cursors WHERE server_key = :serverKey LIMIT 1")
    abstract fun cursor(serverKey: String): ServerEventCursor?

    @Query("SELECT * FROM server_event_cursors")
    abstract fun allCursors(): List<ServerEventCursor>

    @Insert(onConflict = OnConflictStrategy.IGNORE)
    abstract fun insertIfAbsent(cursor: ServerEventCursor)

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    abstract fun upsert(cursor: ServerEventCursor)

    @Query("DELETE FROM server_event_cursors WHERE server_key = :serverKey")
    abstract fun delete(serverKey: String)
}
