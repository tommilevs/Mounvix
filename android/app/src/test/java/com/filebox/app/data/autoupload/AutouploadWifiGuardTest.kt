package com.mounvix.app.data.autoupload

import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.os.CancellationSignal
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.mockito.ArgumentCaptor
import org.mockito.Mockito
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [33])
class AutouploadWifiGuardTest {

    @Test
    fun onlyWifiTransportIsAcceptedIncludingUnvalidatedWifi() {
        val network = Mockito.mock(Network::class.java)

        assertTrue(AutouploadWifiGuard(managerWith(network, capabilities(NetworkCapabilities.TRANSPORT_WIFI)))
            .isWifiConnected())
        assertFalse(AutouploadWifiGuard(managerWith(network, capabilities(NetworkCapabilities.TRANSPORT_CELLULAR)))
            .isWifiConnected())
        assertFalse(AutouploadWifiGuard(managerWith(network, capabilities(NetworkCapabilities.TRANSPORT_ETHERNET)))
            .isWifiConnected())
        assertFalse(AutouploadWifiGuard(managerWith(null, null)).isWifiConnected())
    }

    @Test
    fun monitorCancelsOnWifiLossAndUnregistersCallback() {
        val network = Mockito.mock(Network::class.java)
        val manager = managerWith(network, capabilities(NetworkCapabilities.TRANSPORT_WIFI))
        val signal = CancellationSignal()
        val registration = AutouploadWifiGuard(manager).monitorDisconnect(signal)
        val callback = ArgumentCaptor.forClass(ConnectivityManager.NetworkCallback::class.java)
        Mockito.verify(manager).registerDefaultNetworkCallback(callback.capture())

        callback.value.onLost(network)

        assertTrue(signal.isCanceled)
        registration.close()
        Mockito.verify(manager).unregisterNetworkCallback(callback.value)
    }

    @Test
    fun monitorCancelsWhenDefaultNetworkChangesAwayFromWifi() {
        val network = Mockito.mock(Network::class.java)
        val manager = managerWith(network, capabilities(NetworkCapabilities.TRANSPORT_WIFI))
        val signal = CancellationSignal()
        val registration = AutouploadWifiGuard(manager).monitorDisconnect(signal)
        val callback = ArgumentCaptor.forClass(ConnectivityManager.NetworkCallback::class.java)
        Mockito.verify(manager).registerDefaultNetworkCallback(callback.capture())

        callback.value.onCapabilitiesChanged(network, capabilities(NetworkCapabilities.TRANSPORT_CELLULAR))

        assertTrue(signal.isCanceled)
        registration.close()
    }

    private fun managerWith(network: Network?, networkCapabilities: NetworkCapabilities?): ConnectivityManager {
        val manager = Mockito.mock(ConnectivityManager::class.java)
        Mockito.`when`(manager.activeNetwork).thenReturn(network)
        if (network != null) {
            Mockito.`when`(manager.getNetworkCapabilities(network)).thenReturn(networkCapabilities)
        }
        return manager
    }

    private fun capabilities(transport: Int): NetworkCapabilities = Mockito.mock(NetworkCapabilities::class.java).apply {
        Mockito.`when`(hasTransport(transport)).thenReturn(true)
    }
}
