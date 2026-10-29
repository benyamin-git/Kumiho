package dev.benyamin.kumiho.ui.tv.setup

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.OutlinedTextField
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusDirection
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Text
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.account.BrowserPasteMessage
import dev.benyamin.kumiho.ui.account.pasteMessageText
import dev.benyamin.kumiho.ui.setup.SetupActions
import dev.benyamin.kumiho.ui.setup.SetupHost
import dev.benyamin.kumiho.ui.setup.SetupMessage
import dev.benyamin.kumiho.ui.setup.SetupState
import dev.benyamin.kumiho.ui.setup.SetupStep
import dev.benyamin.kumiho.ui.setup.SetupTags
import dev.benyamin.kumiho.ui.setup.SignInPhase
import dev.benyamin.kumiho.ui.setup.setupMessageText
import dev.benyamin.kumiho.ui.theme.KumihoShapes
import dev.benyamin.kumiho.ui.theme.KumihoTypography
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors
import dev.benyamin.kumiho.ui.tv.components.TvButton
import dev.benyamin.kumiho.ui.tv.components.TvFocusableSurface
import dev.benyamin.kumiho.ui.tv.theme.TvDimens

/**
 * The television setup entry. [SetupHost] owns the engine wait, the setup view
 * model, the Android permission launchers and the in-app browser; this file is
 * only the ten-foot presentation of the same four steps.
 */
@Composable
fun TvSetupRoute(container: AppContainer, modifier: Modifier = Modifier) {
    SetupHost(
        container = container,
        modifier = modifier,
        waiting = { error -> TvSetupWaiting(error = error, modifier = modifier) },
    ) { state, actions ->
        TvSetupScreen(state = state, actions = actions, modifier = modifier)
    }
}

@Composable
private fun TvSetupWaiting(error: String?, modifier: Modifier = Modifier) {
    val colors = MaterialTheme.colorScheme
    Box(
        modifier = modifier
            .fillMaxSize()
            .padding(horizontal = TvDimens.OverscanHorizontal, vertical = TvDimens.OverscanVertical),
        contentAlignment = Alignment.Center,
    ) {
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(TvDimens.Space2),
        ) {
            Text(
                text = if (error == null) {
                    stringResource(R.string.setup_loading)
                } else {
                    stringResource(R.string.setup_engine_unavailable_title)
                },
                style = MaterialTheme.typography.titleLarge,
                color = colors.onSurface,
                textAlign = TextAlign.Center,
            )
            if (error != null) {
                Text(
                    text = stringResource(R.string.setup_engine_unavailable),
                    style = MaterialTheme.typography.bodyMedium,
                    color = colors.onSurfaceVariant,
                    textAlign = TextAlign.Center,
                )
                Text(
                    text = error,
                    style = MaterialTheme.typography.bodySmall,
                    color = colors.onSurfaceVariant,
                    textAlign = TextAlign.Center,
                )
            }
        }
    }
}

@Composable
internal fun TvSetupScreen(
    state: SetupState,
    actions: SetupActions,
    modifier: Modifier = Modifier,
) {
    val colors = MaterialTheme.colorScheme
    val primaryFocus = remember { FocusRequester() }
    val stepScroll = rememberScrollState()
    LaunchedEffect(state.step) {
        stepScroll.scrollTo(0)
        runCatching { primaryFocus.requestFocus() }
    }

    Row(
        modifier = modifier
            .fillMaxSize()
            .padding(horizontal = TvDimens.OverscanHorizontal, vertical = TvDimens.OverscanVertical),
        horizontalArrangement = Arrangement.spacedBy(TvDimens.Space4),
    ) {
        Column(
            modifier = Modifier
                .weight(1f)
                .fillMaxSize(),
            verticalArrangement = Arrangement.spacedBy(TvDimens.Space2),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(TvDimens.Space2),
            ) {
                if (state.step != SetupStep.CONSENT) {
                    TvButton(
                        label = stringResource(R.string.setup_back),
                        onClick = actions.onBack,
                        modifier = Modifier.testTag(SetupTags.BACK),
                    )
                }
                Text(
                    text = stringResource(R.string.setup_title),
                    style = MaterialTheme.typography.displaySmall,
                    color = colors.onSurface,
                )
                Text(
                    text = stringResource(R.string.setup_step_position, state.step.number),
                    style = MaterialTheme.typography.bodyMedium,
                    color = colors.onSurfaceVariant,
                    modifier = Modifier.testTag(SetupTags.TOP_BAR),
                )
            }
            StepLine(current = state.step)
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f)
                    .verticalScroll(stepScroll),
                verticalArrangement = Arrangement.spacedBy(TvDimens.Space2),
            ) {
                when (state.step) {
                    SetupStep.CONSENT -> ConsentStep(
                        state = state,
                        actions = actions,
                        focus = primaryFocus,
                    )
                    SetupStep.SIGN_IN -> SignInStep(
                        state = state,
                        actions = actions,
                        focus = primaryFocus,
                    )
                    SetupStep.PERMISSIONS -> PermissionsStep(
                        state = state,
                        actions = actions,
                        focus = primaryFocus,
                    )
                    SetupStep.ALWAYS_ON -> AlwaysOnStep(
                        state = state,
                        actions = actions,
                        focus = primaryFocus,
                    )
                }
            }
        }
        Column(
            modifier = Modifier
                .weight(1f)
                .fillMaxSize()
                .verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(TvDimens.Space2),
        ) {
            Text(
                text = stringResource(R.string.setup_title),
                style = MaterialTheme.typography.titleMedium,
                color = colors.onSurfaceVariant,
            )
            Text(
                text = stringResource(setupStepLabel(state.step)),
                style = MaterialTheme.typography.headlineMedium,
                color = colors.onSurface,
            )
            state.error?.let { failure ->
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .background(colors.errorContainer)
                        .border(1.dp, colors.error, MaterialTheme.shapes.small)
                        .padding(TvDimens.Space2)
                        .testTag(SetupTags.ERROR),
                    verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
                ) {
                    Text(
                        text = stringResource(R.string.setup_error_title),
                        style = MaterialTheme.typography.titleSmall,
                        color = colors.onErrorContainer,
                    )
                    Text(
                        text = failure,
                        style = MaterialTheme.typography.bodySmall,
                        color = colors.onErrorContainer,
                    )
                }
            }
            if (state.step == SetupStep.ALWAYS_ON) {
                if (state.canConnectNow) {
                    TvButton(
                        label = stringResource(R.string.setup_finish_connect),
                        onClick = actions.onFinishAndConnect,
                        modifier = Modifier
                            .fillMaxWidth()
                            .testTag(SetupTags.CONNECT_NOW),
                        enabled = !state.finishing,
                        primary = true,
                    )
                    TvButton(
                        label = stringResource(R.string.setup_finish_without_connect),
                        onClick = actions.onFinish,
                        modifier = Modifier
                            .fillMaxWidth()
                            .testTag(SetupTags.FINISH),
                        enabled = !state.finishing,
                    )
                } else {
                    TvButton(
                        label = stringResource(R.string.setup_finish),
                        onClick = actions.onFinish,
                        modifier = Modifier
                            .fillMaxWidth()
                            .testTag(SetupTags.FINISH),
                        enabled = !state.finishing,
                        primary = true,
                    )
                }
            } else {
                TvButton(
                    label = stringResource(R.string.setup_continue),
                    onClick = actions.onContinue,
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(SetupTags.CONTINUE),
                    enabled = state.canContinue && !state.finishing,
                    primary = true,
                )
            }
            Text(
                text = stringResource(R.string.always_on_finish_note),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun StepLine(current: SetupStep) {
    val colors = MaterialTheme.colorScheme
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .testTag(SetupTags.STEP_INDICATOR),
        horizontalArrangement = Arrangement.spacedBy(TvDimens.Space2),
    ) {
        SetupStep.entries.forEach { step ->
            val isCurrent = step == current
            Column(
                modifier = Modifier.weight(1f),
                verticalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                Text(
                    text = stringResource(R.string.setup_step_position, step.number),
                    style = MaterialTheme.typography.bodySmall,
                    color = if (isCurrent) colors.primary else colors.onSurfaceVariant,
                )
                Text(
                    text = stringResource(setupStepLabel(step)),
                    style = MaterialTheme.typography.titleSmall,
                    fontWeight = if (isCurrent) FontWeight.SemiBold else FontWeight.Normal,
                    color = if (isCurrent) colors.onSurface else colors.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun ConsentStep(state: SetupState, actions: SetupActions, focus: FocusRequester) {
    val colors = MaterialTheme.colorScheme
    Text(
        text = stringResource(R.string.consent_title),
        style = MaterialTheme.typography.headlineMedium,
        color = colors.onSurface,
        modifier = Modifier.testTag(SetupTags.CONSENT_STEP),
    )
    Text(
        text = stringResource(R.string.consent_intro),
        style = MaterialTheme.typography.bodyMedium,
        color = colors.onSurfaceVariant,
    )
    Text(
        text = stringResource(R.string.consent_future),
        style = MaterialTheme.typography.bodyMedium,
        color = colors.onSurfaceVariant,
    )
    Text(
        text = stringResource(R.string.consent_local_title),
        style = MaterialTheme.typography.titleMedium,
        color = colors.onSurface,
    )
    Text(
        text = stringResource(R.string.consent_local_tokens),
        style = MaterialTheme.typography.bodySmall,
        color = colors.onSurfaceVariant,
    )
    Text(
        text = stringResource(R.string.consent_local_data),
        style = MaterialTheme.typography.bodySmall,
        color = colors.onSurfaceVariant,
    )
    Text(
        text = stringResource(R.string.consent_local_no_telemetry),
        style = MaterialTheme.typography.bodySmall,
        color = colors.onSurfaceVariant,
    )
    Text(
        text = stringResource(R.string.consent_proxy_title),
        style = MaterialTheme.typography.titleMedium,
        color = colors.onSurface,
    )
    Text(
        text = stringResource(R.string.consent_proxy_text),
        style = MaterialTheme.typography.bodySmall,
        color = colors.onSurfaceVariant,
    )
    TvFocusableSurface(
        onClick = { actions.onSetConsent(!state.consentAccepted) },
        modifier = Modifier
            .fillMaxWidth()
            .focusRequester(focus)
            .testTag(SetupTags.CONSENT_ACCEPT),
    ) {
        Row(horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1)) {
            Text(
                text = if (state.consentAccepted) "✓" else " ",
                style = MaterialTheme.typography.titleLarge,
                color = colors.primary,
            )
            Text(
                text = stringResource(R.string.consent_accept),
                style = MaterialTheme.typography.bodyMedium,
                color = colors.onSurface,
            )
        }
    }
}

@Composable
private fun SignInStep(state: SetupState, actions: SetupActions, focus: FocusRequester) {
    val colors = MaterialTheme.colorScheme
    val signIn = state.signIn
    Text(
        text = stringResource(R.string.setup_step_sign_in),
        style = MaterialTheme.typography.headlineMedium,
        color = colors.onSurface,
        modifier = Modifier.testTag(SetupTags.SIGN_IN_STEP),
    )
    state.notice?.let { notice ->
        Text(
            text = notice,
            style = MaterialTheme.typography.bodySmall,
            color = colors.onSurfaceVariant,
            modifier = Modifier.testTag(SetupTags.SIGN_IN_NOTICE),
        )
    }

    when {
        state.signedIn -> {
            Text(
                text = stringResource(R.string.sign_in_signed_title),
                style = MaterialTheme.typography.titleLarge,
                color = colors.onSurface,
            )
            Text(
                text = stringResource(
                    R.string.sign_in_signed_as,
                    signIn.accountEmail?.takeIf { it.isNotBlank() } ?: signIn.email,
                ),
                style = MaterialTheme.typography.bodyLarge,
                color = colors.onSurface,
                modifier = Modifier.testTag(SetupTags.SIGN_IN_ACCOUNT),
            )
            Text(
                text = stringResource(R.string.sign_in_signed_note),
                style = MaterialTheme.typography.bodyMedium,
                color = colors.onSurfaceVariant,
            )
        }
        state.browser != null -> BrowserWaiting(state = state, actions = actions)
        signIn.phase == SignInPhase.EMAIL -> {
            TvButton(
                label = stringResource(R.string.sign_in_browser),
                onClick = actions.onStartBrowser,
                modifier = Modifier
                    .fillMaxWidth()
                    .focusRequester(focus)
                    .testTag(SetupTags.SIGN_IN_BROWSER),
                enabled = !signIn.busy,
                primary = true,
            )
            Text(
                text = stringResource(R.string.sign_in_browser_note),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
            Text(
                text = stringResource(R.string.sign_in_intro),
                style = MaterialTheme.typography.bodyMedium,
                color = colors.onSurfaceVariant,
            )
            EmailField(state = signIn, onSubmit = actions.onSubmitEmail)
        }
        signIn.phase == SignInPhase.PASSWORD -> {
            Text(
                text = stringResource(R.string.sign_in_password_title),
                style = MaterialTheme.typography.titleMedium,
                color = colors.onSurface,
            )
            Text(
                text = stringResource(R.string.sign_in_password_note),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
            PasswordField(state = signIn, onSubmit = actions.onSubmitPassword, focus = focus)
        }
        signIn.phase == SignInPhase.CODE -> {
            Text(
                text = stringResource(R.string.sign_in_code_title),
                style = MaterialTheme.typography.titleMedium,
                color = colors.onSurface,
            )
            signIn.verificationReason?.takeIf { it.isNotBlank() }?.let { reason ->
                Text(
                    text = stringResource(R.string.sign_in_reason, reason),
                    style = MaterialTheme.typography.bodySmall,
                    color = colors.onSurfaceVariant,
                )
            }
            Text(
                text = stringResource(R.string.sign_in_code_note),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
            CodeField(state = signIn, onSubmit = actions.onSubmitCode, focus = focus)
        }
        signIn.phase == SignInPhase.EMAIL_LINK_WAITING -> {
            Text(
                text = stringResource(R.string.sign_in_link_title),
                style = MaterialTheme.typography.titleMedium,
                color = colors.onSurface,
            )
            Text(
                text = stringResource(R.string.sign_in_link_text, signIn.email),
                style = MaterialTheme.typography.bodyMedium,
                color = colors.onSurface,
            )
            Text(
                text = stringResource(R.string.sign_in_link_polling),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
            TvButton(
                label = stringResource(R.string.sign_in_recheck),
                onClick = actions.onRecheckEmailLink,
                modifier = Modifier
                    .fillMaxWidth()
                    .focusRequester(focus)
                    .testTag(SetupTags.SIGN_IN_RECHECK),
                enabled = !signIn.busy,
            )
        }
    }

    val error = signIn.error ?: signIn.message?.let { setupMessageText(it) }
    if (!error.isNullOrBlank()) {
        Text(
            text = stringResource(R.string.sign_in_error_title),
            style = MaterialTheme.typography.titleSmall,
            color = colors.error,
            modifier = Modifier.testTag(SetupTags.SIGN_IN_ERROR),
        )
        Text(
            text = error,
            style = MaterialTheme.typography.bodySmall,
            color = colors.onSurface,
        )
    }
}

@Composable
private fun BrowserWaiting(state: SetupState, actions: SetupActions) {
    val colors = MaterialTheme.colorScheme
    val browser = state.browser ?: return
    Text(
        text = stringResource(R.string.sign_in_browser_waiting_title),
        style = MaterialTheme.typography.titleLarge,
        color = colors.onSurface,
    )
    Text(
        text = stringResource(R.string.sign_in_browser_waiting_text),
        style = MaterialTheme.typography.bodyMedium,
        color = colors.onSurfaceVariant,
    )
    if (state.browserFallback) {
        Text(
            text = stringResource(R.string.browser_waiting_fallback),
            style = MaterialTheme.typography.bodySmall,
            color = colors.onSurfaceVariant,
        )
    }
    browser.error?.let { error ->
        Text(
            text = error,
            style = MaterialTheme.typography.bodySmall,
            color = colors.error,
        )
    }
    TvButton(
        label = stringResource(R.string.browser_waiting_open),
        onClick = actions.onOpenBrowser,
        modifier = Modifier
            .fillMaxWidth()
            .testTag(SetupTags.SIGN_IN_BROWSER_OPEN),
        enabled = !state.browserSubmitting,
    )
    TvButton(
        label = stringResource(
            if (state.browserPasteOpen) R.string.browser_waiting_paste_hide else R.string.browser_waiting_paste_show,
        ),
        onClick = actions.onToggleBrowserPaste,
        modifier = Modifier
            .fillMaxWidth()
            .testTag(SetupTags.SIGN_IN_BROWSER_PASTE_TOGGLE),
        enabled = !state.browserSubmitting,
    )
    if (state.browserPasteOpen) {
        var paste by rememberSaveable { mutableStateOf(state.browserPasteValue) }
        TvField(
            label = stringResource(R.string.browser_waiting_paste_label),
            value = paste,
            onValueChange = {
                paste = it
                actions.onBrowserPasteChange(it)
            },
            enabled = !state.browserSubmitting,
            keyboardType = KeyboardType.Uri,
            modifier = Modifier.testTag(SetupTags.SIGN_IN_BROWSER_PASTE_FIELD),
        )
        val pasteError = state.browserPasteError ?: state.browserPasteMessage?.let { pasteMessageText(it) }
        if (!pasteError.isNullOrBlank()) {
            Text(
                text = pasteError,
                style = MaterialTheme.typography.bodySmall,
                color = colors.error,
            )
        }
        TvButton(
            label = stringResource(R.string.browser_waiting_paste_continue),
            onClick = actions.onSubmitBrowserPaste,
            modifier = Modifier
                .fillMaxWidth()
                .testTag(SetupTags.SIGN_IN_BROWSER_PASTE_SUBMIT),
            enabled = !state.browserSubmitting,
            primary = true,
        )
    }
    TvButton(
        label = stringResource(R.string.browser_waiting_use_password),
        onClick = actions.onCancelBrowser,
        modifier = Modifier
            .fillMaxWidth()
            .testTag(SetupTags.SIGN_IN_BROWSER_USE_PASSWORD),
        enabled = !state.browserSubmitting,
    )
    TvButton(
        label = stringResource(R.string.browser_waiting_cancel),
        onClick = actions.onCancelBrowser,
        modifier = Modifier
            .fillMaxWidth()
            .testTag(SetupTags.SIGN_IN_BROWSER_CANCEL),
        enabled = !state.browserSubmitting,
    )
    Text(
        text = stringResource(R.string.sign_in_browser_waiting_text),
        style = MaterialTheme.typography.bodySmall,
        color = colors.onSurfaceVariant,
    )
}

@Composable
private fun EmailField(state: dev.benyamin.kumiho.ui.setup.SignInPaneState, onSubmit: (String) -> Unit) {
    var email by rememberSaveable { mutableStateOf(state.email) }
    TvField(
        label = stringResource(R.string.sign_in_email_label),
        value = email,
        onValueChange = { email = it },
        enabled = !state.busy,
        keyboardType = KeyboardType.Email,
        imeAction = ImeAction.Done,
        onDone = { onSubmit(email) },
        modifier = Modifier.testTag(SetupTags.SIGN_IN_EMAIL),
    )
    TvButton(
        label = stringResource(R.string.sign_in_email_submit),
        onClick = { onSubmit(email) },
        modifier = Modifier
            .fillMaxWidth()
            .testTag(SetupTags.SIGN_IN_EMAIL_SUBMIT),
        enabled = !state.busy,
        primary = true,
    )
}

@Composable
private fun PasswordField(
    state: dev.benyamin.kumiho.ui.setup.SignInPaneState,
    onSubmit: (String) -> Unit,
    focus: FocusRequester,
) {
    var password by remember { mutableStateOf("") }
    TvField(
        label = stringResource(R.string.sign_in_password_label),
        value = password,
        onValueChange = { password = it },
        enabled = !state.busy,
        password = true,
        imeAction = ImeAction.Done,
        onDone = {
            onSubmit(password)
            password = ""
        },
        modifier = Modifier.testTag(SetupTags.SIGN_IN_PASSWORD),
    )
    TvButton(
        label = stringResource(R.string.sign_in_password_submit),
        onClick = {
            onSubmit(password)
            password = ""
        },
        modifier = Modifier
            .fillMaxWidth()
            .focusRequester(focus)
            .testTag(SetupTags.SIGN_IN_PASSWORD_SUBMIT),
        enabled = !state.busy,
        primary = true,
    )
}

@Composable
private fun CodeField(
    state: dev.benyamin.kumiho.ui.setup.SignInPaneState,
    onSubmit: (String) -> Unit,
    focus: FocusRequester,
) {
    var code by remember { mutableStateOf("") }
    TvField(
        label = stringResource(R.string.sign_in_code_label),
        value = code,
        onValueChange = { code = it },
        enabled = !state.busy,
        keyboardType = KeyboardType.Number,
        imeAction = ImeAction.Done,
        onDone = {
            onSubmit(code)
            code = ""
        },
        modifier = Modifier.testTag(SetupTags.SIGN_IN_CODE),
    )
    TvButton(
        label = stringResource(R.string.sign_in_code_submit),
        onClick = {
            onSubmit(code)
            code = ""
        },
        modifier = Modifier
            .fillMaxWidth()
            .focusRequester(focus)
            .testTag(SetupTags.SIGN_IN_CODE_SUBMIT),
        enabled = !state.busy,
        primary = true,
    )
}

@Composable
private fun PermissionsStep(state: SetupState, actions: SetupActions, focus: FocusRequester) {
    val colors = MaterialTheme.colorScheme
    Text(
        text = stringResource(R.string.permissions_title),
        style = MaterialTheme.typography.headlineMedium,
        color = colors.onSurface,
        modifier = Modifier.testTag(SetupTags.PERMISSIONS_STEP),
    )
    Text(
        text = stringResource(R.string.permissions_intro),
        style = MaterialTheme.typography.bodyMedium,
        color = colors.onSurfaceVariant,
    )
    Panel {
        Text(
            text = stringResource(R.string.permissions_vpn_title),
            style = MaterialTheme.typography.titleMedium,
            color = colors.onSurface,
        )
        Text(
            text = stringResource(R.string.permissions_vpn_text),
            style = MaterialTheme.typography.bodySmall,
            color = colors.onSurfaceVariant,
        )
        Text(
            text = stringResource(
                if (state.permissions.vpnConsentGranted) {
                    R.string.permissions_vpn_granted
                } else {
                    R.string.permissions_vpn_missing
                },
            ),
            style = MaterialTheme.typography.labelLarge,
            color = if (state.permissions.vpnConsentGranted) colors.primary else colors.onSurface,
            modifier = Modifier.testTag(SetupTags.PERMISSIONS_VPN_STATUS),
        )
        if (!state.permissions.vpnConsentGranted) {
            TvButton(
                label = stringResource(R.string.permissions_vpn_action),
                onClick = actions.onRequestVpn,
                modifier = Modifier
                    .fillMaxWidth()
                    .focusRequester(focus)
                    .testTag(SetupTags.PERMISSIONS_VPN_ACTION),
            )
        }
    }
    Panel {
        Text(
            text = stringResource(R.string.permissions_notifications_title),
            style = MaterialTheme.typography.titleMedium,
            color = colors.onSurface,
        )
        Text(
            text = stringResource(R.string.permissions_notifications_text),
            style = MaterialTheme.typography.bodySmall,
            color = colors.onSurfaceVariant,
        )
        Text(
            text = stringResource(
                if (state.permissions.notificationsGranted) {
                    R.string.permissions_notifications_granted
                } else {
                    R.string.permissions_notifications_missing
                },
            ),
            style = MaterialTheme.typography.labelLarge,
            color = if (state.permissions.notificationsGranted) colors.primary else colors.onSurface,
            modifier = Modifier.testTag(SetupTags.PERMISSIONS_NOTIFICATIONS_STATUS),
        )
        if (!state.permissions.notificationsGranted) {
            TvButton(
                label = stringResource(
                    if (state.permissions.notificationsRuntimeRequired) {
                        R.string.permissions_notifications_action
                    } else {
                        R.string.permissions_notifications_settings
                    },
                ),
                onClick = {
                    if (state.permissions.notificationsRuntimeRequired) {
                        actions.onRequestNotifications()
                    } else {
                        actions.onOpenNotificationSettings()
                    }
                },
                modifier = Modifier
                    .fillMaxWidth()
                    .testTag(SetupTags.PERMISSIONS_NOTIFICATIONS_ACTION),
            )
        }
    }
    Text(
        text = stringResource(R.string.permissions_note),
        style = MaterialTheme.typography.bodySmall,
        color = colors.onSurfaceVariant,
    )
}

@Composable
private fun AlwaysOnStep(state: SetupState, actions: SetupActions, focus: FocusRequester) {
    val colors = MaterialTheme.colorScheme
    Text(
        text = stringResource(R.string.setup_step_always_on),
        style = MaterialTheme.typography.headlineMedium,
        color = colors.onSurface,
        modifier = Modifier.testTag(SetupTags.ALWAYS_ON_STEP),
    )
    Text(
        text = stringResource(R.string.always_on_intro),
        style = MaterialTheme.typography.bodyMedium,
        color = colors.onSurfaceVariant,
    )
    Panel {
        Text(
            text = stringResource(R.string.always_on_always_title),
            style = MaterialTheme.typography.titleMedium,
            color = colors.onSurface,
        )
        Text(
            text = stringResource(R.string.always_on_always_text),
            style = MaterialTheme.typography.bodySmall,
            color = colors.onSurfaceVariant,
        )
        StateRow(
            title = stringResource(R.string.always_on_row_state),
            on = state.alwaysOn,
            modifier = Modifier.testTag(SetupTags.ALWAYS_ON_STATUS),
        )
    }
    Panel {
        Text(
            text = stringResource(R.string.always_on_lockdown_title),
            style = MaterialTheme.typography.titleMedium,
            color = colors.onSurface,
        )
        Text(
            text = stringResource(R.string.always_on_lockdown_text),
            style = MaterialTheme.typography.bodySmall,
            color = colors.onSurfaceVariant,
        )
        StateRow(
            title = stringResource(R.string.always_on_row_state),
            on = state.lockdown,
            modifier = Modifier.testTag(SetupTags.LOCKDOWN_STATUS),
        )
    }
    TvButton(
        label = stringResource(R.string.always_on_open_settings),
        onClick = actions.onOpenVpnSettings,
        modifier = Modifier
            .fillMaxWidth()
            .focusRequester(focus)
            .testTag(SetupTags.ALWAYS_ON_SETTINGS),
    )
    Text(
        text = stringResource(
            connectHint(
                canConnectNow = state.canConnectNow,
                signedIn = state.signedIn,
                vpnConsentGranted = state.permissions.vpnConsentGranted,
            ),
        ),
        style = MaterialTheme.typography.bodySmall,
        color = colors.onSurfaceVariant,
        modifier = Modifier.testTag(SetupTags.ALWAYS_ON_HINT),
    )
}

@Composable
private fun StateRow(title: String, on: Boolean, modifier: Modifier = Modifier) {
    val colors = MaterialTheme.colorScheme
    Row(
        modifier = modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Text(text = title, style = MaterialTheme.typography.bodyMedium, color = colors.onSurface)
        Text(
            text = stringResource(if (on) R.string.always_on_state_on else R.string.always_on_state_off),
            style = MaterialTheme.typography.titleSmall,
            color = colors.onSurface,
        )
    }
}

@Composable
private fun Panel(content: @Composable androidx.compose.foundation.layout.ColumnScope.() -> Unit) {
    val colors = MaterialTheme.colorScheme
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(colors.surface)
            .border(1.dp, colors.borderVariant, MaterialTheme.shapes.medium)
            .padding(TvDimens.Space2),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
        content = content,
    )
}

@Composable
private fun TvField(
    label: String,
    value: String,
    onValueChange: (String) -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    password: Boolean = false,
    keyboardType: KeyboardType = KeyboardType.Text,
    imeAction: ImeAction = ImeAction.Next,
    onDone: () -> Unit = {},
) {
    val house = LocalKumihoColors.current
    val focusManager = LocalFocusManager.current
    androidx.compose.material3.MaterialTheme(
        colorScheme = house.toColorScheme(),
        typography = KumihoTypography,
        shapes = KumihoShapes,
    ) {
        OutlinedTextField(
            value = value,
            onValueChange = onValueChange,
            label = { androidx.compose.material3.Text(label) },
            singleLine = true,
            enabled = enabled,
            textStyle = androidx.compose.material3.MaterialTheme.typography.bodyLarge,
            visualTransformation = if (password) PasswordVisualTransformation() else androidx.compose.ui.text.input.VisualTransformation.None,
            keyboardOptions = KeyboardOptions(keyboardType = keyboardType, imeAction = imeAction),
            keyboardActions = KeyboardActions(onDone = { onDone() }),
            modifier = modifier
                .fillMaxWidth()
                .heightIn(min = 64.dp)
                .onPreviewKeyEvent { event ->
                    if (event.type == KeyEventType.KeyDown && event.key == Key.DirectionDown) {
                        focusManager.moveFocus(FocusDirection.Down)
                        true
                    } else {
                        false
                    }
                },
        )
    }
}

@Composable
private fun setupStepLabel(step: SetupStep): Int = when (step) {
    SetupStep.CONSENT -> R.string.setup_step_consent
    SetupStep.SIGN_IN -> R.string.setup_step_sign_in
    SetupStep.PERMISSIONS -> R.string.setup_step_permissions
    SetupStep.ALWAYS_ON -> R.string.setup_step_always_on
}

private fun connectHint(canConnectNow: Boolean, signedIn: Boolean, vpnConsentGranted: Boolean): Int =
    when {
        canConnectNow -> R.string.always_on_connect_hint
        !signedIn && !vpnConsentGranted -> R.string.always_on_connect_hint_needs_both
        !vpnConsentGranted -> R.string.always_on_connect_hint_needs_vpn
        else -> R.string.always_on_connect_hint_needs_sign_in
    }
