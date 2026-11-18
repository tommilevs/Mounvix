package com.mounvix.app.data.net

import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class UpdateCheckerTest {

    private lateinit var server: MockWebServer

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
    }

    @After
    fun tearDown() {
        server.shutdown()
    }

    private fun checker(): UpdateChecker =
        UpdateChecker(repo = "tommilevs/Mounvix", baseUrl = server.url("/").toString().trimEnd('/'))

    @Test
    fun newerReleaseIsAvailable() {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """{"tag_name":"v0.2.0","html_url":"https://github.com/tommilevs/Mounvix/releases/tag/v0.2.0","assets":[{"name":"mounvix-android-0.2.0.apk","browser_download_url":"https://example.com/mounvix-android-0.2.0.apk"}]}"""
            )
        )
        val info = checker().check("0.1.0")
        assertTrue(info.available)
        assertEquals("v0.2.0", info.tag)
        assertEquals("https://github.com/tommilevs/Mounvix/releases/tag/v0.2.0", info.url)
        assertEquals("https://example.com/mounvix-android-0.2.0.apk", info.apkUrl)
        val req = server.takeRequest()
        assertTrue(req.path!!.endsWith("/repos/tommilevs/Mounvix/releases/latest"))
        assertTrue(req.getHeader("User-Agent")!!.startsWith("Mounvix"))
    }

    @Test
    fun sameVersionNotAvailable() {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"tag_name":"v0.1.0","html_url":"x"}"""))
        assertFalse(checker().check("0.1.0").available)
    }

    @Test
    fun httpErrorThrows() {
        server.enqueue(MockResponse().setResponseCode(404))
        var failed = false
        try {
            checker().check("0.1.0")
        } catch (e: Exception) {
            failed = true
        }
        assertTrue(failed)
    }

    @Test
    fun semverComparison() {
        assertTrue(UpdateChecker.compareVersions("0.2.0", "0.1.0") > 0)
        assertEquals(0, UpdateChecker.compareVersions("0.1.0", "0.1.0"))
        assertTrue(UpdateChecker.compareVersions("0.0.9", "0.1.0") < 0)
        assertTrue(UpdateChecker.compareVersions("1.0", "0.9.9") > 0)
        assertTrue(UpdateChecker.compareVersions("0.1.0-beta", "0.1.0") == 0)
    }
}
