package com.mounvix.app.data.feed

import android.content.Context
import android.content.Intent
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

@RunWith(RobolectricTestRunner::class)
class ServerEventNotificationIntentRouterTest {
    private val context = ApplicationProvider.getApplicationContext<Context>()
    private val baseUrl = "https://nas.example:8443/mounvix"

    @Test
    fun notificationRouteIsResolvedOnlyInsideTheActiveProfilePrefix() {
        val intent = ServerEventNotificationIntentRouter.createIntent(
            context = context,
            eventId = "job-42",
            targetRoute = "/mounvix/jobs/42",
        )

        val route = ServerEventNotificationIntentRouter.parse(intent, baseUrl)

        assertEquals("job-42", route?.eventId)
        assertEquals("https://nas.example:8443/mounvix/jobs/42", route?.url)
    }

    @Test
    fun malformedOrOutOfPrefixNotificationRoutesAreRejected() {
        val safeIntent = ServerEventNotificationIntentRouter.createIntent(
            context = context,
            eventId = "job-42",
            targetRoute = "/mounvix/jobs/42",
        )
        val hostile = Intent(safeIntent)
            .putExtra(ServerEventNotificationIntentRouter.EXTRA_TARGET_ROUTE, "/mounvix/%2e%2e/admin")
        val outsidePrefix = Intent(safeIntent)
            .putExtra(ServerEventNotificationIntentRouter.EXTRA_TARGET_ROUTE, "/outside/admin")

        assertNull(ServerEventNotificationIntentRouter.parse(hostile, baseUrl))
        assertNull(ServerEventNotificationIntentRouter.parse(outsidePrefix, baseUrl))
        assertNull(ServerEventNotificationIntentRouter.parse(safeIntent, "not a url"))
    }
}
