package com.mounvix.app.data.feed

import kotlinx.coroutines.CancellationException

sealed class ServerEventCapabilityProbeResult {
    data class Supported(val healthVersion: String?) : ServerEventCapabilityProbeResult()
    data class Unsupported(val healthVersion: String?) : ServerEventCapabilityProbeResult()
    data object NeedsLogin : ServerEventCapabilityProbeResult()
    data object Forbidden : ServerEventCapabilityProbeResult()
    data class Unavailable(val message: String) : ServerEventCapabilityProbeResult()
}

/** Explicitly checks M7 access, or rechecks an unsupported server only after its health version changes. */
class ServerEventCapabilityProbe(
    private val api: ServerEventApi,
    private val statusStore: ServerEventStatusStore,
) {
    suspend fun probe(profile: FeedProfile): ServerEventCapabilityProbeResult {
        val version = try {
            api.getHealthVersion()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (http: MounvixHttpException) {
            return handleHttpFailure(profile, http, null)
        } catch (error: Exception) {
            return ServerEventCapabilityProbeResult.Unavailable(error.message ?: "health check failed")
        }
        return checkNotifications(profile, version)
    }

    suspend fun recheckIfHealthVersionChanged(
        profile: FeedProfile,
    ): ServerEventCapabilityProbeResult? {
        val stored = statusStore.status(profile)
        if (stored.access != ServerEventAccessStatus.UNSUPPORTED) return null
        val currentVersion = try {
            api.getHealthVersion()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            return ServerEventCapabilityProbeResult.Unavailable(error.message ?: "health check failed")
        }
        if (currentVersion == stored.healthVersion) return null
        return checkNotifications(profile, currentVersion)
    }

    private suspend fun checkNotifications(
        profile: FeedProfile,
        healthVersion: String?,
    ): ServerEventCapabilityProbeResult = try {
        api.getServerNotifications(after = null, limit = 1)
        statusStore.markReady(profile)
        ServerEventCapabilityProbeResult.Supported(healthVersion)
    } catch (cancelled: CancellationException) {
        throw cancelled
    } catch (http: MounvixHttpException) {
        handleHttpFailure(profile, http, healthVersion)
    } catch (error: Exception) {
        ServerEventCapabilityProbeResult.Unavailable(error.message ?: "server capability check failed")
    }

    private fun handleHttpFailure(
        profile: FeedProfile,
        error: MounvixHttpException,
        healthVersion: String?,
    ): ServerEventCapabilityProbeResult = when (error.statusCode) {
        HTTP_UNAUTHORIZED -> {
            statusStore.markNeedsLogin(profile)
            ServerEventCapabilityProbeResult.NeedsLogin
        }
        HTTP_FORBIDDEN -> {
            statusStore.markForbidden(profile)
            ServerEventCapabilityProbeResult.Forbidden
        }
        HTTP_NOT_FOUND, HTTP_NOT_IMPLEMENTED -> {
            statusStore.markUnsupported(profile, healthVersion)
            ServerEventCapabilityProbeResult.Unsupported(healthVersion)
        }
        else -> ServerEventCapabilityProbeResult.Unavailable(error.message ?: "HTTP ${error.statusCode}")
    }

    private companion object {
        const val HTTP_UNAUTHORIZED = 401
        const val HTTP_FORBIDDEN = 403
        const val HTTP_NOT_FOUND = 404
        const val HTTP_NOT_IMPLEMENTED = 501
    }
}
