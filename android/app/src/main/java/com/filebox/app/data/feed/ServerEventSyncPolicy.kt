package com.mounvix.app.data.feed

import java.time.LocalTime

data class ServerEventQuietHours(
    val start: LocalTime,
    val end: LocalTime,
    val now: LocalTime,
) {
    fun isQuietNow(): Boolean = when {
        start == end -> false
        start < end -> now >= start && now < end
        else -> now >= start || now < end
    }
}

/** Notification content is limited to safe summary fields; the validated route is tap-only data. */
data class ServerEventAlert(
    val eventId: String,
    val kind: String,
    val summary: String,
    val targetRoute: String,
)

data class ServerEventSyncPlan(
    val alerts: List<ServerEventAlert>,
    val nextCursor: Long,
    val historyGap: Boolean,
    val establishesBaseline: Boolean,
)

/** Pure decision policy; callers commit [ServerEventSyncPlan.nextCursor] after side effects. */
object ServerEventSyncPolicy {
    fun plan(
        page: ServerNotificationsPage,
        currentCursor: Long?,
        alreadyVisible: Set<String>,
        quietHours: ServerEventQuietHours,
        notificationsAllowed: Boolean,
        advancedModeForeground: Boolean = false,
    ): ServerEventSyncPlan {
        require(currentCursor == null || currentCursor >= 0) { "current cursor must not be negative" }
        require(page.latestSeq >= 0 && page.nextCursor >= 0 && page.nextCursor <= page.latestSeq) {
            "server event page cursor metadata is invalid"
        }
        require(page.items.all { it.seq > 0 && it.seq <= page.latestSeq }) {
            "server event page contains an invalid sequence"
        }

        if (currentCursor == null) {
            return ServerEventSyncPlan(
                alerts = emptyList(),
                nextCursor = page.latestSeq,
                historyGap = false,
                establishesBaseline = true,
            )
        }
        if (page.reset) {
            require(page.items.isEmpty() && page.nextCursor == page.latestSeq) {
                "server reset page is inconsistent"
            }
            return ServerEventSyncPlan(
                alerts = emptyList(),
                nextCursor = page.latestSeq,
                historyGap = true,
                establishesBaseline = false,
            )
        }

        require(page.nextCursor >= currentCursor && page.latestSeq >= currentCursor) {
            "server event cursor regressed without a reset"
        }
        require(page.items.zipWithNext().all { (left, right) -> left.seq < right.seq }) {
            "server event page is not ordered by ascending sequence"
        }
        require(page.items.all { it.seq <= page.nextCursor }) {
            "server event page cursor precedes an item"
        }

        val mayAlert = notificationsAllowed && !quietHours.isQuietNow() && !advancedModeForeground
        val alerts = if (mayAlert) {
            page.items.asSequence()
                .filter { it.seq > currentCursor && !it.isRead && it.eventId !in alreadyVisible }
                .map { ServerEventAlert(it.eventId, it.kind, it.summary, it.targetRoute) }
                .toList()
        } else {
            emptyList()
        }
        return ServerEventSyncPlan(
            alerts = alerts,
            nextCursor = page.nextCursor,
            historyGap = false,
            establishesBaseline = false,
        )
    }
}
