package com.mounvix.app.data.repository

import com.mounvix.app.data.db.Recent
import com.mounvix.app.data.db.RecentsDao

/** Обёртка над RecentsDao: запись открытия + ограничение истории (~100 записей). */
class RecentRepository(
    private val dao: RecentsDao,
    private val cap: Int = 100
) {

    suspend fun all(): List<Recent> = dao.getAllOrdered()

    suspend fun record(rec: Recent) {
        dao.upsert(rec)
        if (dao.count() > cap) dao.trimTo(cap)
    }

    suspend fun remove(key: String) {
        dao.deleteByKey(key)
    }

    suspend fun clear() {
        dao.clear()
    }
}
