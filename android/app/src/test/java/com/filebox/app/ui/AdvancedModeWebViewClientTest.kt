package com.mounvix.app.ui

import android.webkit.RenderProcessGoneDetail
import android.webkit.SslErrorHandler
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebView
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.mockito.Mockito.mock
import org.mockito.Mockito.never
import org.mockito.Mockito.verify
import org.mockito.Mockito.`when`
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [33], application = android.app.Application::class)
class AdvancedModeWebViewClientTest {
    @Test
    fun mainFrameLoadFailuresAreReportedButSubresourcesAreIgnored() {
        val request = mock(WebResourceRequest::class.java)
        val failure = mock(WebResourceError::class.java)
        `when`(request.isForMainFrame).thenReturn(true)
        `when`(failure.description).thenReturn("offline")
        val failures = mutableListOf<String>()
        val client = AdvancedModeWebViewClient(
            baseUrl = "https://nas.example/mounvix",
            onOpenExternal = {},
            onLoadError = { failures += it },
        )

        client.onReceivedError(null, request, failure)
        `when`(request.isForMainFrame).thenReturn(false)
        client.onReceivedError(null, request, failure)

        assertEquals(listOf("offline"), failures)
    }

    @Test
    fun successfulInternalPageCompletionReportsCurrentUrlForRestoration() {
        val visited = mutableListOf<String>()
        val baseUrl = "https://nas.example/mounvix"
        val pageUrl = "$baseUrl/backup"
        val webView = mock(WebView::class.java)
        val client = AdvancedModeWebViewClient(
            baseUrl = baseUrl,
            onOpenExternal = {},
            onMainFrameNavigation = { visited += it },
        )

        client.onPageFinished(webView, pageUrl)
        client.onPageFinished(null, "https://other.example/steal")

        assertEquals(listOf(pageUrl), visited)
        verify(webView).setTag("$baseUrl\u0000$pageUrl")
    }

    @Test
    fun rendererCrashIsReportedAndHandled() {
        val detail = mock(RenderProcessGoneDetail::class.java)
        val crashes = mutableListOf<Boolean>()
        val client = AdvancedModeWebViewClient(
            baseUrl = "https://nas.example/mounvix",
            onOpenExternal = {},
            onRendererGone = { crashes += true },
        )

        val handled = client.onRenderProcessGone(mock(WebView::class.java), detail)

        assertTrue(handled)
        assertEquals(listOf(true), crashes)
    }

    @Test
    fun tlsValidationErrorsAreCancelledWithoutProceeding() {
        val handler = mock(SslErrorHandler::class.java)
        val client = AdvancedModeWebViewClient(
            baseUrl = "https://nas.example/mounvix",
            onOpenExternal = {},
        )

        client.onReceivedSslError(null, handler, null)

        verify(handler).cancel()
        verify(handler, never()).proceed()
    }
}
