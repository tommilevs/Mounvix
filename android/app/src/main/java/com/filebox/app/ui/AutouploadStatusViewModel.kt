package com.mounvix.app.ui

import android.content.Context
import android.content.SharedPreferences
import android.net.Uri
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import androidx.work.Data
import androidx.work.WorkInfo
import androidx.work.WorkManager
import com.mounvix.app.data.autoupload.AutouploadIdentity
import com.mounvix.app.data.autoupload.AutouploadLegacyCutoffBootstrap
import com.mounvix.app.data.autoupload.AutouploadScheduler
import com.mounvix.app.data.autoupload.RemoteSourceFactory
import com.mounvix.app.data.db.AppDatabase
import com.mounvix.app.data.db.AutouploadRunState
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch

/** WorkManager observations from both scheduled work and the explicit Run Now request. */
data class AutouploadWorkSnapshot(
    val periodic: List<WorkInfo> = emptyList(),
    val runNow: List<WorkInfo> = emptyList()
)

/** Current input configuration needed to scope status to a tree and destination. */
data class AutouploadStatusConfiguration(
    val treeKey: String? = null,
    val destinationKey: String? = null,
    val missingAction: String? = null,
    val legacyCutoffWarning: Boolean = false
)

/** Immutable status consumed by the server-settings UI. */
data class AutouploadUiState(
    val workState: WorkInfo.State? = null,
    val runOutcome: String? = null,
    val progress: Boolean = false,
    val uploadedCount: Int = 0,
    val errorCount: Int = 0,
    val lastUpdatedMs: Long? = null,
    val missingAction: String? = null,
    val legacyCutoffWarning: Boolean = false
)

/** Combines live WorkManager progress with durable, destination-scoped Room outcomes. */
@OptIn(ExperimentalCoroutinesApi::class)
class AutouploadStatusViewModel(
    workSnapshots: Flow<AutouploadWorkSnapshot>,
    runStates: Flow<AutouploadRunState?>,
    configurations: Flow<AutouploadStatusConfiguration>,
    dispatcher: CoroutineDispatcher = Dispatchers.Main.immediate
) : ViewModel() {
    private val mutableUiState = MutableStateFlow(AutouploadUiState())
    val uiState: StateFlow<AutouploadUiState> = mutableUiState.asStateFlow()

    init {
        viewModelScope.launch(dispatcher) {
            combine(workSnapshots, runStates, configurations, ::mapState)
                .collect { mutableUiState.value = it }
        }
    }

    class Factory(context: Context) : ViewModelProvider.Factory {
        private val appContext = context.applicationContext

        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T {
            require(modelClass.isAssignableFrom(AutouploadStatusViewModel::class.java)) {
                "Unsupported ViewModel: ${modelClass.name}"
            }
            val prefs = appContext.getSharedPreferences(PREFERENCES, Context.MODE_PRIVATE)
            val database = AppDatabase.get(appContext)
            val workManager = WorkManager.getInstance(appContext)
            val configurationFlow = configurationFlow(prefs, appContext)
            val runStateFlow = configurationFlow.flatMapLatest { configuration ->
                val treeKey = configuration.treeKey
                val destinationKey = configuration.destinationKey
                if (treeKey == null || destinationKey == null) {
                    flowOf(null)
                } else {
                    database.autouploadDao().observeRunState(treeKey, destinationKey)
                }
            }
            val workSnapshotFlow = combine(
                workManager.getWorkInfosForUniqueWorkFlow(AutouploadScheduler.PERIODIC_UNIQUE_WORK),
                workManager.getWorkInfosForUniqueWorkFlow(AutouploadScheduler.RUN_NOW_UNIQUE_WORK)
            ) { periodic, runNow -> AutouploadWorkSnapshot(periodic, runNow) }
            return AutouploadStatusViewModel(
                workSnapshots = workSnapshotFlow,
                runStates = runStateFlow,
                configurations = configurationFlow,
                dispatcher = Dispatchers.Main.immediate
            ) as T
        }
    }

    private companion object {
        const val PREFERENCES = "mounvix_prefs"
        const val TREE_KEY = "autoupload_tree"
        const val LEGACY_WARNING_KEY = AutouploadLegacyCutoffBootstrap.WARNING_KEY
        const val PROGRESS_OUTCOME = "outcome"
        const val PROGRESS_UPLOADED = "uploaded_count"
        const val PROGRESS_ERRORS = "error_count"
        const val PROGRESS_UPDATED_AT = "updated_at_ms"

        fun mapState(
            workSnapshot: AutouploadWorkSnapshot,
            runState: AutouploadRunState?,
            configuration: AutouploadStatusConfiguration
        ): AutouploadUiState {
            val runNowInfo = workSnapshot.runNow.preferredWorkInfo()
            val periodicInfo = workSnapshot.periodic.preferredWorkInfo()
            val selectedInfo = when {
                runNowInfo?.state == WorkInfo.State.RUNNING -> runNowInfo
                periodicInfo?.state == WorkInfo.State.RUNNING -> periodicInfo
                runNowInfo?.state == WorkInfo.State.ENQUEUED || runNowInfo?.state == WorkInfo.State.BLOCKED -> runNowInfo
                runNowInfo != null -> runNowInfo
                else -> periodicInfo
            }
            val progressData = selectedInfo?.progress ?: Data.EMPTY
            val progressOutcome = progressData.getString(PROGRESS_OUTCOME)
            val isRunNow = selectedInfo != null && runNowInfo?.id == selectedInfo.id
            val isActive = selectedInfo?.state == WorkInfo.State.RUNNING
            val waitingWorkState = selectedInfo?.state in setOf(WorkInfo.State.ENQUEUED, WorkInfo.State.RUNNING)
            val isWaiting = waitingWorkState &&
                (progressOutcome == "WAITING_FOR_WIFI" || runState?.outcome == "WAITING_FOR_WIFI")
            val isQueuedRunNow = isRunNow && selectedInfo?.state in setOf(WorkInfo.State.ENQUEUED, WorkInfo.State.BLOCKED) &&
                progressOutcome == null && runState?.outcome != "WAITING_FOR_WIFI"
            val outcome = when {
                isActive -> progressOutcome ?: "RUNNING"
                isWaiting -> "WAITING_FOR_WIFI"
                isQueuedRunNow -> "QUEUED"
                selectedInfo?.state == WorkInfo.State.BLOCKED -> "QUEUED"
                selectedInfo?.state == WorkInfo.State.SUCCEEDED -> runState?.outcome ?: progressOutcome
                selectedInfo?.state == WorkInfo.State.FAILED -> "ERROR"
                selectedInfo?.state == WorkInfo.State.CANCELLED -> if (isRunNow) "CANCELLED" else runState?.outcome ?: "CANCELLED"
                else -> runState?.outcome ?: if (selectedInfo?.state == WorkInfo.State.ENQUEUED) "QUEUED" else null
            }
            val useLiveCounts = isActive || (waitingWorkState && progressOutcome == "WAITING_FOR_WIFI")
            val uploadedCount = if (useLiveCounts) {
                progressData.getInt(PROGRESS_UPLOADED, 0)
            } else if (isQueuedRunNow) {
                0
            } else {
                runState?.uploadedCount ?: progressData.getInt(PROGRESS_UPLOADED, 0)
            }
            val errorCount = if (useLiveCounts) {
                progressData.getInt(PROGRESS_ERRORS, 0)
            } else if (isQueuedRunNow) {
                0
            } else {
                runState?.errorCount ?: progressData.getInt(PROGRESS_ERRORS, 0)
            }
            val progressUpdatedAt = progressData.getLong(PROGRESS_UPDATED_AT, 0L).takeIf { it > 0L }
            return AutouploadUiState(
                workState = selectedInfo?.state,
                runOutcome = outcome,
                progress = isActive,
                uploadedCount = uploadedCount,
                errorCount = errorCount,
                lastUpdatedMs = if (useLiveCounts) progressUpdatedAt ?: runState?.updatedAtMs else runState?.updatedAtMs,
                missingAction = configuration.missingAction,
                legacyCutoffWarning = configuration.legacyCutoffWarning
            )
        }

        private fun List<WorkInfo>.preferredWorkInfo(): WorkInfo? =
            firstOrNull { it.state == WorkInfo.State.RUNNING }
                ?: firstOrNull { it.state == WorkInfo.State.ENQUEUED || it.state == WorkInfo.State.BLOCKED }
                ?: lastOrNull()

        private fun configurationFlow(
            prefs: SharedPreferences,
            context: Context
        ): Flow<AutouploadStatusConfiguration> = callbackFlow {
            val listener = SharedPreferences.OnSharedPreferenceChangeListener { _, _ -> trySend(Unit) }
            prefs.registerOnSharedPreferenceChangeListener(listener)
            trySend(Unit)
            awaitClose { prefs.unregisterOnSharedPreferenceChangeListener(listener) }
        }
            .map { readConfiguration(prefs, context) }
            .distinctUntilChanged()
            .flowOn(Dispatchers.IO)

        private fun readConfiguration(
            prefs: SharedPreferences,
            context: Context
        ): AutouploadStatusConfiguration {
            val rawTree = prefs.getString(TREE_KEY, null)
            val treeUri = rawTree?.let { runCatching { Uri.parse(it) }.getOrNull() }
            val treeKey = treeUri?.let { uri -> runCatching { AutouploadIdentity.treeKey(uri) }.getOrNull() }
            val hasReadGrant = treeUri != null && runCatching {
                context.contentResolver.persistedUriPermissions.any { permission ->
                    permission.uri == treeUri && permission.isReadPermission
                }
            }.getOrDefault(false)
            val receiver = RemoteSourceFactory.legacyReceiverIdentity(prefs)
            val missingAction = when {
                rawTree.isNullOrBlank() -> "Выберите папку для автозагрузки."
                treeKey == null || !hasReadGrant -> "Повторно выберите папку, чтобы восстановить доступ к её файлам."
                receiver == null -> "Настройте WebDAV, SMB или SFTP-получатель."
                else -> null
            }
            return AutouploadStatusConfiguration(
                treeKey = treeKey,
                destinationKey = receiver?.destinationKey,
                missingAction = missingAction,
                legacyCutoffWarning = prefs.getBoolean(LEGACY_WARNING_KEY, false)
            )
        }
    }
}
