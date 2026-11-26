package com.mounvix.app.data.feed

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [33], application = android.app.Application::class)
class ServerEventCapabilityProbeTest {
    private lateinit var profile: FeedProfile
    private lateinit var statusStore: ServerEventStatusStore
    private lateinit var api: FakeApi

    @Before
    fun setUp() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val preferences = context.getSharedPreferences("server-probe-test", Context.MODE_PRIVATE)
        preferences.edit().clear().commit()
        profile = FeedProfile("https://nas.example/mounvix", "alice", "secret")
        statusStore = ServerEventStatusStore(preferences)
        api = FakeApi()
    }

    @Test
    fun unsupportedServerIsProbedAgainOnlyAfterHealthVersionChanges() = runBlocking {
        api.notificationsFailure = MounvixHttpException(404, "M7 unavailable")
        val probe = ServerEventCapabilityProbe(api, statusStore)

        val initial = probe.probe(profile)
        assertEquals(ServerEventCapabilityProbeResult.Unsupported("1.0.0"), initial)
        assertEquals(ServerEventAccessStatus.UNSUPPORTED, statusStore.status(profile).access)
        assertEquals("1.0.0", statusStore.status(profile).healthVersion)
        assertEquals(1, api.notificationRequests)

        api.notificationsFailure = null
        assertNull(probe.recheckIfHealthVersionChanged(profile))
        assertEquals(1, api.notificationRequests)

        api.healthVersion = "1.1.0"
        assertEquals(ServerEventCapabilityProbeResult.Supported("1.1.0"), probe.recheckIfHealthVersionChanged(profile))
        assertEquals(ServerEventAccessStatus.READY, statusStore.status(profile).access)
        assertEquals(2, api.notificationRequests)
    }

    @Test
    fun forbiddenAndUnauthorizedProbesPersistTheirDistinctAccessStates() = runBlocking {
        val probe = ServerEventCapabilityProbe(api, statusStore)
        api.notificationsFailure = MounvixHttpException(403, "admin required")
        assertEquals(ServerEventCapabilityProbeResult.Forbidden, probe.probe(profile))
        assertEquals(ServerEventAccessStatus.FORBIDDEN, statusStore.status(profile).access)

        api.notificationsFailure = MounvixHttpException(401, "session expired")
        assertEquals(ServerEventCapabilityProbeResult.NeedsLogin, probe.probe(profile))
        assertEquals(ServerEventAccessStatus.NEEDS_LOGIN, statusStore.status(profile).access)
    }

    @Test
    fun transientProbeFailureDoesNotMarkTheServerUnsupported() = runBlocking {
        val probe = ServerEventCapabilityProbe(api, statusStore)
        api.notificationsFailure = MounvixHttpException(503, "maintenance")

        val result = probe.probe(profile)

        assertTrue(result is ServerEventCapabilityProbeResult.Unavailable)
        assertEquals(ServerEventAccessStatus.UNVERIFIED, statusStore.status(profile).access)
    }

    private class FakeApi : ServerEventApi {
        var healthVersion: String? = "1.0.0"
        var healthFailure: Throwable? = null
        var notificationsFailure: Throwable? = null
        var notificationRequests = 0

        override suspend fun getServerNotifications(after: Long?, limit: Int): ServerNotificationsPage {
            notificationRequests++
            notificationsFailure?.let { throw it }
            return ServerNotificationsPage(emptyList(), 0L, null, false, 0L, 0L, 0)
        }

        override suspend fun getHealthVersion(): String? {
            healthFailure?.let { throw it }
            return healthVersion
        }
    }
}
