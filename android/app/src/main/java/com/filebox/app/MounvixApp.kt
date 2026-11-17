package com.mounvix.app

import android.app.Application
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
    }
}
