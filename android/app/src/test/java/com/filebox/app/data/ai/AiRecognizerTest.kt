package com.mounvix.app.data.ai

import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class AiRecognizerTest {

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
    fun sendsImageAndParsesAnswer() {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """{"choices":[{"message":{"content":"кот на столе"}}]}"""
            )
        )
        val answer = AiRecognizer().describe(
            baseUrl = server.url("/").toString().trimEnd('/'),
            model = "vision-test",
            key = "secret",
            image = byteArrayOf(1, 2, 3),
            mime = "image/png",
            prompt = "Что на фото?",
        )
        assertEquals("кот на столе", answer)
        val req = server.takeRequest()
        assertEquals("/v1/chat/completions", req.path)
        assertEquals("Bearer secret", req.getHeader("Authorization"))
        val body = req.body.readUtf8()
        assertTrue(body.contains("\"model\":\"vision-test\""))
        assertTrue(body.contains("data:image/png;base64,"))
        assertTrue(body.contains("Что на фото?"))
    }

    @Test(expected = IllegalStateException::class)
    fun httpErrorThrows() {
        server.enqueue(MockResponse().setResponseCode(500).setBody("""{"error":{"message":"boom"}}"""))
        AiRecognizer().describe(
            server.url("/").toString().trimEnd('/'), "m", "", byteArrayOf(1), "image/png"
        )
    }

    @Test(expected = IllegalArgumentException::class)
    fun unconfiguredThrows() {
        AiRecognizer().describe("", "", "", byteArrayOf(1), "image/png")
    }
}
