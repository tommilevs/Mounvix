package com.mounvix.app.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class AdvancedModeUrlPolicyTest {
    private val base = "https://nas.example:8443/mounvix"

    @Test
    fun exactConfiguredOriginAndBasePathAreAllowed() {
        assertEquals(
            NavigationDecision.ALLOW_INTERNAL,
            AdvancedModeUrlPolicy.decide(base, "https://nas.example:8443/mounvix/notifications", false, false),
        )
        assertEquals(
            NavigationDecision.ALLOW_INTERNAL,
            AdvancedModeUrlPolicy.decide(base, "https://nas.example:8443/mounvix", false, false),
        )
    }

    @Test
    fun schemeHostPortAndPathPrefixMustMatchExactly() {
        for (url in listOf(
            "http://nas.example:8443/mounvix/page",
            "https://other.example:8443/mounvix/page",
            "https://nas.example:9443/mounvix/page",
            "https://nas.example:8443/mounvix-evil/page",
            "https://nas.example:8443/other/page",
        )) {
            assertEquals(NavigationDecision.BLOCK, AdvancedModeUrlPolicy.decide(base, url, false, false))
        }
    }

    @Test
    fun unsafeSchemesNetworkPathsAndUserInfoAreBlocked() {
        for (url in listOf(
            "//evil.example/path",
            "javascript:alert(1)",
            "file:///etc/passwd",
            "content://provider/private",
            "https://alice:secret@nas.example:8443/mounvix/page",
            "https://@nas.example:8443/mounvix/page",
        )) {
            assertEquals(NavigationDecision.BLOCK, AdvancedModeUrlPolicy.decide(base, url, false, true))
        }
    }

    @Test
    fun configuredBasePathTraversalIsRejectedBeforeRouteResolution() {
        val unsafeBase = "https://nas.example:8443/mounvix/%2e%2e/admin"
        assertEquals(
            NavigationDecision.BLOCK,
            AdvancedModeUrlPolicy.decide(unsafeBase, "$unsafeBase/page", false, false),
        )
        assertEquals(null, AdvancedModeUrlPolicy.resolveInternalRoute(unsafeBase, "/admin/page"))
    }

    @Test
    fun externalUrlOpensOnlyForUserGestureAndNeverForRedirects() {
        val external = "https://example.org/help"
        assertEquals(NavigationDecision.OPEN_EXTERNAL, AdvancedModeUrlPolicy.decide(base, external, false, true))
        assertEquals(NavigationDecision.BLOCK, AdvancedModeUrlPolicy.decide(base, external, false, false))
        assertEquals(NavigationDecision.BLOCK, AdvancedModeUrlPolicy.decide(base, external, true, true))
    }

    @Test
    fun notificationRouteResolvesOnlyInsideConfiguredOriginAndPrefix() {
        assertEquals(
            "https://nas.example:8443/mounvix/jobs/42",
            AdvancedModeUrlPolicy.resolveInternalRoute(base, "/mounvix/jobs/42"),
        )
        for (route in listOf(
            "//attacker.example/steal",
            "https://attacker.example/steal",
            "/outside/jobs/42",
            "/mounvix/%2e%2e/admin",
        )) {
            assertEquals(null, AdvancedModeUrlPolicy.resolveInternalRoute(base, route))
        }
    }

    @Test
    fun encodedTraversalAndEncodedSeparatorsAreBlocked() {
        for (url in listOf(
            "https://nas.example:8443/mounvix/%2e%2e/admin",
            "https://nas.example:8443/mounvix/a/%252e%252e/admin",
            "https://nas.example:8443/mounvix/a%2fb",
            "https://nas.example:8443/mounvix/a%5cb",
        )) {
            assertEquals(NavigationDecision.BLOCK, AdvancedModeUrlPolicy.decide(base, url, false, false))
        }
    }
}
