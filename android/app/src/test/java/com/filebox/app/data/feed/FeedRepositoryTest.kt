package com.mounvix.app.data.feed

import android.content.SharedPreferences
import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class FeedRepositoryTest {
    private lateinit var server: MockWebServer
    private lateinit var preferences: MemoryPreferences
    private lateinit var repository: FeedRepository
    private lateinit var profile: FeedProfile

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
        preferences = MemoryPreferences()
        profile = FeedProfile(server.url("/").toString().trimEnd('/'), "alice", "secret")
        val api = FeedApi(OkHttpClient.Builder().cookieJar(FeedCookieJar()).build())
        repository = FeedRepository(api, preferences)
        repository.saveProfile(profile)
    }

    @After
    fun tearDown() {
        server.shutdown()
    }

    @Test
    fun fetchNewDoesNotAdvanceCursor() = runBlocking {
        repository.commitCursor("2026-11-01T00:00:00Z")
        server.enqueue(MockResponse().setResponseCode(200).setBody(emptyFeed("2026-11-02T00:00:00Z")))

        repository.fetchNewSinceCursor()

        assertEquals("2026-11-01T00:00:00Z", preferences.getString("feed_cursor", null))
        assertEquals(
            "/api/v1/feeds?since=2026-11-01T00%3A00%3A00Z&limit=50",
            server.takeRequest().path,
        )
    }

    @Test
    fun commitCursorPersists() {
        repository.commitCursor("2026-11-03T12:34:56Z")

        assertEquals("2026-11-03T12:34:56Z", preferences.getString("feed_cursor", null))
    }

    @Test
    fun switchingProfileResetsCursor() {
        repository.commitCursor("2026-11-01T00:00:00Z")

        repository.saveProfile(profile.copy(username = "bob", password = "other-secret"))

        assertEquals(null, preferences.getString("feed_cursor", null))
    }

    @Test
    fun networkFailureKeepsCursor() = runBlocking {
        repository.commitCursor("2026-11-01T00:00:00Z")
        server.enqueue(MockResponse().setResponseCode(503).setBody("unavailable"))

        val error = runCatching { repository.fetchNewSinceCursor() }.exceptionOrNull()

        assertTrue("expected network failure", error != null)
        assertEquals("2026-11-01T00:00:00Z", preferences.getString("feed_cursor", null))
    }

    private fun emptyFeed(cursor: String) = """{"items":[],"next_since":"$cursor"}"""
}

private class MemoryPreferences : SharedPreferences {
    private val values = mutableMapOf<String, Any>()

    override fun getAll(): MutableMap<String, *> = values.toMutableMap()
    override fun getString(key: String?, defValue: String?): String? = values[key.orEmpty()] as? String ?: defValue
    override fun getStringSet(key: String?, defValues: MutableSet<String>?): MutableSet<String>? =
        (values[key.orEmpty()] as? Set<*>)?.mapNotNull { it as? String }?.toMutableSet() ?: defValues
    override fun getInt(key: String?, defValue: Int): Int = values[key.orEmpty()] as? Int ?: defValue
    override fun getLong(key: String?, defValue: Long): Long = values[key.orEmpty()] as? Long ?: defValue
    override fun getFloat(key: String?, defValue: Float): Float = values[key.orEmpty()] as? Float ?: defValue
    override fun getBoolean(key: String?, defValue: Boolean): Boolean = values[key.orEmpty()] as? Boolean ?: defValue
    override fun contains(key: String?): Boolean = values.containsKey(key.orEmpty())
    override fun edit(): SharedPreferences.Editor = MemoryEditor(values)
    override fun registerOnSharedPreferenceChangeListener(listener: SharedPreferences.OnSharedPreferenceChangeListener?) = Unit
    override fun unregisterOnSharedPreferenceChangeListener(listener: SharedPreferences.OnSharedPreferenceChangeListener?) = Unit
}

private class MemoryEditor(private val values: MutableMap<String, Any>) : SharedPreferences.Editor {
    private val pending = mutableMapOf<String, Any?>()
    private var clearRequested = false

    override fun putString(key: String?, value: String?): SharedPreferences.Editor = put(key, value)
    override fun putStringSet(key: String?, values: MutableSet<String>?): SharedPreferences.Editor = put(key, values?.toSet())
    override fun putInt(key: String?, value: Int): SharedPreferences.Editor = put(key, value)
    override fun putLong(key: String?, value: Long): SharedPreferences.Editor = put(key, value)
    override fun putFloat(key: String?, value: Float): SharedPreferences.Editor = put(key, value)
    override fun putBoolean(key: String?, value: Boolean): SharedPreferences.Editor = put(key, value)
    override fun remove(key: String?): SharedPreferences.Editor = put(key, null)
    override fun clear(): SharedPreferences.Editor = apply { clearRequested = true }

    override fun commit(): Boolean {
        if (clearRequested) values.clear()
        for ((key, value) in pending) {
            if (value == null) values.remove(key) else values[key] = value
        }
        return true
    }

    override fun apply() {
        commit()
    }

    private fun put(key: String?, value: Any?): SharedPreferences.Editor = apply {
        pending[key.orEmpty()] = value
    }
}
