package com.mounvix.app.data.feed

import android.content.Context
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.NetworkType
import androidx.work.PeriodicWorkRequest
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import java.util.concurrent.TimeUnit

/** One network-constrained server inbox poll; disabled profiles keep their Room cursor. */
object ServerEventScheduler {
    const val UNIQUE_WORK_NAME = "mounvix-server-events"

    fun schedule(context: Context, profile: FeedProfile) {
        val appContext = context.applicationContext
        val preferences = appContext.getSharedPreferences("mounvix_prefs", Context.MODE_PRIVATE)
        reconcile(appContext, profile, ServerEventStatusStore(preferences))
    }

    /** Restore the unique work for whichever profile is active after a worker stops. */
    fun reconcile(context: Context, profile: FeedProfile?, statusStore: ServerEventStatusStore) {
        val appContext = context.applicationContext
        if (profile == null || !statusStore.canPoll(profile)) {
            cancel(appContext)
            return
        }
        WorkManager.getInstance(appContext).enqueueUniquePeriodicWork(
            UNIQUE_WORK_NAME,
            ExistingPeriodicWorkPolicy.UPDATE,
            request(),
        )
    }

    fun cancel(context: Context) {
        WorkManager.getInstance(context.applicationContext).cancelUniqueWork(UNIQUE_WORK_NAME)
    }

    internal fun request(): PeriodicWorkRequest =
        PeriodicWorkRequestBuilder<ServerEventWorker>(15, TimeUnit.MINUTES)
            .setConstraints(
                Constraints.Builder()
                    .setRequiredNetworkType(NetworkType.CONNECTED)
                    .build(),
            )
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 30, TimeUnit.SECONDS)
            .build()
}
