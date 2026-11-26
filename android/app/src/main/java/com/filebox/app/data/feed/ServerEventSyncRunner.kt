package com.mounvix.app.data.feed

import kotlinx.coroutines.CancellationException

enum class ServerEventSyncOutcome {
    SUCCESS,
    RETRY,
    STOPPED,
}

/** Page-at-a-time delivery; a page cursor is committed only after its local side effects finish. */
class ServerEventSyncRunner(
    private val api: ServerEventApi,
    private val repository: ServerEventRepository,
    private val statusStore: ServerEventStatusStore,
    private val showAlert: (ServerEventAlert) -> Unit,
    private val notificationsAllowed: () -> Boolean,
    private val quietHours: () -> ServerEventQuietHours,
    private val advancedModeForeground: () -> Boolean,
) {
    suspend fun run(
        profile: FeedProfile,
        profileIsActive: () -> Boolean = { true },
    ): ServerEventSyncOutcome {
        if (!profileIsActive() || !statusStore.canPoll(profile)) return ServerEventSyncOutcome.STOPPED
        return try {
            val initialCursor = repository.loadCursor(profile)
            if (initialCursor == null) {
                val baselinePage = api.getServerNotifications(after = null, limit = PAGE_LIMIT)
                val plan = ServerEventSyncPolicy.plan(
                    page = baselinePage,
                    currentCursor = null,
                    alreadyVisible = emptySet(),
                    quietHours = quietHours(),
                    notificationsAllowed = notificationsAllowed(),
                    advancedModeForeground = advancedModeForeground(),
                )
                if (profileIsActive()) repository.initializeBaseline(profile, plan.nextCursor)
                return ServerEventSyncOutcome.SUCCESS
            }
            var cursor: Long = checkNotNull(initialCursor)

            repeat(MAX_PAGES_PER_RUN) {
                if (!profileIsActive()) return ServerEventSyncOutcome.SUCCESS
                val page = api.getServerNotifications(after = cursor, limit = PAGE_LIMIT)
                val plan = ServerEventSyncPolicy.plan(
                    page = page,
                    currentCursor = cursor,
                    alreadyVisible = emptySet(),
                    quietHours = quietHours(),
                    notificationsAllowed = notificationsAllowed(),
                    advancedModeForeground = advancedModeForeground(),
                )
                if (plan.historyGap) statusStore.markHistoryGap(profile)
                for (alert in plan.alerts) {
                    if (!profileIsActive()) return ServerEventSyncOutcome.SUCCESS
                    if (advancedModeForeground() || !notificationsAllowed()) continue
                    showAlert(alert)
                }
                if (!profileIsActive()) return ServerEventSyncOutcome.SUCCESS
                repository.commitPage(profile, plan.nextCursor)
                cursor = plan.nextCursor
                if (plan.historyGap || page.items.size < PAGE_LIMIT || cursor >= page.latestSeq) {
                    return ServerEventSyncOutcome.SUCCESS
                }
            }
            ServerEventSyncOutcome.SUCCESS
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (http: MounvixHttpException) {
            handleHttpFailure(profile, http)
        } catch (_: Exception) {
            ServerEventSyncOutcome.RETRY
        }
    }

    private suspend fun handleHttpFailure(
        profile: FeedProfile,
        error: MounvixHttpException,
    ): ServerEventSyncOutcome = when (error.statusCode) {
        HTTP_UNAUTHORIZED -> {
            statusStore.markNeedsLogin(profile)
            ServerEventSyncOutcome.STOPPED
        }
        HTTP_FORBIDDEN -> {
            statusStore.markForbidden(profile)
            ServerEventSyncOutcome.STOPPED
        }
        HTTP_NOT_FOUND, HTTP_NOT_IMPLEMENTED -> {
            val version = try {
                api.getHealthVersion()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (_: Exception) {
                null
            }
            statusStore.markUnsupported(profile, version)
            ServerEventSyncOutcome.STOPPED
        }
        else -> ServerEventSyncOutcome.RETRY
    }

    companion object {
        const val PAGE_LIMIT = 100
        const val MAX_PAGES_PER_RUN = 5
        private const val HTTP_UNAUTHORIZED = 401
        private const val HTTP_FORBIDDEN = 403
        private const val HTTP_NOT_FOUND = 404
        private const val HTTP_NOT_IMPLEMENTED = 501
    }
}
