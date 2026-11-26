package com.mounvix.app

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.lifecycleScope
import com.mounvix.app.data.feed.FeedApi
import com.mounvix.app.data.feed.FeedRepository
import com.mounvix.app.data.feed.ServerEventAccessStatus
import com.mounvix.app.data.feed.ServerEventCapabilityProbe
import com.mounvix.app.data.feed.ServerEventCapabilityProbeResult
import com.mounvix.app.data.feed.ServerEventNotificationIntentRouter
import com.mounvix.app.data.feed.ServerEventScheduler
import com.mounvix.app.data.feed.ServerEventStatusStore
import com.mounvix.app.data.notify.FeedNotificationIntentRouter
import com.mounvix.app.ui.MounvixRoot
import com.mounvix.app.ui.theme.MounvixTheme
import kotlinx.coroutines.launch

class MainActivity : ComponentActivity() {
    private var feedNotificationOpenRequest by mutableIntStateOf(0)
    private var serverEventOpenRequest by mutableIntStateOf(0)
    private var serverEventTargetRoute by mutableStateOf<String?>(null)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        handleFeedIntent(intent)
        handleServerEventIntent(intent)
        setContent {
            MounvixTheme {
                MounvixRoot(
                    notificationOpenRequest = feedNotificationOpenRequest,
                    serverEventOpenRequest = serverEventOpenRequest,
                    serverEventTargetRoute = serverEventTargetRoute,
                )
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        handleFeedIntent(intent)
        handleServerEventIntent(intent)
    }

    override fun onResume() {
        super.onResume()
        lifecycleScope.launch {
            val repository = FeedRepository.from(applicationContext)
            val profile = repository.loadProfile() ?: return@launch
            val preferences = getSharedPreferences("mounvix_prefs", MODE_PRIVATE)
            val statusStore = ServerEventStatusStore(preferences)
            if (statusStore.status(profile).access != ServerEventAccessStatus.UNSUPPORTED) return@launch
            when (ServerEventCapabilityProbe(FeedApi.shared(), statusStore).recheckIfHealthVersionChanged(profile)) {
                is ServerEventCapabilityProbeResult.Supported -> {
                    if (statusStore.canPoll(profile)) ServerEventScheduler.schedule(this@MainActivity, profile)
                }
                is ServerEventCapabilityProbeResult.Unsupported,
                ServerEventCapabilityProbeResult.Forbidden,
                ServerEventCapabilityProbeResult.NeedsLogin -> ServerEventScheduler.cancel(this@MainActivity)
                else -> Unit
            }
        }
    }

    private fun handleServerEventIntent(intent: Intent?) {
        if (intent?.action != ServerEventNotificationIntentRouter.ACTION_OPEN_SERVER_EVENT) return
        val profile = FeedRepository.from(applicationContext).loadProfile()
        val safeRoute = profile?.let { ServerEventNotificationIntentRouter.parse(intent, it.baseUrl) }
        intent.action = null
        if (safeRoute == null) return
        serverEventTargetRoute = safeRoute.url
        serverEventOpenRequest += 1
    }

    private fun handleFeedIntent(intent: Intent?) {
        if (FeedNotificationIntentRouter.shouldOpenMemories(intent?.action)) {
            feedNotificationOpenRequest += 1
            intent?.action = null
        }
    }
}
