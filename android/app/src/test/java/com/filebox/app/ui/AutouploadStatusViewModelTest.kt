package com.mounvix.app.ui

import androidx.lifecycle.ViewModelStore
import androidx.work.Data
import androidx.work.WorkInfo
import com.mounvix.app.data.db.AutouploadRunState
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import java.util.UUID

@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
class AutouploadStatusViewModelTest {
    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() {
        Dispatchers.setMain(dispatcher)
    }

    @After
    fun tearDown() {
        Dispatchers.resetMain()
    }

    @Test
    fun liveWorkTransitionsUpdateStatusWithoutReenteringScreen() = runTest(dispatcher) {
        val workInfos = MutableStateFlow(AutouploadWorkSnapshot(runNow = listOf(workInfo(WorkInfo.State.ENQUEUED))))
        val runState = MutableStateFlow<AutouploadRunState?>(null)
        val configuration = MutableStateFlow(AutouploadStatusConfiguration())
        val viewModel = AutouploadStatusViewModel(workInfos, runState, configuration, dispatcher)
        val store = ViewModelStore().also { it.put("autoupload", viewModel) }
        advanceUntilIdle()
        assertEquals(WorkInfo.State.ENQUEUED, viewModel.uiState.value.workState)

        workInfos.value = AutouploadWorkSnapshot(
            runNow = listOf(
                workInfo(
                    WorkInfo.State.RUNNING,
                    Data.Builder()
                        .putString("outcome", "RUNNING")
                        .putInt("uploaded_count", 2)
                        .putInt("error_count", 1)
                        .putLong("updated_at_ms", 1234L)
                        .build()
                )
            )
        )
        advanceUntilIdle()

        assertEquals(WorkInfo.State.RUNNING, viewModel.uiState.value.workState)
        assertEquals("RUNNING", viewModel.uiState.value.runOutcome)
        assertTrue(viewModel.uiState.value.progress)
        assertEquals(2, viewModel.uiState.value.uploadedCount)
        assertEquals(1, viewModel.uiState.value.errorCount)
        assertEquals(1234L, viewModel.uiState.value.lastUpdatedMs)

        runState.value = AutouploadRunState("tree", "destination", "SUCCESS", 2345L, 3, 1, null)
        workInfos.value = AutouploadWorkSnapshot(runNow = listOf(workInfo(WorkInfo.State.SUCCEEDED)))
        advanceUntilIdle()

        assertEquals("SUCCESS", viewModel.uiState.value.runOutcome)
        assertFalse(viewModel.uiState.value.progress)
        assertEquals(3, viewModel.uiState.value.uploadedCount)
        assertEquals(2345L, viewModel.uiState.value.lastUpdatedMs)
        store.clear()
    }

    @Test
    fun succeededNoOpWithoutDurableRunStateDoesNotReportSuccess() = runTest(dispatcher) {
        val viewModel = AutouploadStatusViewModel(
            flowOf(AutouploadWorkSnapshot(runNow = listOf(workInfo(WorkInfo.State.SUCCEEDED)))),
            flowOf(null),
            flowOf(AutouploadStatusConfiguration(missingAction = "Выберите папку для автозагрузки.")),
            dispatcher
        )
        val store = ViewModelStore().also { it.put("autoupload", viewModel) }
        advanceUntilIdle()

        assertEquals(WorkInfo.State.SUCCEEDED, viewModel.uiState.value.workState)
        assertEquals(null, viewModel.uiState.value.runOutcome)
        assertEquals(null, viewModel.uiState.value.lastUpdatedMs)
        assertEquals(0, viewModel.uiState.value.uploadedCount)
        assertEquals(0, viewModel.uiState.value.errorCount)
        store.clear()
    }

    @Test
    fun waitingForWifiRoomOutcomeRemainsVisibleAfterWorkManagerRetryClearsProgress() = runTest(dispatcher) {
        val waiting = AutouploadRunState("tree", "destination", "WAITING_FOR_WIFI", 3456L, 1, 0, null)
        val viewModel = AutouploadStatusViewModel(
            flowOf(AutouploadWorkSnapshot(runNow = listOf(workInfo(WorkInfo.State.ENQUEUED)))),
            flowOf(waiting),
            flowOf(AutouploadStatusConfiguration(treeKey = "tree", destinationKey = "destination")),
            dispatcher
        )
        val store = ViewModelStore().also { it.put("autoupload", viewModel) }
        advanceUntilIdle()

        assertEquals(WorkInfo.State.ENQUEUED, viewModel.uiState.value.workState)
        assertEquals("WAITING_FOR_WIFI", viewModel.uiState.value.runOutcome)
        assertFalse(viewModel.uiState.value.progress)
        assertEquals(1, viewModel.uiState.value.uploadedCount)
        assertEquals(3456L, viewModel.uiState.value.lastUpdatedMs)
        store.clear()
    }

    @Test
    fun terminalRoomStateIsRestoredWhenViewModelIsRecreated() = runTest(dispatcher) {
        val terminal = AutouploadRunState("tree", "destination", "CANCELLED", 3456L, 1, 0, null)
        val firstStore = ViewModelStore()
        val first = AutouploadStatusViewModel(
            flowOf(AutouploadWorkSnapshot()),
            flowOf(terminal),
            flowOf(AutouploadStatusConfiguration()),
            dispatcher
        )
        firstStore.put("autoupload", first)
        advanceUntilIdle()
        assertEquals("CANCELLED", first.uiState.value.runOutcome)
        firstStore.clear()

        val recreated = AutouploadStatusViewModel(
            flowOf(AutouploadWorkSnapshot()),
            flowOf(terminal),
            flowOf(AutouploadStatusConfiguration()),
            dispatcher
        )
        val recreatedStore = ViewModelStore().also { it.put("autoupload", recreated) }
        advanceUntilIdle()

        assertEquals("CANCELLED", recreated.uiState.value.runOutcome)
        assertEquals(3456L, recreated.uiState.value.lastUpdatedMs)
        recreatedStore.clear()
    }

    @Test
    fun liveRunningStateOverridesStaleTerminalRoomState() = runTest(dispatcher) {
        val staleTerminal = AutouploadRunState("tree", "destination", "SUCCESS", 1000L, 9, 0, null)
        val liveWork = workInfo(
            WorkInfo.State.RUNNING,
            Data.Builder().putString("outcome", "RUNNING").putInt("uploaded_count", 1).build()
        )
        val viewModel = AutouploadStatusViewModel(
            flowOf(AutouploadWorkSnapshot(runNow = listOf(liveWork))),
            flowOf(staleTerminal),
            flowOf(AutouploadStatusConfiguration()),
            dispatcher
        )
        val store = ViewModelStore().also { it.put("autoupload", viewModel) }
        advanceUntilIdle()

        assertEquals("RUNNING", viewModel.uiState.value.runOutcome)
        assertEquals(1, viewModel.uiState.value.uploadedCount)
        assertTrue(viewModel.uiState.value.progress)
        store.clear()
    }

    @Test
    fun cancelledRunNowDoesNotResurrectAnOlderRoomSuccess() = runTest(dispatcher) {
        val staleTerminal = AutouploadRunState("tree", "destination", "SUCCESS", 1000L, 9, 0, null)
        val viewModel = AutouploadStatusViewModel(
            flowOf(AutouploadWorkSnapshot(runNow = listOf(workInfo(WorkInfo.State.CANCELLED)))),
            flowOf(staleTerminal),
            flowOf(AutouploadStatusConfiguration()),
            dispatcher
        )
        val store = ViewModelStore().also { it.put("autoupload", viewModel) }
        advanceUntilIdle()

        assertEquals("CANCELLED", viewModel.uiState.value.runOutcome)
        store.clear()
    }

    @Test
    fun legacyCutoffWarningDoesNotReplaceTerminalOutcome() = runTest(dispatcher) {
        val terminal = AutouploadRunState("tree", "destination", "SUCCESS", 4567L, 2, 0, null)
        val viewModel = AutouploadStatusViewModel(
            flowOf(AutouploadWorkSnapshot()),
            flowOf(terminal),
            flowOf(AutouploadStatusConfiguration(legacyCutoffWarning = true)),
            dispatcher
        )
        val store = ViewModelStore().also { it.put("autoupload", viewModel) }
        advanceUntilIdle()

        assertEquals("SUCCESS", viewModel.uiState.value.runOutcome)
        assertTrue(viewModel.uiState.value.legacyCutoffWarning)
        store.clear()
    }

    @Test
    fun zeroCounterSummaryIsShownAfterACompletedRun() {
        assertTrue(AutouploadUiState(runOutcome = "SUCCESS").shouldShowCounters())
        assertFalse(AutouploadUiState().shouldShowCounters())
    }

    @Test
    fun missingSafSetupShowsActionSeparatelyFromWorkOutcome() = runTest(dispatcher) {
        val viewModel = AutouploadStatusViewModel(
            flowOf(AutouploadWorkSnapshot()),
            flowOf(null),
            flowOf(AutouploadStatusConfiguration(missingAction = "Выберите папку для автозагрузки.")),
            dispatcher
        )
        val store = ViewModelStore().also { it.put("autoupload", viewModel) }
        advanceUntilIdle()

        assertEquals("Выберите папку для автозагрузки.", viewModel.uiState.value.missingAction)
        assertEquals(null, viewModel.uiState.value.runOutcome)
        store.clear()
    }

    private fun workInfo(state: WorkInfo.State, progress: Data = Data.EMPTY): WorkInfo =
        WorkInfo(UUID.randomUUID(), state, emptySet(), Data.EMPTY, progress)
}
