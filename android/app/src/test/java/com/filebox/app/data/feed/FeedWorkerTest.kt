package com.mounvix.app.data.feed

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.work.Configuration
import androidx.work.ListenableWorker
import androidx.work.NetworkType
import androidx.work.WorkInfo
import androidx.work.WorkManager
import androidx.work.testing.TestListenableWorkerBuilder
import androidx.work.testing.WorkManagerTestInitHelper
import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.After
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [33], application = android.app.Application::class)
class FeedWorkerTest {
    private lateinit var context: Context
    private lateinit var repository: FeedRepository

    @Before fun setUp() {
        context = ApplicationProvider.getApplicationContext()
        val preferences = context.getSharedPreferences("mounvix_prefs", Context.MODE_PRIVATE)
        preferences.edit().clear().commit()
        repository = FeedRepository(
            api = FeedApi(OkHttpClient.Builder().cookieJar(FeedCookieJar()).build()),
            preferences = preferences,
            secretStore = InMemoryProfileSecretStore(),
        )
        FeedRepositoryFactory.factory = { repository }
        WorkManagerTestInitHelper.initializeTestWorkManager(context, Configuration.Builder().build())
    }

    @After fun tearDown() {
        FeedRepositoryFactory.factory = { context -> FeedRepository.from(context) }
    }

    @Test fun repeatedCardReplacesNotification() {
        org.robolectric.Shadows.shadowOf(context as android.app.Application)
            .grantPermissions(android.Manifest.permission.POST_NOTIFICATIONS)
        val card = FeedCard("card-1", "2026-11-18T00:00:00Z", "First", emptyList(), false)
        com.mounvix.app.data.notify.FeedNotifier.showNewFeed(context, card)
        com.mounvix.app.data.notify.FeedNotifier.showNewFeed(context, card.copy(title = "Updated"))
        val posted = context.getSystemService(android.app.NotificationManager::class.java).activeNotifications.single()
        assertEquals("card-1", posted.tag)
        assertEquals(0, posted.id)
        assertEquals("Updated", posted.notification.extras.getCharSequence(android.app.Notification.EXTRA_TEXT))
    }

    @Test fun doesNotPostWhenNotificationPermissionIsDenied() {
        org.robolectric.Shadows.shadowOf(context as android.app.Application)
            .denyPermissions(android.Manifest.permission.POST_NOTIFICATIONS)
        val card = FeedCard("card-denied", "2026-11-18T00:00:00Z", "Hidden", emptyList(), false)

        com.mounvix.app.data.notify.FeedNotifier.showNewFeed(context, card)

        assertTrue(context.getSystemService(android.app.NotificationManager::class.java).activeNotifications.isEmpty())
    }

    @Test fun migratesLegacyProfileFixtureBeforeWorkerUsesIt() {
        val prefs = context.getSharedPreferences("mounvix_prefs", Context.MODE_PRIVATE)
        prefs.edit()
            .putString("feed_profile_base_url", "http://127.0.0.1:1")
            .putString("feed_profile_username", "alice")
            .putString("feed_profile_password", "secret")
            .commit()

        val profile = repository.loadProfile()

        assertNotNull("legacy worker profile should migrate to secure storage", profile)
        assertFalse(prefs.contains("feed_profile_password"))
    }

    @Test fun retriesNetworkErrorWithoutAdvancingCursor() = runBlocking {
        val prefs = context.getSharedPreferences("mounvix_prefs", Context.MODE_PRIVATE)
        val cursor = "2026-11-18T00:00:00Z"
        prefs.edit()
            .putString("feed_profile_base_url", "http://127.0.0.1:1")
            .putString("feed_profile_username", "alice")
            .putString("feed_profile_password", "secret")
            .putString("feed_cursor", cursor)
            .putBoolean(FeedWorker.NOTIFICATIONS_ENABLED, true)
            .commit()

        val result = TestListenableWorkerBuilder<FeedWorker>(context).build().doWork()

        assertTrue(result is ListenableWorker.Result.Retry)
        assertEquals(cursor, prefs.getString("feed_cursor", null))
    }

    @Test fun seedsInitialCursorBeforeFirstPoll() = runBlocking {
        val prefs = context.getSharedPreferences("mounvix_prefs", Context.MODE_PRIVATE)
        prefs.edit()
            .putString("feed_profile_base_url", "http://127.0.0.1:1")
            .putString("feed_profile_username", "alice")
            .putString("feed_profile_password", "secret")
            .putBoolean(FeedWorker.NOTIFICATIONS_ENABLED, true)
            .commit()

        val result = TestListenableWorkerBuilder<FeedWorker>(context).build().doWork()

        assertTrue(result is ListenableWorker.Result.Retry)
        assertTrue(!prefs.getString("feed_cursor", null).isNullOrBlank())
    }

    @Test fun enqueuesUnique15MinuteWork() {
        FeedScheduler.schedule(context)
        FeedScheduler.schedule(context)
        val manager = WorkManager.getInstance(context)
        val infos = manager.getWorkInfosForUniqueWork("mounvix-memories-feed").get()
        assertEquals(1, infos.size)
        val request = FeedScheduler.request()
        assertEquals(15L * 60 * 1000, request.workSpec.intervalDuration)
        assertEquals(NetworkType.CONNECTED, request.workSpec.constraints.requiredNetworkType)
    }

    @Test fun cancelStopsUniquePeriodicWork() {
        FeedScheduler.schedule(context)
        FeedScheduler.cancel(context)

        val infos = WorkManager.getInstance(context).getWorkInfosForUniqueWork("mounvix-memories-feed").get()

        assertEquals(WorkInfo.State.CANCELLED, infos.single().state)
    }
}
