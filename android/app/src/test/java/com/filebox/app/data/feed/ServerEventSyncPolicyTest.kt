package com.mounvix.app.data.feed

import java.time.LocalTime
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ServerEventSyncPolicyTest {
    @Test
    fun missingCursorEstablishesLatestSequenceAsBaselineWithoutHistoricalAlerts() {
        val page = page(items = listOf(event(5L)), next = 5L, latest = 5L)

        val plan = ServerEventSyncPolicy.plan(
            page = page,
            currentCursor = null,
            alreadyVisible = emptySet(),
            quietHours = quietHours(now = LocalTime.NOON),
            notificationsAllowed = true,
        )

        assertEquals(5L, plan.nextCursor)
        assertTrue(plan.establishesBaseline)
        assertTrue(plan.alerts.isEmpty())
    }

    @Test
    fun unreadNewEventsAlertButReadEventsOnlyAdvanceTheCursor() {
        val page = page(
            items = listOf(event(11L), event(12L, isRead = true)),
            next = 12L,
            latest = 12L,
        )

        val plan = ServerEventSyncPolicy.plan(
            page = page,
            currentCursor = 10L,
            alreadyVisible = emptySet(),
            quietHours = quietHours(now = LocalTime.NOON),
            notificationsAllowed = true,
        )

        assertEquals(listOf("event-11"), plan.alerts.map { it.eventId })
        assertEquals("Plugin job finished safely.", plan.alerts.single().summary)
        assertEquals(12L, plan.nextCursor)
        assertTrue(!plan.historyGap)
    }

    @Test
    fun resetReportsHistoryGapRebasesAtLatestAndSuppressesOldAlerts() {
        val resetPage = ServerNotificationsPage(
            items = emptyList(),
            nextCursor = 12L,
            beforeCursor = null,
            reset = true,
            oldestSeq = 8L,
            latestSeq = 12L,
            unreadCount = 3,
        )

        val plan = ServerEventSyncPolicy.plan(
            page = resetPage,
            currentCursor = 50L,
            alreadyVisible = emptySet(),
            quietHours = quietHours(now = LocalTime.NOON),
            notificationsAllowed = true,
        )

        assertTrue(plan.historyGap)
        assertEquals(12L, plan.nextCursor)
        assertTrue(plan.alerts.isEmpty())
    }

    @Test
    fun alreadyVisibleEventIsNotAlertedAgain() {
        val page = page(items = listOf(event(1L), event(2L)), next = 2L, latest = 2L)

        val plan = ServerEventSyncPolicy.plan(
            page = page,
            currentCursor = 0L,
            alreadyVisible = setOf("event-2"),
            quietHours = quietHours(now = LocalTime.NOON),
            notificationsAllowed = true,
        )

        assertEquals(listOf("event-1"), plan.alerts.map { it.eventId })
        assertEquals(2L, plan.nextCursor)
    }

    @Test
    fun permissionQuietHoursAndAdvancedModeSuppressAlertsWithoutBlockingCursor() {
        val page = page(items = listOf(event(1L)), next = 1L, latest = 1L)
        val denied = plan(page, notificationsAllowed = false)
        val quiet = plan(page, quietHours = quietHours(now = LocalTime.of(23, 0)))
        val foreground = ServerEventSyncPolicy.plan(
            page = page,
            currentCursor = 0L,
            alreadyVisible = emptySet(),
            quietHours = quietHours(now = LocalTime.NOON),
            notificationsAllowed = true,
            advancedModeForeground = true,
        )

        for (result in listOf(denied, quiet, foreground)) {
            assertTrue(result.alerts.isEmpty())
            assertEquals(1L, result.nextCursor)
        }
    }

    @Test
    fun cursorRegressionIsRejectedUnlessServerExplicitlyResets() {
        val regressedPage = page(items = emptyList(), next = 4L, latest = 5L)

        val error = runCatching {
            plan(regressedPage, currentCursor = 5L)
        }.exceptionOrNull()

        assertTrue(error is IllegalArgumentException)
    }

    private fun plan(
        page: ServerNotificationsPage,
        currentCursor: Long? = 0L,
        quietHours: ServerEventQuietHours = quietHours(now = LocalTime.NOON),
        notificationsAllowed: Boolean = true,
    ) = ServerEventSyncPolicy.plan(
        page = page,
        currentCursor = currentCursor,
        alreadyVisible = emptySet(),
        quietHours = quietHours,
        notificationsAllowed = notificationsAllowed,
    )

    private fun quietHours(now: LocalTime) = ServerEventQuietHours(
        start = LocalTime.of(22, 0),
        end = LocalTime.of(7, 0),
        now = now,
    )

    private fun page(items: List<ServerNotification>, next: Long, latest: Long) = ServerNotificationsPage(
        items = items,
        nextCursor = next,
        beforeCursor = null,
        reset = false,
        oldestSeq = items.minOfOrNull { it.seq } ?: 0L,
        latestSeq = latest,
        unreadCount = items.count { !it.isRead },
    )

    private fun event(seq: Long, isRead: Boolean = false) = ServerNotification(
        eventId = "event-$seq",
        seq = seq,
        createdAt = "2026-11-23T10:00:00Z",
        kind = "plugin",
        summary = "Plugin job finished safely.",
        targetRoute = "/mounvix/notifications/event-$seq",
        isRead = isRead,
    )
}
