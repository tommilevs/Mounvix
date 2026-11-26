package com.mounvix.app.data.autoupload

import android.net.Uri
import android.provider.DocumentsContract
import java.nio.ByteBuffer
import java.nio.charset.StandardCharsets
import java.security.MessageDigest

/** Stable, non-secret keys used by the autoupload receipt store. */
object AutouploadIdentity {
    private const val DEFAULT_DESTINATION = "Mounvix"

    /** Hashes the normalized SAF tree URI; [Uri.normalizeScheme] canonicalizes the scheme casing. */
    fun treeKey(treeUri: Uri): String {
        require(treeUri.scheme.equals("content", ignoreCase = true)) {
            "SAF tree URI must use the content scheme"
        }
        return sha256(treeUri.normalizeScheme().toString())
    }

    /** Hashes provider authority and the provider's document ID without retaining either raw value. */
    fun documentKey(documentUri: Uri): String {
        require(documentUri.scheme.equals("content", ignoreCase = true)) {
            "SAF document URI must use the content scheme"
        }
        val authority = documentUri.authority?.takeIf(String::isNotBlank)
            ?: throw IllegalArgumentException("SAF document URI must have an authority")
        val documentId = DocumentsContract.getDocumentId(documentUri)
        require(documentId.isNotEmpty()) { "SAF document ID must not be empty" }
        return sha256Parts(authority, documentId)
    }

    /** Trims boundary slashes, preserves the legacy default, and rejects unsafe path segments. */
    fun normalizeDestination(raw: String): String {
        val trimmed = raw.trim('/')
        if (trimmed.isEmpty()) return DEFAULT_DESTINATION

        val segments = trimmed.split('/')
        require(segments.none(String::isEmpty)) { "Destination must not contain empty path segments" }
        require(segments.none { it == "." || it == ".." || '\\' in it }) {
            "Destination contains an unsafe path segment"
        }
        return segments.joinToString("/")
    }

    /** Hashes only the adapter's stable, non-secret receiver identity and normalized path. */
    fun destinationKey(
        adapter: String,
        rootIdentity: String,
        accountId: String,
        normalizedDestination: String
    ): String {
        require(adapter.isNotBlank()) { "Adapter identity must not be blank" }
        require(rootIdentity.isNotBlank()) { "Root identity must not be blank" }
        return sha256Parts(
            adapter,
            rootIdentity,
            accountId,
            normalizeDestination(normalizedDestination)
        )
    }

    /** Validates individual provider names and joins them without losing nested structure. */
    fun relativePath(segments: List<String>): String {
        require(segments.isNotEmpty()) { "Relative path must contain at least one segment" }
        require(segments.all { segment ->
            segment.isNotEmpty() && segment != "." && segment != ".." &&
                '/' !in segment && '\\' !in segment
        }) { "Relative path contains an unsafe segment" }
        return segments.joinToString("/")
    }

    private fun sha256(value: String): String =
        MessageDigest.getInstance("SHA-256")
            .digest(value.toByteArray(StandardCharsets.UTF_8))
            .toLowerHex()

    /** Length-prefixes fields so concatenation cannot make two identities ambiguous. */
    private fun sha256Parts(vararg parts: String): String {
        val digest = MessageDigest.getInstance("SHA-256")
        val lengthPrefix = ByteBuffer.allocate(Int.SIZE_BYTES)
        parts.forEach { part ->
            val bytes = part.toByteArray(StandardCharsets.UTF_8)
            lengthPrefix.clear()
            lengthPrefix.putInt(bytes.size)
            digest.update(lengthPrefix.array())
            digest.update(bytes)
        }
        return digest.digest().toLowerHex()
    }

    private fun ByteArray.toLowerHex(): String = joinToString(separator = "") { byte ->
        (byte.toInt() and 0xff).toString(16).padStart(2, '0')
    }
}
