package com.mounvix.app.data.source

import com.mounvix.app.data.model.FileType
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.io.File

class WebDavStorageSourceTest {

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

    private fun src(): WebDavStorageSource = WebDavStorageSource(
        WebDavServer(
            baseUrl = server.url("/dav/share/").toString(),
            user = "admin",
            password = "1234567890",
            trustAll = false,
        )
    )

    private fun multistatus(vararg responses: String): String =
        "<?xml version=\"1.0\" encoding=\"utf-8\"?><D:multistatus xmlns:D=\"DAV:\">" +
            responses.joinToString("") + "</D:multistatus>"

    private fun response(href: String, dir: Boolean, size: Long = 0, modified: String? = null): String {
        val type = if (dir) "<D:collection/>" else ""
        val len = if (dir) "" else "<D:getcontentlength>$size</D:getcontentlength>"
        val date = if (modified == null) "" else "<D:getlastmodified>$modified</D:getlastmodified>"
        return "<D:response><D:href>$href</D:href><D:propstat><D:prop>" +
            "<D:resourcetype>$type</D:resourcetype>$len$date</D:prop>" +
            "<D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>"
    }

    @Test
    fun listDirParsesMultistatus() {
        server.enqueue(
            MockResponse().setResponseCode(207).setBody(
                multistatus(
                    response("/dav/share/", true),
                    response("/dav/share/sub/", true),
                    response("/dav/share/hello%20world.txt", false, 11, "Tue, 17 Nov 2026 20:00:00 GMT"),
                )
            )
        )
        val entries = src().listDir("")
        assertEquals(2, entries.size)
        val folder = entries.first { it.type == FileType.FOLDER }
        assertEquals("sub", folder.name)
        assertEquals("sub", folder.path)
        val file = entries.first { it.type == FileType.FILE }
        assertEquals("hello world.txt", file.name)
        assertEquals(11L, file.size)
        assertTrue(file.modified > 0)
    }

    @Test
    fun listDirSendsBasicAuthAndDepth() {
        server.enqueue(MockResponse().setResponseCode(207).setBody(multistatus()))
        src().listDir("")
        val req = server.takeRequest()
        assertEquals("PROPFIND", req.method)
        assertEquals("1", req.getHeader("Depth"))
        assertTrue(req.getHeader("Authorization")!!.startsWith("Basic "))
    }

    @Test
    fun createDirSendsMkcol() {
        server.enqueue(MockResponse().setResponseCode(201))
        assertTrue(src().createDir("new folder"))
        val req = server.takeRequest()
        assertEquals("MKCOL", req.method)
        assertTrue(req.path!!.contains("/new%20folder"))
    }

    @Test
    fun renameSendsMoveWithDestination() {
        server.enqueue(MockResponse().setResponseCode(201))
        assertTrue(src().rename("docs/a.txt", "b.txt"))
        val req = server.takeRequest()
        assertEquals("MOVE", req.method)
        assertEquals(server.url("/dav/share/docs/b.txt").toString(), req.getHeader("Destination"))
    }

    @Test
    fun uploadSendsPutWithBodyAndProgress() {
        server.enqueue(MockResponse().setResponseCode(201))
        val tmp = File.createTempFile("mvx-upload", ".txt")
        tmp.writeText("hello-upload")
        val progress = ArrayList<Long>()
        try {
            assertTrue(src().upload(tmp, "up.txt") { done, total ->
                progress.add(done)
                assertEquals(12L, total)
            })
        } finally {
            tmp.delete()
        }
        val req = server.takeRequest()
        assertEquals("PUT", req.method)
        assertEquals(12L, req.bodySize)
        assertEquals("hello-upload", req.body.readUtf8())
        assertEquals(12L, progress.last())
    }
}
