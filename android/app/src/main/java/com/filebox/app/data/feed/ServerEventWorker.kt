package com.mounvix.app.data.feed

import android.content.Context
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import com.mounvix.app.data.db.AppDatabase
import java.time.LocalTime
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/** Background REST poller; WorkManager owns retries and never holds a persistent socket. */
class ServerEventWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result = withContext(Dispatchers.IO) {
        val appContext = applicationContext
        val preferences = appContext.getSharedPreferences("mounvix_prefs", Context.MODE_PRIVATE)
        val statusStore = ServerEventStatusStore(preferences)
        val profileRepository = FeedRepository.from(appContext)
        val profile = profileRepository.loadProfile()
        if (profile == null) {
            ServerEventScheduler.cancel(appContext)
            return@withContext Result.success()
        }
        val cursorRepository = ServerEventRepository(AppDatabase.get(appContext).serverEventCursorDao())
        val runner = ServerEventSyncRunner(
            api = FeedApi.shared(),
            repository = cursorRepository,
            statusStore = statusStore,
            showAlert = { alert -> ServerEventNotifier.show(appContext, alert) },
            notificationsAllowed = { ServerEventNotifier.areNotificationsAllowed(appContext) },
            quietHours = {
                ServerEventQuietHours(
                    start = time(preferences, FeedWorker.QUIET_START, LocalTime.of(22, 0)),
                    end = time(preferences, FeedWorker.QUIET_END, LocalTime.of(7, 0)),
                    now = LocalTime.now(),
                )
            },
            advancedModeForeground = ServerEventForegroundState::isAdvancedModeForeground,
        )
        val outcome = try {
            runner.run(profile) {
                profileRepository.loadProfile() == profile && statusStore.isEnabled(profile)
            }
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (_: Exception) {
            ServerEventSyncOutcome.RETRY
        }
        when (outcome) {
            ServerEventSyncOutcome.SUCCESS -> Result.success()
            ServerEventSyncOutcome.RETRY -> Result.retry()
            ServerEventSyncOutcome.STOPPED -> {
                ServerEventScheduler.reconcile(appContext, profileRepository.loadProfile(), statusStore)
                Result.success()
            }
        }
    }

    private fun time(
        preferences: android.content.SharedPreferences,
        key: String,
        fallback: LocalTime,
    ): LocalTime = preferences.getString(key, null)?.let { runCatching { LocalTime.parse(it) }.getOrNull() } ?: fallback
}
