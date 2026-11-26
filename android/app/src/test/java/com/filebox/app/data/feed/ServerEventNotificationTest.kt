package com.mounvix.app.data.feed

import android.Manifest
import android.app.NotificationManager
import android.content.Context
import android.content.Intent
import androidx.test.core.app.ApplicationProvider
import androidx.core.app.NotificationCompat
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [33], application = android.app.Application::class)
class ServerEventNotificationTest {
    private lateinit var context: Context

    @Before
    fun setUp() {
        context = ApplicationProvider.getApplicationContext()
        context.getSystemService(NotificationManager::class.java).cancelAll()
        Shadows.shadowOf(context as android.app.Application).grantPermissions(Manifest.permission.POST_NOTIFICATIONS)
    }

    @Test
    fun advancedModeForegroundFlagCanSuppressBackgroundAlerts() {
        ServerEventForegroundState.setAdvancedModeForeground(false)
        assertFalse(ServerEventForegroundState.isAdvancedModeForeground())

        ServerEventForegroundState.setAdvancedModeForeground(true)
        assertTrue(ServerEventForegroundState.isAdvancedModeForeground())
        ServerEventForegroundState.setAdvancedModeForeground(false)
    }

    @Test
    fun repeatedEventReplacesItsStableNotificationAndShowsOnlySafeSummary() {
        val original = alert("Task completed safely.")
        val updated = original.copy(summary = "Task retried successfully.")

        assertTrue(ServerEventNotifier.show(context, original))
        assertTrue(ServerEventNotifier.show(context, updated))

        val posted = context.getSystemService(NotificationManager::class.java).activeNotifications.single()
        assertEquals(ServerEventNotifier.notificationTag(original.eventId), posted.tag)
        assertEquals(ServerEventNotifier.NOTIFICATION_ID, posted.id)
        assertEquals("Задания сервера", posted.notification.extras.getCharSequence(NotificationCompat.EXTRA_TITLE))
        assertEquals("Task retried successfully.", posted.notification.extras.getCharSequence(NotificationCompat.EXTRA_TEXT))
        assertFalse(posted.notification.extras.getCharSequence(NotificationCompat.EXTRA_TEXT).toString().contains("never-display"))
    }

    @Test
    fun deniedNotificationPermissionDoesNotPost() {
        Shadows.shadowOf(context as android.app.Application).denyPermissions(Manifest.permission.POST_NOTIFICATIONS)

        assertFalse(ServerEventNotifier.show(context, alert("Must remain in inbox.")))
        assertTrue(context.getSystemService(NotificationManager::class.java).activeNotifications.isEmpty())
    }

    @Test
    fun intentRouterAcceptsOnlyValidatedServerEventRouteActions() {
        val route = "/mounvix/notifications/event-1"
        val intent = ServerEventNotificationIntentRouter.createIntent(context, "event-1", route)

        val request = ServerEventNotificationIntentRouter.openRequest(intent)
        assertEquals("event-1", request?.eventId)
        assertEquals(route, request?.targetRoute)
        assertNull(ServerEventNotificationIntentRouter.openRequest(Intent().setAction("com.mounvix.app.OPEN_SETTINGS")))

        val forged = Intent(ServerEventNotificationIntentRouter.ACTION_OPEN_SERVER_EVENT)
            .putExtra(ServerEventNotificationIntentRouter.EXTRA_EVENT_ID, "event-1")
            .putExtra(ServerEventNotificationIntentRouter.EXTRA_TARGET_ROUTE, "//attacker.example/path")
        assertNull(ServerEventNotificationIntentRouter.openRequest(forged))
    }

    private fun alert(summary: String) = ServerEventAlert(
        eventId = "event-1",
        kind = "plugin",
        summary = summary,
        targetRoute = "/mounvix/notifications/event-1?secret=never-display",
    )
}
