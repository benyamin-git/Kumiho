package dev.benyamin.kumiho.vpn

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.os.IBinder
import androidx.core.content.ContextCompat
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.engine.EngineClient
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/**
 * The app-scoped binding to [KumihoVpnService] (C6).
 *
 * It is created once by [dev.benyamin.kumiho.AppContainer], uses the
 * application context, and never unbinds: an activity recreation neither drops
 * the engine nor re-posts anything. Binding alone creates the service (and its
 * engine), but the service does not enter the foreground until a connect.
 *
 * Commands are one-way intents; the source of truth is the binder's
 * [ServiceState] flow, mirrored here with the binding flag overlaid.
 */
class VpnServiceCoordinator(context: Context) : ServiceConnection {
    private val appContext = context.applicationContext
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

    private val mutableState = MutableStateFlow(ServiceState())

    /** The latest service snapshot; starts as unbound. */
    val state: StateFlow<ServiceState> = mutableState.asStateFlow()

    private val mutableEngine = MutableStateFlow<EngineClient?>(null)

    /** The bound client, or null while the service is unavailable. */
    val engine: StateFlow<EngineClient?> = mutableEngine.asStateFlow()

    private val bindLock = Any()
    private var pendingEngine = CompletableDeferred<EngineClient>()
    private var stateJob: Job? = null
    private var binder: KumihoVpnService.LocalBinder? = null

    init {
        val intent = Intent(appContext, KumihoVpnService::class.java)
        val bound = try {
            appContext.bindService(intent, this, Context.BIND_AUTO_CREATE)
        } catch (error: RuntimeException) {
            mutableState.update { it.copy(error = messageOf(error)) }
            false
        }
        if (!bound) {
            loseBinding(appContext.getString(R.string.vpn_error_service_unavailable))
        }
    }

    // ---- commands -------------------------------------------------------

    /** Request a connect; the service enters the foreground synchronously. */
    fun connect(request: ConnectRequest) {
        val intent = serviceIntent(KumihoVpnService.ACTION_CONNECT)
            .putExtra(KumihoVpnService.EXTRA_PROXY_ONLY, request.proxyOnly)
        request.locationCode?.let { intent.putExtra(KumihoVpnService.EXTRA_LOCATION_CODE, it) }
        request.cityCode?.let { intent.putExtra(KumihoVpnService.EXTRA_CITY_CODE, it) }
        request.server?.let { intent.putExtra(KumihoVpnService.EXTRA_SERVER, it) }
        try {
            ContextCompat.startForegroundService(appContext, intent)
        } catch (error: RuntimeException) {
            mutableState.update { it.copy(error = messageOf(error)) }
        }
    }

    /** Disconnect the tunnel, removing the foreground notification. */
    fun disconnect() = send(KumihoVpnService.ACTION_DISCONNECT)

    /** Disconnect and stop the started service (the binding is kept). */
    fun stop() = send(KumihoVpnService.ACTION_STOP)

    /**
     * Asks the service to re-read Android's Always-on/lockdown flags (R16) and
     * returns the fresh snapshot, or null while the service is not bound.
     *
     * The setup screen calls this on resume: the flags live on the VpnService
     * instance, so only the bound service can read what the user changed in
     * Android's VPN settings. Nothing about the connection changes.
     */
    fun refreshPolicy(): ServiceState? {
        val binder = this.binder ?: return null
        return try {
            binder.refreshPolicy()
        } catch (_: RuntimeException) {
            null
        }
    }

    private fun send(action: String) {
        try {
            appContext.startService(serviceIntent(action))
        } catch (error: RuntimeException) {
            mutableState.update { it.copy(error = messageOf(error)) }
        }
    }

    private fun serviceIntent(action: String): Intent =
        Intent(appContext, KumihoVpnService::class.java).setAction(action)

    /**
     * Suspends until the service is bound and the engine has started.
     *
     * Throws [IllegalStateException] when the start failed, the binding died,
     * or the service is otherwise unusable. Callers that only need to observe
     * state should collect [state] instead.
     */
    suspend fun awaitEngine(): EngineClient {
        val client = synchronized(bindLock) { pendingEngine }.await()
        val snapshot = state.first { it.engineReady || it.error != null || !it.binding }
        if (!snapshot.engineReady) {
            throw IllegalStateException(
                snapshot.error ?: appContext.getString(R.string.vpn_error_service_unavailable),
            )
        }
        return client
    }

    // ---- ServiceConnection ---------------------------------------------

    override fun onServiceConnected(name: ComponentName?, service: IBinder?) {
        val binder = service as? KumihoVpnService.LocalBinder
        if (binder == null) {
            loseBinding(appContext.getString(R.string.vpn_error_service_unavailable))
            return
        }
        val client = binder.engine
        this.binder = binder
        mutableEngine.value = client
        mutableState.update { it.copy(binding = true, error = null) }
        synchronized(bindLock) {
            // A fresh, already-completed handle for this binding; callers that
            // waited through a previous failure see a healthy client again.
            pendingEngine = CompletableDeferred<EngineClient>().also { it.complete(client) }
        }
        stateJob?.cancel()
        stateJob = scope.launch {
            binder.state.collect { snapshot ->
                mutableState.value = snapshot.copy(binding = true)
            }
        }
    }

    override fun onServiceDisconnected(name: ComponentName?) {
        loseBinding(appContext.getString(R.string.vpn_error_service_unavailable))
    }

    override fun onNullBinding(name: ComponentName?) {
        loseBinding(appContext.getString(R.string.vpn_error_service_unavailable))
    }

    override fun onBindingDied(name: ComponentName?) {
        loseBinding(appContext.getString(R.string.vpn_error_service_unavailable))
    }

    /**
     * Clears the client and fails any pending [awaitEngine]. Future awaits
     * fail immediately until a fresh bind succeeds, so a dead binding cannot
     * hang a setup screen.
     */
    private fun loseBinding(message: String) {
        stateJob?.cancel()
        stateJob = null
        binder = null
        mutableEngine.value = null
        mutableState.update {
            it.copy(binding = false, engineReady = false, status = null)
        }
        val failure = IllegalStateException(message)
        val failed = synchronized(bindLock) {
            val current = pendingEngine
            pendingEngine = CompletableDeferred<EngineClient>().also {
                it.completeExceptionally(failure)
            }
            current
        }
        if (!failed.isCompleted) {
            failed.completeExceptionally(failure)
        }
    }
}
