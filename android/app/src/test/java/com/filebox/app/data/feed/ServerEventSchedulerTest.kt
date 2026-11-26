package com.mounvix.app.data.feed

import android.content.Context
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import androidx.work.Configuration
import androidx.work.NetworkType
import androidx.work.WorkInfo
import androidx.work.WorkManager
import androidx.work.testing.WorkManagerTestInitHelper
import com.mounvix.app.data.db.AppDatabase
import java.util.concurrent.TimeUnit
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [33], application = android.app.Application::class)
class ServerEventSchedulerTest {
    private lateinit var context: Context
    private lateinit var database: AppDatabase
    private lateinit var profile: FeedProfile
    private lateinit var statusStore: ServerEventStatusStore

    @Before
    fun setUp() {
        context = ApplicationProvider.getApplicationContext()
        database = Room.inMemoryDatabaseBuilder(context, AppDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        profile = FeedProfile("https://nas.example/mounvix", "alice", "secret")
        statusStore = ServerEventStatusStore(context.getSharedPreferences("mounvix_prefs", Context.MODE_PRIVATE))
        context.getSharedPreferences("mounvix_prefs", Context.MODE_PRIVATE).edit().clear().commit()
        WorkManagerTestInitHelper.initializeTestWorkManager(context, Configuration.Builder().build())
    }

    @After
    fun tearDown() {
        database.close()
    }

    @Test
    fun inboxIsDisabledByDefaultAndUsesConnectedFifteenMinuteWork() {
        assertFalse(statusStore.isEnabled(profile))
        assertEquals(ServerEventAccessStatus.UNVERIFIED, statusStore.status(profile).access)

        statusStore.setEnabled(profile, true)
        assertFalse(statusStore.canPoll(profile))
        statusStore.markReady(profile)
        ServerEventScheduler.schedule(context, profile)
        ServerEventScheduler.schedule(context, profile)

        val request = ServerEventScheduler.request()
        assertEquals(15L, TimeUnit.MILLISECONDS.toMinutes(request.workSpec.intervalDuration))
        assertEquals(NetworkType.CONNECTED, request.workSpec.constraints.requiredNetworkType)
        assertEquals(1, WorkManager.getInstance(context).getWorkInfosForUniqueWork(ServerEventScheduler.UNIQUE_WORK_NAME).get().size)
    }

    @Test
    fun disablingCancelsPollingButPreservesCursor() {
        val cursors = ServerEventRepository(database.serverEventCursorDao()) { 10L }
        cursors.commitPage(profile, 9L)
        statusStore.setEnabled(profile, true)
        statusStore.markReady(profile)
        ServerEventScheduler.schedule(context, profile)

        statusStore.setEnabled(profile, false)
        ServerEventScheduler.cancel(context)

        val work = WorkManager.getInstance(context).getWorkInfosForUniqueWork(ServerEventScheduler.UNIQUE_WORK_NAME).get().single()
        assertEquals(WorkInfo.State.CANCELLED, work.state)
        assertEquals(9L, cursors.loadCursor(profile))
    }

    @Test
    fun schedulerReconcilesWorkForTheCurrentProfileAfterAProfileSwitch() {
        val activeProfile = profile.copy(username = "bob")
        statusStore.setEnabled(activeProfile, true)
        statusStore.markReady(activeProfile)

        ServerEventScheduler.reconcile(context, activeProfile, statusStore)

        val work = WorkManager.getInstance(context)
            .getWorkInfosForUniqueWork(ServerEventScheduler.UNIQUE_WORK_NAME).get().single()
        assertEquals(WorkInfo.State.ENQUEUED, work.state)
    }

    @Test
    fun unsupportedStateIsStoredPerProfileAndBlocksSchedulingUntilProbe() {
        val other = profile.copy(username = "bob")
        statusStore.setEnabled(profile, true)
        statusStore.markReady(profile)
        ServerEventScheduler.schedule(context, profile)
        statusStore.markUnsupported(profile, "1.2.3")
        ServerEventScheduler.schedule(context, profile)

        val work = WorkManager.getInstance(context)
            .getWorkInfosForUniqueWork(ServerEventScheduler.UNIQUE_WORK_NAME).get().single()
        assertEquals(WorkInfo.State.CANCELLED, work.state)
        assertEquals(ServerEventAccessStatus.UNSUPPORTED, statusStore.status(profile).access)
        assertEquals("1.2.3", statusStore.status(profile).healthVersion)
        assertEquals(ServerEventAccessStatus.UNVERIFIED, statusStore.status(other).access)
        assertNull(statusStore.status(other).healthVersion)
        assertFalse(statusStore.canPoll(profile))
        assertTrue(statusStore.canPoll(other).not())

        statusStore.markReady(profile)
        assertTrue(statusStore.canPoll(profile))
    }
}
