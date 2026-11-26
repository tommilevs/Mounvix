package com.mounvix.app.ui

import java.net.URI
import java.net.URLDecoder
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

enum class NavigationDecision {
    ALLOW_INTERNAL,
    OPEN_EXTERNAL,
    BLOCK,
}

/** Origin and base-path boundary for every URL loaded by the Advanced Mode WebView. */
object AdvancedModeUrlPolicy {
    fun decide(
        baseUrl: String,
        targetUrl: String,
        isRedirect: Boolean,
        hasUserGesture: Boolean,
    ): NavigationDecision {
        val base = baseUrl.toHttpUrlOrNull() ?: return NavigationDecision.BLOCK
        val target = targetUrl.toHttpUrlOrNull() ?: return NavigationDecision.BLOCK
        if (!isAllowedWebUrl(base) || !isAllowedWebUrl(target) || base.query != null || base.fragment != null) {
            return NavigationDecision.BLOCK
        }
        if (hasUserInfo(base) || hasUserInfo(target)) return NavigationDecision.BLOCK
        val baseUri = runCatching { URI(baseUrl) }.getOrNull() ?: return NavigationDecision.BLOCK
        val targetUri = runCatching { URI(targetUrl) }.getOrNull() ?: return NavigationDecision.BLOCK
        if (baseUri.rawUserInfo != null || targetUri.rawUserInfo != null) return NavigationDecision.BLOCK
        if (hasUnsafePathSegment(baseUri.rawPath.orEmpty()) || hasUnsafePathSegment(targetUri.rawPath.orEmpty())) {
            return NavigationDecision.BLOCK
        }

        if (sameOrigin(base, target)) {
            return if (isWithinBasePath(base, target)) {
                NavigationDecision.ALLOW_INTERNAL
            } else {
                NavigationDecision.BLOCK
            }
        }
        return if (!isRedirect && hasUserGesture) {
            NavigationDecision.OPEN_EXTERNAL
        } else {
            NavigationDecision.BLOCK
        }
    }

    private fun isAllowedWebUrl(url: HttpUrl): Boolean = url.scheme == "http" || url.scheme == "https"

    private fun hasUserInfo(url: HttpUrl): Boolean = url.username.isNotEmpty() || url.password.isNotEmpty()

    private fun sameOrigin(left: HttpUrl, right: HttpUrl): Boolean =
        left.scheme == right.scheme && left.host == right.host && left.port == right.port

    private fun isWithinBasePath(base: HttpUrl, target: HttpUrl): Boolean {
        val basePath = base.encodedPath.trimEnd('/').ifEmpty { "/" }
        val targetPath = target.encodedPath
        if (basePath == "/") return targetPath.startsWith('/')
        return targetPath == basePath || targetPath.startsWith("$basePath/")
    }

    /** Resolve an API-provided absolute path only when it remains inside the configured WebView root. */
    fun resolveInternalRoute(baseUrl: String, targetRoute: String): String? {
        if (!targetRoute.startsWith('/') || targetRoute.startsWith("//") || targetRoute.contains('\\')) return null
        val route = runCatching { URI(targetRoute) }.getOrNull() ?: return null
        if (route.isAbsolute || route.rawAuthority != null || route.rawFragment != null) return null
        if (hasUnsafePathSegment(route.rawPath.orEmpty())) return null
        val base = baseUrl.toHttpUrlOrNull() ?: return null
        if (!isAllowedWebUrl(base) || hasUserInfo(base) || base.query != null || base.fragment != null) return null
        val resolved = base.resolve(targetRoute)?.toString() ?: return null
        return resolved.takeIf {
            decide(baseUrl, it, isRedirect = false, hasUserGesture = false) == NavigationDecision.ALLOW_INTERNAL
        }
    }

    /** Reject literal, encoded, and double-encoded dot traversal and encoded path separators. */
    private fun hasUnsafePathSegment(rawPath: String): Boolean = rawPath.split('/').any { segment ->
        var decoded = segment
        repeat(2) {
            decoded = try {
                URLDecoder.decode(decoded.replace("+", "%2B"), "UTF-8")
            } catch (_: IllegalArgumentException) {
                return@any true
            }
            if (decoded == "." || decoded == ".." || decoded.contains('/') || decoded.contains('\\')) {
                return@any true
            }
        }
        false
    }
}
