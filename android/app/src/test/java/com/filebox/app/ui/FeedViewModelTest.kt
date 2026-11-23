package com.mounvix.app.ui

import android.content.Context
import androidx.lifecycle.ViewModelStore
import androidx.test.core.app.ApplicationProvider
import com.mounvix.app.data.feed.FeedApi
import com.mounvix.app.data.feed.FeedCookieJar
import com.mounvix.app.data.feed.FeedProfile
import com.mounvix.app.data.feed.FeedRepository
import com.mounvix.app.data.feed.InMemoryProfileSecretStore
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeoutOrNull
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.util.concurrent.TimeUnit

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [33])
class FeedViewModelTest {
    private lateinit var server: MockWebServer
    private lateinit var repository: FeedRepository
    private lateinit var viewModel: FeedViewModel
    private lateinit var profile: FeedProfile
    private val viewModelStore = ViewModelStore()

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
        val context = ApplicationProvider.getApplicationContext<Context>()
        val preferences = context.getSharedPreferences("feed-view-model-test", Context.MODE_PRIVATE)
        preferences.edit().clear().commit()
        profile = FeedProfile(server.url("/").toString().trimEnd('/'), "alice", "secret")
        repository = FeedRepository(
            FeedApi(OkHttpClient.Builder().cookieJar(FeedCookieJar()).build()),
            preferences,
            InMemoryProfileSecretStore(),
        )
        viewModel = FeedViewModel(repository, Dispatchers.Unconfined)
        viewModelStore.put("feed", viewModel)
    }

    @After
    fun tearDown() {
        viewModelStore.clear()
        server.shutdown()
    }

    @Test
    fun loadShowsLoadingWhileRequestRunsThenEmptyForEmptyFeed() = runBlocking {
        repository.saveProfile(profile)
        server.enqueue(
            MockResponse()
                .setBody(emptyFeed())
                .setBodyDelay(200, TimeUnit.MILLISECONDS),
        )

        viewModel.load()

        assertEquals(FeedUiState.Loading, viewModel.uiState.value)
        val state = awaitState { it == FeedUiState.Empty }
        assertEquals(FeedUiState.Empty, state)
    }

    @Test
    fun loadShowsProfileNotConfiguredWithoutMakingARequest() = runBlocking {
        viewModel.load()

        val state = awaitState { it == FeedUiState.ProfileNotConfigured }

        assertEquals(FeedUiState.ProfileNotConfigured, state)
        assertEquals(0, server.requestCount)
    }

    @Test
    fun loadShowsRetryableErrorAndRetryLoadsFeed() = runBlocking {
        repository.saveProfile(profile)
        server.enqueue(MockResponse().setResponseCode(503).setBody("offline"))
        server.enqueue(MockResponse().setBody(emptyFeed()))

        viewModel.load()
        val error = awaitState { it is FeedUiState.Error }
        assertTrue("server error should be surfaced", (error as? FeedUiState.Error)?.message?.isNotBlank() == true)

        viewModel.retry()
        assertEquals(FeedUiState.Empty, awaitState { it == FeedUiState.Empty })
        assertEquals(2, server.requestCount)
    }

    @Test
    fun openingCardMarksItReadAndKeepsItOpen() = runBlocking {
        repository.saveProfile(profile)
        server.enqueue(MockResponse().setBody(feedWithUnreadCard()))
        server.enqueue(MockResponse().setResponseCode(204))

        viewModel.load()
        val initial = awaitState { it is FeedUiState.Content }
        assertNotNull(initial)
        viewModel.openCard("card-1")

        val opened = awaitState { state ->
            state is FeedUiState.Content && state.openedCardId == "card-1" && state.cards.single().isRead
        } as? FeedUiState.Content
        assertNotNull("opened card should be read after server acknowledgement", opened)
        assertEquals("GET", server.takeRequest().method)
        val markRead = server.takeRequest()
        assertEquals("POST", markRead.method)
        assertEquals("/api/v1/feeds/card-1/read", markRead.path)
    }

    private suspend fun awaitState(predicate: (FeedUiState) -> Boolean): FeedUiState? =
        withTimeoutOrNull(5_000) { viewModel.uiState.first(predicate) }

    private fun emptyFeed() =
        """{"items":[],"next_since":"2026-11-18T12:00:00Z"}"""

    private fun feedWithUnreadCard() =
        """{"items":[{"id":"card-1","generated_at":"2026-11-17T12:00:00Z","title":"A memory","media":[],"is_read":false}],"next_since":"2026-11-18T12:00:00Z"}"""
}
