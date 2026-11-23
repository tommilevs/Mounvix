package com.mounvix.app.data.feed

import java.io.IOException
import java.net.URI
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
                .url(serverEndpoint(profile, "/api/v1/session"))
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
        val url = serverEndpoint(profile, "/api/v1/feeds").newBuilder()
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
            val url = serverEndpoint(profile, "/api/v1/feeds").newBuilder()
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
        val url = serverEndpoint(profile, relativeUrl)
        val request = Request.Builder().url(url).get().build()
        val response = executeWithReauthentication(request)
        response.requireSuccess("fetch media")
        return response.body
    }

    /** Fetch a bounded page from the independent M7 server-job notification inbox. */
    suspend fun getServerNotifications(after: Long?, limit: Int = DEFAULT_NOTIFICATION_LIMIT): ServerNotificationsPage {
        require(limit in 1..MAX_LIMIT) { "limit must be between 1 and $MAX_LIMIT" }
        require(after == null || after >= 0) { "notification cursor must not be negative" }

        val profile = requireActiveProfile()
        val url = serverEndpoint(profile, "/api/v1/notifications").newBuilder()
            .apply {
                if (after != null) addQueryParameter("after", after.toString())
                addQueryParameter("limit", limit.toString())
            }
            .build()
        val request = Request.Builder()
            .url(url)
            .header("Origin", serverOrigin(profile))
            .get()
            .build()
        val response = executeWithReauthentication(request)
        response.requireSuccess("fetch server notifications")
        return parseServerNotificationsPage(response.body.decodeToString(), after, limit)
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

    private fun parseServerNotificationsPage(
        text: String,
        after: Long?,
        limit: Int,
    ): ServerNotificationsPage {
        val json = try {
            JSONObject(text)
        } catch (error: Exception) {
            throw IOException("invalid Mounvix server notification response", error)
        }
        val array = json.opt("items") as? org.json.JSONArray
            ?: throw IOException("Mounvix server notification response omitted items")
        val items = (0 until array.length()).map { index ->
            val item = array.optJSONObject(index)
                ?: throw IOException("Mounvix server notification item was invalid")
            val notification = ServerNotification(
                eventId = item.requiredString("event_id"),
                seq = item.requiredLong("seq"),
                createdAt = item.requiredString("created_at"),
                kind = item.requiredString("kind"),
                summary = item.requiredString("summary"),
                targetRoute = validateNotificationTargetRoute(item.requiredString("target_route")),
                isRead = item.requiredBoolean("is_read"),
            )
            if (notification.seq <= 0) throw IOException("Mounvix notification sequence was invalid")
            notification
        }
        val nextCursor = json.requiredLong("next_cursor")
        val beforeCursor = json.optionalLong("before_cursor")
        val reset = json.requiredBoolean("reset")
        val oldestSeq = json.requiredLong("oldest_seq")
        val latestSeq = json.requiredLong("latest_seq")
        val unreadCount = json.requiredInt("unread_count")

        if (items.size > limit || oldestSeq < 0 || latestSeq < 0 || unreadCount < 0 ||
            (oldestSeq > 0 && oldestSeq > latestSeq) || nextCursor < 0 || nextCursor > latestSeq ||
            beforeCursor != null && beforeCursor < 0
        ) {
            throw IOException("Mounvix server notification cursor metadata was invalid")
        }
        if (items.any { it.seq > latestSeq || oldestSeq > 0 && it.seq < oldestSeq }) {
            throw IOException("Mounvix server notification sequence was outside the retained range")
        }
        if (reset) {
            if (items.isNotEmpty() || nextCursor != latestSeq) {
                throw IOException("Mounvix server notification reset page was inconsistent")
            }
        } else if (after == null) {
            if (nextCursor != latestSeq || items.zipWithNext().any { (left, right) -> left.seq <= right.seq }) {
                throw IOException("Mounvix server notification page order was invalid")
            }
        } else {
            if (nextCursor < after || items.any { it.seq <= after } ||
                items.zipWithNext().any { (left, right) -> left.seq >= right.seq } ||
                items.lastOrNull()?.seq?.let { it != nextCursor } == true ||
                items.isEmpty() && nextCursor != latestSeq
            ) {
                throw IOException("Mounvix server notification cursor regressed or page order was invalid")
            }
        }

        return ServerNotificationsPage(items, nextCursor, beforeCursor, reset, oldestSeq, latestSeq, unreadCount)
    }

    private fun validateNotificationTargetRoute(route: String): String {
        if (!route.startsWith("/") || route.startsWith("//") || route.contains('\\')) {
            throw IOException("Mounvix notification target route was not relative")
        }
        val uri = try {
            URI(route)
        } catch (error: Exception) {
            throw IOException("Mounvix notification target route was invalid", error)
        }
        if (uri.isAbsolute || uri.rawAuthority != null || uri.rawPath?.startsWith("/") != true || uri.rawFragment != null) {
            throw IOException("Mounvix notification target route was not same-origin relative")
        }
        return route
    }

    private fun JSONObject.requiredString(key: String): String {
        if (!has(key)) throw IOException("Mounvix server notification response omitted $key")
        val value = opt(key) as? String
            ?: throw IOException("Mounvix server notification field $key was invalid")
        if (value.isBlank()) throw IOException("Mounvix server notification field $key was blank")
        return value
    }

    private fun JSONObject.requiredLong(key: String): Long {
        if (!has(key)) throw IOException("Mounvix server notification response omitted $key")
        val number = opt(key) as? Number
            ?: throw IOException("Mounvix server notification field $key was invalid")
        val value = number.toDouble()
        if (!value.isFinite() || value < 0 || value > Long.MAX_VALUE.toDouble() || value != value.toLong().toDouble()) {
            throw IOException("Mounvix server notification field $key was not a non-negative integer")
        }
        return number.toLong()
    }

    private fun JSONObject.optionalLong(key: String): Long? {
        if (!has(key)) throw IOException("Mounvix server notification response omitted $key")
        return if (opt(key) == JSONObject.NULL) null else requiredLong(key)
    }

    private fun JSONObject.requiredInt(key: String): Int {
        val value = requiredLong(key)
        if (value > Int.MAX_VALUE) throw IOException("Mounvix server notification field $key was out of range")
        return value.toInt()
    }

    private fun JSONObject.requiredBoolean(key: String): Boolean {
        if (!has(key)) throw IOException("Mounvix server notification response omitted $key")
        return opt(key) as? Boolean
            ?: throw IOException("Mounvix server notification field $key was invalid")
    }

    private fun HttpResult.requireSuccess(operation: String) {
        if (code !in 200..299) {
            val detail = body.decodeToString().take(MAX_ERROR_CHARS)
            throw IOException("Mounvix $operation failed (HTTP $code)${if (detail.isBlank()) "" else ": $detail"}")
        }
    }

    private fun serverEndpoint(profile: FeedProfile, apiPath: String): HttpUrl {
        require(apiPath.startsWith("/") && !apiPath.startsWith("//") && !apiPath.contains('\\') && !apiPath.contains('#')) {
            "server API path must be a relative path"
        }
        val root = serverRoot(profile)
        val endpoint = root.resolve(apiPath.removePrefix("/"))
            ?: throw IllegalArgumentException("invalid Mounvix API path")
        require(
            endpoint.scheme == root.scheme && endpoint.host == root.host && endpoint.port == root.port &&
                endpoint.encodedPath.startsWith(root.encodedPath)
        ) { "Mounvix API path escaped the configured base path" }
        return endpoint
    }

    private fun serverOrigin(profile: FeedProfile): String = serverRoot(profile)
        .newBuilder()
        .encodedPath("/")
        .query(null)
        .fragment(null)
        .build()
        .toString()
        .trimEnd('/')

    private fun serverRoot(profile: FeedProfile): HttpUrl =
        normalizeMounvixBaseUrl(profile.baseUrl).toHttpUrlOrNull()
            ?: throw IllegalArgumentException("invalid Mounvix server URL")

    private data class HttpResult(val code: Int, val body: ByteArray)

    companion object {
        private const val HTTP_UNAUTHORIZED = 401
        private const val MAX_LIMIT = 100
        private const val DEFAULT_NOTIFICATION_LIMIT = 100
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
