package com.mounvix.app.data.feed

import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class FeedApiTest {
    private lateinit var server: MockWebServer
    private lateinit var api: FeedApi
    private lateinit var profile: FeedProfile

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
        api = FeedApi(OkHttpClient.Builder().cookieJar(FeedCookieJar()).build())
        profile = FeedProfile(server.url("/").toString().trimEnd('/'), "alice", "secret")
    }

    @After
    fun tearDown() {
        server.shutdown()
    }

    @Test
    fun loginStoresCookie() = runBlocking {
        server.enqueue(sessionResponse("sid-one"))
        server.enqueue(MockResponse().setResponseCode(204))

        assertTrue(api.login(profile).isSuccess)
        assertTrue(api.markRead("card-1").isSuccess)
        assertEquals("POST", server.takeRequest().method)
        val read = server.takeRequest()
        assertEquals("/api/v1/feeds/card-1/read", read.path)
        assertEquals("mvx=sid-one", read.getHeader("Cookie"))
    }

    @Test
    fun loginDoesNotForwardCredentialsAcrossRedirect() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(307).addHeader("Location", "/capture"))
        server.enqueue(sessionResponse("captured"))

        val result = api.login(profile)

        assertTrue(result.isFailure)
        assertEquals(1, server.requestCount)
        assertEquals("/api/v1/session", server.takeRequest().path)
    }

    @Test
    fun loginRejects401() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(401).setBody("unauthorized"))

        val result = api.login(profile)

        assertTrue(result.isFailure)
        assertFalse(result.isSuccess)
    }

    @Test
    fun feedRequestSendsCookie() = runBlocking {
        server.enqueue(sessionResponse("sid-feed"))
        server.enqueue(MockResponse().setResponseCode(200).setBody(feedResponse()))

        api.login(profile).getOrThrow()
        val page = api.getFeeds("2026-11-01T00:00:00Z", 12)

        val login = server.takeRequest()
        val request = server.takeRequest()
        assertEquals("/api/v1/session", login.path)
        assertEquals("/api/v1/feeds?since=2026-11-01T00%3A00%3A00Z&limit=12", request.path)
        assertEquals("mvx=sid-feed", request.getHeader("Cookie"))
        assertEquals("card-1", page.items.single().id)
        assertEquals("2026-11-02T03:04:05Z", page.nextSince)
    }

    @Test
    fun mediaFetchRejectsAbsoluteUrl() = runBlocking {
        val error = runCatching { api.fetchMedia("https://attacker.example/api/v1/file") }.exceptionOrNull()

        assertTrue("expected absolute URL rejection", error is IllegalArgumentException)
        assertEquals(0, server.requestCount)
    }

    @Test
    fun mediaFetchSendsCookie() = runBlocking {
        server.enqueue(sessionResponse("sid-media"))
        server.enqueue(MockResponse().setResponseCode(200).setBody("image bytes"))

        api.login(profile).getOrThrow()
        val bytes = api.fetchMedia("/api/v1/file?root=share&path=image.jpg")

        assertEquals("image bytes", bytes.decodeToString())
        assertEquals("/api/v1/session", server.takeRequest().path)
        val request = server.takeRequest()
        assertEquals("/api/v1/file?root=share&path=image.jpg", request.path)
        assertEquals("mvx=sid-media", request.getHeader("Cookie"))
    }

    @Test
    fun expiredSessionReloginOnce() = runBlocking {
        server.enqueue(sessionResponse("sid-expired"))
        server.enqueue(MockResponse().setResponseCode(401))
        server.enqueue(sessionResponse("sid-fresh"))
        server.enqueue(MockResponse().setResponseCode(200).setBody(feedResponse()))

        api.login(profile).getOrThrow()
        val page = api.getFeeds(null, 20)

        assertEquals("card-1", page.items.single().id)
        assertEquals(4, server.requestCount)
        assertEquals("/api/v1/session", server.takeRequest().path)
        assertEquals("mvx=sid-expired", server.takeRequest().getHeader("Cookie"))
        assertEquals("/api/v1/session", server.takeRequest().path)
        assertEquals("mvx=sid-fresh", server.takeRequest().getHeader("Cookie"))
    }

    @Test
    fun malformedResponseFails() = runBlocking {
        server.enqueue(sessionResponse("sid-malformed"))
        server.enqueue(MockResponse().setResponseCode(200).setBody("not-json"))
        api.login(profile).getOrThrow()

        val error = runCatching { api.getFeeds(null, 20) }.exceptionOrNull()

        assertTrue("expected malformed response failure", error != null)
        assertEquals(2, server.requestCount)
    }

    private fun sessionResponse(token: String) = MockResponse()
        .setResponseCode(200)
        .addHeader("Set-Cookie", "mvx=$token; Path=/; HttpOnly; SameSite=Lax")
        .setBody("""{"ok":true}""")

    private fun feedResponse() = """{"items":[{"id":"card-1","generated_at":"2026-11-02T03:04:00Z","title":"Memories","is_read":false,"media":[{"root":"share","path":"image.jpg","kind":"image","modified_at":"2026-11-01T03:04:00Z","thumbnail_url":"/api/v1/thumbs?root=share&path=image.jpg","file_url":"/api/v1/file?root=share&path=image.jpg"}]}],"next_since":"2026-11-02T03:04:05Z"}"""
}
