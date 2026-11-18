package com.mounvix.app.data.update

import okhttp3.OkHttpClient
import okhttp3.Request
import java.io.File
import java.util.concurrent.TimeUnit

/** Скачивание APK обновления во временный файл. */
class ApkDownloader(private val client: OkHttpClient = defaultClient()) {

    fun download(url: String, target: File): File {
        if (url.isBlank()) throw IllegalArgumentException("empty url")
        client.newCall(Request.Builder().url(url).build()).execute().use { resp ->
            if (!resp.isSuccessful) throw IllegalStateException("HTTP ${resp.code}")
            val body = resp.body ?: throw IllegalStateException("empty body")
            target.parentFile?.mkdirs()
            body.byteStream().use { input ->
                target.outputStream().use { output -> input.copyTo(output) }
            }
        }
        if (target.length() <= 0) throw IllegalStateException("empty apk")
        return target
    }

    companion object {
        fun defaultClient(): OkHttpClient = OkHttpClient.Builder()
            .connectTimeout(20, TimeUnit.SECONDS)
            .readTimeout(120, TimeUnit.SECONDS)
            .build()
    }
}
