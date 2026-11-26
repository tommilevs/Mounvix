package com.mounvix.app.data.feed

import java.io.IOException

/** Native interface used by the server-event poller and capability probe. */
interface ServerEventApi {
    suspend fun getServerNotifications(after: Long?, limit: Int = 100): ServerNotificationsPage
    suspend fun getHealthVersion(): String?
}

/** HTTP status is kept typed so the poller can distinguish authentication/capability failures. */
class MounvixHttpException(
    val statusCode: Int,
    message: String,
) : IOException(message)
