package com.mounvix.app.data.autoupload

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.os.CancellationSignal
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference

/** Accepts only the active Wi-Fi transport and cancels work when it is lost. */
class AutouploadWifiGuard(private val connectivityManager: ConnectivityManager) {

    constructor(context: Context) : this(
        context.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager
            ?: throw IllegalStateException("Connectivity service is unavailable")
    )

    fun isWifiConnected(): Boolean {
        val activeNetwork = connectivityManager.activeNetwork ?: return false
        return hasWifiTransport(activeNetwork)
    }

    /** Registers a default-network callback and cancels [signal] if Wi-Fi ceases to be active. */
    fun monitorDisconnect(signal: CancellationSignal): AutoCloseable {
        val activeWifiNetwork = AtomicReference(
            connectivityManager.activeNetwork?.takeIf(::hasWifiTransport)
        )
        val callback = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                if (hasWifiTransport(network)) {
                    activeWifiNetwork.set(network)
                } else if (connectivityManager.activeNetwork == network || !isWifiConnected()) {
                    activeWifiNetwork.set(null)
                    signal.cancel()
                }
            }

            override fun onCapabilitiesChanged(network: Network, networkCapabilities: NetworkCapabilities) {
                if (networkCapabilities.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)) {
                    activeWifiNetwork.set(network)
                } else {
                    activeWifiNetwork.set(null)
                    signal.cancel()
                }
            }

            override fun onLost(network: Network) {
                if (activeWifiNetwork.get() == network) {
                    activeWifiNetwork.set(null)
                    signal.cancel()
                }
            }
        }

        connectivityManager.registerDefaultNetworkCallback(callback)
        val currentNetwork = connectivityManager.activeNetwork
        if (currentNetwork == null || !hasWifiTransport(currentNetwork)) {
            activeWifiNetwork.set(null)
            signal.cancel()
        } else {
            activeWifiNetwork.set(currentNetwork)
        }

        val closed = AtomicBoolean()
        return AutoCloseable {
            if (closed.compareAndSet(false, true)) {
                connectivityManager.unregisterNetworkCallback(callback)
            }
        }
    }

    private fun hasWifiTransport(network: Network): Boolean =
        connectivityManager.getNetworkCapabilities(network)
            ?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) == true
}
