package com.mounvix.app.ui

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mounvix.app.data.feed.FeedCard
import com.mounvix.app.data.feed.FeedRepository
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** Immutable states rendered by the Memories feed screen. */
sealed interface FeedUiState {
    data object Loading : FeedUiState
    data object ProfileNotConfigured : FeedUiState
    data object Empty : FeedUiState
    data class Content(
        val cards: List<FeedCard>,
        val openedCardId: String? = null,
        val message: String? = null,
    ) : FeedUiState
    data class Error(val message: String) : FeedUiState
}

/** Loads Memories cards and exposes user actions to Compose. */
class FeedViewModel @JvmOverloads constructor(
    private val repository: FeedRepository,
    private val dispatcher: CoroutineDispatcher = Dispatchers.Main.immediate,
) : ViewModel() {
    private val _uiState = MutableStateFlow<FeedUiState>(FeedUiState.Loading)
    private var loadJob: Job? = null

    /** Current screen state. */
    val uiState: StateFlow<FeedUiState> = _uiState.asStateFlow()

    /** Load retained feed history, preserving lifecycle cancellation. */
    fun load() {
        val currentLoad = loadJob
        if (currentLoad?.isActive == true) {
            if (_uiState.value !is FeedUiState.Error) return
            currentLoad.cancel()
        }
        _uiState.value = FeedUiState.Loading
        loadJob = viewModelScope.launch(dispatcher) {
            try {
                if (repository.loadProfile() == null) {
                    _uiState.value = FeedUiState.ProfileNotConfigured
                    return@launch
                }
                val page = repository.loadFeed()
                _uiState.value = if (page.items.isEmpty()) {
                    FeedUiState.Empty
                } else {
                    FeedUiState.Content(page.items)
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                _uiState.value = FeedUiState.Error(error.userMessage("Не удалось загрузить воспоминания"))
            }
        }
    }

    /** Retry the most recent feed load. */
    fun retry() = load()

    /** Open a card's media details and mark it read. */
    fun openCard(id: String) {
        val content = _uiState.value as? FeedUiState.Content ?: return
        if (content.cards.none { it.id == id }) return
        _uiState.value = content.copy(openedCardId = id, message = null)
        markRead(id)
    }

    /** Close the currently opened card details. */
    fun closeCard() {
        _uiState.update { current ->
            if (current is FeedUiState.Content) current.copy(openedCardId = null, message = null) else current
        }
    }

    /** Mark a card read for the active Mounvix account. */
    fun markRead(id: String) {
        val content = _uiState.value as? FeedUiState.Content ?: return
        val card = content.cards.firstOrNull { it.id == id } ?: return
        if (card.isRead) return
        viewModelScope.launch(dispatcher) {
            try {
                repository.markRead(id)
                _uiState.update { current ->
                    if (current is FeedUiState.Content) {
                        current.copy(
                            cards = current.cards.map { item -> if (item.id == id) item.copy(isRead = true) else item },
                            message = null,
                        )
                    } else {
                        current
                    }
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                _uiState.update { current ->
                    if (current is FeedUiState.Content) {
                        current.copy(message = error.userMessage("Не удалось отметить воспоминание прочитанным"))
                    } else {
                        current
                    }
                }
            }
        }
    }

    private fun Throwable.userMessage(fallback: String): String =
        message?.takeIf(String::isNotBlank) ?: fallback
}
