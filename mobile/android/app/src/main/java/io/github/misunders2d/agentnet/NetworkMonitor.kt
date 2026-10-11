package io.github.misunders2d.agentnet

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.os.Handler

/** Default-route identity only: capabilities chatter never reconnects the app. */
internal class NetworkRoute {
    private var current: Long? = null
    fun available(handle: Long): Boolean {
        if (current == handle) return false
        current = handle
        return true
    }
    fun lost(handle: Long): Boolean {
        if (current != handle) return false
        current = null
        return true
    }
}

internal class NetworkMonitor(context: Context, handler: Handler, changed: () -> Unit) {
    private val route = NetworkRoute()
    private val callback = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) { if (route.available(network.networkHandle)) changed() }
        override fun onLost(network: Network) { if (route.lost(network.networkHandle)) changed() }
    }
    init {
        // Application-owned callback lasts exactly as long as the embedded host.
        context.getSystemService(ConnectivityManager::class.java).registerDefaultNetworkCallback(callback, handler)
    }
}
