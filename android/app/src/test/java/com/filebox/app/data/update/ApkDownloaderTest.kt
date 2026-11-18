package com.mounvix.app.data.update

import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import java.io.File

class ApkDownloaderTest {

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

    @Test
    fun downloadsApkBytes() {
        server.enqueue(MockResponse().setResponseCode(200).setBody("APK-BYTES"))
        val out = File.createTempFile("mvx-update", ".apk")
        try {
            val f = ApkDownloader().download(server.url("/app.apk").toString(), out)
            assertEquals("APK-BYTES", f.readText())
        } finally {
            out.delete()
        }
    }

    @Test(expected = IllegalStateException::class)
    fun httpErrorThrows() {
        server.enqueue(MockResponse().setResponseCode(404))
        val out = File.createTempFile("mvx-update", ".apk")
        try {
            ApkDownloader().download(server.url("/app.apk").toString(), out)
        } finally {
            out.delete()
        }
    }
}
