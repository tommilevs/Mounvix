package com.mounvix.app.data.source

import android.content.SharedPreferences
import com.mounvix.app.data.feed.ProfileSecretStore
import java.net.URI
import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import java.util.Locale

/** Migrates legacy preference secrets into Android-Keystore-backed storage without losing config. */
class RemoteCredentialRepository(
    private val preferences: SharedPreferences,
    private val secretStore: ProfileSecretStore,
) {
    fun read(profileKey: String, legacyPreferenceKey: String): Result<String> = synchronized(LOCK) {
        runCatching {
            require(profileKey.isNotBlank()) { "credential profile key is required" }
            require(legacyPreferenceKey.isNotBlank()) { "legacy preference key is required" }

            val secureValue = secretStore.read(profileKey).getOrThrow()
            val hasLegacyValue = preferences.contains(legacyPreferenceKey)
            val legacyValue = if (hasLegacyValue) {
                preferences.getString(legacyPreferenceKey, null)
            } else {
                null
            }

            if (secureValue != null) {
                if (hasLegacyValue && legacyValue != secureValue) {
                    error("conflicting secure and legacy credentials; enter the credential again")
                }
                if (hasLegacyValue) removeLegacySecret(legacyPreferenceKey)
                return@runCatching secureValue
            }

            if (!hasLegacyValue) return@runCatching ""
            val plaintext = legacyValue.orEmpty()
            secretStore.write(profileKey, plaintext).getOrThrow()
            val readBack = secretStore.read(profileKey).getOrThrow()
            if (readBack != plaintext) error("secure credential migration read-back failed")
            removeLegacySecret(legacyPreferenceKey)
            plaintext
        }
    }

    /** Securely stores a secret, then atomically commits its non-secret profile fields. */
    fun save(
        profileKey: String,
        legacyPreferenceKey: String,
        secret: String,
        updatePreferences: (SharedPreferences.Editor) -> Unit = {},
    ): Result<Unit> = synchronized(LOCK) {
        runCatching {
            require(profileKey.isNotBlank()) { "credential profile key is required" }
            require(legacyPreferenceKey.isNotBlank()) { "legacy preference key is required" }

            secretStore.write(profileKey, secret).getOrThrow()
            val readBack = secretStore.read(profileKey).getOrThrow()
            if (readBack != secret) error("secure credential save read-back failed")

            val editor = preferences.edit()
            updatePreferences(editor)
            editor.remove(legacyPreferenceKey)
            if (!editor.commit() || preferences.contains(legacyPreferenceKey)) {
                error("secure credential preference commit failed")
            }
        }
    }

    private fun removeLegacySecret(key: String) {
        if (!preferences.edit().remove(key).commit() || preferences.contains(key)) {
            error("legacy credential removal failed")
        }
    }

    private companion object {
        val LOCK = Any()
    }
}

/** Opaque stable identifiers for app secrets; no endpoint or username is stored as a file name. */
object RemoteCredentialKeys {
    fun webDav(url: String, user: String): String = key("webdav", normalizedEndpoint(url), user.trim())

    fun smb(host: String, share: String, domain: String, user: String): String =
        key("smb", host.trim().lowercase(Locale.ROOT), share.trim(), domain.trim(), user.trim())

    fun sftp(host: String, port: Int, basePath: String, user: String): String =
        key("sftp", host.trim().lowercase(Locale.ROOT), port.toString(), normalizePath(basePath), user.trim())

    fun ai(baseUrl: String): String = key("ai", normalizedEndpoint(baseUrl))

    fun legacy(preferenceKey: String): String = key("legacy", preferenceKey)

    private fun normalizePath(path: String): String = path.trim().ifEmpty { "/" }.trimEnd('/').ifEmpty { "/" }

    private fun normalizedEndpoint(value: String): String {
        val raw = value.trim()
        return runCatching {
            val uri = URI(raw)
            val scheme = uri.scheme?.lowercase(Locale.ROOT).orEmpty()
            val host = uri.host?.lowercase(Locale.ROOT) ?: uri.rawAuthority.orEmpty().lowercase(Locale.ROOT)
            val port = when {
                uri.port < 0 -> ""
                scheme == "http" && uri.port == 80 -> ""
                scheme == "https" && uri.port == 443 -> ""
                else -> ":${uri.port}"
            }
            val path = uri.rawPath.orEmpty().trimEnd('/').ifEmpty { "/" }
            val query = uri.rawQuery?.let { "?$it" }.orEmpty()
            "$scheme://$host$port$path$query"
        }.getOrDefault(raw)
    }

    private fun key(namespace: String, vararg components: String): String {
        val input = buildString {
            append("mounvix-remote-secret-v1\n")
            append(namespace)
            components.forEach { component ->
                append('\n').append(component.length).append(':').append(component)
            }
        }.toByteArray(StandardCharsets.UTF_8)
        val digest = MessageDigest.getInstance("SHA-256").digest(input)
        val hex = digest.joinToString(separator = "") { byte -> "%02x".format(byte) }
        return "mounvix-$namespace-v1:$hex"
    }
}
