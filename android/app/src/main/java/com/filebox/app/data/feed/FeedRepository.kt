package com.mounvix.app.data.feed

import android.content.Context
import android.content.SharedPreferences
import java.net.URI
import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

/** Stores the one active server profile and the notification sync cursor. */
class FeedRepository(
    private val api: FeedApi,
    private val preferences: SharedPreferences,
    private val secretStore: ProfileSecretStore,
) {
    /** Persist identity and encrypted secret; the old plaintext key is never written again. */
    fun saveProfile(profile: FeedProfile): Result<Unit> {
        return try {
            val normalizedUrl = normalizeMounvixBaseUrl(profile.baseUrl)
            val normalizedUsername = profile.username.trim()
            require(normalizedUsername.isNotBlank()) { "username is required" }
            require(profile.password.isNotBlank()) { "password is required" }

            val normalized = FeedProfile(normalizedUrl, normalizedUsername, profile.password)
            val previousIdentity = readStoredIdentity()
            val key = stableFeedProfileKey(normalized.baseUrl, normalized.username)
            secretStore.write(key, normalized.password).getOrThrow()
            val readBack = secretStore.read(key).getOrThrow()
            check(readBack == normalized.password) { "secure profile read-back did not match" }

            val editor = preferences.edit()
                .putString(KEY_PROFILE_URL, normalized.baseUrl)
                .putString(KEY_PROFILE_USERNAME, normalized.username)
                .remove(KEY_PROFILE_PASSWORD)
            if (previousIdentity == null ||
                previousIdentity.baseUrl != normalized.baseUrl ||
                previousIdentity.username != normalized.username
            ) {
                editor.remove(KEY_CURSOR)
            }
            check(editor.commit()) { "could not persist Mounvix profile settings" }
            api.setActiveProfile(normalized)
            Result.success(Unit)
        } catch (error: Exception) {
            Result.failure(error)
        }
    }

    /** Load a profile only from a verified encrypted secret, migrating legacy plaintext once. */
    fun loadProfile(): FeedProfile? {
        val identity = readStoredIdentity()
        if (identity == null) {
            api.setActiveProfile(null)
            return null
        }
        val key = stableFeedProfileKey(identity.baseUrl, identity.username)
        val secureResult = secretStore.read(key)
        if (secureResult.isFailure) {
            api.setActiveProfile(null)
            return null
        }

        val legacySecret = preferences.getString(KEY_PROFILE_PASSWORD, null)
        val secureSecret = secureResult.getOrNull()
        val password = when {
            !secureSecret.isNullOrBlank() -> {
                if (!legacySecret.isNullOrBlank()) {
                    if (legacySecret != secureSecret || !removeLegacySecret()) {
                        api.setActiveProfile(null)
                        return null
                    }
                }
                secureSecret
            }
            !legacySecret.isNullOrBlank() -> {
                if (secretStore.write(key, legacySecret).isFailure) {
                    api.setActiveProfile(null)
                    return null
                }
                val readBack = secretStore.read(key)
                if (readBack.isFailure || readBack.getOrNull() != legacySecret || !removeLegacySecret()) {
                    api.setActiveProfile(null)
                    return null
                }
                legacySecret
            }
            else -> {
                api.setActiveProfile(null)
                return null
            }
        }

        val profile = FeedProfile(identity.baseUrl, identity.username, password)
        api.setActiveProfile(profile)
        return profile
    }

    /** True when identity is present but no verified profile secret can be loaded. */
    fun hasStoredProfileIdentity(): Boolean = readStoredIdentity() != null

    /** Remove only the matching profile, encrypted secret, and its Memories cursor. */
    fun clearProfile(profile: FeedProfile): Result<Unit> = try {
        val normalizedUrl = normalizeMounvixBaseUrl(profile.baseUrl)
        val normalizedUsername = profile.username.trim()
        val identity = readStoredIdentity() ?: throw IllegalStateException("no Mounvix profile is configured")
        check(identity.baseUrl == normalizedUrl && identity.username == normalizedUsername) {
            "profile does not match the configured Mounvix identity"
        }
        val key = stableFeedProfileKey(normalizedUrl, normalizedUsername)
        val previousSecret = secretStore.read(key).getOrThrow()
        secretStore.delete(key).getOrThrow()
        val removed = preferences.edit()
            .remove(KEY_PROFILE_URL)
            .remove(KEY_PROFILE_USERNAME)
            .remove(KEY_PROFILE_PASSWORD)
            .remove(KEY_CURSOR)
            .commit()
        if (!removed) {
            if (!previousSecret.isNullOrBlank()) secretStore.write(key, previousSecret).getOrThrow()
            throw IllegalStateException("could not persist Mounvix profile removal")
        }
        api.setActiveProfile(null)
        Result.success(Unit)
    } catch (error: Exception) {
        Result.failure(error)
    }

    /** Fetch items newer than the committed sync cursor without changing that cursor. */
    suspend fun fetchNewSinceCursor(): FeedPage {
        requireProfile()
        return api.getFeeds(preferences.getString(KEY_CURSOR, null), PAGE_LIMIT)
    }

    /** Set the initial notification baseline once, so old retained cards are not announced. */
    fun initializeCursorIfAbsent(baseline: String) {
        require(baseline.isNotBlank()) { "initial cursor must not be blank" }
        if (preferences.getString(KEY_CURSOR, null).isNullOrBlank()) {
            check(preferences.edit().putString(KEY_CURSOR, baseline).commit()) {
                "could not initialize memories feed cursor"
            }
        }
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

    private fun readStoredIdentity(): ProfileIdentity? {
        val rawBaseUrl = preferences.getString(KEY_PROFILE_URL, null)?.trim().orEmpty()
        val username = preferences.getString(KEY_PROFILE_USERNAME, null)?.trim().orEmpty()
        if (rawBaseUrl.isBlank() || username.isBlank()) return null
        val normalizedUrl = runCatching { normalizeMounvixBaseUrl(rawBaseUrl) }.getOrNull() ?: return null
        return ProfileIdentity(normalizedUrl, username)
    }

    private fun removeLegacySecret(): Boolean =
        preferences.edit().remove(KEY_PROFILE_PASSWORD).commit()

    private data class ProfileIdentity(val baseUrl: String, val username: String)

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
                secretStore = NativeProfileSecretStore(appContext),
            )
        }
    }
}

/** Normalize an HTTP(S) server base while preserving any configured path prefix. */
internal fun normalizeMounvixBaseUrl(raw: String): String {
    val value = raw.trim()
    require(value.isNotBlank()) { "server URL is required" }
    val uri = try {
        URI(value)
    } catch (_: Exception) {
        throw IllegalArgumentException("invalid Mounvix server URL")
    }
    require(uri.scheme.equals("http", ignoreCase = true) || uri.scheme.equals("https", ignoreCase = true)) {
        "server URL must use HTTP or HTTPS"
    }
    require(uri.rawAuthority != null && uri.rawUserInfo == null && uri.rawQuery == null && uri.rawFragment == null) {
        "server URL must not contain credentials, query, or fragment"
    }

    val parsed = value.toHttpUrlOrNull() ?: throw IllegalArgumentException("invalid Mounvix server URL")
    require(parsed.scheme == "http" || parsed.scheme == "https") { "server URL must use HTTP or HTTPS" }
    require(parsed.host.isNotBlank() && parsed.username.isEmpty() && parsed.password.isEmpty() && parsed.query == null && parsed.fragment == null) {
        "server URL must not contain credentials, query, or fragment"
    }
    val path = parsed.encodedPath.trimEnd('/').ifEmpty { "/" }
    val normalizedPath = if (path == "/") "/" else "$path/"
    return parsed.newBuilder().encodedPath(normalizedPath).build().toString()
}

/** Stable, non-secret key for a server/account profile; only its hash reaches secret storage. */
internal fun stableFeedProfileKey(baseUrl: String, username: String): String {
    val identity = "$baseUrl\n$username".toByteArray(StandardCharsets.UTF_8)
    return MessageDigest.getInstance("SHA-256")
        .digest(identity)
        .joinToString(separator = "") { byte -> "%02x".format(byte.toInt() and 0xff) }
}
