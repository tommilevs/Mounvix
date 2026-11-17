package com.mounvix.app.data.repository

import com.mounvix.app.data.db.Favorite
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test

class FavoriteRepositoryTest {

    private fun fav(key: String, name: String = "x", kind: String = Favorite.KIND_FILE) =
        Favorite(
            storageKey = key,
            kind = kind,
            name = name,
            treeId = "tree-1",
            relPath = key,
            size = 10L,
            modifiedAt = System.currentTimeMillis()
        )

    @Test
    fun addAndGetRoundTrip() = runBlocking {
        val repo = FavoriteRepository(FakeFavoritesDao())
        repo.add(fav("k1", "a.txt"))
        val got = repo.get("k1")
        assertNotNull(got)
        assertEquals("a.txt", got!!.name)
    }

    @Test
    fun upsertReplacesByStorageKey() = runBlocking {
        val dao = FakeFavoritesDao()
        val repo = FavoriteRepository(dao)
        repo.add(fav("k1", "old name"))
        repo.add(fav("k1", "new name")) // повторное добавление того же ключа
        assertEquals(1, repo.all().size)
        assertEquals("new name", repo.get("k1")!!.name)
    }

    @Test
    fun removeDeletesByKey() = runBlocking {
        val repo = FavoriteRepository(FakeFavoritesDao())
        repo.add(fav("k1"))
        repo.remove("k1")
        assertNull(repo.get("k1"))
        assertEquals(0, repo.all().size)
    }

    @Test
    fun orderedByModifiedAtDesc() = runBlocking {
        val dao = FakeFavoritesDao()
        val repo = FavoriteRepository(dao)
        repo.add(fav("a").copy(modifiedAt = 100L))
        repo.add(fav("b").copy(modifiedAt = 200L))
        assertEquals(listOf("b", "a"), repo.all().map { it.storageKey })
    }
}
