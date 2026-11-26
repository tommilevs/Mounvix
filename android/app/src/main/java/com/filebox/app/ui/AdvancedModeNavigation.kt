package com.mounvix.app.ui

import android.content.SharedPreferences

/** Encapsulates the rule that browser history is consumed before Advanced Mode exits. */
internal object AdvancedModeBackNavigation {
    fun handle(canGoBack: Boolean, goBack: () -> Unit, exitAdvancedMode: () -> Unit) {
        if (canGoBack) goBack() else exitAdvancedMode()
    }
}

/** Persists only the native-versus-Advanced-Mode choice, never a WebView password. */
object AdvancedModePreferences {
    const val KEY_MODE_ENABLED = "mounvix_advanced_mode_enabled"

    fun isEnabled(preferences: SharedPreferences): Boolean = preferences.getBoolean(KEY_MODE_ENABLED, false)

    fun setEnabled(preferences: SharedPreferences, enabled: Boolean) {
        check(preferences.edit().putBoolean(KEY_MODE_ENABLED, enabled).commit()) {
            "could not persist Advanced Mode selection"
        }
    }
}
