package com.mounvix.app.data.db

import androidx.room.ColumnInfo
import androidx.room.Entity
import androidx.room.PrimaryKey

@Entity(tableName = "server_event_cursors")
data class ServerEventCursor(
    @PrimaryKey
    @ColumnInfo(name = "server_key")
    val serverKey: String,
    @ColumnInfo(name = "last_seq")
    val lastSeq: Long,
    @ColumnInfo(name = "updated_at_ms")
    val updatedAtMs: Long,
)
