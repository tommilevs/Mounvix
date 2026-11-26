package com.mounvix.app.ui

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

@RunWith(RobolectricTestRunner::class)
class AdvancedModeNavigationTest {
    @Test
    fun nativeModeIsDefaultAndOnlyTheModeChoiceIsPersisted() {
        val preferences = ApplicationProvider.getApplicationContext<Context>()
            .getSharedPreferences("advanced_mode_navigation_test", Context.MODE_PRIVATE)
        preferences.edit().clear().commit()

        assertFalse(AdvancedModePreferences.isEnabled(preferences))
        AdvancedModePreferences.setEnabled(preferences, true)

        assertTrue(AdvancedModePreferences.isEnabled(preferences))
        assertEquals(setOf(AdvancedModePreferences.KEY_MODE_ENABLED), preferences.all.keys)
    }

    @Test
    fun backNavigatesWebViewHistoryBeforeReturningToNativeMode() {
        val actions = mutableListOf<String>()

        AdvancedModeBackNavigation.handle(
            canGoBack = true,
            goBack = { actions += "web-back" },
            exitAdvancedMode = { actions += "native" },
        )
        AdvancedModeBackNavigation.handle(
            canGoBack = false,
            goBack = { actions += "web-back" },
            exitAdvancedMode = { actions += "native" },
        )

        assertEquals(listOf("web-back", "native"), actions)
    }
}
