package com.mounvix.app.data.feed

import java.time.LocalTime

object FeedSyncPolicy {
    fun shouldNotify(
        card: FeedCard,
        enabled: Boolean,
        localTime: LocalTime,
        quietStart: LocalTime = LocalTime.of(22, 0),
        quietEnd: LocalTime = LocalTime.of(7, 0),
    ): Boolean {
        val quiet = when {
            quietStart == quietEnd -> false
            quietStart < quietEnd -> localTime >= quietStart && localTime < quietEnd
            else -> localTime >= quietStart || localTime < quietEnd
        }
        return enabled && !card.isRead && !quiet
    }
}
