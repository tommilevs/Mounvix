package com.mounvix.app.data.feed

import java.time.LocalTime
import kotlinx.coroutines.CancellationException

internal object FeedSyncRunner {
    enum class Outcome { SUCCESS, RETRY }

    suspend fun run(
        fetch: suspend () -> FeedPage,
        notify: suspend (FeedCard) -> Unit,
        commit: (String) -> Unit,
        enabled: Boolean,
        localTime: LocalTime,
        quietStart: LocalTime = LocalTime.of(22, 0),
        quietEnd: LocalTime = LocalTime.of(7, 0),
    ): Outcome = try {
        FeedBatchSync.run(fetch, notify, commit, enabled, localTime, quietStart, quietEnd)
        Outcome.SUCCESS
    } catch (cancelled: CancellationException) {
        throw cancelled
    } catch (_: Exception) {
        Outcome.RETRY
    }
}
