package com.mounvix.app.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class AdvancedModeSessionCookieTest {
    @Test
    fun httpsExpirationTargetsOnlyMvxAtTheConfiguredBasePath() {
        val header = AdvancedModeSessionCookieCleaner.expirationHeader("https://nas.example:8443/mounvix")

        assertEquals(
            "mvx=; Max-Age=0; Path=/mounvix; Expires=Thu, 01 Jan 1970 00:00:00 GMT; HttpOnly; SameSite=Lax; Secure",
            header,
        )
        assertFalse(header!!.contains("Domain="))
    }

    @Test
    fun httpExpirationOmitsSecureButKeepsThePathBoundary() {
        val header = AdvancedModeSessionCookieCleaner.expirationHeader("http://nas.example/files")

        assertTrue(header!!.contains("Path=/files"))
        assertFalse(header.contains("Secure"))
        assertTrue(header.startsWith("mvx=;"))
    }

    @Test
    fun rootBaseUrlUsesTheExactRootCookiePath() {
        val header = AdvancedModeSessionCookieCleaner.expirationHeader("https://nas.example")

        assertTrue(header!!.contains("Path=/;"))
    }

    @Test
    fun invalidOriginDoesNotProduceACookieMutation() {
        assertNull(AdvancedModeSessionCookieCleaner.expirationHeader("javascript:alert(1)"))
    }
}
