package com.mounvix.app.data.autoupload

import android.content.Context
import androidx.work.Constraints
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import java.util.concurrent.TimeUnit

/** Планирование автозагрузки через WorkManager. */
object AutouploadScheduler {

    private const val PERIODIC = "mounvix-autoupload"
    private const val ONCE = "mounvix-autoupload-now"

    fun schedule(ctx: Context) {
        val req = PeriodicWorkRequestBuilder<AutouploadWorker>(15, TimeUnit.MINUTES)
            .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
            .build()
        WorkManager.getInstance(ctx).enqueueUniquePeriodicWork(PERIODIC, ExistingPeriodicWorkPolicy.UPDATE, req)
    }

    fun cancel(ctx: Context) {
        WorkManager.getInstance(ctx).cancelUniqueWork(PERIODIC)
    }

    fun runNow(ctx: Context) {
        val req = OneTimeWorkRequestBuilder<AutouploadWorker>()
            .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
            .build()
        WorkManager.getInstance(ctx).enqueueUniqueWork(ONCE, ExistingWorkPolicy.REPLACE, req)
    }
}
