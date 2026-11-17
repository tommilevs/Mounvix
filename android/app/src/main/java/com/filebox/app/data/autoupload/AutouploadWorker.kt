package com.mounvix.app.data.autoupload

import android.content.Context
import android.net.Uri
import androidx.documentfile.provider.DocumentFile
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.File

/**
 * Фоновая автозагрузка фото/видео из выбранной SAF-папки в текущее удалённое хранилище.
 * Прогресс не публикуется; состояние и lastMs пишутся в prefs.
 */
class AutouploadWorker(ctx: Context, params: WorkerParameters) : CoroutineWorker(ctx, params) {

    override suspend fun doWork(): Result = withContext(Dispatchers.IO) {
        val prefs = applicationContext.getSharedPreferences("mounvix_prefs", Context.MODE_PRIVATE)
        if (!prefs.getBoolean("autoupload_enabled", false)) return@withContext Result.success()
        val tree = prefs.getString("autoupload_tree", null) ?: return@withContext Result.success()
        val source = RemoteSourceFactory.fromPrefs(prefs) ?: return@withContext Result.retry()
        val destDir = (prefs.getString("autoupload_dest", "Mounvix") ?: "Mounvix").trim('/').ifEmpty { "Mounvix" }
        val last = prefs.getLong("autoupload_last_ms", 0L)
        val root = DocumentFile.fromTreeUri(applicationContext, Uri.parse(tree)) ?: return@withContext Result.failure()

        val files = ArrayList<AutoFile>()
        collect(root, files, limit = 500)
        val plan = AutouploadPlanner.plan(files, last)
        if (plan.isEmpty()) {
            prefs.edit().putString("autoupload_status", "нет новых файлов").apply()
            return@withContext Result.success()
        }
        if (!source.exists(destDir)) source.createDir(destDir)

        var uploaded = 0
        var maxModified = last
        var errors = 0
        for (f in plan) {
            val tmp = File(applicationContext.cacheDir, "auto-" + System.currentTimeMillis() + "-" + f.name)
            try {
                val input = applicationContext.contentResolver.openInputStream(Uri.parse(f.uri))
                    ?: throw IllegalStateException("open failed")
                input.use { i -> tmp.outputStream().use { o -> i.copyTo(o) } }
                val ok = source.upload(tmp, "$destDir/${f.name}") { _, _ -> }
                if (ok) {
                    uploaded++
                    if (f.modified > maxModified) maxModified = f.modified
                } else {
                    errors++
                }
            } catch (e: Exception) {
                errors++
            } finally {
                tmp.delete()
            }
        }
        prefs.edit()
            .putLong("autoupload_last_ms", maxModified)
            .putString("autoupload_status", "загружено: $uploaded, ошибок: $errors")
            .putLong("autoupload_last_run", System.currentTimeMillis())
            .apply()
        Result.success()
    }

    private fun collect(dir: DocumentFile, out: MutableList<AutoFile>, limit: Int) {
        if (out.size >= limit) return
        for (f in dir.listFiles()) {
            if (out.size >= limit) return
            if (f.isDirectory) {
                collect(f, out, limit)
            } else {
                val name = f.name ?: continue
                if (!AutouploadPlanner.isMedia(name)) continue
                out.add(AutoFile(name, f.length(), f.lastModified(), f.uri.toString()))
            }
        }
    }
}
