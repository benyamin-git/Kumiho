package dev.benyamin.kumiho.ui.tv.settings

import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.OutlinedTextField
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Text
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.data.BypassError
import dev.benyamin.kumiho.ui.settings.ApplyTiming
import dev.benyamin.kumiho.ui.settings.SettingKind
import dev.benyamin.kumiho.ui.settings.SettingSpec
import dev.benyamin.kumiho.ui.settings.SettingInput
import dev.benyamin.kumiho.ui.settings.SettingsActions
import dev.benyamin.kumiho.ui.settings.SettingsAppState
import dev.benyamin.kumiho.ui.settings.SettingsCatalog
import dev.benyamin.kumiho.ui.settings.SettingsGroup
import dev.benyamin.kumiho.ui.settings.SettingsMapper
import dev.benyamin.kumiho.ui.settings.SettingsMessage
import dev.benyamin.kumiho.ui.settings.SettingsRoute
import dev.benyamin.kumiho.ui.settings.SettingsState
import dev.benyamin.kumiho.ui.settings.SettingsTags
import dev.benyamin.kumiho.ui.settings.UpdateState
import dev.benyamin.kumiho.ui.settings.batteryLabelRes
import dev.benyamin.kumiho.ui.settings.batteryStatusTone
import dev.benyamin.kumiho.ui.settings.settingDisplayValue
import dev.benyamin.kumiho.ui.settings.settingOptionLabel
import dev.benyamin.kumiho.ui.settings.settingValidationText
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoShapes
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.ui.components.StatusTone
import androidx.compose.ui.graphics.Color
import dev.benyamin.kumiho.vpn.SplitMode
import dev.benyamin.kumiho.ui.tv.components.TvButton
import dev.benyamin.kumiho.ui.tv.components.TvDialog
import dev.benyamin.kumiho.ui.tv.components.TvFocusableSurface
import dev.benyamin.kumiho.ui.tv.components.TvPageHeader
import dev.benyamin.kumiho.ui.tv.theme.TvDimens
import dev.benyamin.kumiho.ui.tv.theme.TvM3Typography

/** The television settings entry; the phone route owns the view model and Android calls. */
@Composable
fun TvSettingsRoute(container: AppContainer, modifier: Modifier = Modifier) {
    SettingsRoute(
        container = container,
        modifier = modifier,
        content = { state, appState, actions ->
            TvSettingsScreen(state = state, appState = appState, actions = actions, modifier = modifier)
        },
        splitContent = { onBack -> TvSplitTunnelRoute(container = container, onBack = onBack, modifier = modifier) },
    )
}

/**
 * The ten-foot settings page: the same twenty-one-row catalog, the app-level
 * theme/accent/update controls, the Android-owned rows and the split/bypass
 * seams. Boolean rows toggle in place; every other editable row opens a TV
 * dialog, so the whole page is remote-only.
 */
@Composable
fun TvSettingsScreen(
    state: SettingsState,
    appState: SettingsAppState,
    actions: SettingsActions,
    modifier: Modifier = Modifier,
) {
    var editing by remember { mutableStateOf<SettingSpec?>(null) }
    var choosing by remember { mutableStateOf<SettingSpec?>(null) }
    var releaseUrl by remember { mutableStateOf<String?>(null) }
    val colors = MaterialTheme.colorScheme

    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(TvDimens.Space2)
            .testTag(SettingsTags.SCREEN),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
    ) {
        TvPageHeader(title = stringResource(R.string.tv_settings_title))
        state.notice?.let { notice ->
            Notice(
                text = stringResource(
                    when (notice.message) {
                        SettingsMessage.REFRESH_FAILED -> R.string.settings_message_refresh_failed
                        SettingsMessage.REFRESH_AFTER_SAVE_FAILED ->
                            R.string.settings_message_refresh_after_save_failed
                    },
                ),
                detail = notice.detail,
                retry = if (notice.retry) actions.onRetryNotice else null,
                dismiss = actions.onDismissNotice,
                tag = SettingsTags.NOTICE,
            )
        }
        appState.launchError?.let { detail ->
            Notice(
                text = stringResource(R.string.settings_launch_failed),
                detail = detail,
                retry = null,
                dismiss = actions.onDismissLaunchError,
                tag = SettingsTags.LAUNCH_ERROR,
            )
            DetailAction(
                label = stringResource(R.string.settings_launch_failed_dismiss),
                onClick = actions.onDismissLaunchError,
            )
        }

        Column(
            modifier = Modifier
                .fillMaxWidth()
                .weight(1f)
                .verticalScroll(rememberScrollState())
                .testTag(SettingsTags.LIST),
            verticalArrangement = Arrangement.spacedBy(TvDimens.Space2),
        ) {
            GroupHeader(stringResource(R.string.settings_group_app), SettingsTags.GROUP_APP)

            TvSectionCard {
                SectionTitle(stringResource(R.string.settings_theme_title), stringResource(R.string.settings_theme_hint))
                Row(horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1)) {
                    ThemeId.entries.forEach { theme ->
                        TvButton(
                            label = themeLabel(theme),
                            onClick = { actions.onThemeSelected(theme) },
                            modifier = Modifier.testTag(SettingsTags.themeOption(theme.id)),
                            primary = appState.theme == theme,
                        )
                    }
                }

                SectionTitle(stringResource(R.string.settings_accent_title), stringResource(R.string.settings_accent_hint))
                KumihoAccent.entries.chunked(3).forEach { row ->
                    Row(horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1)) {
                        row.forEach { accent ->
                            val preview = appState.accentPreviews[accent]
                            TvFocusableSurface(
                                onClick = { actions.onAccentSelected(accent) },
                                modifier = Modifier
                                    .weight(1f)
                                    .testTag(SettingsTags.accentOption(accent.id)),
                            ) {
                                Row(
                                    horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
                                    verticalAlignment = Alignment.CenterVertically,
                                ) {
                                    if (preview != null) {
                                        Box(
                                            modifier = Modifier
                                                .background(preview)
                                                .border(1.dp, colors.borderVariant, MaterialTheme.shapes.small)
                                                .padding(10.dp),
                                        )
                                    }
                                    Text(
                                        text = accentLabel(accent),
                                        style = MaterialTheme.typography.bodyMedium,
                                        color = colors.onSurface,
                                    )
                                    if (appState.accent == accent) {
                                        Text(
                                            text = "✓",
                                            style = MaterialTheme.typography.titleSmall,
                                            color = colors.primary,
                                        )
                                    }
                                }
                            }
                        }
                        repeat(3 - row.size) {
                            Box(modifier = Modifier.weight(1f))
                        }
                    }
                }

                SectionTitle(stringResource(R.string.settings_version_title), stringResource(R.string.settings_version_hint))
                Text(
                    text = stringResource(R.string.settings_version_app, state.appVersion),
                    style = MaterialTheme.typography.bodyMedium,
                    color = colors.onSurface,
                    modifier = Modifier.testTag(SettingsTags.VERSION_APP),
                )
                Text(
                    text = stringResource(R.string.settings_version_engine, state.engineVersion),
                    style = MaterialTheme.typography.bodyMedium,
                    color = colors.onSurfaceVariant,
                    modifier = Modifier.testTag(SettingsTags.VERSION_ENGINE),
                )

                UpdateBlock(state = state, actions = actions, onOpenRelease = { releaseUrl = it })
            }

            SystemRow(
                title = stringResource(R.string.settings_notifications_title),
                hint = stringResource(R.string.settings_notifications_hint),
                value = stringResource(
                    if (appState.notificationsAllowed) {
                        R.string.settings_notifications_allowed
                    } else {
                        R.string.settings_notifications_missing
                    },
                ),
                action = stringResource(R.string.settings_notifications_open),
                onAction = actions.onOpenNotificationSettings,
                tag = SettingsTags.NOTIFICATIONS_STATE,
                actionTag = SettingsTags.NOTIFICATIONS_OPEN,
            )
            SystemRow(
                title = stringResource(R.string.settings_battery_title),
                hint = stringResource(R.string.settings_battery_hint),
                value = stringResource(batteryLabelRes(appState.battery)),
                action = stringResource(R.string.settings_battery_open),
                onAction = actions.onOpenBatterySettings,
                tag = SettingsTags.BATTERY_STATE,
                actionTag = SettingsTags.BATTERY_OPEN,
                valueColor = when (batteryStatusTone(appState.battery)) {
                    StatusTone.Profit -> LocalKumihoColors.current.profit
                    StatusTone.Loss -> LocalKumihoColors.current.loss
                    StatusTone.Warning -> LocalKumihoColors.current.warning
                    StatusTone.Neutral -> colors.onSurfaceVariant
                },
            )
            SystemRow(
                title = stringResource(R.string.settings_always_on_title),
                hint = stringResource(R.string.settings_always_on_hint),
                value = stringResource(
                    if (appState.alwaysOn) R.string.settings_on else R.string.settings_off,
                ),
                action = stringResource(R.string.settings_always_on_open),
                onAction = actions.onOpenVpnSettings,
                tag = SettingsTags.ALWAYS_ON_STATE,
                actionTag = SettingsTags.ALWAYS_ON_OPEN,
            )

            SplitRow(appState = appState, onClick = actions.onOpenSplitTunneling)
            BypassRow(appState = appState, onToggle = actions.onToggleBypass)

            if (state.hasSnapshot) {
                GroupHeader(stringResource(R.string.settings_group_connection), SettingsTags.GROUP_CONNECTION)
                SettingsCatalog.specsIn(SettingsGroup.CONNECTION).forEach { spec ->
                    EngineRow(
                        spec = spec,
                        state = state,
                        appState = appState,
                        onToggle = actions.onToggle,
                        onEdit = { editing = it },
                        onChoose = { choosing = it },
                        onReset = actions.onReset,
                    )
                }
            }

            GroupHeader(stringResource(R.string.settings_group_android_vpn), SettingsTags.GROUP_ANDROID_VPN)
            Text(
                text = stringResource(R.string.settings_android_vpn_explanation),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
                modifier = Modifier.testTag(SettingsTags.VPN_EXPLANATION),
            )
            SettingsCatalog.specsIn(SettingsGroup.ANDROID_VPN).forEach { spec ->
                ReadOnlyRow(
                    spec = spec,
                    state = state,
                    appState = appState,
                    onOpen = actions.onOpenVpnSettings,
                )
            }

            GroupHeader(stringResource(R.string.settings_group_advanced), SettingsTags.GROUP_ADVANCED)
            TvButton(
                label = stringResource(
                    if (state.advancedExpanded) R.string.settings_collapse else R.string.settings_expand,
                ),
                onClick = actions.onToggleAdvanced,
                modifier = Modifier.testTag(SettingsTags.ADVANCED_TOGGLE),
            )
            Text(
                text = stringResource(R.string.settings_advanced_hint),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
            if (state.advancedExpanded) {
                SettingsCatalog.specsIn(SettingsGroup.ADVANCED).forEach { spec ->
                    EngineRow(
                        spec = spec,
                        state = state,
                        appState = appState,
                        onToggle = actions.onToggle,
                        onEdit = { editing = it },
                        onChoose = { choosing = it },
                        onReset = actions.onReset,
                    )
                }
            }
        }
    }

    editing?.let { spec ->
        TvSettingEditor(
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
    choosing?.let { spec ->
        TvEnumEditor(
            spec = spec,
            current = state.valueOf(spec.key),
            onApply = { value ->
                actions.onApply(spec.key, value)
                choosing = null
            },
            onDismiss = { choosing = null },
        )
    }
    releaseUrl?.let { url ->
        ReleaseBrowser(url = url, onClose = { releaseUrl = null })
    }
}

/** A full-screen in-app browser for the update release page, so TV never needs another browser. */
@Composable
private fun ReleaseBrowser(url: String, onClose: () -> Unit) {
    Dialog(
        onDismissRequest = onClose,
        properties = DialogProperties(usePlatformDefaultWidth = false),
    ) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .background(MaterialTheme.colorScheme.background)
                .padding(TvDimens.Space2),
            verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                TvButton(
                    label = stringResource(R.string.tv_back),
                    onClick = onClose,
                    modifier = Modifier.testTag(SettingsTags.UPDATE_OPEN),
                )
                Text(
                    text = url,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            AndroidView(
                factory = { context ->
                    WebView(context).apply {
                        webViewClient = WebViewClient()
                        settings.javaScriptEnabled = true
                        settings.domStorageEnabled = true
                        loadUrl(url)
                    }
                },
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f),
            )
        }
    }
}

@Composable
private fun GroupHeader(title: String, tag: String) {
    Text(
        text = title,
        style = MaterialTheme.typography.titleLarge,
        color = MaterialTheme.colorScheme.primary,
        modifier = Modifier
            .padding(top = TvDimens.Space2)
            .testTag(tag),
    )
}

@Composable
private fun TvSectionCard(content: @Composable ColumnScope.() -> Unit) {
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
private fun SectionTitle(title: String, hint: String? = null) {    Column {
        Text(
            text = title,
            style = MaterialTheme.typography.titleMedium,
            color = MaterialTheme.colorScheme.onSurface,
        )
        if (hint != null) {
            Text(
                text = hint,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun UpdateBlock(
    state: SettingsState,
    actions: SettingsActions,
    onOpenRelease: (String) -> Unit,
) {
    SectionTitle(stringResource(R.string.settings_update_title), stringResource(R.string.settings_update_hint))
    when (val update = state.update) {
        UpdateState.Idle, UpdateState.UpToDate -> {
            if (update == UpdateState.UpToDate) {
                Text(
                    text = stringResource(R.string.settings_update_up_to_date),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                    modifier = Modifier.testTag(SettingsTags.UPDATE_STATUS),
                )
            }
            TvButton(
                label = stringResource(R.string.settings_update_check),
                onClick = actions.onCheckForUpdates,
                modifier = Modifier.testTag(SettingsTags.UPDATE_CHECK),
            )
        }
        UpdateState.Checking -> {
            Text(
                text = stringResource(R.string.settings_update_checking),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.testTag(SettingsTags.UPDATE_STATUS),
            )
        }
        is UpdateState.Available -> {
            Text(
                text = stringResource(R.string.settings_update_available, update.version),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurface,
                modifier = Modifier.testTag(SettingsTags.UPDATE_STATUS),
            )
            TvButton(
                label = stringResource(R.string.settings_update_open),
                onClick = { onOpenRelease(update.url) },
                modifier = Modifier.testTag(SettingsTags.UPDATE_OPEN),
                primary = true,
            )
        }
        is UpdateState.Failed -> {
            Text(
                text = stringResource(R.string.settings_update_failed) + " " + update.message,
                style = MaterialTheme.typography.bodyMedium,
                color = LocalKumihoColors.current.loss,
                modifier = Modifier.testTag(SettingsTags.UPDATE_STATUS),
            )
            TvButton(
                label = stringResource(R.string.settings_update_retry),
                onClick = actions.onCheckForUpdates,
                modifier = Modifier.testTag(SettingsTags.UPDATE_RETRY),
            )
        }
    }
}

@Composable
private fun SystemRow(
    title: String,
    hint: String,
    value: String,
    action: String,
    onAction: () -> Unit,
    tag: String,
    actionTag: String,
    valueColor: Color = Color.Unspecified,
) {
    val colors = MaterialTheme.colorScheme
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(colors.surface)
            .border(1.dp, colors.borderVariant, MaterialTheme.shapes.medium)
            .padding(TvDimens.Space2)
            .testTag(tag),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
    ) {
        Text(text = title, style = MaterialTheme.typography.titleSmall, color = colors.onSurface)
        Text(text = hint, style = MaterialTheme.typography.bodySmall, color = colors.onSurfaceVariant)
        Text(
            text = value,
            style = MaterialTheme.typography.labelLarge,
            color = if (valueColor == Color.Unspecified) colors.primary else valueColor,
        )
        TvButton(
            label = action,
            onClick = onAction,
            modifier = Modifier.testTag(actionTag),
        )
    }
}

@Composable
private fun SplitRow(appState: SettingsAppState, onClick: (() -> Unit)?) {
    val colors = MaterialTheme.colorScheme
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
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(colors.surface)
            .border(1.dp, colors.borderVariant, MaterialTheme.shapes.medium)
            .padding(TvDimens.Space2),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
    ) {
        Text(
            text = stringResource(R.string.settings_split_title),
            style = MaterialTheme.typography.titleSmall,
            color = colors.onSurface,
            modifier = Modifier.testTag(SettingsTags.SPLIT_ROW),
        )
        Text(
            text = stringResource(R.string.settings_split_hint),
            style = MaterialTheme.typography.bodySmall,
            color = colors.onSurfaceVariant,
        )
        Text(
            text = summary,
            style = MaterialTheme.typography.labelLarge,
            color = colors.primary,
            modifier = Modifier.testTag(SettingsTags.SPLIT_SUMMARY),
        )
        if (onClick != null) {
            TvButton(
                label = stringResource(R.string.settings_split_title),
                onClick = onClick,
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}

@Composable
private fun BypassRow(appState: SettingsAppState, onToggle: (Boolean) -> Unit) {
    val colors = MaterialTheme.colorScheme
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(colors.surface)
            .border(1.dp, colors.borderVariant, MaterialTheme.shapes.medium)
            .padding(TvDimens.Space2),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
    ) {
        TvFocusableSurface(
            onClick = { onToggle(!appState.bypassEnabled) },
            enabled = appState.bypassLoaded,
            modifier = Modifier
                .fillMaxWidth()
                .testTag(SettingsTags.BYPASS_ROW),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = stringResource(R.string.settings_bypass_title),
                        style = MaterialTheme.typography.titleSmall,
                        color = colors.onSurface,
                    )
                    Text(
                        text = stringResource(R.string.settings_bypass_hint),
                        style = MaterialTheme.typography.bodySmall,
                        color = colors.onSurfaceVariant,
                    )
                }
                Text(
                    text = stringResource(if (appState.bypassEnabled) R.string.settings_on else R.string.settings_off),
                    style = MaterialTheme.typography.labelLarge,
                    color = if (appState.bypassEnabled) colors.primary else colors.onSurfaceVariant,
                    modifier = Modifier.testTag(SettingsTags.BYPASS_VALUE),
                )
            }
        }
        if (appState.bypassNeedsReconnect) {
            Text(
                text = stringResource(R.string.settings_bypass_reconnect),
                style = MaterialTheme.typography.bodySmall,
                color = colors.primary,
                modifier = Modifier.testTag(SettingsTags.BYPASS_RECONNECT),
            )
        }
        if (appState.lockdown) {
            Text(
                text = stringResource(R.string.settings_bypass_lockdown),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
                modifier = Modifier.testTag(SettingsTags.BYPASS_LOCKDOWN),
            )
        }
        appState.bypassError?.let { error ->
            Text(
                text = stringResource(
                    when (error) {
                        BypassError.LOAD -> R.string.settings_bypass_load_failed
                        BypassError.SAVE -> R.string.settings_bypass_save_failed
                    },
                ),
                style = MaterialTheme.typography.bodySmall,
                color = LocalKumihoColors.current.loss,
                modifier = Modifier.testTag(SettingsTags.BYPASS_ERROR),
            )
        }
    }
}

@Composable
private fun EngineRow(
    spec: SettingSpec,
    state: SettingsState,
    appState: SettingsAppState,
    onToggle: (String, Boolean) -> Unit,
    onEdit: (SettingSpec) -> Unit,
    onChoose: (SettingSpec) -> Unit,
    onReset: (String) -> Unit,
) {
    val colors = MaterialTheme.colorScheme
    val value = state.valueOf(spec.key)
    val blockedByDependency = spec.dependsOn?.let { state.valueOf(it) != "on" } == true
    val editable = !state.applying && !blockedByDependency
    val display = settingDisplayValue(spec, value)
    val error = state.errorFor(spec.key)
    val onClick = when {
        !editable -> null
        spec.kind == SettingKind.BOOLEAN -> ({ onToggle(spec.key, value != "on") })
        spec.kind == SettingKind.ENUM -> ({ onChoose(spec) })
        else -> ({ onEdit(spec) })
    }
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        TvFocusableSurface(
            onClick = { onClick?.invoke() },
            enabled = onClick != null,
            modifier = Modifier
                .fillMaxWidth()
                .testTag(SettingsTags.row(spec.key)),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = stringResource(spec.labelRes),
                        style = MaterialTheme.typography.titleSmall,
                        color = colors.onSurface,
                    )
                    Text(
                        text = stringResource(spec.hintRes),
                        style = MaterialTheme.typography.bodySmall,
                        color = colors.onSurfaceVariant,
                    )
                    if (error != null) {
                        if (error.validation != null) {
                            Text(
                                text = settingValidationText(error.validation),
                                style = MaterialTheme.typography.bodySmall,
                                color = LocalKumihoColors.current.loss,
                                modifier = Modifier.testTag(SettingsTags.error(spec.key)),
                            )
                        } else {
                            Text(
                                text = stringResource(R.string.settings_validation_apply_failed),
                                style = MaterialTheme.typography.bodySmall,
                                color = LocalKumihoColors.current.loss,
                                modifier = Modifier.testTag(SettingsTags.error(spec.key)),
                            )
                            if (!error.text.isNullOrBlank()) {
                                Text(
                                    text = error.text,
                                    style = MaterialTheme.typography.bodySmall,
                                    color = colors.onSurfaceVariant,
                                )
                            }
                        }
                    }
                }
                Text(
                    text = when {
                        state.applyingKey == spec.key -> stringResource(R.string.settings_applying)
                        spec.kind == SettingKind.BOOLEAN -> stringResource(
                            if (value == "on") R.string.settings_on else R.string.settings_off,
                        )
                        else -> display
                    },
                    style = MaterialTheme.typography.labelLarge,
                    color = colors.primary,
                    modifier = Modifier.testTag(SettingsTags.value(spec.key)),
                )
            }
        }
        if (state.isOverridden(spec.key)) {
            Row(
                horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = stringResource(R.string.settings_overridden),
                    style = MaterialTheme.typography.bodySmall,
                    color = colors.onSurfaceVariant,
                    modifier = Modifier.testTag(SettingsTags.override(spec.key)),
                )
                TvButton(
                    label = stringResource(R.string.settings_reset),
                    onClick = { onReset(spec.key) },
                    modifier = Modifier.testTag(SettingsTags.reset(spec.key)),
                    enabled = !state.applying,
                )
            }
        }
        if (spec.applyTiming == ApplyTiming.NEXT_CONNECT) {
            // The hint already says when it applies; no extra line is needed.
        }
    }
}

@Composable
private fun ReadOnlyRow(
    spec: SettingSpec,
    state: SettingsState,
    appState: SettingsAppState,
    onOpen: () -> Unit,
) {
    val colors = MaterialTheme.colorScheme
    val value = when (spec.key) {
        "kill_switch" -> stringResource(
            if (appState.lockdown) R.string.settings_kill_switch_always_on else R.string.settings_kill_switch_off,
        )
        "autoconnect" -> stringResource(
            if (appState.alwaysOn) R.string.settings_autoconnect_on else R.string.settings_autoconnect_off,
        )
        else -> stringResource(R.string.settings_off)
    }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(colors.surface)
            .border(1.dp, colors.borderVariant, MaterialTheme.shapes.medium)
            .padding(TvDimens.Space2),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = stringResource(spec.labelRes),
                    style = MaterialTheme.typography.titleSmall,
                    color = colors.onSurface,
                )
                Text(
                    text = stringResource(spec.hintRes),
                    style = MaterialTheme.typography.bodySmall,
                    color = colors.onSurfaceVariant,
                )
            }
            Text(
                text = value,
                style = MaterialTheme.typography.labelLarge,
                color = colors.onSurface,
                modifier = Modifier.testTag(SettingsTags.value(spec.key)),
            )
        }
        TvButton(
            label = stringResource(R.string.settings_open_vpn_settings),
            onClick = onOpen,
            modifier = Modifier.testTag(SettingsTags.vpnSettings(spec.key)),
        )
    }
}

@Composable
private fun Notice(
    text: String,
    detail: String?,
    retry: (() -> Unit)?,
    dismiss: () -> Unit,
    tag: String,
) {
    val colors = MaterialTheme.colorScheme
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(colors.surfaceVariant)
            .border(1.dp, colors.borderVariant, MaterialTheme.shapes.medium)
            .padding(TvDimens.Space2)
            .testTag(tag),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
    ) {
        Text(text = text, style = MaterialTheme.typography.bodyMedium, color = colors.onSurface)
        if (!detail.isNullOrBlank()) {
            Text(text = detail, style = MaterialTheme.typography.bodySmall, color = colors.onSurfaceVariant)
        }
        Row(horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1)) {
            if (retry != null) {
                TvButton(label = stringResource(R.string.locations_retry), onClick = retry)
            }
            TvButton(label = stringResource(R.string.settings_launch_failed_dismiss), onClick = dismiss)
        }
    }
}

@Composable
private fun DetailAction(label: String, onClick: () -> Unit) {
    TvButton(label = label, onClick = onClick)
}

@Composable
private fun TvSettingEditor(
    spec: SettingSpec,
    initialValue: String,
    locked: Boolean,
    onApply: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    var text by remember(spec.key) { mutableStateOf(initialValue) }
    var invalid by remember(spec.key) { mutableStateOf<SettingInput.Invalid?>(null) }
    val focus = remember { FocusRequester() }
    val apply = {
        when (val input = SettingsMapper.serialize(spec, text)) {
            is SettingInput.Valid -> onApply(input.value)
            is SettingInput.Invalid -> invalid = input
        }
    }
    TvDialog(
        title = stringResource(spec.labelRes),
        onDismiss = onDismiss,
        initialFocus = focus,
        content = {
            Text(
                text = stringResource(spec.hintRes),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            TvInput(
                value = text,
                onValueChange = {
                    text = it
                    invalid = null
                },
                keyboardType = if (spec.kind == SettingKind.INTEGER) KeyboardType.Number else KeyboardType.Text,
                modifier = Modifier.testTag(SettingsTags.DIALOG_FIELD),
            )
            invalid?.let {
                Text(
                    text = settingValidationText(it),
                    style = MaterialTheme.typography.bodySmall,
                    color = LocalKumihoColors.current.loss,
                    modifier = Modifier.testTag(SettingsTags.DIALOG_ERROR),
                )
            }
        },
        actions = {
            TvButton(
                label = stringResource(R.string.settings_cancel),
                onClick = onDismiss,
                modifier = Modifier
                    .focusRequester(focus)
                    .testTag(SettingsTags.DIALOG_CANCEL),
            )
            TvButton(
                label = stringResource(R.string.settings_apply),
                onClick = apply,
                modifier = Modifier.testTag(SettingsTags.DIALOG_APPLY),
                enabled = !locked,
                primary = true,
            )
        },
    )
}

@Composable
private fun TvEnumEditor(
    spec: SettingSpec,
    current: String,
    onApply: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    val focus = remember { FocusRequester() }
    TvDialog(
        title = stringResource(spec.labelRes),
        onDismiss = onDismiss,
        initialFocus = focus,
        content = {
            Text(
                text = stringResource(spec.hintRes),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            spec.options.forEach { option ->
                TvButton(
                    label = (if (option == current) "✓ " else "") + settingOptionLabel(option),
                    onClick = { onApply(option) },
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(SettingsTags.option(option)),
                )
            }
        },
        actions = {
            TvButton(
                label = stringResource(R.string.settings_cancel),
                onClick = onDismiss,
                modifier = Modifier
                    .focusRequester(focus)
                    .testTag(SettingsTags.DIALOG_CANCEL),
            )
        },
    )
}

@Composable
private fun TvInput(
    value: String,
    onValueChange: (String) -> Unit,
    keyboardType: KeyboardType,
    modifier: Modifier = Modifier,
) {
    val house = LocalKumihoColors.current
    androidx.compose.material3.MaterialTheme(
        colorScheme = house.toColorScheme(),
        typography = TvM3Typography,
        shapes = KumihoShapes,
    ) {
        OutlinedTextField(
            value = value,
            onValueChange = onValueChange,
            singleLine = true,
            textStyle = androidx.compose.material3.MaterialTheme.typography.bodyLarge,
            keyboardOptions = KeyboardOptions(keyboardType = keyboardType),
            modifier = modifier
                .fillMaxWidth()
                .heightIn(min = 64.dp),
        )
    }
}

@Composable
private fun themeLabel(theme: ThemeId): String {
    val res = SettingsCatalog.themeLabelRes(theme.id)
    return if (res != null) stringResource(res) else theme.id
}

@Composable
private fun accentLabel(accent: KumihoAccent): String {
    val res = SettingsCatalog.accentLabelRes(accent.id)
    return if (res != null) stringResource(res) else accent.id
}
