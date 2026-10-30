package dev.benyamin.kumiho.ui.setup

import android.Manifest
import android.content.ActivityNotFoundException
import android.os.Build
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.exclude
import androidx.compose.foundation.layout.ime
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.permissions.PermissionController
import dev.benyamin.kumiho.ui.account.MozillaAccountBrowserDialog
import dev.benyamin.kumiho.ui.account.openBrowserSignIn
import dev.benyamin.kumiho.ui.components.KumihoLoadingState
import dev.benyamin.kumiho.ui.theme.Dimens
import dev.benyamin.kumiho.ui.theme.PillShape
import dev.benyamin.kumiho.vpn.ConnectRequest

/**
 * The setup entry point. It waits for the app-scoped coordinator to hand over a
 * started engine, then owns one rotation-safe [SetupViewModel] keyed by that
 * engine. A service that never starts gets a themed error, never a blank setup
 * screen and never a fake "ready" state.
 */
@Composable
fun SetupRoute(
    container: AppContainer,
    modifier: Modifier = Modifier,
) {
    val serviceState by container.vpnServiceCoordinator.state.collectAsStateWithLifecycle()
    val engine by container.vpnServiceCoordinator.engine.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val appContext = context.applicationContext
    val client = engine

    if (client == null) {
        SetupWaitingSurface(error = serviceState.error, modifier = modifier)
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
    SetupFlow(viewModel = setupViewModel, modifier = modifier)
}

/** Themed loading before the engine is ready; a themed error if it never is. */
@Composable
private fun SetupWaitingSurface(error: String?, modifier: Modifier = Modifier) {
    Surface(
        modifier = modifier.fillMaxSize(),
        color = MaterialTheme.colorScheme.background,
    ) {
        Box(contentAlignment = Alignment.Center, modifier = Modifier.fillMaxSize()) {
            if (error == null) {
                KumihoLoadingState(label = stringResource(R.string.setup_loading))
            } else {
                Column(
                    modifier = Modifier.padding(Dimens.Space5),
                    horizontalAlignment = Alignment.CenterHorizontally,
                    verticalArrangement = Arrangement.spacedBy(Dimens.Space3),
                ) {
                    Icon(
                        imageVector = Icons.Filled.Warning,
                        contentDescription = null,
                        tint = MaterialTheme.colorScheme.onSurface,
                        modifier = Modifier.size(40.dp),
                    )
                    Text(
                        text = stringResource(R.string.setup_engine_unavailable_title),
                        style = MaterialTheme.typography.titleLarge,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    Text(
                        text = stringResource(R.string.setup_engine_unavailable),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        textAlign = TextAlign.Center,
                    )
                    Text(
                        text = error,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        textAlign = TextAlign.Center,
                    )
                }
            }
        }
    }
}

/**
 * The four-step setup chrome: top bar with back, the numbered step indicator,
 * the step content and the fixed bottom action. It owns the ActivityResult
 * launchers (VPN consent, POST_NOTIFICATIONS) because they are Android UI
 * concerns, and re-reads the real permission state after every return.
 *
 * No bottom navigation: setup sits outside the shell, and one action at a time
 * is the whole navigation model besides Back.
 */
@Composable
fun SetupFlow(
    viewModel: SetupViewModel,
    modifier: Modifier = Modifier,
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val lifecycleOwner = LocalLifecycleOwner.current

    val vpnLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.StartActivityForResult(),
    ) {
        // The result code is not the permission: re-read VpnService.prepare.
        viewModel.onVpnConsentResult()
    }
    val notificationsLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) {
        // The same rule for notifications: ask the system, do not trust a
        // callback value that a user can revoke a second later.
        viewModel.onNotificationResult()
    }

    val requestVpn: () -> Unit = {
        val intent = PermissionController.vpnPrepareIntent(context)
        if (intent == null) {
            viewModel.onVpnConsentResult()
        } else {
            try {
                vpnLauncher.launch(intent)
            } catch (_: ActivityNotFoundException) {
                viewModel.onVpnConsentResult()
            }
        }
    }
    val openNotificationSettings: () -> Unit = {
        try {
            context.startActivity(PermissionController.notificationSettingsIntent(context))
        } catch (_: ActivityNotFoundException) {
            viewModel.onNotificationResult()
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
            viewModel.refreshPermissions()
        }
    }

    // Polling only runs while the screen is resumed; a return from Android's
    // VPN settings is a resume, which is when the OS policy is re-read (R16).
    DisposableEffect(lifecycleOwner) {
        val observer = LifecycleEventObserver { _, event ->
            when (event) {
                Lifecycle.Event.ON_RESUME -> {
                    viewModel.setScreenResumed(true)
                    viewModel.refreshPermissions()
                }
                Lifecycle.Event.ON_PAUSE -> viewModel.setScreenResumed(false)
                else -> Unit
            }
        }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose { lifecycleOwner.lifecycle.removeObserver(observer) }
    }

    // Back is one step at a time; at the first step the system exits the app.
    BackHandler(enabled = state.step != SetupStep.CONSENT) { viewModel.back() }

    Surface(
        modifier = modifier.fillMaxSize(),
        color = MaterialTheme.colorScheme.background,
    ) {
        Box(
            modifier = Modifier
                .fillMaxSize()
                .windowInsetsPadding(WindowInsets.safeDrawing.exclude(WindowInsets.ime)),
            contentAlignment = Alignment.TopCenter,
        ) {
            Column(
                modifier = Modifier
                    .widthIn(max = Dimens.ContentMaxWidth)
                    .fillMaxSize()
                    .imePadding(),
            ) {
                SetupBar(
                    state = state,
                    onBack = { viewModel.back() },
                )
                StepIndicator(step = state.step)
                Column(
                    modifier = Modifier
                        .weight(1f)
                        .verticalScroll(rememberScrollState())
                        .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space4),
                ) {
                    when (state.step) {
                        SetupStep.CONSENT -> ConsentStep()
                        SetupStep.SIGN_IN -> SignInStep(
                            state = state.signIn,
                            notice = state.notice,
                            browser = state.browser,
                            browserPasteOpen = state.browserPasteOpen,
                            browserPasteValue = state.browserPasteValue,
                            browserPasteError = state.browserPasteError,
                            browserPasteMessage = state.browserPasteMessage,
                            browserSubmitting = state.browserSubmitting,
                            browserFallback = state.browserFallback,
                            onBrowser = viewModel::startBrowser,
                            onOpenBrowser = viewModel::openBrowser,
                            onToggleBrowserPaste = viewModel::toggleBrowserPaste,
                            onBrowserPasteChange = viewModel::setBrowserPasteValue,
                            onBrowserPasteSubmit = viewModel::submitBrowserPaste,
                            onUsePassword = viewModel::cancelBrowser,
                            onCancelBrowser = viewModel::cancelBrowser,
                            onSubmitEmail = viewModel::submitEmail,
                            onSubmitPassword = viewModel::submitPassword,
                            onSubmitCode = viewModel::submitCode,
                            onRecheck = viewModel::recheckEmailLink,
                        )
                        SetupStep.PERMISSIONS -> PermissionsStep(
                            permissions = state.permissions,
                            onRequestVpn = requestVpn,
                            onRequestNotifications = requestNotifications,
                            onOpenNotificationSettings = openNotificationSettings,
                        )
                        SetupStep.ALWAYS_ON -> AlwaysOnStep(
                            alwaysOn = state.alwaysOn,
                            lockdown = state.lockdown,
                            canConnectNow = state.canConnectNow,
                            signedIn = state.signedIn,
                            vpnConsentGranted = state.permissions.vpnConsentGranted,
                            onOpenVpnSettings = openVpnSettings,
                        )
                    }
                }
                // The required tick is part of the fixed bottom action, not
                // the scrollable copy, so a disabled Continue is never shown
                // without the tick that enables it in view.
                val consentRow: (@Composable () -> Unit)? =
                    if (state.step == SetupStep.CONSENT) {
                        {
                            ConsentTickRow(
                                accepted = state.consentAccepted,
                                onAcceptedChange = viewModel::setConsentAccepted,
                                modifier = Modifier.padding(bottom = Dimens.Space2),
                            )
                        }
                    } else {
                        null
                    }
                SetupActions(
                    state = state,
                    consentRow = consentRow,
                    onContinue = viewModel::next,
                    onFinish = { viewModel.finish(connectNow = false) },
                    onConnectNow = { viewModel.finish(connectNow = true) },
                )
            }
        }
    }

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
            onRedirect = viewModel::submitBrowserRedirect,
            onClose = viewModel::closeBrowserWebView,
            onUnavailable = viewModel::browserUnavailable,
        )
    }
}

/** The 64dp top bar: back when there is a previous step, the title and the count. */
@Composable
private fun SetupBar(
    state: SetupState,
    onBack: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .height(Dimens.TopBarHeight)
            .padding(horizontal = Dimens.Space2)
            .testTag(SetupTags.TOP_BAR),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        val canGoBack = state.step != SetupStep.CONSENT
        if (canGoBack) {
            IconButton(
                onClick = onBack,
                modifier = Modifier
                    .size(Dimens.TouchTarget)
                    .testTag(SetupTags.BACK),
            ) {
                Icon(
                    imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                    contentDescription = stringResource(R.string.setup_back),
                    tint = MaterialTheme.colorScheme.onSurface,
                )
            }
        }
        Column(
            modifier = Modifier
                .weight(1f)
                .padding(start = if (canGoBack) Dimens.Space1 else Dimens.Space2),
        ) {
            Text(
                text = stringResource(R.string.setup_title),
                style = MaterialTheme.typography.titleLarge,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Text(
                text = stringResource(R.string.setup_step_position, state.step.number),
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/**
 * The step indicator. The numbers are the real sequence (Ban 11 carve-out:
 * this is a genuine ordered flow), the current step is marked by a filled
 * badge and a heavier label as well as the accent, and the whole row reads as
 * one accessible sentence.
 */
@Composable
private fun StepIndicator(step: SetupStep) {
    val description = stringResource(
        R.string.setup_step_of_current,
        step.number,
        stepLabel(step),
    )
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space2)
            .testTag(SetupTags.STEP_INDICATOR)
            .clearAndSetSemantics { contentDescription = description },
        horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
    ) {
        SetupStep.entries.forEach { entry ->
            StepBadge(
                step = entry,
                current = entry == step,
                modifier = Modifier.weight(1f),
            )
        }
    }
}

@Composable
private fun StepBadge(
    step: SetupStep,
    current: Boolean,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier,
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(Dimens.Space1),
    ) {
        Surface(
            shape = PillShape,
            color = if (current) MaterialTheme.colorScheme.primary else Color.Transparent,
            border = BorderStroke(
                1.dp,
                if (current) {
                    MaterialTheme.colorScheme.primary
                } else {
                    MaterialTheme.colorScheme.outline.copy(alpha = 0.6f)
                },
            ),
            modifier = Modifier.size(24.dp),
        ) {
            Box(contentAlignment = Alignment.Center) {
                Text(
                    text = step.number.toString(),
                    style = MaterialTheme.typography.labelMedium,
                    color = if (current) {
                        MaterialTheme.colorScheme.onPrimary
                    } else {
                        MaterialTheme.colorScheme.onSurfaceVariant
                    },
                )
            }
        }
        Text(
            text = stepLabel(step),
            style = MaterialTheme.typography.labelSmall,
            fontWeight = if (current) FontWeight.SemiBold else FontWeight.Normal,
            color = if (current) {
                MaterialTheme.colorScheme.onSurface
            } else {
                MaterialTheme.colorScheme.onSurfaceVariant
            },
            textAlign = TextAlign.Center,
            maxLines = 1,
        )
    }
}

@Composable
private fun stepLabel(step: SetupStep): String = stringResource(
    when (step) {
        SetupStep.CONSENT -> R.string.setup_step_consent
        SetupStep.SIGN_IN -> R.string.setup_step_sign_in
        SetupStep.PERMISSIONS -> R.string.setup_step_permissions
        SetupStep.ALWAYS_ON -> R.string.setup_step_always_on
    },
)

/**
 * The fixed bottom action. Steps 1–3 have one Continue; the last step has
 * Finish, plus "Finish and connect" only when the state allows a connect. An
 * inline error keeps the user on the step instead of dropping them out.
 */
@Composable
private fun SetupActions(
    state: SetupState,
    onContinue: () -> Unit,
    onFinish: () -> Unit,
    onConnectNow: () -> Unit,
    consentRow: (@Composable () -> Unit)? = null,
) {
    Surface(
        color = MaterialTheme.colorScheme.background,
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space3),
            verticalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            consentRow?.invoke()
            val failure = state.error
            if (failure != null) {
                Surface(
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(SetupTags.ERROR),
                    shape = MaterialTheme.shapes.small,
                    color = MaterialTheme.colorScheme.errorContainer,
                ) {
                    Column(
                        modifier = Modifier.padding(Dimens.Space3),
                        verticalArrangement = Arrangement.spacedBy(Dimens.Space1),
                    ) {
                        Text(
                            text = stringResource(R.string.setup_error_title),
                            style = MaterialTheme.typography.titleSmall,
                            color = MaterialTheme.colorScheme.onErrorContainer,
                        )
                        Text(
                            text = failure,
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onErrorContainer,
                        )
                    }
                }
            }
            if (state.step == SetupStep.ALWAYS_ON) {
                if (state.canConnectNow) {
                    Button(
                        onClick = onConnectNow,
                        enabled = !state.finishing,
                        modifier = Modifier
                            .fillMaxWidth()
                            .heightIn(min = Dimens.ControlHeight)
                            .testTag(SetupTags.CONNECT_NOW),
                    ) {
                        Text(stringResource(R.string.setup_finish_connect))
                    }
                    TextButton(
                        onClick = onFinish,
                        enabled = !state.finishing,
                        modifier = Modifier
                            .fillMaxWidth()
                            .heightIn(min = Dimens.TouchTarget)
                            .testTag(SetupTags.FINISH),
                    ) {
                        Text(stringResource(R.string.setup_finish_without_connect))
                    }
                } else {
                    Button(
                        onClick = onFinish,
                        enabled = !state.finishing,
                        modifier = Modifier
                            .fillMaxWidth()
                            .heightIn(min = Dimens.ControlHeight)
                            .testTag(SetupTags.FINISH),
                    ) {
                        Text(stringResource(R.string.setup_finish))
                    }
                }
            } else {
                Button(
                    onClick = onContinue,
                    enabled = state.canContinue && !state.finishing,
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(min = Dimens.ControlHeight)
                        .testTag(SetupTags.CONTINUE),
                ) {
                    Text(stringResource(R.string.setup_continue))
                }
            }
        }
    }
}
