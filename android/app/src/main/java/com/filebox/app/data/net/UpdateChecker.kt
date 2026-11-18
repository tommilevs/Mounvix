package com.mounvix.app.data.net

import okhttp3.OkHttpClient
import okhttp3.Request
import java.util.concurrent.TimeUnit

data class UpdateInfo(
    val available: Boolean,
    val tag: String,
    val url: String,
    val apkUrl: String = "",
)

/** Проверка обновлений через GitHub Releases API (без токена, User-Agent обязателен). */
class UpdateChecker(
    private val repo: String = "tommilevs/Mounvix",
    private val client: OkHttpClient = defaultClient(),
    private val baseUrl: String = "https://api.github.com",
) {

    fun check(currentVersion: String): UpdateInfo {
        val req = Request.Builder()
            .url("$baseUrl/repos/$repo/releases/latest")
            .header("Accept", "application/vnd.github+json")
            .header("User-Agent", "Mounvix-Android")
            .build()
        client.newCall(req).execute().use { resp ->
            if (!resp.isSuccessful) throw IllegalStateException("GitHub HTTP ${resp.code}")
            val text = resp.body?.string().orEmpty()
            val tag = regexValue(text, "tag_name")
            val url = regexValue(text, "html_url")
            val latest = tag.trimStart('v', 'V')
            val apkUrl = Regex("\"browser_download_url\"\\s*:\\s*\"([^\"]*\\.apk)\"")
                .find(text)?.groupValues?.get(1) ?: ""
            return UpdateInfo(
                available = compareVersions(latest, currentVersion) > 0,
                tag = tag,
                url = url,
                apkUrl = apkUrl,
            )
        }
    }

    private fun regexValue(json: String, key: String): String =
        Regex("\"$key\"\\s*:\\s*\"([^\"]*)\"").find(json)?.groupValues?.get(1) ?: ""

    companion object {
        /** Сравнение версий вида 1.2.3 / 1.2.3-beta: >0 если a новее b. */
        fun compareVersions(a: String, b: String): Int {
            fun nums(s: String): List<Int> = s.substringBefore('-')
                .split('.')
                .mapNotNull { it.trim().toIntOrNull() }
            val x = nums(a)
            val y = nums(b)
            for (i in 0 until maxOf(x.size, y.size)) {
                val vx = x.getOrElse(i) { 0 }
                val vy = y.getOrElse(i) { 0 }
                if (vx != vy) return vx - vy
            }
            return 0
        }

        private fun defaultClient(): OkHttpClient = OkHttpClient.Builder()
            .connectTimeout(10, TimeUnit.SECONDS)
            .readTimeout(20, TimeUnit.SECONDS)
            .build()
    }
}
