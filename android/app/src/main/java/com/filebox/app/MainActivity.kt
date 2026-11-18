package com.mounvix.app

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.setValue
import com.mounvix.app.data.notify.FeedNotificationIntentRouter
import com.mounvix.app.ui.MounvixRoot
import com.mounvix.app.ui.theme.MounvixTheme

class MainActivity : ComponentActivity() {
    private var feedNotificationOpenRequest by mutableIntStateOf(0)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        handleFeedIntent(intent)
        setContent {
            MounvixTheme {
                MounvixRoot(notificationOpenRequest = feedNotificationOpenRequest)
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        handleFeedIntent(intent)
    }

    private fun handleFeedIntent(intent: Intent?) {
        if (FeedNotificationIntentRouter.shouldOpenMemories(intent?.action)) {
            feedNotificationOpenRequest += 1
            intent?.action = null
        }
    }
}
