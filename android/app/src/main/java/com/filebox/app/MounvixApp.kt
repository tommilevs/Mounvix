package com.mounvix.app

import android.app.Application
import com.mounvix.app.data.autoupload.AutouploadScheduler
import org.bouncycastle.jce.provider.BouncyCastleProvider
import java.security.Security

class MounvixApp : Application() {
    override fun onCreate() {
        super.onCreate()
        // sshj требует полноценный BouncyCastle: Android-овский "BC" урезан (нет X25519).
        try {
            Security.removeProvider("BC")
            Security.insertProviderAt(BouncyCastleProvider(), 1)
        } catch (_: Exception) {
        }
        val prefs = getSharedPreferences("mounvix_prefs", MODE_PRIVATE)
        if (prefs.getBoolean("autoupload_enabled", false)) {
            AutouploadScheduler.schedule(this)
        }
    }
}