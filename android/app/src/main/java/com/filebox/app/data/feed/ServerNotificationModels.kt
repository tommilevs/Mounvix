package com.mounvix.app.data.feed

/** One safe job-outcome notification from the M7 server inbox. */
data class ServerNotification(
    val eventId: String,
    val seq: Long,
    val createdAt: String,
    val kind: String,
    val summary: String,
    val targetRoute: String,
    val isRead: Boolean,
)

/** A bounded page of M7 notifications and its independent server-event cursors. */
data class ServerNotificationsPage(
    val items: List<ServerNotification>,
    val nextCursor: Long,
    val beforeCursor: Long?,
    val reset: Boolean,
    val oldestSeq: Long,
    val latestSeq: Long,
    val unreadCount: Int,
)
