package com.mounvix.app.data.db

import androidx.room.Dao
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query

@Dao
interface FavoritesDao {

    @Query("SELECT * FROM favorites ORDER BY modifiedAt DESC, id DESC")
    fun getAll(): List<Favorite>

    @Query("SELECT * FROM favorites WHERE storageKey = :key LIMIT 1")
    fun getByKey(key: String): Favorite?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    fun upsert(fav: Favorite)

    @Query("DELETE FROM favorites WHERE storageKey = :key")
    fun deleteByKey(key: String)
}
