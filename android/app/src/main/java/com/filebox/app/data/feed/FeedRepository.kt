package com.mounvix.app.data.feed

import android.content.Context
import android.content.SharedPreferences

/** Stores the one active server profile and the notification sync cursor. */
class FeedRepository(
    private val api: FeedApi,
    private val preferences: SharedPreferences,
) {
    /** Persist the profile; changing server/account clears the previous account's cursor. */
    fun saveProfile(profile: FeedProfile) {
        val normalized = FeedProfile(
            baseUrl = profile.baseUrl.trim().trimEnd('/'),
            username = profile.username.trim(),
            password = profile.password,
        )
        require(normalized.baseUrl.isNotBlank()) { "server URL is required" }
        require(normalized.username.isNotBlank()) { "username is required" }
        require(normalized.password.isNotBlank()) { "password is required" }

        val previous = readStoredProfile()
        val editor = preferences.edit()
            .putString(KEY_PROFILE_URL, normalized.baseUrl)
            .putString(KEY_PROFILE_USERNAME, normalized.username)
            .putString(KEY_PROFILE_PASSWORD, normalized.password)
        if (previous == null || previous.baseUrl != normalized.baseUrl || previous.username != normalized.username) {
            editor.remove(KEY_CURSOR)
        }
        editor.apply()
        api.setActiveProfile(normalized)
    }

    /** Read the active profile and make it available for one-time session reauthentication. */
    fun loadProfile(): FeedProfile? {
        val profile = readStoredProfile()
        api.setActiveProfile(profile)
        return profile
    }

    /** Fetch items newer than the committed sync cursor without changing that cursor. */
    suspend fun fetchNewSinceCursor(): FeedPage {
        requireProfile()
        return api.getFeeds(preferences.getString(KEY_CURSOR, null), PAGE_LIMIT)
    }

    /** Commit the cursor only after the caller has completed notification side effects. */
    fun commitCursor(nextSince: String) {
        require(nextSince.isNotBlank()) { "sync cursor must not be blank" }
        check(preferences.edit().putString(KEY_CURSOR, nextSince).commit()) {
            "could not persist memories feed cursor"
        }
    }

    /** Load retained history from the server without advancing the notification cursor. */
    suspend fun loadFeed(): FeedPage {
        requireProfile()
        return api.getFeeds(since = null, limit = PAGE_LIMIT)
    }

    /** Mark a card read for the active server account. */
    suspend fun markRead(id: String) {
        requireProfile()
        api.markRead(id).getOrThrow()
    }

    private fun requireProfile(): FeedProfile =
        loadProfile() ?: throw IllegalStateException("Mounvix feed profile is not configured")

    private fun readStoredProfile(): FeedProfile? {
        val baseUrl = preferences.getString(KEY_PROFILE_URL, null)?.trim().orEmpty()
        val username = preferences.getString(KEY_PROFILE_USERNAME, null)?.trim().orEmpty()
        val password = preferences.getString(KEY_PROFILE_PASSWORD, null).orEmpty()
        if (baseUrl.isBlank() || username.isBlank() || password.isBlank()) return null
        return FeedProfile(baseUrl.trimEnd('/'), username, password)
    }

    companion object {
        private const val PAGE_LIMIT = 50
        private const val KEY_PROFILE_URL = "feed_profile_base_url"
        private const val KEY_PROFILE_USERNAME = "feed_profile_username"
        private const val KEY_PROFILE_PASSWORD = "feed_profile_password"
        private const val KEY_CURSOR = "feed_cursor"

        /** Build a repository over the app's existing preferences and shared API client. */
        fun from(context: Context): FeedRepository {
            val appContext = context.applicationContext
            return FeedRepository(
                api = FeedApi.shared(),
                preferences = appContext.getSharedPreferences("mounvix_prefs", Context.MODE_PRIVATE),
            )
        }
    }
}
