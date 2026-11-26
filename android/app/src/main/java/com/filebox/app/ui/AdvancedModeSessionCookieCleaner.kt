package com.mounvix.app.ui

import android.webkit.CookieManager
import android.webkit.ValueCallback
import com.mounvix.app.data.feed.normalizeMounvixBaseUrl
import kotlinx.coroutines.suspendCancellableCoroutine
import java.net.URI
import kotlin.coroutines.resume
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

/** Expires only Mounvix's WebView session cookie at the configured origin and path prefix. */
object AdvancedModeSessionCookieCleaner {
    private const val COOKIE_NAME = "mvx"
    private const val EXPIRED_DATE = "Thu, 01 Jan 1970 00:00:00 GMT"

    fun expirationHeader(baseUrl: String): String? {
        val normalizedBase = runCatching { normalizeMounvixBaseUrl(baseUrl) }.getOrNull() ?: return null
        val parsed = normalizedBase.toHttpUrlOrNull() ?: return null
        val rawOrigin = runCatching { URI(normalizedBase) }.getOrNull() ?: return null
        if (rawOrigin.rawUserInfo != null || parsed.username.isNotEmpty() || parsed.password.isNotEmpty()) return null
        val secure = if (parsed.scheme == "https") "; Secure" else ""
        val cookiePath = parsed.encodedPath.trimEnd('/').ifEmpty { "/" }
        return "$COOKIE_NAME=; Max-Age=0; Path=$cookiePath; Expires=$EXPIRED_DATE; HttpOnly; SameSite=Lax$secure"
    }

    suspend fun expire(baseUrl: String): Boolean = suspendCancellableCoroutine { continuation ->
        val normalizedBase = runCatching { normalizeMounvixBaseUrl(baseUrl) }.getOrNull()
        val header = expirationHeader(baseUrl)
        if (normalizedBase == null || header == null) {
            continuation.resume(false)
            return@suspendCancellableCoroutine
        }
        val manager = CookieManager.getInstance()
        try {
            manager.setCookie(normalizedBase, header, ValueCallback { accepted ->
                val persisted = runCatching { manager.flush() }.isSuccess
                if (continuation.isActive) continuation.resume(accepted == true && persisted)
            })
        } catch (_: Exception) {
            if (continuation.isActive) continuation.resume(false)
        }
    }
}
