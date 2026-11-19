package com.mounvix.app.data.autoupload

import android.content.ContentResolver
import android.database.Cursor
import android.net.Uri
import android.provider.DocumentsContract
import java.util.ArrayDeque

/** One SAF child row. Relative paths are assigned only by [entries] during tree traversal. */
data class SafEntry(
    val documentUri: Uri,
    val displayName: String?,
    val mimeType: String?,
    val sizeBytes: Long?,
    val modifiedMs: Long?,
    val relativePath: String? = null
) {
    val isDirectory: Boolean
        get() = mimeType == DocumentsContract.Document.MIME_TYPE_DIR
}

/** Reads SAF metadata only; it never opens a content stream. */
class AutouploadSafReader(private val contentResolver: ContentResolver) {
    private data class DirectoryFrame(
        val relativeSegments: List<String>,
        val children: Iterator<SafEntry>
    )

    fun listChildren(parentUri: Uri): List<SafEntry> {
        val parentDocumentId = parentDocumentId(parentUri)
        val childrenUri = DocumentsContract.buildChildDocumentsUriUsingTree(parentUri, parentDocumentId)
        val cursor = contentResolver.query(childrenUri, PROJECTION, null, null, null) ?: return emptyList()
        return cursor.use { rows ->
            val documentIdColumn = rows.getColumnIndex(DocumentsContract.Document.COLUMN_DOCUMENT_ID)
            val mimeTypeColumn = rows.getColumnIndex(DocumentsContract.Document.COLUMN_MIME_TYPE)
            if (documentIdColumn < 0 || mimeTypeColumn < 0) return@use emptyList()

            buildList {
                while (rows.moveToNext()) {
                    val documentId = rows.stringOrNull(documentIdColumn)
                        ?.takeIf(String::isNotBlank)
                        ?: continue
                    val mimeType = rows.stringOrNull(mimeTypeColumn)
                        ?.takeIf(String::isNotBlank)
                        ?: continue
                    add(
                        SafEntry(
                            documentUri = DocumentsContract.buildDocumentUriUsingTree(parentUri, documentId),
                            displayName = rows.stringOrNull(rows.getColumnIndex(DocumentsContract.Document.COLUMN_DISPLAY_NAME)),
                            mimeType = mimeType,
                            sizeBytes = rows.longOrNull(rows.getColumnIndex(DocumentsContract.Document.COLUMN_SIZE)),
                            modifiedMs = rows.longOrNull(rows.getColumnIndex(DocumentsContract.Document.COLUMN_LAST_MODIFIED))
                        )
                    )
                }
            }.sortedBy { DocumentsContract.getDocumentId(it.documentUri) }
        }
    }

    /** Iterative deterministic depth-first traversal; only the active directory frames are retained. */
    fun entries(treeUri: Uri): Sequence<SafEntry> = sequence {
        val stack = ArrayDeque<DirectoryFrame>()
        stack.addLast(DirectoryFrame(emptyList(), listChildren(treeUri).iterator()))

        while (stack.isNotEmpty()) {
            val frame = stack.peekLast() ?: break
            if (!frame.children.hasNext()) {
                stack.removeLast()
                continue
            }

            val child = frame.children.next()
            val name = child.displayName?.takeIf(String::isNotBlank) ?: continue
            val segments = frame.relativeSegments + name
            val relativePath = try {
                AutouploadIdentity.relativePath(segments)
            } catch (_: IllegalArgumentException) {
                continue
            }

            if (child.isDirectory) {
                stack.addLast(
                    DirectoryFrame(segments, listChildren(child.documentUri).iterator())
                )
                continue
            }

            if (child.sizeBytes == null || child.modifiedMs == null) continue
            yield(child.copy(relativePath = relativePath))
        }
    }

    private fun parentDocumentId(parentUri: Uri): String {
        val segments = parentUri.pathSegments
        val treeSegment = segments.indexOf("tree")
        val isTreeDocument = treeSegment >= 0 &&
            segments.size > treeSegment + 3 &&
            segments[treeSegment + 2] == "document"
        return when {
            isTreeDocument -> DocumentsContract.getDocumentId(parentUri)
            treeSegment >= 0 -> DocumentsContract.getTreeDocumentId(parentUri)
            else -> DocumentsContract.getDocumentId(parentUri)
        }
    }

    private fun Cursor.stringOrNull(column: Int): String? =
        if (column < 0 || isNull(column)) null else getString(column)

    private fun Cursor.longOrNull(column: Int): Long? =
        if (column < 0 || isNull(column)) null else getLong(column)

    private companion object {
        val PROJECTION = arrayOf(
            DocumentsContract.Document.COLUMN_DOCUMENT_ID,
            DocumentsContract.Document.COLUMN_MIME_TYPE,
            DocumentsContract.Document.COLUMN_DISPLAY_NAME,
            DocumentsContract.Document.COLUMN_SIZE,
            DocumentsContract.Document.COLUMN_LAST_MODIFIED
        )
    }
}
