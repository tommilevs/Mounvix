package com.mounvix.app.data.feed

import java.time.LocalTime
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive

/** Applies notification side effects before persisting a page's cursor. */
internal object FeedBatchSync {
    suspend fun run(
        fetch: suspend () -> FeedPage,
        notify: suspend (FeedCard) -> Unit,
        commit: (String) -> Unit,
        enabled: Boolean,
        localTime: LocalTime,
        quietStart: LocalTime = LocalTime.of(22, 0),
        quietEnd: LocalTime = LocalTime.of(7, 0),
    ) {
        val page = fetch()
        for (card in page.items) {
            currentCoroutineContext().ensureActive()
            if (FeedSyncPolicy.shouldNotify(card, enabled, localTime, quietStart, quietEnd)) notify(card)
        }
        currentCoroutineContext().ensureActive()
        commit(page.nextSince)
    }
}
