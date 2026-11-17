package com.mounvix.app.data.source

import com.mounvix.app.data.model.FileEntry
import com.mounvix.app.data.model.FileType
import okhttp3.Credentials
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.w3c.dom.Document
import org.w3c.dom.Element
import org.xml.sax.InputSource
import java.io.File
import java.io.IOException
import java.io.StringReader
import java.net.URLDecoder
import java.security.SecureRandom
import java.security.cert.X509Certificate
import java.text.SimpleDateFormat
import java.util.Locale
import java.util.concurrent.TimeUnit
import javax.net.ssl.SSLContext
import javax.net.ssl.TrustManager
import javax.net.ssl.X509TrustManager
import javax.xml.parsers.DocumentBuilderFactory

/** Настройки WebDAV-сервера (хранятся в SharedPreferences). */
data class WebDavServer(
    val baseUrl: String,
    val user: String = "",
    val password: String = "",
    val name: String = "WebDAV",
    val trustAll: Boolean = true,
)

/**
 * WebDAV-источник: листинг (PROPFIND Depth:1), stat (Depth:0), MKCOL/PUT/DELETE/COPY/MOVE,
 * скачивание (GET). Basic auth; опционально доверяет самоподписанным сертификатам.
 */
class WebDavStorageSource(private val server: WebDavServer) : StorageSource {

    private val baseUrl: String = server.baseUrl.trimEnd('/')
    private val client: OkHttpClient = buildClient(server.trustAll)
    private val dateFormat = SimpleDateFormat("EEE, dd MMM yyyy HH:mm:ss zzz", Locale.US)
    private val xmlType = "application/xml; charset=utf-8".toMediaType()

    override fun rootId(): String = baseUrl
    override fun stableKey(path: String): String = "$baseUrl/${path.trim('/')}".trimEnd('/')
    override fun uriOf(path: String): android.net.Uri? = null
    override fun mimeTypeOf(path: String): String? = mimeOf(path)

    override fun listDir(path: String): List<FileEntry> {
        val doc = propfind(path, 1)
        val out = ArrayList<FileEntry>()
        val responses = doc.getElementsByTagNameNS(NS, "response")
        val normalized = path.trim('/')
        for (i in 0 until responses.length) {
            val resp = responses.item(i) as? Element ?: continue
            val href = textOf(resp, "href") ?: continue
            val rel = relPathOf(href)
            if (rel == normalized) continue
            val isDir = firstOf(resp, "collection") != null
            val size = textOf(resp, "getcontentlength")?.trim()?.toLongOrNull() ?: 0L
            val modified = textOf(resp, "getlastmodified")?.let { parseDate(it) } ?: 0L
            val name = rel.substringAfterLast('/')
            if (name.isEmpty()) continue
            out.add(
                FileEntry(
                    name = name,
                    path = rel,
                    type = if (isDir) FileType.FOLDER else FileType.FILE,
                    size = size,
                    modified = modified,
                    docKey = href,
                )
            )
        }
        System.out.println("listDir path=$path items=${out.size}")
        return out
    }

    override fun stat(path: String): FileEntry? {
        val doc = propfind(path, 0)
        val resp = doc.getElementsByTagNameNS(NS, "response").item(0) as? Element ?: return null
        val href = textOf(resp, "href") ?: return null
        val isDir = firstOf(resp, "collection") != null
        val rel = relPathOf(href)
        val name = path.trim('/').substringAfterLast('/').ifEmpty { "root" }
        val size = textOf(resp, "getcontentlength")?.trim()?.toLongOrNull() ?: 0L
        val modified = textOf(resp, "getlastmodified")?.let { parseDate(it) } ?: 0L
        return FileEntry(
            name = name,
            path = path.trim('/'),
            type = if (isDir) FileType.FOLDER else FileType.FILE,
            size = size,
            modified = modified,
            docKey = href,
        )
    }

    override fun createDir(path: String): Boolean {
        val code = exec("MKCOL", buildUrl(path, true), null)
        return code in 200..299
    }

    override fun createFile(path: String): Boolean {
        val code = exec("PUT", buildUrl(path, false), "".toRequestBody(null))
        return code in 200..299
    }

    override fun delete(path: String): Boolean = exec("DELETE", buildUrl(path, false), null) in 200..299

    override fun rename(path: String, newName: String): Boolean {
        val rel = path.trim('/')
        val parent = rel.substringBeforeLast('/', "")
        val dest = buildUrl(if (parent.isEmpty()) newName else "$parent/$newName", false)
        return exec("MOVE", buildUrl(rel, false), null, mapOf("Destination" to dest.toString(), "Overwrite" to "F")) in 200..299
    }

    override fun exists(path: String): Boolean = try {
        stat(path) != null
    } catch (e: Exception) {
        false
    }

    override fun copy(src: String, dst: String): Boolean = exec(
        "COPY", buildUrl(src, false), null,
        mapOf("Destination" to buildUrl(dst, false).toString(), "Overwrite" to "F"),
    ) in 200..299

    override fun move(src: String, dst: String): Boolean = exec(
        "MOVE", buildUrl(src, false), null,
        mapOf("Destination" to buildUrl(dst, false).toString(), "Overwrite" to "F"),
    ) in 200..299

    override fun downloadTo(path: String, target: java.io.File): Boolean = try {
        download(path, target); true
    } catch (e: Exception) {
        false
    }

    /** Скачать файл в target (для «открыть» через FileProvider). */
    fun download(path: String, target: File): File {
        target.parentFile?.mkdirs()
        val req = Request.Builder().url(buildUrl(path, false)).get().apply { addAuth(this) }.build()
        client.newCall(req).execute().use { resp ->
            if (!resp.isSuccessful) throw IOException("GET ${resp.code}")
            val body = resp.body ?: throw IOException("empty body")
            body.byteStream().use { input -> target.outputStream().use { out -> input.copyTo(out) } }
        }
        return target
    }

    private fun propfind(path: String, depth: Int): Document {
        val url = buildUrl(path, depth > 0)
        val body = "<?xml version=\"1.0\" encoding=\"utf-8\"?>" +
            "<d:propfind xmlns:d=\"DAV:\"><d:prop>" +
            "<d:resourcetype/><d:getcontentlength/><d:getlastmodified/><d:displayname/>" +
            "</d:prop></d:propfind>"
        val req = Request.Builder().url(url)
            .header("Depth", depth.toString())
            .method("PROPFIND", body.toRequestBody(xmlType))
            .apply { addAuth(this) }
            .build()
        client.newCall(req).execute().use { resp ->
            if (!resp.isSuccessful) {
                throw IOException(
                    "PROPFIND ${resp.code} url=$url auth=${server.user.isNotEmpty()} " +
                        "www=" + (resp.header("WWW-Authenticate") ?: "-")
                )
            }
            val text = resp.body?.string().orEmpty()
            val factory = DocumentBuilderFactory.newInstance()
            factory.isNamespaceAware = true
            val doc = factory.newDocumentBuilder().parse(InputSource(StringReader(text)))
            System.out.println("PROPFIND OK $url bytes=${text.length}")
            return doc
        }
    }

    private fun exec(method: String, url: HttpUrl, body: okhttp3.RequestBody?, headers: Map<String, String> = emptyMap()): Int {
        val b = Request.Builder().url(url).method(method, body).apply { addAuth(this) }
        for ((k, v) in headers) b.header(k, v)
        client.newCall(b.build()).execute().use { return it.code }
    }

    private fun addAuth(b: Request.Builder) {
        if (server.user.isNotEmpty()) {
            b.header("Authorization", Credentials.basic(server.user, server.password))
        }
    }

    private fun buildUrl(path: String, dir: Boolean): HttpUrl {
        val base = baseUrl.toHttpUrl()
        val b = base.newBuilder()
        val safe = path.trim('/')
        if (safe.isNotEmpty()) b.addPathSegments(safe)
        val url = b.build()
        if (dir && !url.encodedPath.endsWith("/")) {
            return url.newBuilder().addPathSegment("").build()
        }
        return url
    }

    private fun relPathOf(href: String): String {
        val decoded = try {
            URLDecoder.decode(href, "UTF-8")
        } catch (e: Exception) {
            href
        }
        val basePath = try {
            (java.net.URI(baseUrl).path ?: "/")
        } catch (e: Exception) {
            "/"
        }
        val baseWithSlash = if (basePath.endsWith("/")) basePath else "$basePath/"
        var rel = decoded
        if (rel.startsWith("/")) {
            rel = when {
                rel.startsWith(baseWithSlash) -> rel.removePrefix(baseWithSlash)
                rel.trimEnd('/') == basePath.trimEnd('/') -> ""
                else -> rel.trimStart('/')
            }
        }
        return rel.trimEnd('/')
    }

    private fun parseDate(value: String): Long = try {
        dateFormat.parse(value.trim())?.time ?: 0L
    } catch (e: Exception) {
        0L
    }

    private fun mimeOf(path: String): String? = when (path.substringAfterLast('.', "").lowercase(Locale.US)) {
        "jpg", "jpeg" -> "image/jpeg"
        "png" -> "image/png"
        "gif" -> "image/gif"
        "webp" -> "image/webp"
        "mp4" -> "video/mp4"
        "mkv" -> "video/x-matroska"
        "mp3" -> "audio/mpeg"
        "txt" -> "text/plain"
        "pdf" -> "application/pdf"
        "zip" -> "application/zip"
        else -> "application/octet-stream"
    }

    private fun buildClient(trustAll: Boolean): OkHttpClient {
        val b = OkHttpClient.Builder()
            .connectTimeout(10, TimeUnit.SECONDS)
            .readTimeout(60, TimeUnit.SECONDS)
            .writeTimeout(60, TimeUnit.SECONDS)
        if (trustAll) {
            val tm = object : X509TrustManager {
                override fun checkClientTrusted(chain: Array<out X509Certificate>?, authType: String?) {}
                override fun checkServerTrusted(chain: Array<out X509Certificate>?, authType: String?) {}
                override fun getAcceptedIssuers(): Array<X509Certificate> = emptyArray()
            }
            val ctx = SSLContext.getInstance("TLS")
            ctx.init(null, arrayOf<TrustManager>(tm), SecureRandom())
            b.sslSocketFactory(ctx.socketFactory, tm)
            b.hostnameVerifier { _, _ -> true }
        }
        return b.build()
    }

    private companion object {
        const val NS = "DAV:"
        fun firstOf(parent: Element, local: String): Element? =
            parent.getElementsByTagNameNS(NS, local).item(0) as? Element

        fun textOf(parent: Element, local: String): String? =
            firstOf(parent, local)?.textContent
    }
}
