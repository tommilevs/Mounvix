package com.mounvix.app.data.feed

import java.io.IOException
import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class ServerNotificationApiTest {
    private lateinit var server: MockWebServer
    private lateinit var api: FeedApi
    private lateinit var profile: FeedProfile

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
        api = FeedApi(OkHttpClient.Builder().cookieJar(FeedCookieJar()).build())
        profile = FeedProfile(server.url("/mounvix/").toString().trimEnd('/'), "alice", "secret")
    }

    @After
    fun tearDown() {
        server.shutdown()
    }

    @Test
    fun notificationPageMatchesM7ContractAndSendsOriginAndProfileCookie() = runBlocking {
        server.enqueue(sessionResponse("sid-notifications"))
        server.enqueue(notificationResponse())
        api.login(profile).getOrThrow()

        val page = api.getServerNotifications(after = 2L, limit = 10)

        val login = server.takeRequest()
        val request = server.takeRequest()
        assertEquals("/mounvix/api/v1/session", login.path)
        assertEquals("/mounvix/api/v1/notifications?after=2&limit=10", request.path)
        assertEquals("mvx=sid-notifications", request.getHeader("Cookie"))
        assertEquals(server.url("/").toString().trimEnd('/'), request.getHeader("Origin"))
        assertEquals(1, page.items.size)
        assertEquals("event-3", page.items.single().eventId)
        assertEquals(3L, page.items.single().seq)
        assertEquals("2026-11-23T10:00:00Z", page.items.single().createdAt)
        assertEquals("plugin", page.items.single().kind)
        assertEquals("Task completed", page.items.single().summary)
        assertEquals("/mounvix/notifications?event=event-3", page.items.single().targetRoute)
        assertFalse(page.items.single().isRead)
        assertEquals(3L, page.nextCursor)
        assertEquals(null, page.beforeCursor)
        assertFalse(page.reset)
        assertEquals(1L, page.oldestSeq)
        assertEquals(3L, page.latestSeq)
        assertEquals(1, page.unreadCount)
    }

    @Test
    fun noCursorSupportsAnEmptyPageAndUsesDefaultLimit() = runBlocking {
        server.enqueue(sessionResponse("sid-empty"))
        server.enqueue(emptyPageResponse())
        api.login(profile).getOrThrow()

        val page = api.getServerNotifications(after = null)

        server.takeRequest()
        val request = server.takeRequest()
        assertEquals("/mounvix/api/v1/notifications?limit=100", request.path)
        assertTrue(page.items.isEmpty())
        assertEquals(0L, page.nextCursor)
        assertEquals(null, page.beforeCursor)
        assertFalse(page.reset)
        assertEquals(0L, page.oldestSeq)
        assertEquals(0L, page.latestSeq)
        assertEquals(0, page.unreadCount)
    }

    @Test
    fun resetPageCanRebaseCursorAtTheCurrentServerSequence() = runBlocking {
        server.enqueue(sessionResponse("sid-reset"))
        server.enqueue(resetPageResponse())
        api.login(profile).getOrThrow()

        val page = api.getServerNotifications(after = 50L, limit = 10)

        assertTrue(page.reset)
        assertTrue(page.items.isEmpty())
        assertEquals(12L, page.nextCursor)
        assertEquals(12L, page.latestSeq)
        assertEquals(2, server.requestCount)
    }

    @Test
    fun limitsAndCursorAreValidatedBeforeNetworkAccess() = runBlocking {
        api.setActiveProfile(profile)

        for (limit in listOf(0, 101)) {
            val error = runCatching { api.getServerNotifications(after = null, limit = limit) }.exceptionOrNull()
            assertTrue("expected limit rejection for $limit", error is IllegalArgumentException)
        }
        val negativeCursor = runCatching { api.getServerNotifications(after = -1L) }.exceptionOrNull()

        assertTrue("expected negative cursor rejection", negativeCursor is IllegalArgumentException)
        assertEquals(0, server.requestCount)
    }

    @Test
    fun unsafeTargetRoutesAreRejected() = runBlocking {
        val unsafeRoutes = listOf(
            "https://attacker.example/mounvix/notifications",
            "//attacker.example/notifications",
            "http://attacker.example/notifications",
        )
        server.enqueue(sessionResponse("sid-routes"))
        unsafeRoutes.forEach { server.enqueue(notificationResponse(route = it)) }
        api.login(profile).getOrThrow()

        for (route in unsafeRoutes) {
            val error = runCatching { api.getServerNotifications(after = 0L, limit = 10) }.exceptionOrNull()
            assertTrue("expected unsafe route rejection for $route", error is IOException)
        }
        assertEquals(unsafeRoutes.size + 1, server.requestCount)
    }

    @Test
    fun notificationCursorRegressionIsRejected() = runBlocking {
        server.enqueue(sessionResponse("sid-regression"))
        server.enqueue(notificationResponse(nextCursor = 4, latestSeq = 5, seq = 5))
        api.login(profile).getOrThrow()

        val error = runCatching { api.getServerNotifications(after = 5L, limit = 10) }.exceptionOrNull()

        assertTrue("expected cursor regression failure", error is IOException)
    }

    @Test
    fun notification401ReauthenticatesExactlyOnce() = runBlocking {
        server.enqueue(sessionResponse("sid-old"))
        server.enqueue(MockResponse().setResponseCode(401).setBody("unauthorized"))
        server.enqueue(sessionResponse("sid-new"))
        server.enqueue(notificationResponse())
        api.login(profile).getOrThrow()

        val page = api.getServerNotifications(after = 2L, limit = 10)

        assertEquals("event-3", page.items.single().eventId)
        assertEquals(4, server.requestCount)
        assertEquals("/mounvix/api/v1/session", server.takeRequest().path)
        assertEquals("/mounvix/api/v1/notifications?after=2&limit=10", server.takeRequest().path)
        assertEquals("/mounvix/api/v1/session", server.takeRequest().path)
        assertEquals("/mounvix/api/v1/notifications?after=2&limit=10", server.takeRequest().path)
    }

    @Test
    fun secondNotification401FailsAfterSingleReauthentication() = runBlocking {
        server.enqueue(sessionResponse("sid-old"))
        server.enqueue(MockResponse().setResponseCode(401).setBody("unauthorized"))
        server.enqueue(sessionResponse("sid-new"))
        server.enqueue(MockResponse().setResponseCode(401).setBody("unauthorized"))
        api.login(profile).getOrThrow()

        val error = runCatching { api.getServerNotifications(after = 2L, limit = 10) }.exceptionOrNull()

        assertTrue(error is IOException)
        assertTrue(error?.message.orEmpty().contains("session expired"))
        assertEquals(4, server.requestCount)
    }

    @Test
    fun forbiddenStatusIsNotTreatedAsAnEmptyPage() = runBlocking {
        server.enqueue(sessionResponse("sid-forbidden"))
        server.enqueue(MockResponse().setResponseCode(403).setBody("forbidden"))
        api.login(profile).getOrThrow()

        val error = runCatching { api.getServerNotifications(after = 0L, limit = 10) }.exceptionOrNull()

        assertTrue(error is IOException)
        assertTrue(error?.message.orEmpty().contains("HTTP 403"))
        assertEquals(2, server.requestCount)
    }

    @Test
    fun loginFeedReadAndMediaRequestsPreserveTheConfiguredBasePath() = runBlocking {
        server.enqueue(sessionResponse("sid-prefix"))
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"items":[],"next_since":"2026-11-23T10:00:00Z"}"""))
        server.enqueue(MockResponse().setResponseCode(204))
        server.enqueue(MockResponse().setResponseCode(200).setBody("image bytes"))
        api.login(profile).getOrThrow()

        api.getFeeds(since = null, limit = 10)
        assertTrue(api.markRead("card-1").isSuccess)
        assertEquals("image bytes", api.fetchMedia("/api/v1/file?root=share&path=image.jpg").decodeToString())

        assertEquals("/mounvix/api/v1/session", server.takeRequest().path)
        assertEquals("/mounvix/api/v1/feeds?limit=10", server.takeRequest().path)
        assertEquals("/mounvix/api/v1/feeds/card-1/read", server.takeRequest().path)
        assertEquals("/mounvix/api/v1/file?root=share&path=image.jpg", server.takeRequest().path)
    }

    private fun sessionResponse(token: String) = MockResponse()
        .setResponseCode(200)
        .addHeader("Set-Cookie", "mvx=$token; Path=/mounvix; HttpOnly; SameSite=Lax")
        .setBody("""{"ok":true}""")

    private fun notificationResponse(
        route: String = "/mounvix/notifications?event=event-3",
        nextCursor: Long = 3,
        latestSeq: Long = 3,
        seq: Long = 3,
    ) = MockResponse().setResponseCode(200).setBody(
        """{"items":[{"event_id":"event-3","seq":$seq,"created_at":"2026-11-23T10:00:00Z","kind":"plugin","summary":"Task completed","target_route":"$route","is_read":false}],"next_cursor":$nextCursor,"before_cursor":null,"reset":false,"oldest_seq":1,"latest_seq":$latestSeq,"unread_count":1}""",
    )

    private fun emptyPageResponse() = MockResponse().setResponseCode(200).setBody(
        """{"items":[],"next_cursor":0,"before_cursor":null,"reset":false,"oldest_seq":0,"latest_seq":0,"unread_count":0}""",
    )

    private fun resetPageResponse() = MockResponse().setResponseCode(200).setBody(
        """{"items":[],"next_cursor":12,"before_cursor":null,"reset":true,"oldest_seq":8,"latest_seq":12,"unread_count":2}""",
    )
}
