package com.mounvix.app.data.feed

import java.io.IOException
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.Cookie
import okhttp3.CookieJar
import okhttp3.HttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import org.json.JSONObject

/** In-memory cookie storage for the authenticated Mounvix session. */
class FeedCookieJar : CookieJar {
    private val cookies = mutableListOf<Cookie>()

    @Synchronized
    override fun saveFromResponse(url: HttpUrl, cookies: List<Cookie>) {
        for (cookie in cookies) {
            this.cookies.removeAll { it.name == cookie.name && it.domain == cookie.domain && it.path == cookie.path }
            if (cookie.expiresAt > System.currentTimeMillis()) this.cookies.add(cookie)
        }
    }

    @Synchronized
    override fun loadForRequest(url: HttpUrl): List<Cookie> {
        val now = System.currentTimeMillis()
        cookies.removeAll { it.expiresAt <= now }
        return cookies.filter { it.matches(url) }
    }

    @Synchronized
    fun clear() {
        cookies.clear()
    }
}

/** Cookie-aware client for the Mounvix session and memories endpoints. */
class FeedApi(
    client: OkHttpClient = defaultClient(),
) {
    /** Shared network client, configured not to forward credentials through redirects. */
    val client: OkHttpClient = client.newBuilder()
        .followRedirects(false)
        .followSslRedirects(false)
        .build()

    @Volatile
    private var activeProfile: FeedProfile? = null

    /** Authenticate and retain the profile for a single automatic retry after HTTP 401. */
    suspend fun login(profile: FeedProfile): Result<Unit> {
        setActiveProfile(profile)
        return try {
            val body = JSONObject()
                .put("username", profile.username)
                .put("password", profile.password)
                .toString()
                .toRequestBody(JSON_MEDIA_TYPE)
            val request = Request.Builder()
                .url(endpoint(profile, "/api/v1/session"))
                .post(body)
                .build()
            val response = executeOnce(request)
            response.requireSuccess("login")
            val json = JSONObject(response.body.decodeToString())
            if (!json.optBoolean("ok")) throw IOException("Mounvix login was rejected")
            Result.success(Unit)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            Result.failure(error)
        }
    }

    /** Fetch a bounded page; JSON `items`/`next_since` maps to the internal feed model. */
    suspend fun getFeeds(since: String?, limit: Int): FeedPage {
        require(limit in 1..MAX_LIMIT) { "limit must be between 1 and $MAX_LIMIT" }
        val profile = requireActiveProfile()
        val url = serverRoot(profile).newBuilder()
            .encodedPath("/api/v1/feeds")
            .apply {
                if (!since.isNullOrBlank()) addQueryParameter("since", since)
                addQueryParameter("limit", limit.toString())
            }
            .build()
        val request = Request.Builder().url(url).get().build()
        val response = executeWithReauthentication(request)
        response.requireSuccess("fetch feed")
        return parseFeedPage(response.body.decodeToString())
    }

    /** Mark one card read. The server operation is idempotent. */
    suspend fun markRead(id: String): Result<Unit> {
        if (id.isBlank() || id.length > MAX_ID_LENGTH) {
            return Result.failure(IllegalArgumentException("invalid feed id"))
        }
        return try {
            val profile = requireActiveProfile()
            val url = serverRoot(profile).newBuilder()
                .encodedPath("/api/v1/feeds")
                .addPathSegment(id)
                .addPathSegment("read")
                .build()
            val request = Request.Builder()
                .url(url)
                .post("{}".toRequestBody(JSON_MEDIA_TYPE))
                .build()
            executeWithReauthentication(request).requireSuccess("mark feed read")
            Result.success(Unit)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            Result.failure(error)
        }
    }

    /** Load media only from the server's protected relative file and thumbnail endpoints. */
    suspend fun fetchMedia(relativeUrl: String): ByteArray {
        require(relativeUrl.startsWith("/") && !relativeUrl.startsWith("//") && !relativeUrl.contains('#')) {
            "media URL must be a relative API path"
        }
        val path = relativeUrl.substringBefore('?')
        require(path == "/api/v1/file" || path == "/api/v1/thumbs") {
            "unsupported media URL"
        }
        val profile = requireActiveProfile()
        val url = serverRoot(profile).resolve(relativeUrl)
            ?: throw IllegalArgumentException("invalid media URL")
        val request = Request.Builder().url(url).get().build()
        val response = executeWithReauthentication(request)
        response.requireSuccess("fetch media")
        return response.body
    }

    /** Replace the current profile and clear cookies when the identity changes. */
    @Synchronized
    internal fun setActiveProfile(profile: FeedProfile?) {
        if (activeProfile != profile) (client.cookieJar as? FeedCookieJar)?.clear()
        activeProfile = profile
    }

    private fun requireActiveProfile(): FeedProfile =
        activeProfile ?: throw IllegalStateException("Mounvix feed profile is not configured")

    private suspend fun executeWithReauthentication(request: Request): HttpResult {
        val first = executeOnce(request)
        if (first.code != HTTP_UNAUTHORIZED) {
            return first
        }
        val profile = requireActiveProfile()
        login(profile).getOrElse { throw it }
        val retry = executeOnce(request)
        if (retry.code == HTTP_UNAUTHORIZED) throw IOException("Mounvix session expired")
        return retry
    }

    private suspend fun executeOnce(request: Request): HttpResult = withContext(Dispatchers.IO) {
        client.newCall(request).execute().use { response ->
            HttpResult(response.code, response.body?.bytes() ?: ByteArray(0))
        }
    }

    private fun parseFeedPage(text: String): FeedPage {
        val json = JSONObject(text)
        val array = json.getJSONArray("items")
        val items = (0 until array.length()).map { index ->
            val card = array.getJSONObject(index)
            val mediaArray = card.getJSONArray("media")
            val media = (0 until mediaArray.length()).map { mediaIndex ->
                val item = mediaArray.getJSONObject(mediaIndex)
                FeedMedia(
                    root = item.getString("root"),
                    path = item.getString("path"),
                    kind = item.getString("kind"),
                    modifiedAt = item.getString("modified_at"),
                    thumbnailUrl = item.optString("thumbnail_url")
                        .takeIf { it.isNotBlank() && it != JSONObject.NULL.toString() },
                    fileUrl = item.getString("file_url"),
                )
            }
            FeedCard(
                id = card.getString("id"),
                generatedAt = card.getString("generated_at"),
                title = card.getString("title"),
                media = media,
                isRead = card.getBoolean("is_read"),
            )
        }
        val nextSince = json.getString("next_since")
        if (nextSince.isBlank()) throw IOException("Mounvix feed response omitted next_since")
        return FeedPage(items, nextSince)
    }

    private fun HttpResult.requireSuccess(operation: String) {
        if (code !in 200..299) {
            val detail = body.decodeToString().take(MAX_ERROR_CHARS)
            throw IOException("Mounvix $operation failed (HTTP $code)${if (detail.isBlank()) "" else ": $detail"}")
        }
    }

    private fun endpoint(profile: FeedProfile, path: String): HttpUrl =
        serverRoot(profile).newBuilder().encodedPath(path).build()

    private fun serverRoot(profile: FeedProfile): HttpUrl {
        val url = profile.baseUrl.trim().toHttpUrlOrNull()
            ?: throw IllegalArgumentException("invalid Mounvix server URL")
        require(url.scheme == "http" || url.scheme == "https") { "server URL must use HTTP or HTTPS" }
        require(url.username.isEmpty() && url.password.isEmpty() && url.query == null && url.fragment == null) {
            "server URL must not contain credentials, query, or fragment"
        }
        return url
    }

    private data class HttpResult(val code: Int, val body: ByteArray)

    companion object {
        private const val HTTP_UNAUTHORIZED = 401
        private const val MAX_LIMIT = 100
        private const val MAX_ID_LENGTH = 200
        private const val MAX_ERROR_CHARS = 200
        private val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()

        @Volatile
        private var sharedInstance: FeedApi? = null

        /** Process-wide instance shared by repository, worker, and authenticated media loading. */
        fun shared(): FeedApi = sharedInstance ?: synchronized(this) {
            sharedInstance ?: FeedApi().also { sharedInstance = it }
        }

        private fun defaultClient(): OkHttpClient = OkHttpClient.Builder()
            .cookieJar(FeedCookieJar())
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(30, TimeUnit.SECONDS)
            .build()
    }
}
