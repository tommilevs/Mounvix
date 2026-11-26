package com.mounvix.app.ui

import android.view.View
import android.view.ViewGroup
import android.webkit.WebView
import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.toPixelMap
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onRoot
import androidx.test.ext.junit.runners.AndroidJUnit4
import com.mounvix.app.data.feed.FeedProfile
import com.mounvix.app.ui.theme.MounvixTheme
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import java.util.concurrent.TimeUnit

private fun findWebView(view: View): WebView? {
    if (view is WebView) return view
    if (view !is ViewGroup) return null
    for (index in 0 until view.childCount) {
        findWebView(view.getChildAt(index))?.let { return it }
    }
    return null
}

@RunWith(AndroidJUnit4::class)
class AdvancedModeScreenTest {
    @get:Rule
    val composeRule = createAndroidComposeRule<ComponentActivity>()

    @Test
    fun advancedModeWebViewRequestsTrustedUrl() {
        val server = MockWebServer()
        server.start()
        val marker = "MOUNVIX_WEBVIEW_QA"
        server.enqueue(
            MockResponse()
                .setHeader("Content-Type", "text/html; charset=utf-8")
                .setBody("<!doctype html><html><body><h1>$marker</h1></body></html>"),
        )

        try {
            val baseUrl = server.url("/mounvix/").toString().trimEnd('/')
            composeRule.setContent {
                AdvancedModeWebView(
                    baseUrl = baseUrl,
                    targetUrl = baseUrl,
                    onOpenExternal = {},
                    onChooseDocument = { _, complete -> complete(null) },
                    onWebViewChanged = {},
                    modifier = Modifier.fillMaxSize(),
                )
            }

            val request = server.takeRequest(10, TimeUnit.SECONDS)
            org.junit.Assert.assertNotNull("AdvancedModeWebView should request the trusted URL", request)
            org.junit.Assert.assertEquals("/mounvix", request?.path)
        } finally {
            server.shutdown()
        }
    }

    @Test
    fun screenRequestsTrustedSameOriginPage() {
        val server = MockWebServer()
        server.start()
        val marker = "MOUNVIX_WEBVIEW_QA"
        server.enqueue(
            MockResponse()
                .setHeader("Content-Type", "text/html; charset=utf-8")
                .setBody("<!doctype html><html><body><h1>$marker</h1></body></html>"),
        )

        try {
            val baseUrl = server.url("/mounvix/").toString().trimEnd('/')
            org.junit.Assert.assertEquals(
                NavigationDecision.ALLOW_INTERNAL,
                AdvancedModeUrlPolicy.decide(baseUrl, baseUrl, isRedirect = false, hasUserGesture = false),
            )
            val profile = FeedProfile(baseUrl = baseUrl, username = "qa", password = "not-used-by-webview")
            composeRule.setContent {
                MounvixTheme {
                    AdvancedModeScreen(profile = profile, onExit = {})
                }
            }

            composeRule.waitUntil(5_000) {
                findWebView(composeRule.activity.window.decorView) != null
            }
            val webView = findWebView(composeRule.activity.window.decorView)
            org.junit.Assert.assertNotNull("Advanced Mode should keep a WebView in its Activity view tree", webView)
            org.junit.Assert.assertTrue(
                "Advanced Mode WebView must have a positive measured area: ${webView?.width}x${webView?.height}",
                (webView?.width ?: 0) > 0 && (webView?.height ?: 0) > 0,
            )
            var loadedUrl: String? = null
            composeRule.runOnIdle { loadedUrl = webView?.url }
            org.junit.Assert.assertEquals("WebView current URL should match the trusted target", baseUrl, loadedUrl)

            val request = server.takeRequest(10, TimeUnit.SECONDS)
            org.junit.Assert.assertNotNull("Advanced Mode should request the trusted same-origin page", request)
            org.junit.Assert.assertEquals("/mounvix", request?.path)
        } finally {
            server.shutdown()
        }
    }

    @Test
    fun httpWarningAndCredentialNoticeRemainVisibleAlongsideWebView() {
        val profile = FeedProfile(
            baseUrl = "http://127.0.0.1:1/mounvix/",
            username = "qa",
            password = "not-used-by-webview",
        )

        composeRule.setContent {
            MounvixTheme {
                AdvancedModeScreen(profile = profile, onExit = {})
            }
        }
        composeRule.waitForIdle()

        val warningText =
            "Незашифрованное HTTP-соединение: пароль и сессия передаются без шифрования. Не используйте в публичной сети."
        val warningNode = composeRule.onNodeWithText(warningText)
        warningNode.assertIsDisplayed()
        composeRule.onNodeWithText(
            "Панель сервера Mounvix. Войдите на странице сервера; пароль нативного профиля сюда не передаётся.",
        ).assertIsDisplayed()

        val bounds = warningNode.fetchSemanticsNode().boundsInRoot
        val pixels = composeRule.onRoot().captureToImage().toPixelMap()
        val left = bounds.left.toInt().coerceIn(0, pixels.width - 1)
        val right = bounds.right.toInt().coerceIn(left, pixels.width - 1)
        val top = bounds.top.toInt().coerceIn(0, pixels.height - 1)
        val bottom = bounds.bottom.toInt().coerceIn(top, pixels.height - 1)
        val visibleInkPixels = (top..bottom).sumOf { y ->
            (left..right).count { x ->
                val color = pixels[x, y]
                color.alpha > 0.5f && color.red + color.green + color.blue < 2.4f
            }
        }
        org.junit.Assert.assertTrue(
            "HTTP warning must render visible ink inside its bounds: count=$visibleInkPixels, bounds=$bounds, image=${pixels.width}x${pixels.height}",
            visibleInkPixels > 5,
        )
    }
}
