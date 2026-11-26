package com.mounvix.app.data.feed

import android.content.SharedPreferences

enum class ServerEventAccessStatus {
    UNVERIFIED,
    READY,
    NEEDS_LOGIN,
    FORBIDDEN,
    UNSUPPORTED,
}

data class ServerEventProfileStatus(
    val access: ServerEventAccessStatus,
    val healthVersion: String?,
    val historyGap: Boolean,
)

/** Per-profile polling enablement and capability state; secrets never enter these preferences. */
class ServerEventStatusStore(private val preferences: SharedPreferences) {
    fun isEnabled(profile: FeedProfile): Boolean = preferences.getBoolean(key(profile, "enabled"), false)

    fun setEnabled(profile: FeedProfile, enabled: Boolean) {
        check(preferences.edit().putBoolean(key(profile, "enabled"), enabled).commit()) {
            "could not persist server inbox preference"
        }
    }

    fun status(profile: FeedProfile): ServerEventProfileStatus {
        val access = preferences.getString(key(profile, "access"), null)
            ?.let { runCatching { ServerEventAccessStatus.valueOf(it) }.getOrNull() }
            ?: ServerEventAccessStatus.UNVERIFIED
        return ServerEventProfileStatus(
            access = access,
            healthVersion = preferences.getString(key(profile, "health_version"), null),
            historyGap = preferences.getBoolean(key(profile, "history_gap"), false),
        )
    }

    fun canPoll(profile: FeedProfile): Boolean =
        isEnabled(profile) && status(profile).access == ServerEventAccessStatus.READY

    fun markReady(profile: FeedProfile) {
        check(
            preferences.edit()
                .putString(key(profile, "access"), ServerEventAccessStatus.READY.name)
                .remove(key(profile, "health_version"))
                .commit(),
        ) { "could not persist ready server inbox state" }
    }

    fun markNeedsLogin(profile: FeedProfile) = writeAccess(profile, ServerEventAccessStatus.NEEDS_LOGIN)

    fun markForbidden(profile: FeedProfile) = writeAccess(profile, ServerEventAccessStatus.FORBIDDEN)

    fun markUnsupported(profile: FeedProfile, healthVersion: String?) {
        val editor = preferences.edit()
            .putString(key(profile, "access"), ServerEventAccessStatus.UNSUPPORTED.name)
        if (healthVersion.isNullOrBlank()) editor.remove(key(profile, "health_version"))
        else editor.putString(key(profile, "health_version"), healthVersion)
        check(editor.commit()) { "could not persist unsupported server version" }
    }

    fun markHistoryGap(profile: FeedProfile) {
        check(preferences.edit().putBoolean(key(profile, "history_gap"), true).commit()) {
            "could not persist server event history gap"
        }
    }

    fun clearHistoryGap(profile: FeedProfile) {
        check(preferences.edit().remove(key(profile, "history_gap")).commit()) {
            "could not clear server event history gap"
        }
    }

    fun clearProfile(profile: FeedProfile) {
        val editor = preferences.edit()
        listOf("enabled", "access", "health_version", "history_gap").forEach { editor.remove(key(profile, it)) }
        check(editor.commit()) { "could not clear server inbox profile state" }
    }

    private fun writeAccess(profile: FeedProfile, access: ServerEventAccessStatus) {
        val editor = preferences.edit().putString(key(profile, "access"), access.name)
        if (access != ServerEventAccessStatus.UNSUPPORTED) editor.remove(key(profile, "health_version"))
        check(editor.commit()) { "could not persist server inbox status" }
    }

    private fun key(profile: FeedProfile, suffix: String): String {
        val url = normalizeMounvixBaseUrl(profile.baseUrl)
        val username = profile.username.trim()
        require(username.isNotBlank()) { "username is required" }
        val identity = stableFeedProfileKey(url, username)
        return "$PREFERENCE_PREFIX$identity.$suffix"
    }

    private companion object {
        const val PREFERENCE_PREFIX = "server_event_profile."
    }
}
