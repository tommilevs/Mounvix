package com.mounvix.app.data.autoupload

import android.content.Context
import android.content.SharedPreferences
import android.net.Uri
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import com.mounvix.app.data.db.AppDatabase
import com.mounvix.app.data.db.AutouploadReceipt
import com.mounvix.app.data.notify.Notifier
import com.mounvix.app.data.source.SftpStorageSource
import com.mounvix.app.data.source.SmbStorageSource
import com.mounvix.app.data.source.StorageSource
import com.mounvix.app.data.source.WebDavStorageSource
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.File
import java.util.Locale

/** Metadata-only candidate planning followed by bounded remote upload batches. */
class AutouploadWorker(ctx: Context, params: WorkerParameters) : CoroutineWorker(ctx, params) {

    override suspend fun doWork(): Result = withContext(Dispatchers.IO) {
        val prefs = applicationContext.getSharedPreferences("mounvix_prefs", Context.MODE_PRIVATE)
        if (!prefs.getBoolean("autoupload_enabled", false)) return@withContext Result.success()
        val treeValue = prefs.getString("autoupload_tree", null) ?: return@withContext Result.success()
        val source = RemoteSourceFactory.fromPrefs(prefs) ?: return@withContext Result.retry()
        val treeUri = Uri.parse(treeValue)
        val destination = try {
            AutouploadIdentity.normalizeDestination(prefs.getString("autoupload_dest", "Mounvix") ?: "Mounvix")
        } catch (_: IllegalArgumentException) {
            return@withContext Result.failure()
        }
        val treeKey: String
        val destinationKey: String
        try {
            treeKey = AutouploadIdentity.treeKey(treeUri)
            val identity = receiverIdentity(source, prefs) ?: return@withContext Result.failure()
            destinationKey = AutouploadIdentity.destinationKey(
                identity.adapter,
                identity.rootIdentity,
                identity.accountId,
                destination
            )
        } catch (_: IllegalArgumentException) {
            return@withContext Result.failure()
        }

        val database = AppDatabase.get(applicationContext)
        val dao = database.autouploadDao()
        val reader = AutouploadSafReader(applicationContext.contentResolver)
        val cutoff = dao.legacyCutoff(treeKey, destinationKey)
        val stateLookup = object : AutouploadStateLookup {
            override fun loadBatch(
                treeKey: String,
                destinationKey: String,
                documentKeys: List<String>
            ): Map<String, CandidateState> {
                require(documentKeys.size <= AutouploadPlanner.MAX_CANDIDATE_BATCH_SIZE)
                val keys = documentKeys.distinct()
                val receipts = dao.receiptsForKeys(treeKey, destinationKey, keys).associateBy { it.documentKey }
                val attempts = dao.attemptsForKeys(treeKey, destinationKey, keys).associateBy { it.documentKey }
                return keys.associateWith { documentKey ->
                    CandidateState(receipts[documentKey], attempts[documentKey])
                }
            }
        }
        val candidates = reader.entries(treeUri).mapNotNull { entry ->
            val name = entry.displayName ?: return@mapNotNull null
            val relativePath = entry.relativePath ?: return@mapNotNull null
            val sizeBytes = entry.sizeBytes ?: return@mapNotNull null
            val modifiedMs = entry.modifiedMs ?: return@mapNotNull null
            AutouploadCandidate(
                treeKey = treeKey,
                documentKey = AutouploadIdentity.documentKey(entry.documentUri),
                destinationKey = destinationKey,
                name = name,
                relativePath = relativePath,
                sizeBytes = sizeBytes,
                modifiedMs = modifiedMs,
                sourceUri = entry.documentUri.toString()
            )
        }
        val plan = try {
            AutouploadPlanner.selectBatch(candidates, stateLookup, cutoff, System.currentTimeMillis())
        } catch (_: SecurityException) {
            return@withContext Result.failure()
        }
        if (plan.isEmpty()) {
            prefs.edit().putString("autoupload_status", "нет новых файлов").apply()
            return@withContext Result.success()
        }

        var uploaded = 0
        var errors = 0
        if (!ensureDirectory(source, destination)) return@withContext Result.retry()

        val transferred = LinkedHashMap<String, AutouploadCandidate>()
        for (candidate in plan) {
            val parentPath = candidate.relativePath.substringBeforeLast('/', "")
            val remoteParent = if (parentPath.isEmpty()) destination else "$destination/$parentPath"
            if (!ensureDirectory(source, remoteParent)) {
                errors++
                continue
            }

            val temp = File(applicationContext.cacheDir, "auto-${System.currentTimeMillis()}-${candidate.name}")
            try {
                val input = applicationContext.contentResolver.openInputStream(Uri.parse(candidate.sourceUri))
                    ?: throw IllegalStateException("open failed")
                input.use { stream -> temp.outputStream().use { output -> stream.copyTo(output) } }
                if (source.upload(temp, "$destination/${candidate.relativePath}") { _, _ -> }) {
                    transferred[candidate.documentKey] = candidate
                } else {
                    errors++
                }
            } catch (_: Exception) {
                errors++
            } finally {
                temp.delete()
            }
        }

        if (transferred.isNotEmpty()) {
            val currentByKey = HashMap<String, SafEntry>(transferred.size)
            try {
                for (entry in reader.entries(treeUri)) {
                    val key = AutouploadIdentity.documentKey(entry.documentUri)
                    if (key in transferred) currentByKey[key] = entry
                    if (currentByKey.size == transferred.size) break
                }
            } catch (_: Exception) {
                currentByKey.clear()
            }

            for ((documentKey, candidate) in transferred) {
                val current = currentByKey[documentKey]
                if (current == null ||
                    current.displayName != candidate.name ||
                    current.relativePath != candidate.relativePath ||
                    current.sizeBytes != candidate.sizeBytes ||
                    current.modifiedMs != candidate.modifiedMs
                ) {
                    errors++
                    continue
                }

                dao.upsertReceipt(
                    AutouploadReceipt(
                        treeKey = candidate.treeKey,
                        documentKey = candidate.documentKey,
                        destinationKey = candidate.destinationKey,
                        sizeBytes = candidate.sizeBytes,
                        modifiedMs = candidate.modifiedMs,
                        relativePath = candidate.relativePath,
                        uploadedAtMs = System.currentTimeMillis()
                    )
                )
                dao.deleteAttempt(candidate.treeKey, candidate.documentKey, candidate.destinationKey)
                uploaded++
            }
        }

        prefs.edit()
            .putString("autoupload_status", "загружено: $uploaded, ошибок: $errors")
            .putLong("autoupload_last_run", System.currentTimeMillis())
            .apply()
        Notifier.show(applicationContext, "Mounvix: автозагрузка", AutouploadSummary.text(uploaded, errors))
        Result.success()
    }

    private data class ReceiverIdentity(
        val adapter: String,
        val rootIdentity: String,
        val accountId: String
    )

    private fun receiverIdentity(source: StorageSource, prefs: SharedPreferences): ReceiverIdentity? {
        val adapter: String
        val accountId: String
        when (source) {
            is WebDavStorageSource -> {
                adapter = "webdav"
                accountId = prefs.getString("webdav_user", "") ?: ""
            }
            is SmbStorageSource -> {
                adapter = "smb"
                val domain = prefs.getString("smb_domain", "") ?: ""
                val user = prefs.getString("smb_user", "") ?: ""
                accountId = "$domain\\$user"
            }
            is SftpStorageSource -> {
                adapter = "sftp"
                accountId = prefs.getString("sftp_user", "") ?: ""
            }
            else -> return null
        }

        val rawRoot = source.rootId() ?: return null
        val uri = Uri.parse(rawRoot)
        val scheme = uri.scheme?.lowercase(Locale.ROOT) ?: return null
        val rawHost = uri.host?.lowercase(Locale.ROOT) ?: return null
        val host = rawHost.removePrefix("[").removeSuffix("]")
        val authorityHost = if (':' in host) "[$host]" else host
        val port = if (uri.port >= 0) ":${uri.port}" else ""
        val path = uri.encodedPath.orEmpty().trimEnd('/')
        return ReceiverIdentity(adapter, "$scheme://$authorityHost$port$path", accountId)
    }

    private fun ensureDirectory(source: StorageSource, path: String): Boolean {
        var current = ""
        for (segment in path.split('/').filter(String::isNotEmpty)) {
            current = if (current.isEmpty()) segment else "$current/$segment"
            if (!source.exists(current) && !source.createDir(current) && !source.exists(current)) return false
        }
        return true
    }
}
