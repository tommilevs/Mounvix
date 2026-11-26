package com.mounvix.app.data.autoupload

import android.net.Uri
import android.provider.DocumentsContract
import com.mounvix.app.data.db.AutouploadReceipt
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [33])
class AutouploadIdentityTest {

    @Test
    fun treeKeyIsStableForSchemeNormalization() {
        val tree = DocumentsContract.buildTreeDocumentUri(
            "com.example.documents",
            "primary:Pictures"
        )
        val upperCaseScheme = Uri.parse(tree.toString().replaceFirst("content:", "CONTENT:"))

        val first = AutouploadIdentity.treeKey(tree)
        val normalized = AutouploadIdentity.treeKey(upperCaseScheme)

        assertEquals(first, normalized)
        assertTrue(first.matches(Regex("[0-9a-f]{64}")))
    }

    @Test
    fun documentKeyChangesWithProviderOrDocumentId() {
        val first = DocumentsContract.buildDocumentUri(
            "com.example.documents",
            "primary:Pictures/one.jpg"
        )
        val otherProvider = DocumentsContract.buildDocumentUri(
            "com.other.documents",
            "primary:Pictures/one.jpg"
        )
        val otherDocument = DocumentsContract.buildDocumentUri(
            "com.example.documents",
            "primary:Pictures/two.jpg"
        )

        val firstKey = AutouploadIdentity.documentKey(first)

        assertNotEquals(firstKey, AutouploadIdentity.documentKey(otherProvider))
        assertNotEquals(firstKey, AutouploadIdentity.documentKey(otherDocument))
        assertTrue(firstKey.matches(Regex("[0-9a-f]{64}")))
    }

    @Test
    fun receiptStoresHashedKeysRatherThanRawSafIdentifiers() {
        val treeUri = DocumentsContract.buildTreeDocumentUri(
            "com.example.documents",
            "primary:Pictures"
        )
        val documentUri = DocumentsContract.buildDocumentUri(
            "com.example.documents",
            "primary:Pictures/private-photo.jpg"
        )
        val documentId = DocumentsContract.getDocumentId(documentUri)
        val treeDocumentId = DocumentsContract.getTreeDocumentId(treeUri)
        val receipt = AutouploadReceipt(
            treeKey = AutouploadIdentity.treeKey(treeUri),
            documentKey = AutouploadIdentity.documentKey(documentUri),
            destinationKey = AutouploadIdentity.destinationKey(
                "webdav",
                "https://nas.example:8443",
                "alice",
                "Mounvix"
            ),
            sizeBytes = 123L,
            modifiedMs = 456L,
            relativePath = "private-photo.jpg",
            uploadedAtMs = 789L
        )
        val persistedValues = listOf(
            receipt.treeKey,
            receipt.documentKey,
            receipt.destinationKey,
            receipt.relativePath
        ).joinToString("|")
        val fieldNames = AutouploadReceipt::class.java.declaredFields.map { it.name.lowercase() }

        assertFalse(persistedValues.contains(treeUri.toString()))
        assertFalse(persistedValues.contains(documentUri.toString()))
        assertFalse(persistedValues.contains(documentId))
        assertFalse(persistedValues.contains(treeDocumentId))
        assertFalse(fieldNames.any { it.contains("uri") || it.contains("documentid") })
    }

    @Test
    fun destinationKeyChangesForAdapterRootAccountAndPath() {
        val baseline = AutouploadIdentity.destinationKey(
            "webdav",
            "https://nas-a.example:8443",
            "alice",
            "Mounvix"
        )

        assertNotEquals(
            baseline,
            AutouploadIdentity.destinationKey("smb", "https://nas-a.example:8443", "alice", "Mounvix")
        )
        assertNotEquals(
            baseline,
            AutouploadIdentity.destinationKey("webdav", "https://nas-b.example:8443", "alice", "Mounvix")
        )
        assertNotEquals(
            baseline,
            AutouploadIdentity.destinationKey("webdav", "https://nas-a.example:8443", "bob", "Mounvix")
        )
        assertNotEquals(
            baseline,
            AutouploadIdentity.destinationKey("webdav", "https://nas-a.example:8443", "alice", "Archive")
        )
        assertEquals(
            baseline,
            AutouploadIdentity.destinationKey("webdav", "https://nas-a.example:8443", "alice", "Mounvix")
        )
        assertTrue(baseline.matches(Regex("[0-9a-f]{64}")))
    }

    @Test
    fun normalizeDestinationKeepsLegacyDefaultAndTrimsOnlyBoundarySlashes() {
        assertEquals("Mounvix", AutouploadIdentity.normalizeDestination(""))
        assertEquals("Mounvix", AutouploadIdentity.normalizeDestination("///"))
        assertEquals("photos/2025", AutouploadIdentity.normalizeDestination("/photos/2025/"))
    }

    @Test
    fun normalizeDestinationRejectsUnsafeSegments() {
        listOf("photos//2025", "photos/./2025", "photos/../2025", "photos\\2025").forEach { raw ->
            try {
                AutouploadIdentity.normalizeDestination(raw)
                throw AssertionError("Expected unsafe destination to be rejected: $raw")
            } catch (_: IllegalArgumentException) {
                // Expected validation failure.
            }
        }
    }

    @Test
    fun relativePathPreservesUnicodeAndNesting() {
        assertEquals(
            "Поездка 2025/Фото.webp",
            AutouploadIdentity.relativePath(listOf("Поездка 2025", "Фото.webp"))
        )
    }

    @Test
    fun relativePathRejectsEmptyDotAndEmbeddedSeparators() {
        listOf(
            listOf("folder", ""),
            listOf("."),
            listOf(".."),
            listOf("folder/photo.jpg"),
            listOf("folder\\photo.jpg")
        ).forEach { segments ->
            try {
                AutouploadIdentity.relativePath(segments)
                throw AssertionError("Expected unsafe relative path to be rejected: $segments")
            } catch (_: IllegalArgumentException) {
                // Expected validation failure.
            }
        }
    }
}
