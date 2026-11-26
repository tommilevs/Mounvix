package com.mounvix.app.data.feed

import android.content.Context
import android.content.Intent
import android.net.Uri
import com.mounvix.app.MainActivity
import com.mounvix.app.ui.AdvancedModeUrlPolicy
import java.net.URI

data class ServerEventOpenRequest(val eventId: String, val targetRoute: String)
data class SafeServerRoute(val eventId: String, val url: String)

/** Internal route payload for taps; it never sends a read acknowledgement by itself. */
object ServerEventNotificationIntentRouter {
    const val ACTION_OPEN_SERVER_EVENT = "com.mounvix.app.OPEN_SERVER_EVENT"
    const val EXTRA_EVENT_ID = "com.mounvix.app.extra.SERVER_EVENT_ID"
    const val EXTRA_TARGET_ROUTE = "com.mounvix.app.extra.SERVER_EVENT_TARGET_ROUTE"

    fun createIntent(context: Context, eventId: String, targetRoute: String): Intent {
        require(eventId.isNotBlank() && isSafeRelativeRoute(targetRoute)) { "invalid server event notification target" }
        return Intent(context, MainActivity::class.java)
            .setAction(ACTION_OPEN_SERVER_EVENT)
            .setData(Uri.parse("mounvix-event://notification/${Uri.encode(eventId)}"))
            .putExtra(EXTRA_EVENT_ID, eventId)
            .putExtra(EXTRA_TARGET_ROUTE, targetRoute)
            .addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP)
    }

    fun openRequest(intent: Intent?): ServerEventOpenRequest? {
        if (intent?.action != ACTION_OPEN_SERVER_EVENT) return null
        val eventId = intent.getStringExtra(EXTRA_EVENT_ID)?.takeIf { it.isNotBlank() } ?: return null
        val route = intent.getStringExtra(EXTRA_TARGET_ROUTE)?.takeIf(::isSafeRelativeRoute) ?: return null
        return ServerEventOpenRequest(eventId, route)
    }

    /** Resolve an inbox tap against the profile that is active at handling time. */
    fun parse(intent: Intent?, profileBaseUrl: String): SafeServerRoute? {
        val request = openRequest(intent) ?: return null
        val url = AdvancedModeUrlPolicy.resolveInternalRoute(profileBaseUrl, request.targetRoute) ?: return null
        return SafeServerRoute(request.eventId, url)
    }

    private fun isSafeRelativeRoute(route: String): Boolean {
        if (!route.startsWith("/") || route.startsWith("//") || route.contains('\\')) return false
        val parsed = runCatching { URI(route) }.getOrNull() ?: return false
        return !parsed.isAbsolute && parsed.rawAuthority == null &&
            parsed.rawPath?.startsWith("/") == true && parsed.rawFragment == null
    }
}
