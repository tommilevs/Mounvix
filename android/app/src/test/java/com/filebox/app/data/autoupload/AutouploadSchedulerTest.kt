package com.mounvix.app.data.autoupload

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.work.BackoffPolicy
import androidx.work.Configuration
import androidx.work.NetworkType
import androidx.work.WorkManager
import androidx.work.testing.WorkManagerTestInitHelper
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.util.concurrent.TimeUnit

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [33], application = android.app.Application::class)
class AutouploadSchedulerTest {
    private lateinit var context: Context

    @Before
    fun setUp() {
        context = ApplicationProvider.getApplicationContext()
        WorkManagerTestInitHelper.initializeTestWorkManager(context, Configuration.Builder().build())
        WorkManager.getInstance(context).cancelAllWork()
    }

    @Test
    fun periodicRequestUsesFifteenMinutesAndConnectedNetwork() {
        val request = AutouploadScheduler.periodicRequest()

        assertEquals(15L * 60L * 1000L, request.workSpec.intervalDuration)
        assertEquals(NetworkType.CONNECTED, request.workSpec.constraints.requiredNetworkType)
    }

    @Test
    fun runNowRequestMarksItsModeAndUsesExponentialBackoff() {
        val request = AutouploadScheduler.runNowRequest()

        assertTrue(request.workSpec.input.getBoolean("run_now", false))
        assertEquals(NetworkType.CONNECTED, request.workSpec.constraints.requiredNetworkType)
        assertEquals(BackoffPolicy.EXPONENTIAL, request.workSpec.backoffPolicy)
        assertEquals(TimeUnit.MINUTES.toMillis(1), request.workSpec.backoffDelayDuration)
    }

    @Test
    fun repeatedSchedulingKeepsOnePeriodicWorkAndOneActiveRunNowWork() {
        AutouploadScheduler.schedule(context)
        AutouploadScheduler.schedule(context)
        AutouploadScheduler.runNow(context)
        AutouploadScheduler.runNow(context)

        val manager = WorkManager.getInstance(context)
        val periodic = manager.getWorkInfosForUniqueWork("mounvix-autoupload").get()
        val runNow = manager.getWorkInfosForUniqueWork("mounvix-autoupload-now").get()

        assertEquals(1, periodic.size)
        assertEquals(1, runNow.count { !it.state.isFinished })
    }
}
