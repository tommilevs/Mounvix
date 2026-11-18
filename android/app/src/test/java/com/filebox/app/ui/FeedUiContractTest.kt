package com.mounvix.app.ui

import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

class FeedUiContractTest {
    @Test
    fun feedAndProfileScreensAreAvailable() {
        assertComposable("com.mounvix.app.ui.FeedScreenKt", "FeedScreen")
        assertComposable("com.mounvix.app.ui.FeedProfileSectionKt", "FeedProfileSection")
    }

    @Test
    fun rootAcceptsNotificationOpenRequests() {
        val root = loadClass("com.mounvix.app.ui.MounvixRootKt")
        assertNotNull("MounvixRoot should expose a notification-open request", root)
        assertTrue(
            "MounvixRoot should accept an integer notification request signal",
            root!!.declaredMethods.any { method ->
                method.name == "MounvixRoot" && method.parameterTypes.firstOrNull() == Int::class.javaPrimitiveType
            },
        )
    }

    @Test
    fun notificationIntentRouterRecognizesOnlyMemoriesAction() {
        val router = loadClass("com.mounvix.app.data.notify.FeedNotificationIntentRouter")
        assertNotNull("notification intent routing should be independently testable", router)
        val instance = router!!.getField("INSTANCE").get(null)
        val shouldOpen = router.getMethod("shouldOpenMemories", String::class.java)
        assertTrue(shouldOpen.invoke(instance, "com.mounvix.app.OPEN_MEMORIES") as Boolean)
        assertFalse(shouldOpen.invoke(instance, "com.mounvix.app.OPEN_SETTINGS") as Boolean)
    }

    private fun assertComposable(ownerName: String, methodName: String) {
        val owner = loadClass(ownerName)
        assertNotNull("$methodName composable should exist", owner)
        assertTrue("$methodName composable should be exposed", owner!!.declaredMethods.any { it.name == methodName })
    }

    private fun loadClass(name: String): Class<*>? = runCatching { Class.forName(name) }.getOrNull()
}
