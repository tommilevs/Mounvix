package com.mounvix.app.data.feed

import com.mounvix.app.data.db.ServerEventCursor
import com.mounvix.app.data.db.ServerEventCursorDao

/** Durable per-server/account cursor, intentionally separate from the Memories feed cursor. */
class ServerEventRepository(
    private val dao: ServerEventCursorDao,
    private val nowMs: () -> Long = { System.currentTimeMillis() },
) {
    fun loadCursor(profile: FeedProfile): Long? = dao.cursor(serverKey(profile))?.lastSeq

    /** Establish a first-enable baseline without producing alerts for retained history. */
    fun initializeBaseline(profile: FeedProfile, latestSeq: Long) {
        require(latestSeq >= 0) { "server event baseline must not be negative" }
        dao.insertIfAbsent(ServerEventCursor(serverKey(profile), latestSeq, nowMs()))
    }

    /** Persist the completed page cursor after all page side effects have succeeded. */
    fun commitPage(profile: FeedProfile, nextCursor: Long) {
        require(nextCursor >= 0) { "server event cursor must not be negative" }
        dao.upsert(ServerEventCursor(serverKey(profile), nextCursor, nowMs()))
    }

    fun clearProfile(profile: FeedProfile) {
        dao.delete(serverKey(profile))
    }

    private fun serverKey(profile: FeedProfile): String {
        val normalizedUrl = normalizeMounvixBaseUrl(profile.baseUrl)
        val normalizedUsername = profile.username.trim()
        require(normalizedUsername.isNotBlank()) { "username is required" }
        return stableFeedProfileKey(normalizedUrl, normalizedUsername)
    }
}
