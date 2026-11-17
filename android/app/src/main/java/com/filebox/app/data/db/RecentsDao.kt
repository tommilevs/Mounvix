package com.mounvix.app.data.db

import androidx.room.Dao
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query

@Dao
interface RecentsDao {

    @Query("SELECT * FROM recents ORDER BY openedAt DESC, id DESC LIMIT 200")
    fun getAllOrdered(): List<Recent>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    fun upsert(rec: Recent)

    @Query("DELETE FROM recents WHERE storageKey = :key")
    fun deleteByKey(key: String)

    @Query("SELECT COUNT(*) FROM recents")
    fun count(): Int

    @Query("DELETE FROM recents WHERE id NOT IN (SELECT id FROM recents ORDER BY openedAt DESC, id DESC LIMIT :cap)")
    fun trimTo(cap: Int): Int

    @Query("DELETE FROM recents")
    fun clear()
}
