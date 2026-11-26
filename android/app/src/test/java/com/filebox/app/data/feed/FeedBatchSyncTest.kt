package com.mounvix.app.data.feed

import java.io.IOException
import java.time.LocalTime
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class FeedBatchSyncTest {
    private val card = FeedCard("card-1", "2026-11-18T00:00:00Z", "Memories", emptyList(), false)
    private val page = FeedPage(listOf(card), "2026-11-18T01:00:00Z")

    @Test fun failedDeliveryDoesNotCommitCursor() = runBlocking {
        val commits = mutableListOf<String>()
        val result = runCatching {
            FeedBatchSync.run({ page }, { throw IOException("delivery failed") }, commits::add, true, LocalTime.NOON)
        }
        assertTrue(result.isFailure)
        assertTrue(commits.isEmpty())
    }

    @Test fun repeatedBatchUsesStableCardIdBeforeCommitting() = runBlocking {
        val events = mutableListOf<String>()
        repeat(2) {
            FeedBatchSync.run({ page }, { events.add("notify:${it.id}") }, { events.add("commit:$it") }, true, LocalTime.NOON)
        }
        assertEquals(listOf("notify:card-1", "commit:2026-11-18T01:00:00Z", "notify:card-1", "commit:2026-11-18T01:00:00Z"), events)
    }

    @Test fun workerRetriesAfterDeliveryFailureWithoutCommitting() = runBlocking {
        val commits = mutableListOf<String>()
        val outcome = FeedSyncRunner.run({ page }, { throw IOException("delivery failed") }, commits::add, true, LocalTime.NOON)
        assertEquals(FeedSyncRunner.Outcome.RETRY, outcome)
        assertTrue(commits.isEmpty())
    }

    @Test fun workerPropagatesCancellation() = runBlocking {
        val result = runCatching {
            FeedSyncRunner.run({ throw kotlinx.coroutines.CancellationException("cancelled") }, { }, { }, true, LocalTime.NOON)
        }
        assertTrue(result.exceptionOrNull() is kotlinx.coroutines.CancellationException)
    }

    @Test fun fetchFailureDoesNotCommitCursor() = runBlocking {
        val commits = mutableListOf<String>()
        val result = runCatching {
            FeedBatchSync.run({ throw IOException("offline") }, { }, commits::add, true, LocalTime.NOON)
        }
        assertTrue(result.isFailure)
        assertTrue(commits.isEmpty())
    }
}
