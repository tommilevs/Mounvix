package com.mounvix.app.data.feed

import android.content.Context
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import com.mounvix.app.data.notify.FeedNotifier
import java.time.Instant
import java.time.LocalTime
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/** Narrow repository seam so worker behavior can be tested without a platform Android Keystore. */
internal object FeedRepositoryFactory {
    @Volatile
    var factory: (Context) -> FeedRepository = { context -> FeedRepository.from(context) }

    fun create(context: Context): FeedRepository = factory(context.applicationContext)
}

class FeedWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result = withContext(Dispatchers.IO) {
        val prefs = applicationContext.getSharedPreferences("mounvix_prefs", Context.MODE_PRIVATE)
        if (!prefs.getBoolean(NOTIFICATIONS_ENABLED, false)) return@withContext Result.success()
        val repository = FeedRepositoryFactory.create(applicationContext)
        val profile = repository.loadProfile() ?: return@withContext Result.success()
        repository.initializeCursorIfAbsent(Instant.now().toString())
        fun stillActive() = repository.loadProfile() == profile && prefs.getBoolean(NOTIFICATIONS_ENABLED, false)
        fun time(key: String, fallback: LocalTime): LocalTime =
            prefs.getString(key, null)?.let { runCatching { LocalTime.parse(it) }.getOrNull() } ?: fallback

        when (FeedSyncRunner.run(
            fetch = repository::fetchNewSinceCursor,
            notify = { if (stillActive()) FeedNotifier.showNewFeed(applicationContext, it) },
            commit = { if (stillActive()) repository.commitCursor(it) },
            enabled = true,
            localTime = LocalTime.now(),
            quietStart = time(QUIET_START, LocalTime.of(22, 0)),
            quietEnd = time(QUIET_END, LocalTime.of(7, 0)),
        )) {
            FeedSyncRunner.Outcome.SUCCESS -> Result.success()
            FeedSyncRunner.Outcome.RETRY -> Result.retry()
        }
    }

    companion object {
        const val NOTIFICATIONS_ENABLED = "feed_notifications_enabled"
        const val QUIET_START = "feed_quiet_start"
        const val QUIET_END = "feed_quiet_end"
    }
}
