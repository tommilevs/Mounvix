package com.mounvix.app.data.autoupload

import android.content.SharedPreferences
import android.net.Uri
import androidx.room.withTransaction
import com.mounvix.app.data.db.AppDatabase
import com.mounvix.app.data.db.AutouploadLegacyCutoff
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/** Outcome of the one-time migration from the legacy global upload watermark. */
data class LegacyCutoffBootstrapResult(
    val warningCode: String?,
    val cutoffInserted: Boolean,
    val markerCommitted: Boolean
)

/** Scopes the old global timestamp only when both SAF tree and receiver identity are unambiguous. */
class AutouploadLegacyCutoffBootstrap(private val database: AppDatabase) {
    suspend fun migrateOnce(
        prefs: SharedPreferences,
        legacyReceiverIdentity: LegacyReceiverIdentity?
    ): LegacyCutoffBootstrapResult = withContext(Dispatchers.IO) {
        if (prefs.getBoolean(BOOTSTRAPPED_KEY, false)) {
            val warning = if (prefs.getBoolean(WARNING_KEY, false)) WARNING_UNSCOPED else null
            return@withContext LegacyCutoffBootstrapResult(warning, cutoffInserted = false, markerCommitted = true)
        }

        val watermark = runCatching { prefs.getLong(LEGACY_WATERMARK_KEY, 0L) }.getOrDefault(0L)
        var warningCode: String? = null
        var inserted = false
        if (watermark > 0L) {
            val rawTree = prefs.getString(LEGACY_TREE_KEY, null)
            val rawDestination = prefs.getString(LEGACY_DESTINATION_KEY, null)
            val normalizedDestination = rawDestination?.let {
                runCatching { AutouploadIdentity.normalizeDestination(it) }.getOrNull()
            }
            val treeKey = rawTree?.let { value ->
                runCatching { AutouploadIdentity.treeKey(Uri.parse(value)) }.getOrNull()
            }
            val identity = legacyReceiverIdentity?.takeIf {
                normalizedDestination != null && it.destination == normalizedDestination
            }

            if (treeKey == null || identity == null) {
                warningCode = WARNING_UNSCOPED
            } else {
                val dao = database.autouploadDao()
                database.withTransaction {
                    // The row is also a durable transaction marker if the process dies before
                    // SharedPreferences.commit(); never retarget the legacy timestamp afterward.
                    if (!dao.hasAnyLegacyCutoff()) {
                        dao.insertCutoffIfAbsent(
                            AutouploadLegacyCutoff(
                                treeKey = treeKey,
                                destinationKey = identity.destinationKey,
                                cutoffMs = watermark
                            )
                        )
                        inserted = true
                    }
                }
            }
        }

        val committed = prefs.edit()
            .putBoolean(WARNING_KEY, warningCode != null)
            .putBoolean(BOOTSTRAPPED_KEY, true)
            .commit()
        LegacyCutoffBootstrapResult(
            warningCode = warningCode,
            cutoffInserted = inserted,
            markerCommitted = committed
        )
    }

    companion object {
        const val BOOTSTRAPPED_KEY = "autoupload_legacy_cutoff_bootstrapped"
        const val WARNING_KEY = "autoupload_legacy_cutoff_warning"
        const val WARNING_UNSCOPED = "LEGACY_CUTOFF_UNSCOPED"
        const val LEGACY_WATERMARK_KEY = "autoupload_last_ms"
        const val LEGACY_TREE_KEY = "autoupload_tree"
        const val LEGACY_DESTINATION_KEY = "autoupload_dest"
    }
}
