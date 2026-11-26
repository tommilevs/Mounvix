package com.mounvix.app.data.autoupload

import android.content.Context
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.Data
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import java.util.concurrent.TimeUnit

/** Планирование автозагрузки через WorkManager. */
object AutouploadScheduler {

    internal const val PERIODIC_UNIQUE_WORK = "mounvix-autoupload"
    internal const val RUN_NOW_UNIQUE_WORK = "mounvix-autoupload-now"

    fun schedule(ctx: Context) {
        WorkManager.getInstance(ctx).enqueueUniquePeriodicWork(
            PERIODIC_UNIQUE_WORK,
            ExistingPeriodicWorkPolicy.UPDATE,
            periodicRequest()
        )
    }

    internal fun periodicRequest() = PeriodicWorkRequestBuilder<AutouploadWorker>(15, TimeUnit.MINUTES)
        .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
        .build()

    fun cancel(ctx: Context) {
        WorkManager.getInstance(ctx).cancelUniqueWork(PERIODIC_UNIQUE_WORK)
    }

    fun runNow(ctx: Context) {
        WorkManager.getInstance(ctx).enqueueUniqueWork(RUN_NOW_UNIQUE_WORK, ExistingWorkPolicy.REPLACE, runNowRequest())
    }

    internal fun runNowRequest() = OneTimeWorkRequestBuilder<AutouploadWorker>()
        .setInputData(Data.Builder().putBoolean("run_now", true).build())
        .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
        .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 1, TimeUnit.MINUTES)
        .build()
}
