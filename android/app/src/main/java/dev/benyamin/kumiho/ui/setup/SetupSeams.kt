package dev.benyamin.kumiho.ui.setup

import android.content.Context
import android.os.Build
import androidx.core.app.NotificationManagerCompat
import dev.benyamin.kumiho.engine.Accounts
import dev.benyamin.kumiho.engine.EngineClient
import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.engine.LoginState
import dev.benyamin.kumiho.permissions.PermissionController
import dev.benyamin.kumiho.vpn.VpnServiceCoordinator

/**
 * The engine calls the setup flow makes. It is the only Go-facing seam in T10;
 * the view model never touches [EngineClient] or gomobile directly, so every
 * sign-in path is a plain JVM test.
 */
interface SetupEngine {
    suspend fun loginEmail(email: String): LoginState

    suspend fun loginPassword(password: String): LoginState

    /** Supplies the emailed code; an empty code re-checks the email link. */
    suspend fun login2fa(code: String): LoginState

    /** Starts the active-account browser flow (`method == "browser"`). */
    suspend fun loginStart(method: String, email: String? = null): LoginState

    /** Polls the active browser flow; an empty id adopts the engine flow. */
    suspend fun loginStatus(id: String): LoginState

    /** Completes the browser flow with the full redirect URL from the WebView. */
    suspend fun loginComplete(redirectUrl: String, id: String? = null)

    suspend fun loginCancel(id: String)

    /** The account list, used only to prefill the last sign-in method. */
    suspend fun accountsList(): Accounts

    suspend fun refreshStatus(): EngineStatus
}

/** The production [SetupEngine]: a thin adapter over [EngineClient]. */
class EngineSetupEngine(private val client: EngineClient) : SetupEngine {
    override suspend fun loginEmail(email: String): LoginState = client.loginEmail(email)

    override suspend fun loginPassword(password: String): LoginState = client.loginPassword(password)

    override suspend fun login2fa(code: String): LoginState = client.login2fa(code)

    override suspend fun loginStart(method: String, email: String?): LoginState =
        client.loginStart(method, email)

    override suspend fun loginStatus(id: String): LoginState = client.loginStatus(id)

    override suspend fun loginComplete(redirectUrl: String, id: String?) =
        client.loginComplete(redirectUrl, id)

    override suspend fun loginCancel(id: String) = client.loginCancel(id)

    override suspend fun accountsList(): Accounts = client.accountsList()

    override suspend fun refreshStatus(): EngineStatus = client.refreshStatus()
}

/**
 * The Android facts the setup flow needs: permission state and the OS-owned
 * Always-on/lockdown policy. Every reader is honest: a permission that cannot
 * be verified is reported as not granted.
 */
interface SetupPermissions {
    /** True when [android.net.VpnService.prepare] has nothing left to ask. */
    fun vpnConsentGranted(): Boolean

    /** True only when notifications really reach the user. */
    fun notificationsGranted(): Boolean

    /** True on API 33+, where POST_NOTIFICATIONS is a runtime permission. */
    fun notificationsRuntimePermissionRequired(): Boolean

    /** Android's Always-on VPN flag for Kumiho, as last refreshed. */
    fun alwaysOn(): Boolean

    /** Android's lockdown flag for Kumiho, as last refreshed. */
    fun lockdown(): Boolean

    /**
     * Re-reads the OS policy. Called when the screen resumes, which is how the
     * Always-on step sees what the user changed in Android's VPN settings.
     */
    fun refreshOsPolicy()
}

/**
 * The production [SetupPermissions]. VPN consent is re-derived from
 * [VpnService.prepare] every time (never from an ActivityResult code), and
 * notifications are re-derived from [NotificationManagerCompat] plus the
 * runtime grant, because either can be revoked while the app is alive.
 *
 * Always-on/lockdown come from the bound service ([VpnServiceCoordinator]),
 * refreshed through the additive `refreshPolicy()` binding (R16).
 */
class AndroidSetupPermissions(
    context: Context,
    private val coordinator: VpnServiceCoordinator,
) : SetupPermissions {
    private val appContext = context.applicationContext
    private var policy = Policy(
        alwaysOn = coordinator.state.value.alwaysOn,
        lockdown = coordinator.state.value.lockdown,
    )

    override fun vpnConsentGranted(): Boolean =
        PermissionController.vpnPrepareIntent(appContext) == null

    override fun notificationsGranted(): Boolean {
        if (!PermissionController.hasNotificationPermission(appContext)) return false
        return notificationsEnabled()
    }

    override fun notificationsRuntimePermissionRequired(): Boolean =
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU

    override fun alwaysOn(): Boolean = policy.alwaysOn

    override fun lockdown(): Boolean = policy.lockdown

    override fun refreshOsPolicy() {
        val snapshot = coordinator.refreshPolicy() ?: return
        policy = Policy(alwaysOn = snapshot.alwaysOn, lockdown = snapshot.lockdown)
    }

    /**
     * The app-level switch, which a user can turn off without revoking the
     * runtime permission. Both must be true for a notification to appear.
     */
    private fun notificationsEnabled(): Boolean = try {
        NotificationManagerCompat.from(appContext).areNotificationsEnabled()
    } catch (_: RuntimeException) {
        // The compat shim can only throw on a broken framework service; an
        // unreadable state is reported as "not granted", never as granted.
        false
    }

    private data class Policy(val alwaysOn: Boolean, val lockdown: Boolean)
}
