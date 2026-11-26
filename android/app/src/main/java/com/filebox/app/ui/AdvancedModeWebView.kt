package com.mounvix.app.ui

import android.annotation.SuppressLint
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.net.http.SslError
import android.webkit.CookieManager
import android.webkit.SslErrorHandler
import android.webkit.RenderProcessGoneDetail
import android.webkit.WebResourceError
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver

/** Invoked with an ACTION_OPEN_DOCUMENT intent and the selected URI, or null on cancel. */
typealias AdvancedModeDocumentPicker = (Intent, (Uri?) -> Unit) -> Unit

/** Compose host for a same-origin WebView. Native credentials are never passed to web content. */
@Composable
fun AdvancedModeWebView(
    baseUrl: String,
    targetUrl: String,
    onOpenExternal: (String) -> Unit,
    onChooseDocument: AdvancedModeDocumentPicker,
    onWebViewChanged: (WebView?) -> Unit,
    modifier: Modifier = Modifier,
    onLoadError: (String) -> Unit = {},
    onLoadFinished: () -> Unit = {},
    onRendererGone: () -> Unit = {},
    onMainFrameNavigation: (String) -> Unit = {},
) {
    val context = LocalContext.current
    val lifecycleOwner = LocalLifecycleOwner.current
    val currentExternalHandler = rememberUpdatedState(onOpenExternal)
    val currentDocumentPicker = rememberUpdatedState(onChooseDocument)
    val currentWebViewHandler = rememberUpdatedState(onWebViewChanged)
    val currentLoadErrorHandler = rememberUpdatedState(onLoadError)
    val currentLoadFinishedHandler = rememberUpdatedState(onLoadFinished)
    val currentRendererGoneHandler = rememberUpdatedState(onRendererGone)
    val currentMainFrameNavigation = rememberUpdatedState(onMainFrameNavigation)
    val webViewState = remember { mutableStateOf<WebView?>(null) }
    val desiredLoadKey = "$baseUrl\u0000$targetUrl"

    key(baseUrl) {
        AndroidView(
            modifier = modifier,
            factory = {
                AdvancedModeWebViewFactory.create(
                    context = context,
                    baseUrl = baseUrl,
                    onOpenExternal = { currentExternalHandler.value(it) },
                    onChooseDocument = { intent, complete -> currentDocumentPicker.value(intent, complete) },
                    onLoadError = { currentLoadErrorHandler.value(it) },
                    onLoadFinished = { currentLoadFinishedHandler.value() },
                    onRendererGone = { currentRendererGoneHandler.value() },
                    onMainFrameNavigation = { currentMainFrameNavigation.value(it) },
                ).also { webView ->
                    webViewState.value = webView
                    currentWebViewHandler.value(webView)
                    if (AdvancedModeWebViewFactory.loadTrustedUrl(webView, baseUrl, targetUrl)) {
                        webView.tag = desiredLoadKey
                    }
                }
            },
            update = { webView ->
                if (webView.tag != desiredLoadKey &&
                    AdvancedModeWebViewFactory.loadTrustedUrl(webView, baseUrl, targetUrl)
                ) {
                    webView.tag = desiredLoadKey
                }
            },
        )
    }

    // Keep the effect key and captured WebView from the same composition snapshot.
    val webViewForEffect = webViewState.value
    DisposableEffect(webViewForEffect, lifecycleOwner) {
        val webView = webViewForEffect
        if (webView == null) {
            onDispose { }
        } else {
            val observer = LifecycleEventObserver { _, event ->
                when (event) {
                    Lifecycle.Event.ON_RESUME -> webView.onResume()
                    Lifecycle.Event.ON_PAUSE -> webView.onPause()
                    else -> Unit
                }
            }
            lifecycleOwner.lifecycle.addObserver(observer)
            onDispose {
                lifecycleOwner.lifecycle.removeObserver(observer)
                AdvancedModeWebViewFactory.release(webView)
                currentWebViewHandler.value(null)
                if (webViewState.value === webView) webViewState.value = null
            }
        }
    }
}

internal object AdvancedModeWebViewFactory {
    internal const val WEB_CONTENTS_DEBUGGING_ENABLED = false

    @Suppress("DEPRECATION")
    fun create(
        context: Context,
        baseUrl: String,
        onOpenExternal: (String) -> Unit,
        onChooseDocument: AdvancedModeDocumentPicker,
        onLoadError: (String) -> Unit = {},
        onLoadFinished: () -> Unit = {},
        onRendererGone: () -> Unit = {},
        onMainFrameNavigation: (String) -> Unit = {},
    ): WebView {
        WebView.setWebContentsDebuggingEnabled(WEB_CONTENTS_DEBUGGING_ENABLED)
        return WebView(context).apply {
            settings.apply {
                javaScriptEnabled = false
                domStorageEnabled = true
                allowFileAccess = false
                allowContentAccess = false
                allowFileAccessFromFileURLs = false
                allowUniversalAccessFromFileURLs = false
                mixedContentMode = WebSettings.MIXED_CONTENT_NEVER_ALLOW
                safeBrowsingEnabled = true
                javaScriptCanOpenWindowsAutomatically = false
                setSupportMultipleWindows(false)
                mediaPlaybackRequiresUserGesture = true
            }
            CookieManager.getInstance().setAcceptThirdPartyCookies(this, false)
            webViewClient = AdvancedModeWebViewClient(
                baseUrl,
                onOpenExternal,
                onLoadError,
                onLoadFinished,
                onRendererGone,
                onMainFrameNavigation,
            )
            webChromeClient = AdvancedModeWebChromeClient(onChooseDocument)
        }
    }

    /** JavaScript becomes available only after the initial URL passes origin/path policy; no bridge or credentials are injected. */
    @SuppressLint("SetJavaScriptEnabled")
    fun loadTrustedUrl(webView: WebView, baseUrl: String, targetUrl: String): Boolean {
        if (AdvancedModeUrlPolicy.decide(baseUrl, targetUrl, isRedirect = false, hasUserGesture = false) !=
            NavigationDecision.ALLOW_INTERNAL
        ) {
            return false
        }
        webView.settings.javaScriptEnabled = true
        webView.loadUrl(targetUrl)
        return true
    }

    fun release(webView: WebView) {
        (webView.webChromeClient as? AdvancedModeWebChromeClient)?.cancelPendingChooser()
        webView.onPause()
        webView.stopLoading()
        webView.removeAllViews()
        webView.destroy()
    }
}

internal class AdvancedModeWebViewClient(
    private val baseUrl: String,
    private val onOpenExternal: (String) -> Unit,
    private val onLoadError: (String) -> Unit = {},
    private val onLoadFinished: () -> Unit = {},
    private val onRendererGone: () -> Unit = {},
    private val onMainFrameNavigation: (String) -> Unit = {},
) : WebViewClient() {
    private var mainFrameHttpError: Int? = null

    override fun shouldOverrideUrlLoading(view: WebView?, request: WebResourceRequest?): Boolean {
        if (request == null) return true
        val url = request.url.toString()
        return when (
            AdvancedModeUrlPolicy.decide(
                baseUrl = baseUrl,
                targetUrl = url,
                isRedirect = request.isRedirect,
                hasUserGesture = request.hasGesture(),
            )
        ) {
            NavigationDecision.ALLOW_INTERNAL -> false
            NavigationDecision.OPEN_EXTERNAL -> {
                onOpenExternal(url)
                true
            }
            NavigationDecision.BLOCK -> true
        }
    }

    override fun onPageStarted(view: WebView?, url: String?, favicon: android.graphics.Bitmap?) {
        mainFrameHttpError = null
    }

    override fun onPageFinished(view: WebView?, url: String?) {
        if (url != null && mainFrameHttpError == null &&
            AdvancedModeUrlPolicy.decide(baseUrl, url, isRedirect = false, hasUserGesture = false) ==
            NavigationDecision.ALLOW_INTERNAL
        ) {
            view?.setTag("$baseUrl\u0000$url")
            onMainFrameNavigation(url)
            onLoadFinished()
        }
    }

    override fun onReceivedError(
        view: WebView?,
        request: WebResourceRequest?,
        error: WebResourceError?,
    ) {
        if (request?.isForMainFrame == true) {
            onLoadError(error?.description?.toString()?.takeIf(String::isNotBlank) ?: "Не удалось загрузить страницу")
        }
    }

    override fun onReceivedHttpError(
        view: WebView?,
        request: WebResourceRequest?,
        errorResponse: android.webkit.WebResourceResponse?,
    ) {
        if (request?.isForMainFrame == true) {
            val statusCode = errorResponse?.statusCode
            mainFrameHttpError = statusCode
            onLoadError(if (statusCode == null) "Ошибка HTTP сервера" else "Ошибка HTTP $statusCode")
        }
    }

    override fun onRenderProcessGone(view: WebView?, detail: RenderProcessGoneDetail?): Boolean {
        onRendererGone()
        return true
    }

    override fun onReceivedSslError(view: WebView?, handler: SslErrorHandler?, error: SslError?) {
        handler?.cancel()
        onLoadError("Сертификат сервера не прошёл проверку")
    }
}

internal class AdvancedModeWebChromeClient(
    private val onChooseDocument: AdvancedModeDocumentPicker,
) : WebChromeClient() {
    private var pendingFileCallback: ValueCallback<Array<Uri>>? = null

    override fun onShowFileChooser(
        webView: WebView?,
        filePathCallback: ValueCallback<Array<Uri>>?,
        fileChooserParams: FileChooserParams?,
    ): Boolean {
        val callback = filePathCallback ?: return false
        cancelPendingChooser()
        pendingFileCallback = callback
        val pickerIntent = Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
            addCategory(Intent.CATEGORY_OPENABLE)
            type = "*/*"
            putExtra(Intent.EXTRA_ALLOW_MULTIPLE, false)
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        }
        onChooseDocument(pickerIntent) completion@{ selectedUri ->
            if (pendingFileCallback !== callback) return@completion
            pendingFileCallback = null
            val safeUri = selectedUri?.takeIf { it.scheme == "content" && !it.authority.isNullOrBlank() }
            callback.onReceiveValue(safeUri?.let { arrayOf(it) })
        }
        return true
    }

    override fun onCreateWindow(
        view: WebView?,
        isDialog: Boolean,
        isUserGesture: Boolean,
        resultMsg: android.os.Message?,
    ): Boolean = false

    fun cancelPendingChooser() {
        pendingFileCallback?.onReceiveValue(null)
        pendingFileCallback = null
    }
}
