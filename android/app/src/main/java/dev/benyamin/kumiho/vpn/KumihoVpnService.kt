package dev.benyamin.kumiho.vpn

import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.VpnService
import android.os.Binder
import android.os.Build
import android.os.IBinder
import androidx.core.app.ServiceCompat
import dev.benyamin.kumiho.KumihoApplication
import dev.benyamin.kumiho.engine.EngineClient
import dev.benyamin.kumiho.engine.EnginePaths
import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.engine.GomobileEngineTransport
import dev.benyamin.kumiho.permissions.PermissionController
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking

/**
 * The one VpnService in the process. It owns the engine session and the
 * device bridge; the app-scoped [VpnServiceCoordinator] only binds to it.
 *
 * Lifecycle rules:
 * - `onCreate` starts the engine but never enters the foreground, so setup and
 *   settings can bind without a notification appearing.
 * - Every connect path (including an Always-on restart) calls
 *   `startForeground` before doing any await, as Android requires.
 * - `ACTION_DISCONNECT` removes the foreground notification but leaves the
 *   service alive while the UI is bound; `ACTION_STOP` and a failed connect
 *   stop it. `onTaskRemoved` keeps an active tunnel, otherwise stops.
 * - `onDestroy` closes the engine before the scope is cancelled; the adopted
 *   interface descriptor stays Go's to close (C5).
 */
class KumihoVpnService : VpnService() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val ready = CompletableDeferred<Unit>()

    private val mutableState = MutableStateFlow(ServiceState())

    /** The service's own view of the engine, mirrored to the binder. */
    val state: StateFlow<ServiceState> = mutableState.asStateFlow()

    private lateinit var engine: EngineClient
    private lateinit var bridge: KumihoDeviceBridge
    private lateinit var notifications: NotificationFactory

    private val binder = LocalBinder()

    @Volatile
    private var foreground = false

    @Volatile
    private var bound = false

    /** The local binder: no AIDL, no IPC, same process (C6). */
    inner class LocalBinder : Binder() {
        val engine: EngineClient get() = this@KumihoVpnService.engine
        val state: StateFlow<ServiceState> get() = this@KumihoVpnService.state
    }

    override fun onCreate() {
        super.onCreate()
        notifications = NotificationFactory(this)
        engine = EngineClient(GomobileEngineTransport())
        val split = (application as KumihoApplication).container.splitSettingsProvider
        bridge = KumihoDeviceBridge(this, split)
        refreshOsPolicy()

        // Engine status is advisory; the service mirrors it for the UI.
        scope.launch {
            engine.status.collect { status ->
                mutableState.update { it.copy(status = status) }
            }
        }
        // Keep the foreground notification truthful while it is showing.
        scope.launch {
            mutableState.collect { snapshot ->
                if (foreground) {
                    notifications.update(
                        NotificationFactory.FOREGROUND_ID,
                        VpnStateReducer.reduce(snapshot),
                    )
                }
            }
        }
        scope.launch { startEngine() }
    }

    override fun onBind(intent: Intent): IBinder {
        bound = true
        mutableState.update { it.copy(binding = true) }
        return binder
    }

    override fun onUnbind(intent: Intent): Boolean {
        bound = false
        mutableState.update { it.copy(binding = false) }
        return super.onUnbind(intent)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        refreshOsPolicy()
        if (intent == null) return handleRestart()

        return when (intent.action) {
            ACTION_CONNECT -> {
                beginForeground()
                handleConnect(connectRequestOf(intent))
                // A connect path is sticky even while it is still in flight.
                START_STICKY
            }
            ACTION_DISCONNECT -> {
                handleDisconnect()
                stickyIfActive()
            }
            ACTION_STOP -> {
                handleStop()
                START_NOT_STICKY
            }
            ACTION_REVOKE -> {
                handleRevoke()
                START_NOT_STICKY
            }
            else -> stickyIfActive()
        }
    }

    /** The system revoked the VPN (another app took over, or the user did). */
    override fun onRevoke() {
        handleRevoke()
    }

    override fun onTaskRemoved(rootIntent: Intent?) {
        if (isActive(mutableState.value.status?.state)) {
            // Never tear down a live tunnel just because the task went away.
            super.onTaskRemoved(rootIntent)
            return
        }
        detachForeground()
        notifications.cancelSignIn()
        stopSelf()
        super.onTaskRemoved(rootIntent)
    }

    override fun onDestroy() {
        bound = false
        foreground = false
        // Close the Go session before cancelling the scope: the engine shuts
        // the tunnel down and the adopted fd stays Go's to close.
        try {
            runBlocking { engine.close() }
        } catch (_: Exception) {
            // Shutdown is best-effort; the process is going away.
        }
        scope.cancel()
        if (!ready.isCompleted) ready.cancel()
        super.onDestroy()
    }

    // ---- start ----------------------------------------------------------

    private suspend fun startEngine() {
        try {
            engine.start(
                EnginePaths.forAppDirs(filesDir, noBackupFilesDir, cacheDir),
                bridge,
            )
            ready.complete(Unit)
            mutableState.update { it.copy(engineReady = true, error = null) }
            refreshStatus()
        } catch (cancelled: CancellationException) {
            ready.cancel(cancelled)
            throw cancelled
        } catch (error: Exception) {
            ready.completeExceptionally(error)
            mutableState.update {
                it.copy(engineReady = false, error = messageOf(error))
            }
        }
    }

    // ---- connect / disconnect paths ------------------------------------

    /**
     * Always-on or a sticky restart: decide from the OS policy and the live
     * status. The foreground notification starts before the engine is even
     * ready, so the system never sees a background-only service.
     */
    private fun handleRestart(): Int {
        beginForeground()
        scope.launch {
            if (!awaitReady()) {
                failAndStop()
                return@launch
            }
            val status = refreshStatus() ?: run {
                failAndStop()
                return@launch
            }
            val snapshot = mutableState.value
            val decision = AlwaysOnPolicy.decide(
                alwaysOn = snapshot.alwaysOn,
                authenticated = status.authenticated,
                state = status.state,
                freshStart = true,
            )
            when (decision) {
                AlwaysOnDecision.AUTO_CONNECT -> handleConnect(ConnectRequest())
                AlwaysOnDecision.SIGN_IN_NOTIFICATION -> {
                    notifications.postSignIn()
                    detachForeground()
                    stopSelf()
                }
                AlwaysOnDecision.NONE -> {
                    if (!isActive(status.state)) {
                        detachForeground()
                        stopSelf()
                    }
                }
            }
        }
        return START_STICKY
    }

    private fun handleConnect(request: ConnectRequest) {
        scope.launch {
            if (!awaitReady()) {
                failAndStop()
                return@launch
            }
            val status = refreshStatus() ?: run {
                failAndStop()
                return@launch
            }
            if (!status.authenticated) {
                notifications.postSignIn()
                detachForeground()
                stopSelf()
                return@launch
            }
            // Proxy-only never touches the interface, so it needs no consent.
            if (!request.proxyOnly &&
                PermissionController.vpnPrepareIntent(this@KumihoVpnService) != null
            ) {
                mutableState.update { it.copy(permissionNeeded = true, error = null) }
                detachForeground()
                stopSelf()
                return@launch
            }
            mutableState.update { it.copy(permissionNeeded = false, error = null) }
            try {
                engine.connect(
                    locationCode = request.locationCode,
                    cityCode = request.cityCode,
                    server = request.server,
                    proxyOnly = request.proxyOnly,
                )
                notifications.cancelSignIn()
                mutableState.update { it.copy(error = null, permissionNeeded = false) }
                refreshStatus()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                mutableState.update { it.copy(error = messageOf(error)) }
                detachForeground()
                stopSelf()
            }
        }
    }

    private fun handleDisconnect() {
        scope.launch {
            if (awaitReady()) {
                try {
                    engine.disconnect()
                    refreshStatus()
                    notifications.cancelSignIn()
                } catch (cancelled: CancellationException) {
                    throw cancelled
                } catch (error: Exception) {
                    mutableState.update { it.copy(error = messageOf(error)) }
                }
            }
            detachForeground()
            // While the UI is bound the coordinator still needs the engine for
            // status; only an unbound service actually goes away.
            if (!bound) stopSelf()
        }
    }

    private fun handleStop() {
        scope.launch {
            if (awaitReady()) {
                try {
                    engine.disconnect()
                } catch (cancelled: CancellationException) {
                    throw cancelled
                } catch (_: Exception) {
                    // Stopping is best-effort; the service is going away.
                }
            }
            detachForeground()
            notifications.cancelSignIn()
            stopSelf()
        }
    }

    private fun handleRevoke() {
        scope.launch {
            if (awaitReady()) {
                try {
                    engine.revoke()
                    refreshStatus()
                } catch (cancelled: CancellationException) {
                    throw cancelled
                } catch (error: Exception) {
                    mutableState.update { it.copy(error = messageOf(error)) }
                }
            }
            detachForeground()
            notifications.cancelSignIn()
            stopSelf()
        }
    }

    // ---- helpers --------------------------------------------------------

    private suspend fun awaitReady(): Boolean = try {
        ready.await()
        true
    } catch (cancelled: CancellationException) {
        throw cancelled
    } catch (_: Exception) {
        false
    }

    /** Refreshes status and mirrors it; null on failure (state.error is set). */
    private suspend fun refreshStatus(): EngineStatus? = try {
        engine.refreshStatus()
    } catch (cancelled: CancellationException) {
        throw cancelled
    } catch (error: Exception) {
        mutableState.update { it.copy(error = messageOf(error)) }
        null
    }

    private fun failAndStop() {
        // startEngine already recorded the reason.
        detachForeground()
        stopSelf()
    }

    private fun refreshOsPolicy() {
        mutableState.update {
            it.copy(
                alwaysOn = PermissionController.isAlwaysOn(this),
                lockdown = PermissionController.isLockdownEnabled(this),
            )
        }
    }

    private fun beginForeground(uiState: VpnUiState = VpnUiState.CONNECTING) {
        foreground = true
        notifications.ensureChannel()
        ServiceCompat.startForeground(
            this,
            NotificationFactory.FOREGROUND_ID,
            notifications.build(uiState),
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
                ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE
            } else {
                0
            },
        )
    }

    private fun detachForeground() {
        foreground = false
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
    }

    private fun stickyIfActive(): Int =
        if (isActive(mutableState.value.status?.state)) START_STICKY else START_NOT_STICKY

    private fun isActive(state: String?): Boolean = when (state) {
        EngineStates.CONNECTING,
        EngineStates.CONNECTED,
        EngineStates.RECONNECTING,
        EngineStates.WAITING_NETWORK,
        EngineStates.PROXY_ONLY,
        -> true
        else -> false
    }

    private fun connectRequestOf(intent: Intent): ConnectRequest = ConnectRequest(
        locationCode = intent.getStringExtra(EXTRA_LOCATION_CODE),
        cityCode = intent.getStringExtra(EXTRA_CITY_CODE),
        server = intent.getStringExtra(EXTRA_SERVER),
        proxyOnly = intent.getBooleanExtra(EXTRA_PROXY_ONLY, false),
    )

    companion object {
        const val ACTION_CONNECT = "dev.benyamin.kumiho.action.CONNECT"
        const val ACTION_DISCONNECT = "dev.benyamin.kumiho.action.DISCONNECT"
        const val ACTION_STOP = "dev.benyamin.kumiho.action.STOP"
        const val ACTION_REVOKE = "dev.benyamin.kumiho.action.REVOKE"

        const val EXTRA_LOCATION_CODE = "location_code"
        const val EXTRA_CITY_CODE = "city_code"
        const val EXTRA_SERVER = "server"
        const val EXTRA_PROXY_ONLY = "proxy_only"
    }
}
