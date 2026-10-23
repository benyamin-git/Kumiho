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
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
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
import dev.benyamin.kumiho.data.AllowBypassState
import dev.benyamin.kumiho.data.BypassError
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
import dev.benyamin.kumiho.vpn.ServiceState
import dev.benyamin.kumiho.vpn.SplitMode
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

    // Re-entering Settings restarts a failed read; start() is idempotent.
    LaunchedEffect(Unit) { container.allowBypassRepository.start() }

    // Split tunneling is a nested page inside Settings: it keeps the shell's
    // top bar and bottom navigation, provides its own back row, and survives
    // rotation through rememberSaveable.
    var splitOpen by rememberSaveable { mutableStateOf(false) }
    if (splitOpen) {
        SplitTunnelRoute(
            container = container,
            onBack = { splitOpen = false },
            modifier = modifier,
        )
        return
    }

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
    val splitState by container.splitTunnelRepository.state.collectAsStateWithLifecycle()
    val bypassState by container.allowBypassRepository.state
        .collectAsStateWithLifecycle()
    val bypassReconnect = remember(serviceState, bypassState) {
        bypassReconnectRequired(serviceState, bypassState)
    }

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
            splitMode = splitState.settings.mode,
            splitAppCount = splitState.settings.packages.size,
            bypassEnabled = bypassState.enabled,
            bypassLoaded = !bypassState.loading,
            bypassNeedsReconnect = bypassReconnect,
            bypassError = bypassState.error,
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
            onToggleBypass = container.allowBypassRepository::setEnabled,
            onOpenSplitTunneling = { splitOpen = true },
        ),
        modifier = modifier,
    )
}

/**
 * A live full tunnel built with a different allow-bypass value. Reuse the
 * split screen's full-tunnel states; proxy-only and idle sessions never warn.
 */
internal fun bypassReconnectRequired(
    state: ServiceState,
    bypass: AllowBypassState,
): Boolean = !bypass.loading &&
    bypass.enabled != bypass.applied &&
    splitReconnectRequired(state)

private fun Context.isIgnoringBatteryOptimizations(): Boolean {
    val power = getSystemService(Context.POWER_SERVICE) as? PowerManager ?: return false
    return power.isIgnoringBatteryOptimizations(packageName)
}

/**
 * The stateless Settings screen: notices, a compact engine status when there is
 * no snapshot, then one grouped list. All nineteen engine keys are reachable
 * exactly once, Advanced starts collapsed, and the kill-switch/autoconnect rows
 * show Android's own state.
 *
 * The app section and those Android-owned rows do not read the engine snapshot,
 * so they stay usable while the engine settings load or fail; only the engine
 * groups wait for a snapshot. A Compose test renders it from fake state and
 * asserts callbacks instead.
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
        if (state.showLoading) {
            LoadingContent()
        }
        if (state.showError) {
            ErrorContent(
                error = state.loadError.orEmpty(),
                onRetry = actions.onRetryLoad,
            )
        }
        Box(modifier = Modifier.fillMaxWidth().weight(1f)) {
            SettingsList(
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
        // App-level and deliberately outside the engine snapshot: the split
        // selection and the allow-bypass choice live in DataStore and stay
        // reachable while engine settings load or fail.
        item(key = "split") {
            SplitTunnelRow(
                appState = appState,
                onClick = actions.onOpenSplitTunneling,
            )
        }
        item(key = "bypass") {
            BypassSettingRow(
                appState = appState,
                onToggle = actions.onToggleBypass,
            )
        }

        if (state.hasSnapshot) {
            item(key = "header_connection") {
                GroupHeader(R.string.settings_group_connection, SettingsTags.GROUP_CONNECTION)
            }
            items(SettingsCatalog.specsIn(SettingsGroup.CONNECTION), key = { it.key }) { spec ->
                EngineRow(spec = spec, state = state, actions = actions, onEdit = onEdit)
            }
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

        if (state.hasSnapshot) {
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
}

/**
 * The Split tunneling shortcut: mode and app count at a glance, and a chevron
 * that opens the nested screen. It is App-level state from DataStore, so it
 * renders even while the engine snapshot is loading or missing.
 */
@Composable
private fun SplitTunnelRow(
    appState: SettingsAppState,
    onClick: (() -> Unit)?,
) {
    val summary = when (appState.splitMode) {
        SplitMode.OFF -> stringResource(R.string.settings_split_summary_off)
        SplitMode.EXCLUDE -> pluralStringResource(
            R.plurals.settings_split_summary_exclude,
            appState.splitAppCount,
            appState.splitAppCount,
        )
        SplitMode.INCLUDE -> pluralStringResource(
            R.plurals.settings_split_summary_include,
            appState.splitAppCount,
            appState.splitAppCount,
        )
    }
    Surface(color = Color.Transparent, modifier = Modifier.fillMaxWidth()) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .defaultMinSize(minHeight = Dimens.TouchTarget)
                .then(
                    if (onClick != null) {
                        Modifier.clickable(role = Role.Button, onClick = onClick)
                    } else {
                        Modifier
                    },
                )
                .testTag(SettingsTags.SPLIT_ROW)
                .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space2),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = stringResource(R.string.settings_split_title),
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Text(
                    text = summary,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.testTag(SettingsTags.SPLIT_SUMMARY),
                )
            }
            Icon(
                imageVector = Icons.AutoMirrored.Filled.KeyboardArrowRight,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(24.dp),
            )
        }
    }
}

/**
 * The app-owned allow-bypass choice. It renders from DataStore state, not the
 * engine snapshot, so it stays usable while engine settings load or fail.
 *
 * The On/Off word stays visible in every state; the warnings and the error are
 * words with an icon, never colour alone.
 */
@Composable
private fun BypassSettingRow(
    appState: SettingsAppState,
    onToggle: (Boolean) -> Unit,
) {
    val title = stringResource(R.string.settings_bypass_title)
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .testTag(SettingsTags.BYPASS_ROW)
            .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space2),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = title,
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Text(
                    text = stringResource(R.string.settings_bypass_hint),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Text(
                text = stringResource(
                    if (appState.bypassEnabled) R.string.settings_on else R.string.settings_off,
                ),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.testTag(SettingsTags.BYPASS_VALUE),
            )
            Switch(
                checked = appState.bypassEnabled,
                onCheckedChange = onToggle,
                enabled = appState.bypassLoaded,
                modifier = Modifier
                    .testTag(SettingsTags.BYPASS_SWITCH)
                    .semantics { contentDescription = title },
            )
        }
        if (appState.bypassNeedsReconnect) {
            BypassNotice(
                text = stringResource(R.string.settings_bypass_reconnect),
                tint = MaterialTheme.colorScheme.primary,
                icon = Icons.Filled.Info,
                tag = SettingsTags.BYPASS_RECONNECT,
            )
        }
        if (appState.lockdown) {
            BypassNotice(
                text = stringResource(R.string.settings_bypass_lockdown),
                tint = LocalKumihoColors.current.warning,
                icon = Icons.Filled.Warning,
                tag = SettingsTags.BYPASS_LOCKDOWN,
            )
        }
        appState.bypassError?.let { error ->
            BypassNotice(
                text = stringResource(
                    when (error) {
                        BypassError.LOAD -> R.string.settings_bypass_load_failed
                        BypassError.SAVE -> R.string.settings_bypass_save_failed
                    },
                ),
                tint = LocalKumihoColors.current.loss,
                icon = Icons.Filled.Warning,
                tag = SettingsTags.BYPASS_ERROR,
            )
        }
    }
}

/** One fixed warning or error line under the allow-bypass row. */
@Composable
private fun BypassNotice(
    text: String,
    tint: Color,
    icon: ImageVector,
    tag: String,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = Dimens.Space1)
            .testTag(tag),
        verticalAlignment = Alignment.Top,
        horizontalArrangement = Arrangement.spacedBy(Dimens.Space1),
    ) {
        Icon(
            imageVector = icon,
            contentDescription = null,
            tint = tint,
            modifier = Modifier
                .padding(top = 2.dp)
                .size(16.dp),
        )
        Text(
            text = text,
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurface,
        )
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
    val dependencyOff = spec.dependsOn?.let { state.valueOf(it) != "on" } ?: false
    EngineSettingRow(
        spec = spec,
        value = state.valueOf(spec.key),
        overridden = state.isOverridden(spec.key),
        locked = state.applying,
        applying = state.applyingKey == spec.key,
        error = state.errorFor(spec.key),
        enabled = !dependencyOff,
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
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space3),
        contentAlignment = Alignment.Center,
    ) {
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
            .fillMaxWidth()
            .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space3),
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
