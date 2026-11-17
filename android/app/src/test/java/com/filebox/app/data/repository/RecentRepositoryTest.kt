package com.mounvix.app.data.repository

import com.mounvix.app.data.db.Favorite
import com.mounvix.app.data.db.Recent
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RecentRepositoryTest {

    private fun rec(key: String, openedAt: Long) =
        Recent(
            storageKey = key,
            kind = Favorite.KIND_FILE,
            name = "$key.txt",
            treeId = "tree-1",
            relPath = key,
            size = 1L,
            modifiedAt = 0L,
            openedAt = openedAt
        )

    @Test
    fun recordDedupeByStorageKey() = runBlocking {
        val repo = RecentRepository(FakeRecentsDao(), cap = 100)
        repo.record(rec("a", 1000L))
        repo.record(rec("a", 2000L)) // повторное открытие того же файла
        val all = repo.all()
        assertEquals(1, all.size)
        assertEquals(2000L, all[0].openedAt)
    }

    @Test
    fun newestFirstOrdering() = runBlocking {
        val repo = RecentRepository(FakeRecentsDao(), cap = 100)
        repo.record(rec("a", 100L))
        repo.record(rec("b", 300L))
        repo.record(rec("c", 200L))
        assertEquals(listOf("b", "c", "a"), repo.all().map { it.storageKey })
    }

    @Test
    fun trimKeepsNewestUpToCap() = runBlocking {
        val repo = RecentRepository(FakeRecentsDao(), cap = 3)
        listOf(100L, 200L, 300L, 400L).forEachIndexed { i, t -> repo.record(rec("k$i", t)) }
        val all = repo.all()
        assertEquals(3, all.size)
        // Старейшая запись (100) вырезана; остались самые свежие три.
        assertEquals(listOf("k3", "k2", "k1"), all.map { it.storageKey })
    }

    @Test
    fun clearRemovesAll() = runBlocking {
        val repo = RecentRepository(FakeRecentsDao(), cap = 100)
        repo.record(rec("a", 100L))
        repo.record(rec("b", 200L))
        repo.clear()
        assertTrue(repo.all().isEmpty())
    }

    @Test
    fun removeByKey() = runBlocking {
        val repo = RecentRepository(FakeRecentsDao(), cap = 100)
        repo.record(rec("a", 100L))
        repo.record(rec("b", 200L))
        repo.remove("a")
        assertEquals(listOf("b"), repo.all().map { it.storageKey })
    }
}
