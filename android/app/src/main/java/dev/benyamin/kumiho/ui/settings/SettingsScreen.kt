package dev.benyamin.kumiho.ui.settings

import android.content.Context
import android.content.Intent
import android.os.PowerManager
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.LifecycleResumeEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.core.net.toUri
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.BuildConfig
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.data.PaletteState
import dev.benyamin.kumiho.go.mobile.Mobile
import dev.benyamin.kumiho.permissions.PermissionController
import dev.benyamin.kumiho.ui.components.KumihoErrorState
import dev.benyamin.kumiho.ui.components.KumihoLoadingState
import dev.benyamin.kumiho.ui.components.StatusTone
import dev.benyamin.kumiho.ui.theme.Dimens
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * The Settings entry point. It waits for the app-scoped coordinator to start
 * the engine, then owns one rotation-safe [SettingsViewModel] keyed by that
 * engine, and reads the Android facts (theme, notifications, battery, OS VPN
 * policy) itself so the view model stays a plain JVM state machine.
 *
 * Every system-settings launch is caught: a missing activity can never crash
 * Settings, it becomes an inline message with a Dismiss.
 */
@Composable
fun SettingsRoute(
    container: AppContainer,
    modifier: Modifier = Modifier,
) {
    val coordinator = container.vpnServiceCoordinator
    val serviceState by coordinator.state.collectAsStateWithLifecycle()
    val engine by coordinator.engine.collectAsStateWithLifecycle()
    val paletteState by container.themeRepository.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val client = engine

    if (client == null) {
        SettingsWaitingSurface(error = serviceState.error, modifier = modifier)
        return
    }

    val gateway = remember(coordinator) { CoordinatorSettingsGateway(coordinator) }
    val checker = remember { GitHubUpdateChecker() }
    val engineVersion = remember(client) { Mobile.version() }
    val key = remember(client) { "settings-${System.identityHashCode(client)}" }
    val settingsViewModel: SettingsViewModel = viewModel(key = key) {
        SettingsViewModel(
            gateway = gateway,
            updateChecker = checker,
            appVersion = BuildConfig.VERSION_NAME,
            engineVersion = engineVersion,
        )
    }
    val state by settingsViewModel.state.collectAsStateWithLifecycle()

    val colors = (paletteState as? PaletteState.Ready)?.colors
    val theme = colors?.theme ?: ThemeId.LIGHT
    val accent = colors?.accent ?: KumihoAccent.Default
    val previews by produceState(
        initialValue = emptyMap<KumihoAccent, Color>(),
        key1 = theme,
    ) {
        value = withContext(Dispatchers.Default) {
            KumihoAccent.entries.mapNotNull { candidate ->
                runCatching {
                    PaletteMapping.fromJson(
                        Mobile.palette(theme.id, candidate.id),
                        theme,
                        candidate,
                    ).primary
                }.getOrNull()?.let { candidate to it }
            }.toMap()
        }
    }

    var notificationsAllowed by remember {
        mutableStateOf(PermissionController.hasNotificationPermission(context))
    }
    var batteryUnrestricted by remember { mutableStateOf(context.isIgnoringBatteryOptimizations()) }
    var launchError by remember { mutableStateOf<String?>(null) }

    // A return from any system settings page is a resume: re-read the facts the
    // user may have changed there, and ask the service for the fresh OS policy.
    LifecycleResumeEffect(coordinator, context) {
        coordinator.refreshPolicy()
        notificationsAllowed = PermissionController.hasNotificationPermission(context)
        batteryUnrestricted = context.isIgnoringBatteryOptimizations()
        onPauseOrDispose { }
    }

    val launchFailed = stringResource(R.string.settings_launch_failed)
    val open: (Intent) -> Unit = { intent ->
        try {
            context.startActivity(intent)
        } catch (error: RuntimeException) {
            launchError = error.message?.takeIf { it.isNotBlank() } ?: launchFailed
        }
    }

    SettingsScreen(
        state = state,
        appState = SettingsAppState(
            theme = theme,
            accent = accent,
            accentPreviews = previews,
            alwaysOn = serviceState.alwaysOn,
            lockdown = serviceState.lockdown,
            notificationsAllowed = notificationsAllowed,
            batteryUnrestricted = batteryUnrestricted,
            launchError = launchError,
        ),
        actions = SettingsActions(
            onToggleAdvanced = settingsViewModel::toggleAdvanced,
            onToggle = settingsViewModel::toggle,
            onApply = settingsViewModel::apply,
            onReset = settingsViewModel::reset,
            onClearError = settingsViewModel::clearError,
            onRetryLoad = settingsViewModel::retryLoad,
            onRetryNotice = settingsViewModel::retryNotice,
            onDismissNotice = settingsViewModel::dismissNotice,
            onThemeSelected = container.themeRepository::setTheme,
            onAccentSelected = container.themeRepository::setAccent,
            onCheckForUpdates = settingsViewModel::checkForUpdates,
            onOpenRelease = { url ->
                if (url.startsWith("https://")) {
                    open(Intent(Intent.ACTION_VIEW, url.toUri()))
                }
            },
            onOpenNotificationSettings = {
                open(PermissionController.notificationSettingsIntent(context))
            },
            onOpenBatterySettings = {
                open(PermissionController.batterySettingsIntent())
            },
            onOpenVpnSettings = { open(PermissionController.vpnSettingsIntent()) },
            onDismissLaunchError = { launchError = null },
            // T14 owns split tunneling. T13 deliberately renders no row and no
            // dead click; T14 passes its navigation callback here.
            onOpenSplitTunneling = null,
        ),
        modifier = modifier,
    )
}

private fun Context.isIgnoringBatteryOptimizations(): Boolean {
    val power = getSystemService(Context.POWER_SERVICE) as? PowerManager ?: return false
    return power.isIgnoringBatteryOptimizations(packageName)
}

/**
 * The stateless Settings screen: notices, then one grouped list. All nineteen
 * engine keys are reachable exactly once, Advanced starts collapsed, and the
 * kill-switch/autoconnect rows show Android's own state.
 *
 * It makes no engine or Android call, so a Compose test renders it from fake
 * state and asserts callbacks instead.
 */
@Composable
fun SettingsScreen(
    state: SettingsState,
    appState: SettingsAppState,
    actions: SettingsActions,
    modifier: Modifier = Modifier,
) {
    var editing by remember { mutableStateOf<SettingSpec?>(null) }

    Column(
        modifier = modifier
            .fillMaxSize()
            .testTag(SettingsTags.SCREEN),
    ) {
        state.notice?.let { notice ->
            NoticeBanner(
                notice = notice,
                onRetry = actions.onRetryNotice,
                onDismiss = actions.onDismissNotice,
                modifier = Modifier.padding(
                    start = Dimens.ScreenPadding,
                    end = Dimens.ScreenPadding,
                    top = Dimens.Space3,
                ),
            )
        }
        appState.launchError?.let { detail ->
            LaunchErrorBanner(
                detail = detail,
                onDismiss = actions.onDismissLaunchError,
                modifier = Modifier.padding(
                    start = Dimens.ScreenPadding,
                    end = Dimens.ScreenPadding,
                    top = Dimens.Space3,
                ),
            )
        }
        Box(modifier = Modifier.fillMaxWidth().weight(1f)) {
            when {
                state.showLoading -> LoadingContent()
                state.showError -> ErrorContent(
                    error = state.loadError.orEmpty(),
                    onRetry = actions.onRetryLoad,
                )
                else -> SettingsList(
                    state = state,
                    appState = appState,
                    actions = actions,
                    onEdit = { spec ->
                        actions.onClearError(spec.key)
                        editing = spec
                    },
                )
            }
        }
    }

    editing?.let { spec ->
        SettingEditorDialog(
            spec = spec,
            initialValue = state.valueOf(spec.key),
            locked = state.applying,
            onApply = { value ->
                actions.onApply(spec.key, value)
                editing = null
            },
            onDismiss = { editing = null },
        )
    }
}

// ---- the list ------------------------------------------------------------

@Composable
private fun SettingsList(
    state: SettingsState,
    appState: SettingsAppState,
    actions: SettingsActions,
    onEdit: (SettingSpec) -> Unit,
) {
    LazyColumn(
        modifier = Modifier
            .fillMaxSize()
            .testTag(SettingsTags.LIST),
        contentPadding = PaddingValues(bottom = Dimens.Space6),
    ) {
        item(key = "header_app") {
            GroupHeader(R.string.settings_group_app, SettingsTags.GROUP_APP)
        }
        item(key = "theme") {
            ThemeSetting(
                theme = appState.theme,
                onThemeSelected = actions.onThemeSelected,
            )
        }
        item(key = "accent") {
            AccentSetting(
                accent = appState.accent,
                previews = appState.accentPreviews,
                onAccentSelected = actions.onAccentSelected,
            )
        }
        item(key = "version") { VersionBlock(state) }
        item(key = "update") { UpdateBlock(state, actions) }
        item(key = "notifications") {
            NotificationStateRow(
                allowed = appState.notificationsAllowed,
                onOpenSettings = actions.onOpenNotificationSettings,
            )
        }
        item(key = "battery") {
            BatteryStateRow(
                unrestricted = appState.batteryUnrestricted,
                onOpenSettings = actions.onOpenBatterySettings,
            )
        }
        item(key = "always_on") {
            AlwaysOnStateRow(
                alwaysOn = appState.alwaysOn,
                onOpenSettings = actions.onOpenVpnSettings,
            )
        }

        item(key = "header_connection") {
            GroupHeader(R.string.settings_group_connection, SettingsTags.GROUP_CONNECTION)
        }
        items(SettingsCatalog.specsIn(SettingsGroup.CONNECTION), key = { it.key }) { spec ->
            EngineRow(spec = spec, state = state, actions = actions, onEdit = onEdit)
        }

        item(key = "header_android") {
            GroupHeader(R.string.settings_group_android_vpn, SettingsTags.GROUP_ANDROID_VPN)
        }
        item(key = "android_explanation") {
            Text(
                text = stringResource(R.string.settings_android_vpn_explanation),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space1)
                    .testTag(SettingsTags.VPN_EXPLANATION),
            )
        }
        items(SettingsCatalog.specsIn(SettingsGroup.ANDROID_VPN), key = { it.key }) { spec ->
            AndroidVpnRow(spec = spec, appState = appState, actions = actions)
        }

        item(key = "advanced_header") {
            AdvancedHeader(
                expanded = state.advancedExpanded,
                onToggle = actions.onToggleAdvanced,
            )
        }
        if (state.advancedExpanded) {
            items(SettingsCatalog.specsIn(SettingsGroup.ADVANCED), key = { it.key }) { spec ->
                EngineRow(spec = spec, state = state, actions = actions, onEdit = onEdit)
            }
        }
    }
}

@Composable
private fun GroupHeader(titleRes: Int, tag: String) {
    Text(
        text = stringResource(titleRes),
        style = MaterialTheme.typography.titleMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier
            .fillMaxWidth()
            .padding(
                start = Dimens.ScreenPadding,
                end = Dimens.ScreenPadding,
                top = Dimens.Space5,
                bottom = Dimens.Space1,
            )
            .semantics { heading() }
            .testTag(tag),
    )
}

/** The one collapsible group: the whole header is the 48dp target. */
@Composable
private fun AdvancedHeader(expanded: Boolean, onToggle: () -> Unit) {
    Surface(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = Dimens.Space5),
        color = Color.Transparent,
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .defaultMinSize(minHeight = Dimens.TouchTarget)
                .clickable(role = Role.Button, onClick = onToggle)
                .testTag(SettingsTags.ADVANCED_TOGGLE)
                .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space2),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = stringResource(R.string.settings_group_advanced),
                    style = MaterialTheme.typography.titleMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                    modifier = Modifier
                        .semantics { heading() }
                        .testTag(SettingsTags.GROUP_ADVANCED),
                )
                Text(
                    text = stringResource(R.string.settings_advanced_hint),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Icon(
                imageVector = if (expanded) {
                    Icons.Filled.KeyboardArrowDown
                } else {
                    Icons.AutoMirrored.Filled.KeyboardArrowRight
                },
                contentDescription = stringResource(
                    if (expanded) R.string.settings_collapse else R.string.settings_expand,
                ),
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(24.dp),
            )
        }
    }
}

@Composable
private fun EngineRow(
    spec: SettingSpec,
    state: SettingsState,
    actions: SettingsActions,
    onEdit: (SettingSpec) -> Unit,
) {
    EngineSettingRow(
        spec = spec,
        value = state.valueOf(spec.key),
        overridden = state.isOverridden(spec.key),
        locked = state.applying,
        applying = state.applyingKey == spec.key,
        error = state.errorFor(spec.key),
        onToggle = { checked ->
            actions.onClearError(spec.key)
            actions.onToggle(spec.key, checked)
        },
        onEdit = { onEdit(spec) },
        onReset = {
            actions.onClearError(spec.key)
            actions.onReset(spec.key)
        },
    )
}

/**
 * A read-only Android VPN row. The value is Always-on/lockdown from the OS, not
 * the engine's `kill_switch`/`autoconnect`; the action opens Android's VPN
 * settings, where those flags actually live.
 */
@Composable
private fun AndroidVpnRow(
    spec: SettingSpec,
    appState: SettingsAppState,
    actions: SettingsActions,
) {
    val labelRes = when (spec.key) {
        "kill_switch" -> killSwitchDisplay(appState.alwaysOn, appState.lockdown).labelRes
        else -> autoconnectDisplay(appState.alwaysOn).labelRes
    }
    AndroidStateRow(
        titleRes = spec.labelRes,
        hintRes = spec.hintRes,
        value = stringResource(labelRes),
        valueTag = SettingsTags.value(spec.key),
        actionLabelRes = R.string.settings_open_vpn_settings,
        actionTag = SettingsTags.vpnSettings(spec.key),
        onAction = actions.onOpenVpnSettings,
        modifier = Modifier.testTag(SettingsTags.row(spec.key)),
        tone = if (spec.key == "kill_switch" && appState.lockdown) {
            StatusTone.Warning
        } else {
            StatusTone.Neutral
        },
    )
}

// ---- app blocks ----------------------------------------------------------

@Composable
private fun VersionBlock(state: SettingsState) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space2),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space1),
    ) {
        Text(
            text = stringResource(R.string.settings_version_title),
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Text(
            text = stringResource(R.string.settings_version_hint),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(
            text = stringResource(R.string.settings_version_app, state.appVersion),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.testTag(SettingsTags.VERSION_APP),
        )
        Text(
            text = stringResource(R.string.settings_version_engine, state.engineVersion),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.testTag(SettingsTags.VERSION_ENGINE),
        )
    }
}

/**
 * The manual update check. The only trigger is the tap; there is no startup or
 * periodic call, no download and no install. A release is opened in the
 * browser and nothing else.
 */
@Composable
private fun UpdateBlock(state: SettingsState, actions: SettingsActions) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space2),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space1),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = stringResource(R.string.settings_update_title),
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Text(
                    text = stringResource(R.string.settings_update_hint),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            TextButton(
                onClick = actions.onCheckForUpdates,
                enabled = state.update !is UpdateState.Checking,
                modifier = Modifier
                    .heightIn(min = Dimens.TouchTarget)
                    .testTag(SettingsTags.UPDATE_CHECK),
            ) {
                Text(stringResource(R.string.settings_update_check))
            }
        }
        when (val update = state.update) {
            UpdateState.Idle -> Unit
            UpdateState.Checking -> Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
            ) {
                CircularProgressIndicator(
                    modifier = Modifier.size(16.dp),
                    strokeWidth = 2.dp,
                    color = MaterialTheme.colorScheme.primary,
                )
                Text(
                    text = stringResource(R.string.settings_update_checking),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.testTag(SettingsTags.UPDATE_STATUS),
                )
            }
            UpdateState.UpToDate -> Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
            ) {
                Icon(
                    imageVector = Icons.Filled.CheckCircle,
                    contentDescription = null,
                    tint = LocalKumihoColors.current.profit,
                    modifier = Modifier.size(16.dp),
                )
                Text(
                    text = stringResource(R.string.settings_update_up_to_date),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                    modifier = Modifier.testTag(SettingsTags.UPDATE_STATUS),
                )
            }
            is UpdateState.Available -> Column(
                verticalArrangement = Arrangement.spacedBy(Dimens.Space1),
            ) {
                Text(
                    text = stringResource(R.string.settings_update_available, update.version),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                    modifier = Modifier.testTag(SettingsTags.UPDATE_STATUS),
                )
                TextButton(
                    onClick = { actions.onOpenRelease(update.url) },
                    modifier = Modifier
                        .heightIn(min = Dimens.TouchTarget)
                        .testTag(SettingsTags.UPDATE_OPEN),
                ) {
                    Text(stringResource(R.string.settings_update_open))
                }
            }
            is UpdateState.Failed -> Column(
                verticalArrangement = Arrangement.spacedBy(Dimens.Space1),
            ) {
                Text(
                    text = stringResource(R.string.settings_update_failed),
                    style = MaterialTheme.typography.bodyMedium,
                    color = LocalKumihoColors.current.loss,
                    modifier = Modifier.testTag(SettingsTags.UPDATE_STATUS),
                )
                Text(
                    text = update.message,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                TextButton(
                    onClick = actions.onCheckForUpdates,
                    modifier = Modifier
                        .heightIn(min = Dimens.TouchTarget)
                        .testTag(SettingsTags.UPDATE_RETRY),
                ) {
                    Text(stringResource(R.string.settings_update_retry))
                }
            }
        }
    }
}

// ---- banners and content states -----------------------------------------

/** The one refresh notice, its words and its one retry. */
@Composable
private fun NoticeBanner(
    notice: SettingsNotice,
    onRetry: () -> Unit,
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val tint = when (notice.tone) {
        SettingsNoticeTone.WARNING -> LocalKumihoColors.current.warning
        SettingsNoticeTone.ERROR -> LocalKumihoColors.current.loss
    }
    Surface(
        modifier = modifier
            .fillMaxWidth()
            .testTag(SettingsTags.NOTICE),
        shape = MaterialTheme.shapes.medium,
        color = tint.copy(alpha = 0.12f),
        border = BorderStroke(1.dp, tint.copy(alpha = 0.5f)),
    ) {
        Column(modifier = Modifier.padding(Dimens.Space3)) {
            Row(
                horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
                verticalAlignment = Alignment.Top,
            ) {
                Icon(
                    imageVector = Icons.Filled.Warning,
                    contentDescription = null,
                    tint = tint,
                    modifier = Modifier
                        .padding(top = 2.dp)
                        .size(20.dp),
                )
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = stringResource(noticeMessage(notice.message)),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    if (notice.detail.isNotBlank()) {
                        Text(
                            text = notice.detail,
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.End,
            ) {
                if (notice.retry) {
                    TextButton(
                        onClick = onRetry,
                        modifier = Modifier
                            .heightIn(min = Dimens.TouchTarget)
                            .testTag(SettingsTags.NOTICE_RETRY),
                    ) {
                        Text(stringResource(R.string.settings_retry))
                    }
                }
                TextButton(
                    onClick = onDismiss,
                    modifier = Modifier
                        .heightIn(min = Dimens.TouchTarget)
                        .testTag(SettingsTags.NOTICE_DISMISS),
                ) {
                    Text(stringResource(R.string.settings_dismiss))
                }
            }
        }
    }
}

/** A failed system-settings launch, shown inline instead of crashing. */
@Composable
private fun LaunchErrorBanner(
    detail: String,
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val tint = LocalKumihoColors.current.loss
    Surface(
        modifier = modifier
            .fillMaxWidth()
            .testTag(SettingsTags.LAUNCH_ERROR),
        shape = MaterialTheme.shapes.medium,
        color = tint.copy(alpha = 0.12f),
        border = BorderStroke(1.dp, tint.copy(alpha = 0.5f)),
    ) {
        Column(modifier = Modifier.padding(Dimens.Space3)) {
            Row(
                horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
                verticalAlignment = Alignment.Top,
            ) {
                Icon(
                    imageVector = Icons.Filled.Warning,
                    contentDescription = null,
                    tint = tint,
                    modifier = Modifier
                        .padding(top = 2.dp)
                        .size(20.dp),
                )
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = stringResource(R.string.settings_launch_failed),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    Text(
                        text = detail,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            TextButton(
                onClick = onDismiss,
                modifier = Modifier
                    .align(Alignment.End)
                    .heightIn(min = Dimens.TouchTarget)
                    .testTag(SettingsTags.LAUNCH_ERROR_DISMISS),
            ) {
                Text(stringResource(R.string.settings_launch_failed_dismiss))
            }
        }
    }
}

private fun noticeMessage(message: SettingsMessage): Int = when (message) {
    SettingsMessage.REFRESH_FAILED -> R.string.settings_message_refresh_failed
    SettingsMessage.REFRESH_AFTER_SAVE_FAILED -> R.string.settings_message_refresh_after_save_failed
}
@Composable
private fun LoadingContent() {
    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        KumihoLoadingState(
            label = stringResource(R.string.settings_loading),
            modifier = Modifier.testTag(SettingsTags.LOADING),
        )
    }
}

@Composable
private fun ErrorContent(error: String, onRetry: () -> Unit) {
    Box(
        modifier = Modifier
            .fillMaxSize()
            .padding(Dimens.ScreenPadding),
        contentAlignment = Alignment.TopCenter,
    ) {
        KumihoErrorState(
            title = stringResource(R.string.settings_load_failed_title),
            message = error,
            onRetry = onRetry,
            modifier = Modifier.testTag(SettingsTags.ERROR),
        )
    }
}

/** Themed loading while the service binds, or the engine error it reported. */
@Composable
private fun SettingsWaitingSurface(error: String?, modifier: Modifier = Modifier) {
    Surface(
        modifier = modifier.fillMaxSize(),
        color = MaterialTheme.colorScheme.background,
    ) {
        Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            if (error == null) {
                KumihoLoadingState(
                    label = stringResource(R.string.settings_loading),
                    modifier = Modifier.testTag(SettingsTags.LOADING),
                )
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
                        text = stringResource(R.string.settings_engine_unavailable_title),
                        style = MaterialTheme.typography.titleLarge,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    Text(
                        text = stringResource(R.string.settings_engine_unavailable),
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
