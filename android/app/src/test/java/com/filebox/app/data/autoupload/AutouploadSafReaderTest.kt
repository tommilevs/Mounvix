package com.mounvix.app.data.autoupload

import android.content.ContentResolver
import android.database.MatrixCursor
import android.net.Uri
import android.provider.DocumentsContract
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.mockito.Mockito
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [33])
class AutouploadSafReaderTest {
    private val columns = arrayOf(
        DocumentsContract.Document.COLUMN_DOCUMENT_ID,
        DocumentsContract.Document.COLUMN_MIME_TYPE,
        DocumentsContract.Document.COLUMN_DISPLAY_NAME,
        DocumentsContract.Document.COLUMN_SIZE,
        DocumentsContract.Document.COLUMN_LAST_MODIFIED
    )

    @Test
    fun excludesFilesWhenProviderDoesNotReturnRequiredMetadataColumns() {
        val treeUri = DocumentsContract.buildTreeDocumentUri("provider", "root-id")
        val cursor = MatrixCursor(
            arrayOf(
                DocumentsContract.Document.COLUMN_DOCUMENT_ID,
                DocumentsContract.Document.COLUMN_MIME_TYPE,
                DocumentsContract.Document.COLUMN_DISPLAY_NAME
            )
        ).apply {
            addRow(arrayOf<Any?>("file-id", "image/jpeg", "photo.jpg"))
        }
        val resolver = Mockito.mock(ContentResolver::class.java)
        Mockito.`when`(
            resolver.query(
                Mockito.any(Uri::class.java),
                Mockito.any(Array<String>::class.java),
                Mockito.isNull<String>(),
                Mockito.isNull<Array<String>>(),
                Mockito.isNull<String>()
            )
        ).thenReturn(cursor)

        val entries = AutouploadSafReader(resolver).entries(treeUri).toList()

        assertTrue(entries.isEmpty())
        Mockito.verify(resolver, Mockito.never()).openInputStream(Mockito.any(Uri::class.java))
    }

    @Test
    fun traversesDirectoriesWithoutFileMetadataAndKeepsOnlySafeCompleteFiles() {
        val treeUri = DocumentsContract.buildTreeDocumentUri("provider", "root-id")
        val rootChildrenUri = DocumentsContract.buildChildDocumentsUriUsingTree(treeUri, "root-id")
        val folderChildrenUri = DocumentsContract.buildChildDocumentsUriUsingTree(treeUri, "folder-id")
        val rootCursor = MatrixCursor(columns).apply {
            addRow(arrayOf<Any?>("zero-id", "image/jpeg", "zero.jpg", 0L, 20L))
            addRow(arrayOf<Any?>("folder-id", DocumentsContract.Document.MIME_TYPE_DIR, "folder", null, null))
            addRow(arrayOf<Any?>("missing-name-id", "image/jpeg", null, 10L, 30L))
            addRow(arrayOf<Any?>("missing-size-id", "image/jpeg", "no-size.jpg", null, 30L))
            addRow(arrayOf<Any?>("missing-time-id", "image/jpeg", "no-time.jpg", 10L, null))
            addRow(arrayOf<Any?>("unsafe-name-id", "image/jpeg", "../escape.jpg", 10L, 30L))
        }
        val folderCursor = MatrixCursor(columns).apply {
            addRow(arrayOf<Any?>("nested-id", "image/jpeg", "same.jpg", 12L, 40L))
        }
        val projections = mutableListOf<List<String>>()
        val resolver = Mockito.mock(ContentResolver::class.java)
        Mockito.`when`(
            resolver.query(
                Mockito.any(Uri::class.java),
                Mockito.any(Array<String>::class.java),
                Mockito.isNull<String>(),
                Mockito.isNull<Array<String>>(),
                Mockito.isNull<String>()
            )
        ).thenAnswer { invocation ->
            val requested = invocation.getArgument<Array<String>>(1)
            projections += requested.toList()
            when (invocation.getArgument<Uri>(0)) {
                rootChildrenUri -> rootCursor
                folderChildrenUri -> folderCursor
                else -> error("Unexpected SAF query URI: ${invocation.getArgument<Uri>(0)}")
            }
        }

        val entries = AutouploadSafReader(resolver).entries(treeUri).toList()

        assertEquals(setOf("zero.jpg", "folder/same.jpg"), entries.map { it.relativePath }.toSet())
        assertEquals(0L, entries.single { it.relativePath == "zero.jpg" }.sizeBytes)
        assertTrue(projections.isNotEmpty())
        assertTrue(projections.all { it.containsAll(columns.toList()) })
        Mockito.verify(resolver, Mockito.never()).openInputStream(Mockito.any(Uri::class.java))
    }
}
