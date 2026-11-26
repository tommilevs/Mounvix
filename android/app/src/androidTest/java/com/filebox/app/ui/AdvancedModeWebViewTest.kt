package com.mounvix.app.ui

import android.content.Intent
import android.net.Uri
import android.webkit.CookieManager
import android.webkit.ValueCallback
import android.webkit.WebResourceRequest
import android.webkit.WebView
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class AdvancedModeWebViewTest {
    private val baseUrl = "https://nas.example:8443/mounvix"

    @Test
    fun secureSettingsAreAppliedAndJavascriptStartsDisabledUntilTrustedLoad() {
        val observation = onMain {
            val webView = createWebView()
            val initialJavascript = webView.settings.javaScriptEnabled
            val loaded = AdvancedModeWebViewFactory.loadTrustedUrl(
                webView,
                baseUrl,
                "$baseUrl/dashboard",
            )
            val settings = webView.settings
            val result = listOf(
                initialJavascript,
                loaded,
                settings.javaScriptEnabled,
                settings.allowFileAccess,
                settings.allowContentAccess,
                settings.allowFileAccessFromFileURLs,
                settings.allowUniversalAccessFromFileURLs,
                settings.mixedContentMode == android.webkit.WebSettings.MIXED_CONTENT_NEVER_ALLOW,
                settings.safeBrowsingEnabled,
                settings.javaScriptCanOpenWindowsAutomatically,
                settings.supportMultipleWindows(),
                CookieManager.getInstance().acceptThirdPartyCookies(webView),
                AdvancedModeWebViewFactory.WEB_CONTENTS_DEBUGGING_ENABLED,
            )
            AdvancedModeWebViewFactory.release(webView)
            result
        }

        assertFalse(observation[0])
        assertTrue(observation[1])
        assertTrue(observation[2])
        assertFalse(observation[3])
        assertFalse(observation[4])
        assertFalse(observation[5])
        assertFalse(observation[6])
        assertTrue(observation[7])
        assertTrue(observation[8])
        assertFalse(observation[9])
        assertFalse(observation[10])
        assertFalse(observation[11])
        assertFalse(observation[12])
    }

    @Test
    fun rejectedInitialUrlNeverEnablesJavascript() {
        val observation = onMain {
            val webView = createWebView()
            val accepted = AdvancedModeWebViewFactory.loadTrustedUrl(
                webView,
                baseUrl,
                "https://attacker.example/panel",
            )
            val enabled = webView.settings.javaScriptEnabled
            AdvancedModeWebViewFactory.release(webView)
            accepted to enabled
        }

        assertFalse(observation.first)
        assertFalse(observation.second)
    }

    @Test
    fun chooserReturnsOnlyExplicitContentUrisAndDisposalCancelsPendingCallback() {
        val observation = onMain {
            var pickerIntent: Intent? = null
            var completePicker: ((Uri?) -> Unit)? = null
            val webView = createWebView(onChooseDocument = { intent, complete ->
                pickerIntent = intent
                completePicker = complete
            })
            val client = webView.webChromeClient as AdvancedModeWebChromeClient
            val delivered = mutableListOf<List<Uri>?>()
            fun requestChooser() = client.onShowFileChooser(
                webView,
                ValueCallback { value -> delivered += value?.toList() },
                null,
            )

            assertTrue(requestChooser())
            val action = pickerIntent?.action
            val category = pickerIntent?.categories?.contains(Intent.CATEGORY_OPENABLE) == true
            completePicker?.invoke(Uri.parse("file:///private/file.txt"))
            val rejectedFile = delivered.last()

            assertTrue(requestChooser())
            completePicker?.invoke(Uri.parse("content:private/document/7"))
            val rejectedContentWithoutAuthority = delivered.last()

            assertTrue(requestChooser())
            completePicker?.invoke(Uri.parse("content://provider/document/7"))
            val acceptedContent = delivered.last()

            val pendingDelivered = mutableListOf<List<Uri>?>()
            client.onShowFileChooser(
                webView,
                ValueCallback { value -> pendingDelivered += value?.toList() },
                null,
            )
            AdvancedModeWebViewFactory.release(webView)
            val pendingCancelled = pendingDelivered.size == 1 && pendingDelivered.single() == null

            listOf(action == Intent.ACTION_OPEN_DOCUMENT, category, rejectedFile == null,
                rejectedContentWithoutAuthority == null,
                acceptedContent?.singleOrNull()?.scheme == "content", pendingCancelled)
        }

        assertTrue(observation.all { it })
    }

    @Test
    fun externalUserGestureOpensBrowserButRedirectDoesNot() {
        val externalUrls = mutableListOf<String>()
        val results = onMain {
            val webView = createWebView(onOpenExternal = { externalUrls += it })
            val client = webView.webViewClient as AdvancedModeWebViewClient
            val external = request("https://example.org/help", redirect = false, gesture = true)
            val redirect = request("https://attacker.example/redirect", redirect = true, gesture = true)
            val externalConsumed = client.shouldOverrideUrlLoading(webView, external)
            val redirectConsumed = client.shouldOverrideUrlLoading(webView, redirect)
            AdvancedModeWebViewFactory.release(webView)
            externalConsumed to redirectConsumed
        }

        assertTrue(results.first)
        assertTrue(results.second)
        assertEquals(listOf("https://example.org/help"), externalUrls)
    }

    private fun createWebView(
        onChooseDocument: (Intent, (Uri?) -> Unit) -> Unit = { _, complete -> complete(null) },
        onOpenExternal: (String) -> Unit = {},
    ): WebView = AdvancedModeWebViewFactory.create(
        context = InstrumentationRegistry.getInstrumentation().targetContext,
        baseUrl = baseUrl,
        onOpenExternal = onOpenExternal,
        onChooseDocument = onChooseDocument,
    )

    private fun request(url: String, redirect: Boolean, gesture: Boolean): WebResourceRequest =
        object : WebResourceRequest {
            override fun getUrl(): Uri = Uri.parse(url)
            override fun isForMainFrame(): Boolean = true
            override fun isRedirect(): Boolean = redirect
            override fun hasGesture(): Boolean = gesture
            override fun getMethod(): String = "GET"
            override fun getRequestHeaders(): Map<String, String> = emptyMap()
        }

    private fun <T> onMain(block: () -> T): T {
        val result = AtomicReference<T>()
        InstrumentationRegistry.getInstrumentation().runOnMainSync { result.set(block()) }
        return result.get()
    }

}
