package com.mounvix.app.data.feed

/** Process-local visibility flag; only Advanced Mode sets it while resumed. */
object ServerEventForegroundState {
    @Volatile
    private var advancedModeForeground = false

    fun isAdvancedModeForeground(): Boolean = advancedModeForeground

    fun setAdvancedModeForeground(isForeground: Boolean) {
        advancedModeForeground = isForeground
    }
}
