package dev.benyamin.kumiho.ui.setup

import android.Manifest
import android.content.ActivityNotFoundException
import android.os.Build
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.permissions.PermissionController
import dev.benyamin.kumiho.ui.account.MozillaAccountBrowserDialog
import dev.benyamin.kumiho.ui.account.openBrowserSignIn
import dev.benyamin.kumiho.vpn.ConnectRequest

/** Every callback the setup presentation needs, independent of form factor. */
data class SetupActions(
    val onBack: () -> Unit,
    val onContinue: () -> Unit,
    val onFinish: () -> Unit,
    val onFinishAndConnect: () -> Unit,
    val onSetConsent: (Boolean) -> Unit,
    val onStartBrowser: () -> Unit,
    val onOpenBrowser: () -> Unit,
    val onToggleBrowserPaste: () -> Unit,
    val onBrowserPasteChange: (String) -> Unit,
    val onSubmitBrowserPaste: () -> Unit,
    val onSubmitBrowserRedirect: (String) -> Unit,
    val onCancelBrowser: () -> Unit,
    val onCloseBrowserWebView: () -> Unit,
    val onBrowserUnavailable: () -> Unit,
    val onSubmitEmail: (String) -> Unit,
    val onSubmitPassword: (String) -> Unit,
    val onSubmitCode: (String) -> Unit,
    val onRecheckEmailLink: () -> Unit,
    val onRequestVpn: () -> Unit,
    val onRequestNotifications: () -> Unit,
    val onOpenNotificationSettings: () -> Unit,
    val onOpenVpnSettings: () -> Unit,
)

/**
 * The shared setup host: waits for the started engine, owns the rotation-safe
 * view model, the Android consent/notification launchers, the resume polling
 * and the in-app browser window. Callers only supply the form-factor chrome.
 *
 * The phone `SetupFlow` keeps its own copy of the launchers for now; this host
 * is the seam the TV flow uses, so the phone setup behavior is untouched.
 */
@Composable
fun SetupHost(
    container: AppContainer,
    modifier: Modifier = Modifier,
    waiting: @Composable (String?) -> Unit,
    content: @Composable (SetupState, SetupActions) -> Unit,
) {
    val serviceState by container.vpnServiceCoordinator.state.collectAsStateWithLifecycle()
    val engine by container.vpnServiceCoordinator.engine.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val appContext = context.applicationContext
    val lifecycleOwner = LocalLifecycleOwner.current
    val client = engine

    if (client == null) {
        waiting(serviceState.error)
        return
    }

    val permissions = remember(container) {
        AndroidSetupPermissions(context, container.vpnServiceCoordinator)
    }
    val key = remember(client) { "setup-${System.identityHashCode(client)}" }
    val setupViewModel: SetupViewModel = viewModel(key = key) {
        SetupViewModel(
            engine = EngineSetupEngine(client),
            permissions = permissions,
            persistSetupComplete = { container.appPrefs.setSetupComplete(true) },
            connectNow = { container.vpnServiceCoordinator.connect(ConnectRequest()) },
            openExternalBrowser = { url -> openBrowserSignIn(appContext, url) },
        )
    }
    val state by setupViewModel.state.collectAsStateWithLifecycle()

    val vpnLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.StartActivityForResult(),
    ) { setupViewModel.onVpnConsentResult() }
    val notificationsLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) { setupViewModel.onNotificationResult() }

    val requestVpn: () -> Unit = {
        val intent = PermissionController.vpnPrepareIntent(context)
        if (intent == null) {
            setupViewModel.onVpnConsentResult()
        } else {
            try {
                vpnLauncher.launch(intent)
            } catch (_: ActivityNotFoundException) {
                setupViewModel.onVpnConsentResult()
            }
        }
    }
    val openNotificationSettings: () -> Unit = {
        try {
            context.startActivity(PermissionController.notificationSettingsIntent(context))
        } catch (_: ActivityNotFoundException) {
            setupViewModel.onNotificationResult()
        }
    }
    val requestNotifications: () -> Unit = {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            notificationsLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
        } else {
            openNotificationSettings()
        }
    }
    val openVpnSettings: () -> Unit = {
        try {
            context.startActivity(PermissionController.vpnSettingsIntent())
        } catch (_: ActivityNotFoundException) {
            setupViewModel.refreshPermissions()
        }
    }

    DisposableEffect(lifecycleOwner) {
        val observer = LifecycleEventObserver { _, event ->
            when (event) {
                Lifecycle.Event.ON_RESUME -> {
                    setupViewModel.setScreenResumed(true)
                    setupViewModel.refreshPermissions()
                }
                Lifecycle.Event.ON_PAUSE -> setupViewModel.setScreenResumed(false)
                else -> Unit
            }
        }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose { lifecycleOwner.lifecycle.removeObserver(observer) }
    }

    BackHandler(enabled = state.step != SetupStep.CONSENT) { setupViewModel.back() }

    val actions = SetupActions(
        onBack = { setupViewModel.back() },
        onContinue = setupViewModel::next,
        onFinish = { setupViewModel.finish(connectNow = false) },
        onFinishAndConnect = { setupViewModel.finish(connectNow = true) },
        onSetConsent = setupViewModel::setConsentAccepted,
        onStartBrowser = setupViewModel::startBrowser,
        onOpenBrowser = setupViewModel::openBrowser,
        onToggleBrowserPaste = setupViewModel::toggleBrowserPaste,
        onBrowserPasteChange = setupViewModel::setBrowserPasteValue,
        onSubmitBrowserPaste = setupViewModel::submitBrowserPaste,
        onSubmitBrowserRedirect = setupViewModel::submitBrowserRedirect,
        onCancelBrowser = setupViewModel::cancelBrowser,
        onCloseBrowserWebView = setupViewModel::closeBrowserWebView,
        onBrowserUnavailable = setupViewModel::browserUnavailable,
        onSubmitEmail = setupViewModel::submitEmail,
        onSubmitPassword = setupViewModel::submitPassword,
        onSubmitCode = setupViewModel::submitCode,
        onRecheckEmailLink = setupViewModel::recheckEmailLink,
        onRequestVpn = requestVpn,
        onRequestNotifications = requestNotifications,
        onOpenNotificationSettings = openNotificationSettings,
        onOpenVpnSettings = openVpnSettings,
    )

    content(state, actions)

    val browser = state.browser
    val browserURL = state.browserLaunchURL ?: browser?.url
    if (state.step == SetupStep.SIGN_IN &&
        state.browserWebViewOpen &&
        browser != null &&
        browserURL != null
    ) {
        MozillaAccountBrowserDialog(
            loginId = browser.id,
            url = browserURL,
            onRedirect = setupViewModel::submitBrowserRedirect,
            onClose = setupViewModel::closeBrowserWebView,
            onUnavailable = setupViewModel::browserUnavailable,
        )
    }
}
