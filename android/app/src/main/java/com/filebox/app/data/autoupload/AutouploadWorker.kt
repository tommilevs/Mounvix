package com.mounvix.app.data.autoupload

import android.content.ContentResolver
import android.content.Context
import android.content.SharedPreferences
import android.net.Uri
import android.os.CancellationSignal
import android.os.OperationCanceledException
import androidx.work.CoroutineWorker
import androidx.work.Data
import androidx.work.ListenableWorker
import androidx.work.WorkerParameters
import com.mounvix.app.data.db.AppDatabase
import com.mounvix.app.data.feed.NativeProfileSecretStore
import com.mounvix.app.data.notify.Notifier
import com.mounvix.app.data.source.RemoteCredentialRepository
import com.mounvix.app.data.source.StorageSource
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.InternalCoroutinesApi
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.job
import kotlinx.coroutines.withContext
import java.io.IOException
import java.io.InputStream
import java.util.concurrent.atomic.AtomicBoolean

/** Metadata-only candidate planning followed by cancellable, Wi-Fi-only remote uploads. */
class AutouploadWorker(ctx: Context, params: WorkerParameters) : CoroutineWorker(ctx, params) {

    internal data class Dependencies(
        val databaseProvider: (Context) -> AppDatabase = { context -> AppDatabase.get(context) },
        val sourceFactory: ((SharedPreferences) -> StorageSource?)? = null,
        val wifiGuardFactory: (Context) -> AutouploadNetworkGuard = ::AutouploadWifiGuard,
        val readerFactory: (ContentResolver) -> AutouploadMetadataReader = ::AutouploadSafReader,
        val inputStreamFactory: (Context, Uri) -> InputStream? = { context, uri ->
            context.contentResolver.openInputStream(uri)
        },
        val nowMs: () -> Long = System::currentTimeMillis,
        val notifier: (Context, String, String) -> Unit = { context, title, text ->
            Notifier.show(context, title, text)
        }
    )

    /** Replaced only by unit tests; WorkManager continues to use the two-argument constructor. */
    internal var dependencies: Dependencies = Dependencies()

    private val stopRequested = AtomicBoolean(false)
    @Volatile
    private var activeSignal: CancellationSignal? = null

    @OptIn(InternalCoroutinesApi::class)
    override suspend fun doWork(): Result {
        val stopListener = currentCoroutineContext().job.invokeOnCompletion(
            onCancelling = true,
            invokeImmediately = true
        ) { cause ->
            if (cause is CancellationException) {
                stopRequested.set(true)
                activeSignal?.cancel()
            }
        }
        return try {
            withContext(Dispatchers.IO) {
        var repository: AutouploadRunRepository? = null
        var treeKey: String? = null
        var destinationKey: String? = null
        var stateReady = false
        var activeSignalForRun: CancellationSignal? = null
        var disconnectMonitor: AutoCloseable? = null
        var outcome = "SUCCESS"
        var uploaded = 0
        var errors = 0
        var lastSafeErrorClass: String? = null
        var finalResult: Result = Result.success()
        val runNow = inputData.getBoolean(RUN_NOW_INPUT, false)
        val prefs = applicationContext.getSharedPreferences(PREFERENCES, Context.MODE_PRIVATE)

        try {
            if (!prefs.getBoolean(ENABLED_KEY, false)) return@withContext Result.success()

            val database = dependencies.databaseProvider(applicationContext)
            val legacyIdentity = RemoteSourceFactory.legacyReceiverIdentity(prefs)
            val bootstrap = AutouploadLegacyCutoffBootstrap(database).migrateOnce(prefs, legacyIdentity)
            if (!bootstrap.markerCommitted) {
                outcome = "ERROR"
                lastSafeErrorClass = "LegacyCutoffMarkerCommitFailed"
                finalResult = Result.retry()
                return@withContext finalResult
            }

            val rawTreeUri = prefs.getString(TREE_KEY, null)
                ?: return@withContext Result.success()
            val treeUri = runCatching { Uri.parse(rawTreeUri) }.getOrNull()
                ?: return@withContext Result.failure()
            val destination = runCatching {
                AutouploadIdentity.normalizeDestination(prefs.getString(DESTINATION_KEY, "Mounvix") ?: "Mounvix")
            }.getOrNull() ?: return@withContext Result.failure()
            val identity = legacyIdentity ?: return@withContext Result.failure()

            val runTreeKey = runCatching { AutouploadIdentity.treeKey(treeUri) }.getOrNull()
                ?: return@withContext Result.failure()
            val runDestinationKey = identity.destinationKey
            treeKey = runTreeKey
            destinationKey = runDestinationKey
            val runRepository = AutouploadRunRepository(database)
            repository = runRepository
            stateReady = true

            val source = dependencies.sourceFactory?.invoke(prefs) ?: if (dependencies.sourceFactory == null) {
                val credentialRepository = RemoteCredentialRepository(
                    prefs,
                    NativeProfileSecretStore(applicationContext),
                )
                RemoteSourceFactory.fromPrefs(prefs, credentialRepository)
            } else {
                null
            }
            if (source == null) {
                outcome = "ERROR"
                lastSafeErrorClass = "RemoteSourceUnavailable"
                finalResult = Result.failure()
                return@withContext finalResult
            }

            val wifiGuard = dependencies.wifiGuardFactory(applicationContext)
            if (stopRequested.get() || isStopped) {
                outcome = "CANCELLED"
                return@withContext Result.success()
            }
            if (!wifiGuard.isWifiConnected()) {
                outcome = "WAITING_FOR_WIFI"
                finalResult = waitingResult(runNow)
                publishProgress(outcome, uploaded, errors)
                return@withContext finalResult
            }

            val signal = CancellationSignal()
            activeSignalForRun = signal
            activeSignal = signal
            disconnectMonitor = wifiGuard.monitorDisconnect(signal)
            if (stopRequested.get() || isStopped) {
                outcome = "CANCELLED"
                return@withContext Result.success()
            }
            if (signal.isCanceled || !wifiGuard.isWifiConnected()) {
                outcome = if (stopRequested.get() || isStopped) "CANCELLED" else "WAITING_FOR_WIFI"
                finalResult = if (outcome == "WAITING_FOR_WIFI") waitingResult(runNow) else Result.success()
                publishProgress(outcome, uploaded, errors)
                return@withContext finalResult
            }

            val dao = database.autouploadDao()
            val reader = dependencies.readerFactory(applicationContext.contentResolver)
            publishProgress("RUNNING", uploaded, errors)
            val cutoff = dao.legacyCutoff(runTreeKey, runDestinationKey)
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
                    return keys.associateWith { key -> CandidateState(receipts[key], attempts[key]) }
                }
            }
            val candidateSequence = reader.entries(treeUri).mapNotNull { entry ->
                val name = entry.displayName ?: return@mapNotNull null
                val relativePath = entry.relativePath ?: return@mapNotNull null
                val sizeBytes = entry.sizeBytes ?: return@mapNotNull null
                val modifiedMs = entry.modifiedMs ?: return@mapNotNull null
                runCatching {
                    AutouploadCandidate(
                        treeKey = runTreeKey,
                        documentKey = AutouploadIdentity.documentKey(entry.documentUri),
                        destinationKey = runDestinationKey,
                        name = name,
                        relativePath = relativePath,
                        sizeBytes = sizeBytes,
                        modifiedMs = modifiedMs,
                        sourceUri = entry.documentUri.toString()
                    )
                }.getOrNull()
            }
            val plan = try {
                AutouploadPlanner.selectBatch(
                    entries = candidateSequence,
                    stateLookup = stateLookup,
                    legacyCutoff = cutoff,
                    nowMs = dependencies.nowMs()
                )
            } catch (error: SecurityException) {
                outcome = "ERROR"
                lastSafeErrorClass = error.javaClass.simpleName
                finalResult = Result.failure()
                return@withContext finalResult
            }

            for (candidate in plan) {
                if (stopRequested.get() || isStopped) {
                    outcome = "CANCELLED"
                    finalResult = Result.success()
                    break
                }
                if (signal.isCanceled || !wifiGuard.isWifiConnected()) {
                    outcome = if (stopRequested.get() || isStopped) "CANCELLED" else "WAITING_FOR_WIFI"
                    finalResult = if (outcome == "WAITING_FOR_WIFI") waitingResult(runNow) else Result.success()
                    break
                }

                val remotePath = "$destination/${candidate.relativePath}"
                val directoriesReady = try {
                    source.ensureParentDirectories(remotePath)
                } catch (error: CancellationException) {
                    throw error
                } catch (error: Exception) {
                    if (signal.isCanceled || stopRequested.get() || isStopped || !wifiGuard.isWifiConnected()) {
                        outcome = if (stopRequested.get() || isStopped) "CANCELLED" else "WAITING_FOR_WIFI"
                        finalResult = if (outcome == "WAITING_FOR_WIFI") waitingResult(runNow) else Result.success()
                        break
                    }
                    errors++
                    val safeClass = error.javaClass.simpleName
                    lastSafeErrorClass = safeClass
                    runRepository.recordFailure(candidate, safeClass, dependencies.nowMs())
                    publishProgress("RUNNING", uploaded, errors)
                    continue
                }

                if (stopRequested.get() || isStopped || signal.isCanceled || !wifiGuard.isWifiConnected()) {
                    outcome = if (stopRequested.get() || isStopped) "CANCELLED" else "WAITING_FOR_WIFI"
                    finalResult = if (outcome == "WAITING_FOR_WIFI") waitingResult(runNow) else Result.success()
                    break
                }
                if (!directoriesReady) {
                    errors++
                    lastSafeErrorClass = "DirectoryCreationFailed"
                    runRepository.recordFailure(candidate, "DirectoryCreationFailed", dependencies.nowMs())
                    publishProgress("RUNNING", uploaded, errors)
                    continue
                }

                try {
                    if (stopRequested.get() || isStopped || signal.isCanceled || !wifiGuard.isWifiConnected()) {
                        outcome = if (stopRequested.get() || isStopped) "CANCELLED" else "WAITING_FOR_WIFI"
                        finalResult = if (outcome == "WAITING_FOR_WIFI") waitingResult(runNow) else Result.success()
                        break
                    }
                    val uri = Uri.parse(candidate.sourceUri)
                    val input = dependencies.inputStreamFactory(applicationContext, uri)
                        ?: throw IOException("Unable to open selected document")
                    input.use { stream ->
                        if (signal.isCanceled) throw OperationCanceledException("Upload cancelled")
                        if (!wifiGuard.isWifiConnected()) throw OperationCanceledException("Wi-Fi is no longer active")
                        source.uploadCancellable(
                            input = stream,
                            contentLength = candidate.sizeBytes,
                            path = remotePath,
                            signal = signal
                        ) { _, _ -> }
                    }

                    if (stopRequested.get() || isStopped || signal.isCanceled) {
                        outcome = if (stopRequested.get() || isStopped) "CANCELLED" else "WAITING_FOR_WIFI"
                        finalResult = if (outcome == "WAITING_FOR_WIFI") waitingResult(runNow) else Result.success()
                        break
                    }

                    var metadataErrorClass: String? = null
                    val postUploadMetadata = try {
                        reader.entries(treeUri).firstOrNull { entry ->
                            runCatching { AutouploadIdentity.documentKey(entry.documentUri) == candidate.documentKey }
                                .getOrDefault(false)
                        }
                    } catch (error: CancellationException) {
                        throw error
                    } catch (error: Exception) {
                        metadataErrorClass = error.javaClass.simpleName
                        null
                    }
                    if (stopRequested.get() || isStopped || signal.isCanceled) {
                        outcome = if (stopRequested.get() || isStopped) "CANCELLED" else "WAITING_FOR_WIFI"
                        finalResult = if (outcome == "WAITING_FOR_WIFI") waitingResult(runNow) else Result.success()
                        break
                    }
                    if (runRepository.recordSuccess(candidate, postUploadMetadata)) {
                        uploaded++
                    } else {
                        errors++
                        val safeClass = metadataErrorClass ?: "SourceChanged"
                        lastSafeErrorClass = safeClass
                        runRepository.recordFailure(candidate, safeClass, dependencies.nowMs())
                    }
                } catch (error: CancellationException) {
                    outcome = "CANCELLED"
                    lastSafeErrorClass = null
                    throw error
                } catch (error: OperationCanceledException) {
                    outcome = if (stopRequested.get() || isStopped) "CANCELLED" else "WAITING_FOR_WIFI"
                    finalResult = if (outcome == "WAITING_FOR_WIFI") waitingResult(runNow) else Result.success()
                    break
                } catch (error: Exception) {
                    if (signal.isCanceled || stopRequested.get() || isStopped || !wifiGuard.isWifiConnected()) {
                        outcome = if (stopRequested.get() || isStopped) "CANCELLED" else "WAITING_FOR_WIFI"
                        finalResult = if (outcome == "WAITING_FOR_WIFI") waitingResult(runNow) else Result.success()
                        break
                    }
                    errors++
                    val safeClass = error.javaClass.simpleName
                    lastSafeErrorClass = safeClass
                    runRepository.recordFailure(candidate, safeClass, dependencies.nowMs())
                }
                publishProgress("RUNNING", uploaded, errors)
            }

            if (outcome == "SUCCESS" && errors > 0) outcome = "ERROR"
            if (outcome == "SUCCESS" && plan.isEmpty()) {
                prefs.edit().putString(STATUS_KEY, "нет новых файлов").putLong(LAST_RUN_KEY, dependencies.nowMs()).apply()
            } else if (outcome == "SUCCESS" || outcome == "ERROR") {
                prefs.edit()
                    .putString(STATUS_KEY, "загружено: $uploaded, ошибок: $errors")
                    .putLong(LAST_RUN_KEY, dependencies.nowMs())
                    .apply()
                dependencies.notifier(
                    applicationContext,
                    "Mounvix: автозагрузка",
                    AutouploadSummary.text(uploaded, errors)
                )
            } else if (outcome == "WAITING_FOR_WIFI") {
                prefs.edit().putString(STATUS_KEY, "ожидание Wi-Fi").putLong(LAST_RUN_KEY, dependencies.nowMs()).apply()
            }
        } catch (error: CancellationException) {
            outcome = "CANCELLED"
            lastSafeErrorClass = null
            throw error
        } catch (error: Exception) {
            if (activeSignalForRun?.isCanceled == true && !stopRequested.get() && !isStopped) {
                outcome = "WAITING_FOR_WIFI"
                finalResult = waitingResult(runNow)
            } else if (stopRequested.get() || isStopped) {
                outcome = "CANCELLED"
                finalResult = Result.success()
            } else {
                outcome = "ERROR"
                lastSafeErrorClass = error.javaClass.simpleName
                finalResult = Result.retry()
            }
        } finally {
            runCatching { disconnectMonitor?.close() }
            if (activeSignal === activeSignalForRun) activeSignal = null
            if (stateReady) {
                withContext(NonCancellable + Dispatchers.IO) {
                    repository?.setRunState(
                        treeKey = requireNotNull(treeKey),
                        destinationKey = requireNotNull(destinationKey),
                        outcome = outcome,
                        updatedAtMs = dependencies.nowMs(),
                        uploadedCount = uploaded,
                        errorCount = errors,
                        safeErrorClass = lastSafeErrorClass
                    )
                    publishProgress(outcome, uploaded, errors)
                }
            }
        }
                finalResult
            }
        } finally {
            stopListener.dispose()
        }
    }

    private suspend fun publishProgress(outcome: String, uploaded: Int, errors: Int) {
        setProgress(
            Data.Builder()
                .putString(PROGRESS_OUTCOME, outcome)
                .putInt(PROGRESS_UPLOADED, uploaded)
                .putInt(PROGRESS_ERRORS, errors)
                .putLong(PROGRESS_UPDATED_AT, dependencies.nowMs())
                .build()
        )
    }

    private fun waitingResult(runNow: Boolean): ListenableWorker.Result =
        if (runNow) Result.retry() else Result.success()

    private companion object {
        const val PREFERENCES = "mounvix_prefs"
        const val ENABLED_KEY = "autoupload_enabled"
        const val TREE_KEY = "autoupload_tree"
        const val DESTINATION_KEY = "autoupload_dest"
        const val STATUS_KEY = "autoupload_status"
        const val LAST_RUN_KEY = "autoupload_last_run"
        const val RUN_NOW_INPUT = "run_now"
        const val PROGRESS_OUTCOME = "outcome"
        const val PROGRESS_UPLOADED = "uploaded_count"
        const val PROGRESS_ERRORS = "error_count"
        const val PROGRESS_UPDATED_AT = "updated_at_ms"
    }
}
