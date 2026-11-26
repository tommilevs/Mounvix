package com.mounvix.app.data.feed

import android.content.Context
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import com.mounvix.app.data.db.AppDatabase
import java.io.IOException
import java.time.LocalTime
import java.util.ArrayDeque
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [33], application = android.app.Application::class)
class ServerEventWorkerTest {
    private lateinit var context: Context
    private lateinit var database: AppDatabase
    private lateinit var profile: FeedProfile
    private lateinit var repository: ServerEventRepository
    private lateinit var statusStore: ServerEventStatusStore
    private val quietHours = ServerEventQuietHours(LocalTime.of(22, 0), LocalTime.of(7, 0), LocalTime.NOON)

    @Before
    fun setUp() {
        context = ApplicationProvider.getApplicationContext()
        database = Room.inMemoryDatabaseBuilder(context, AppDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        repository = ServerEventRepository(database.serverEventCursorDao()) { 100L }
        profile = FeedProfile("https://nas.example/mounvix", "alice", "secret")
        val preferences = context.getSharedPreferences("mounvix_prefs", Context.MODE_PRIVATE)
        preferences.edit().clear().commit()
        statusStore = ServerEventStatusStore(preferences)
        statusStore.setEnabled(profile, true)
        statusStore.markReady(profile)
    }

    @After
    fun tearDown() {
        database.close()
    }

    @Test
    fun unverifiedProfileDoesNotPollBeforeCapabilityCheck() = runBlocking {
        statusStore.clearProfile(profile)
        statusStore.setEnabled(profile, true)
        val api = FakeApi()

        assertEquals(ServerEventSyncOutcome.STOPPED, runner(api).run(profile))
        assertTrue(api.afterRequests.isEmpty())
        assertEquals(null, repository.loadCursor(profile))
    }

    @Test
    fun firstEnableBaselinesLatestWithoutAlertingRetainedHistory() = runBlocking {
        val api = FakeApi(listOf(page(items = listOf(event(9L), event(8L)), next = 9L, latest = 9L)))
        val alerts = mutableListOf<ServerEventAlert>()

        val result = runner(api, alerts::add).run(profile)

        assertEquals(ServerEventSyncOutcome.SUCCESS, result)
        assertEquals(9L, repository.loadCursor(profile))
        assertTrue(alerts.isEmpty())
        assertEquals(listOf(null), api.afterRequests)
    }

    @Test
    fun processesAtMostFivePagesAndCommitsEachCompletedPage() = runBlocking {
        repository.commitPage(profile, 0L)
        val pages = (0 until 6).map { pageIndex ->
            val first = pageIndex * 100L + 1L
            val last = first + 99L
            page((first..last).map { event(it, isRead = true) }, next = last, latest = 600L)
        }
        val api = FakeApi(pages)
        val alerts = mutableListOf<ServerEventAlert>()

        val result = runner(api, alerts::add).run(profile)

        assertEquals(ServerEventSyncOutcome.SUCCESS, result)
        assertEquals(listOf(0L, 100L, 200L, 300L, 400L), api.afterRequests)
        assertEquals(500L, repository.loadCursor(profile))
        assertTrue(alerts.isEmpty())
    }

    @Test
    fun failedNotificationSideEffectLeavesThatPageCursorUncommittedForSafeRetry() = runBlocking {
        repository.commitPage(profile, 0L)
        val response = page(listOf(event(1L)), next = 1L, latest = 1L)
        val api = FakeApi(listOf(response, response))
        var failOnce = true
        val alerts = mutableListOf<ServerEventAlert>()
        val show: (ServerEventAlert) -> Unit = {
            alerts += it
            if (failOnce) {
                failOnce = false
                throw IOException("notification manager unavailable")
            }
        }

        assertEquals(ServerEventSyncOutcome.RETRY, runner(api, show).run(profile))
        assertEquals(0L, repository.loadCursor(profile))
        assertEquals(ServerEventSyncOutcome.SUCCESS, runner(api, show).run(profile))
        assertEquals(1L, repository.loadCursor(profile))
        assertEquals(listOf("event-1", "event-1"), alerts.map { it.eventId })
    }

    @Test
    fun second401MarksProfileNeedsLoginAndStopsPolling() = runBlocking {
        repository.commitPage(profile, 3L)
        val api = FakeApi(failures = listOf(MounvixHttpException(401, "session expired")))

        val result = runner(api).run(profile)

        assertEquals(ServerEventSyncOutcome.STOPPED, result)
        assertEquals(ServerEventAccessStatus.NEEDS_LOGIN, statusStore.status(profile).access)
        assertEquals(3L, repository.loadCursor(profile))
        assertEquals(1, api.afterRequests.size)
    }

    @Test
    fun forbiddenStopsOnlyServerInboxAndDoesNotChangeMemoriesPreference() = runBlocking {
        repository.commitPage(profile, 3L)
        val prefs = context.getSharedPreferences("mounvix_prefs", Context.MODE_PRIVATE)
        prefs.edit().putBoolean(FeedWorker.NOTIFICATIONS_ENABLED, true).commit()
        val api = FakeApi(failures = listOf(MounvixHttpException(403, "admin required")))

        assertEquals(ServerEventSyncOutcome.STOPPED, runner(api).run(profile))

        assertEquals(ServerEventAccessStatus.FORBIDDEN, statusStore.status(profile).access)
        assertFalse(statusStore.canPoll(profile))
        assertTrue(prefs.getBoolean(FeedWorker.NOTIFICATIONS_ENABLED, false))
        assertEquals(3L, repository.loadCursor(profile))
    }

    @Test
    fun unsupported404And501PersistHealthVersionAndDoNotAdvanceCursor() = runBlocking {
        repository.commitPage(profile, 4L)
        for (code in listOf(404, 501)) {
            statusStore.markReady(profile)
            val api = FakeApi(failures = listOf(MounvixHttpException(code, "unsupported")), healthVersion = "1.2.3")

            assertEquals(ServerEventSyncOutcome.STOPPED, runner(api).run(profile))
            assertEquals(ServerEventAccessStatus.UNSUPPORTED, statusStore.status(profile).access)
            assertEquals("1.2.3", statusStore.status(profile).healthVersion)
            assertEquals(4L, repository.loadCursor(profile))
        }
    }

    @Test
    fun transientHttpAndNetworkFailuresRetryWithoutAdvancingCursor() = runBlocking {
        repository.commitPage(profile, 4L)
        for (failure in listOf(MounvixHttpException(429, "limited"), MounvixHttpException(503, "unavailable"), IOException("offline"))) {
            statusStore.markReady(profile)
            val api = FakeApi(failures = listOf(failure))

            assertEquals(ServerEventSyncOutcome.RETRY, runner(api).run(profile))
            assertEquals(4L, repository.loadCursor(profile))
            assertEquals(ServerEventAccessStatus.READY, statusStore.status(profile).access)
        }
    }

    @Test
    fun permissionQuietHoursAndForegroundSuppressAlertsButStillCommitCursor() = runBlocking {
        repository.commitPage(profile, 0L)
        val response = page(listOf(event(1L)), next = 1L, latest = 1L)

        val denied = runner(FakeApi(listOf(response)), notificationsAllowed = { false })
        assertEquals(ServerEventSyncOutcome.SUCCESS, denied.run(profile))
        assertEquals(1L, repository.loadCursor(profile))

        repository.commitPage(profile, 0L)
        val quiet = runner(FakeApi(listOf(response)), quiet = {
            ServerEventQuietHours(LocalTime.of(22, 0), LocalTime.of(7, 0), LocalTime.of(23, 0))
        })
        assertEquals(ServerEventSyncOutcome.SUCCESS, quiet.run(profile))
        assertEquals(1L, repository.loadCursor(profile))

        repository.commitPage(profile, 0L)
        val foreground = runner(FakeApi(listOf(response)), foreground = { true })
        assertEquals(ServerEventSyncOutcome.SUCCESS, foreground.run(profile))
        assertEquals(1L, repository.loadCursor(profile))
    }

    private fun runner(
        api: FakeApi,
        showAlert: (ServerEventAlert) -> Unit = {},
        notificationsAllowed: () -> Boolean = { true },
        quiet: () -> ServerEventQuietHours = { quietHours },
        foreground: () -> Boolean = { false },
    ) = ServerEventSyncRunner(
        api = api,
        repository = repository,
        statusStore = statusStore,
        showAlert = showAlert,
        notificationsAllowed = notificationsAllowed,
        quietHours = quiet,
        advancedModeForeground = foreground,
    )

    private fun page(items: List<ServerNotification>, next: Long, latest: Long) = ServerNotificationsPage(
        items = items,
        nextCursor = next,
        beforeCursor = null,
        reset = false,
        oldestSeq = items.minOfOrNull { it.seq } ?: 0L,
        latestSeq = latest,
        unreadCount = items.count { !it.isRead },
    )

    private fun event(seq: Long, isRead: Boolean = false) = ServerNotification(
        eventId = "event-$seq",
        seq = seq,
        createdAt = "2026-11-23T10:00:00Z",
        kind = "plugin",
        summary = "Task completed safely.",
        targetRoute = "/mounvix/notifications/event-$seq",
        isRead = isRead,
    )

    private class FakeApi(
        pages: List<ServerNotificationsPage> = emptyList(),
        failures: List<Throwable> = emptyList(),
        private val healthVersion: String? = null,
    ) : ServerEventApi {
        private val results = ArrayDeque<Result<ServerNotificationsPage>>().apply {
            pages.forEach { add(Result.success(it)) }
            failures.forEach { add(Result.failure(it)) }
        }
        val afterRequests = mutableListOf<Long?>()

        override suspend fun getServerNotifications(after: Long?, limit: Int): ServerNotificationsPage {
            afterRequests += after
            return results.removeFirst().getOrThrow()
        }

        override suspend fun getHealthVersion(): String? = healthVersion
    }
}
