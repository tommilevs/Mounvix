package com.mounvix.app

import android.app.Application
import com.mounvix.app.data.autoupload.AutouploadLegacyCutoffBootstrap
import com.mounvix.app.data.autoupload.AutouploadScheduler
import com.mounvix.app.data.autoupload.RemoteSourceFactory
import com.mounvix.app.data.db.AppDatabase
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import org.bouncycastle.jce.provider.BouncyCastleProvider
import java.security.Security

class MounvixApp : Application() {
    private val applicationIoScope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

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
            applicationIoScope.launch {
                val database = AppDatabase.get(this@MounvixApp)
                try {
                    AutouploadLegacyCutoffBootstrap(database).migrateOnce(
                        prefs,
                        RemoteSourceFactory.legacyReceiverIdentity(prefs)
                    )
                } catch (cancelled: CancellationException) {
                    throw cancelled
                } catch (_: Exception) {
                    // The worker repeats the idempotent bootstrap before planning a transfer.
                }
                if (prefs.getBoolean("autoupload_enabled", false)) {
                    AutouploadScheduler.schedule(this@MounvixApp)
                }
            }
        }
        if (prefs.getBoolean(com.mounvix.app.data.feed.FeedWorker.NOTIFICATIONS_ENABLED, false) &&
            com.mounvix.app.data.feed.FeedRepository.from(this).loadProfile() != null
        ) {
            com.mounvix.app.data.feed.FeedScheduler.schedule(this)
        }
    }
}