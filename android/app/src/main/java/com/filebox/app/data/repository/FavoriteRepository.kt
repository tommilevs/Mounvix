package com.mounvix.app.data.repository

import com.mounvix.app.data.db.Favorite
import com.mounvix.app.data.db.FavoritesDao

/** Тонкая обёртка над FavoritesDao; Room-логика не попадает в Compose. */
class FavoriteRepository(private val dao: FavoritesDao) {

    suspend fun all(): List<Favorite> = dao.getAll()

    suspend fun get(key: String): Favorite? = dao.getByKey(key)

    suspend fun add(fav: Favorite) {
        dao.upsert(fav)
    }

    suspend fun remove(key: String) {
        dao.deleteByKey(key)
    }
}
