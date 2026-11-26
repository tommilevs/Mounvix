package com.mounvix.app.data.feed

import java.time.LocalTime
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class FeedSyncPolicyTest {
    private val card = FeedCard("id", "2026-11-18T00:00:00Z", "Memories", emptyList(), false)

    @Test fun disabledOrQuietHoursSuppressNotification() {
        assertFalse(FeedSyncPolicy.shouldNotify(card, false, LocalTime.of(12, 0)))
        assertFalse(FeedSyncPolicy.shouldNotify(card, true, LocalTime.of(23, 0)))
        assertFalse(FeedSyncPolicy.shouldNotify(card, true, LocalTime.of(6, 59)))
        assertTrue(FeedSyncPolicy.shouldNotify(card, true, LocalTime.of(7, 0)))
        assertFalse(FeedSyncPolicy.shouldNotify(card.copy(isRead = true), true, LocalTime.NOON))
    }
}
