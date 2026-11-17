package com.mounvix.app.data.repository

import com.mounvix.app.data.db.Favorite
import com.mounvix.app.data.db.FavoritesDao
import com.mounvix.app.data.db.Recent
import com.mounvix.app.data.db.RecentsDao

/** Памятный фейк FavoritesDao: дедупликация по storageKey (REPLACE). */
class FakeFavoritesDao : FavoritesDao {
    private val rows = LinkedHashMap<String, Favorite>()

    override fun getAll(): List<Favorite> =
        rows.values.sortedByDescending { it.modifiedAt }

    override fun getByKey(key: String): Favorite? = rows[key]

    override fun upsert(fav: Favorite) {
        rows[fav.storageKey] = fav
    }

    override fun deleteByKey(key: String) {
        rows.remove(key)
    }
}

/** Памятный фейк RecentsDao: порядок openedAt DESC, LIMIT 200 не нужен в тестах. */
class FakeRecentsDao : RecentsDao {
    private val rows = LinkedHashMap<String, Recent>()

    override fun getAllOrdered(): List<Recent> =
        rows.values.sortedByDescending { it.openedAt }

    override fun upsert(rec: Recent) {
        rows[rec.storageKey] = rec
    }

    override fun deleteByKey(key: String) {
        rows.remove(key)
    }

    override fun count(): Int = rows.size

    override fun trimTo(cap: Int): Int {
        val keep = rows.values.sortedByDescending { it.openedAt }.take(cap).mapTo(mutableSetOf()) { it.storageKey }
        val before = rows.size
        for (key in rows.keys.toList()) {
            if (!keep.contains(key)) rows.remove(key)
        }
        return before - rows.size
    }

    override fun clear() {
        rows.clear()
    }
}
