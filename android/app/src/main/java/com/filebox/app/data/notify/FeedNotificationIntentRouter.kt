package com.mounvix.app.data.notify

/** Identifies notification intents that should open the Memories feed. */
object FeedNotificationIntentRouter {
    fun shouldOpenMemories(action: String?): Boolean = action == FeedNotifier.ACTION_OPEN_MEMORIES
}
