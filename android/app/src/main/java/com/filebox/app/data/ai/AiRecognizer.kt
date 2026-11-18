package com.mounvix.app.data.ai

import java.util.Base64
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject
import java.util.concurrent.TimeUnit

/** Клиент OpenAI-совместимого vision API (chat/completions с image_url). */
class AiRecognizer(private val client: OkHttpClient = defaultClient()) {

    fun describe(
        baseUrl: String,
        model: String,
        key: String,
        image: ByteArray,
        mime: String,
        prompt: String = "",
    ): String {
        if (baseUrl.isBlank() || model.isBlank()) throw IllegalArgumentException("AI не настроен")
        val question = prompt.ifBlank { "Опиши изображение кратко: что на нём, текст и ключевые объекты." }
        val dataUrl = "data:" + (mime.ifBlank { "image/jpeg" }) + ";base64," +
            Base64.getEncoder().encodeToString(image)
        val content = JSONArray()
            .put(JSONObject().put("type", "text").put("text", question))
            .put(JSONObject().put("type", "image_url").put("image_url", JSONObject().put("url", dataUrl)))
        val body = JSONObject()
            .put("model", model)
            .put("messages", JSONArray().put(JSONObject().put("role", "user").put("content", content)))
            .toString()
        val url = baseUrl.trimEnd('/') + "/v1/chat/completions"
        val req = Request.Builder()
            .url(url)
            .post(body.toRequestBody("application/json".toMediaType()))
            .apply { if (key.isNotBlank()) header("Authorization", "Bearer $key") }
            .build()
        client.newCall(req).execute().use { resp ->
            val text = resp.body?.string().orEmpty()
            if (!resp.isSuccessful) throw IllegalStateException("AI HTTP ${resp.code}: ${text.take(200)}")
            val choices = JSONObject(text).optJSONArray("choices")
                ?: throw IllegalStateException("AI: нет choices")
            val answer = choices.optJSONObject(0)?.optJSONObject("message")?.optString("content").orEmpty()
            if (answer.isBlank()) throw IllegalStateException("AI: пустой ответ")
            return answer.trim()
        }
    }

    companion object {
        fun defaultClient(): OkHttpClient = OkHttpClient.Builder()
            .connectTimeout(20, TimeUnit.SECONDS)
            .readTimeout(120, TimeUnit.SECONDS)
            .build()
    }
}
