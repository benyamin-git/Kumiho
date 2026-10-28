package dev.benyamin.kumiho.ui.tv.settings

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
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Text
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.data.InstalledApp
import dev.benyamin.kumiho.data.InstalledAppsFilter
import dev.benyamin.kumiho.ui.settings.SplitTunnelActions
import dev.benyamin.kumiho.ui.settings.SplitTunnelRoute
import dev.benyamin.kumiho.ui.settings.SplitTunnelTags
import dev.benyamin.kumiho.ui.settings.SplitTunnelUiState
import dev.benyamin.kumiho.ui.theme.KumihoShapes
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors
import dev.benyamin.kumiho.ui.tv.components.TvButton
import dev.benyamin.kumiho.ui.tv.components.TvFocusableSurface
import dev.benyamin.kumiho.ui.tv.theme.TvDimens
import dev.benyamin.kumiho.ui.tv.theme.TvM3Typography
import dev.benyamin.kumiho.vpn.SplitMode

/** The television split-tunnel entry; the phone route owns the repositories and view model. */
@Composable
fun TvSplitTunnelRoute(container: AppContainer, onBack: () -> Unit, modifier: Modifier = Modifier) {
    SplitTunnelRoute(container = container, onBack = onBack, modifier = modifier) { state, reconnect, actions ->
        TvSplitTunnelScreen(
            state = state,
            reconnectRequired = reconnect,
            actions = actions,
            modifier = modifier,
        )
    }
}

/**
 * The ten-foot split-tunnel page: the three modes as focusable cards, the
 * banners, the app search and the app rows. Changes apply on the next connect.
 */
@Composable
fun TvSplitTunnelScreen(
    state: SplitTunnelUiState,
    reconnectRequired: Boolean,
    actions: SplitTunnelActions,
    modifier: Modifier = Modifier,
) {
    val colors = MaterialTheme.colorScheme
    val apps = remember(state.apps, state.search, state.showSystem) {
        InstalledAppsFilter.filter(state.apps, state.search, state.showSystem)
    }
    val self = remember(state.apps) { state.apps.firstOrNull { it.isSelf } }

    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(TvDimens.Space2)
            .testTag(SplitTunnelTags.SCREEN),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
        ) {
            TvButton(
                label = stringResource(R.string.split_back),
                onClick = actions.onBack,
                modifier = Modifier.testTag(SplitTunnelTags.BACK),
            )
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = stringResource(R.string.settings_split_title),
                    style = MaterialTheme.typography.displaySmall,
                    color = colors.onSurface,
                    modifier = Modifier.testTag(SplitTunnelTags.TITLE),
                )
                Text(
                    text = stringResource(R.string.settings_split_hint),
                    style = MaterialTheme.typography.bodySmall,
                    color = colors.onSurfaceVariant,
                )
            }
            Text(
                text = pluralStringResource(
                    R.plurals.split_selected_apps,
                    state.selectedCount,
                    state.selectedCount,
                ),
                style = MaterialTheme.typography.labelLarge,
                color = colors.primary,
                modifier = Modifier.testTag(SplitTunnelTags.COUNT),
            )
        }

        Column(
            modifier = Modifier
                .fillMaxWidth()
                .weight(1f)
                .verticalScroll(rememberScrollState())
                .testTag(SplitTunnelTags.LIST),
            verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
        ) {
            if (state.loading) {
                Text(
                    text = stringResource(R.string.split_loading),
                    style = MaterialTheme.typography.titleMedium,
                    color = colors.onSurfaceVariant,
                    modifier = Modifier
                        .padding(TvDimens.Space2)
                        .testTag(SplitTunnelTags.LOADING),
                )
            } else if (state.repositoryError != null) {
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(TvDimens.Space2)
                        .testTag(SplitTunnelTags.ERROR),
                    verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
                ) {
                    Text(
                        text = stringResource(R.string.split_load_failed_title),
                        style = MaterialTheme.typography.titleLarge,
                        color = colors.onSurface,
                    )
                    Text(
                        text = stringResource(R.string.split_load_failed_hint),
                        style = MaterialTheme.typography.bodyMedium,
                        color = colors.onSurfaceVariant,
                    )
                    Text(
                        text = state.repositoryError,
                        style = MaterialTheme.typography.bodySmall,
                        color = colors.onSurfaceVariant,
                    )
                    TvButton(
                        label = stringResource(R.string.locations_retry),
                        onClick = actions.onRetryRepository,
                    )
                }
            } else {
                if (reconnectRequired) {
                    Banner(
                        text = stringResource(R.string.split_reconnect),
                        tone = colors.primary,
                        tag = SplitTunnelTags.RECONNECT,
                    )
                }
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(SplitTunnelTags.MODE_GROUP),
                    horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
                ) {
                    SplitMode.entries.forEach { mode ->
                        ModeCard(
                            mode = mode,
                            selected = state.mode == mode,
                            onClick = { actions.onModeSelected(mode) },
                            modifier = Modifier
                                .weight(1f)
                                .testTag(SplitTunnelTags.mode(mode)),
                        )
                    }
                }
                Text(
                    text = stringResource(R.string.split_apply_note),
                    style = MaterialTheme.typography.bodySmall,
                    color = colors.onSurfaceVariant,
                    modifier = Modifier.testTag(SplitTunnelTags.APPLY_NOTE),
                )
                if (state.mode == SplitMode.OFF) {
                    Banner(
                        text = stringResource(R.string.split_off_hint),
                        tone = colors.onSurfaceVariant,
                        tag = SplitTunnelTags.OFF_HINT,
                    )
                }
                if (state.warnEmptyInclude) {
                    Banner(
                        text = stringResource(R.string.split_empty_include_warning),
                        tone = LocalKumihoColors.current.warning,
                        tag = SplitTunnelTags.EMPTY_INCLUDE,
                    )
                }

                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    SplitSearch(
                        query = state.search,
                        onQueryChange = actions.onSearchChange,
                        onClear = actions.onClearSearch,
                        modifier = Modifier.weight(1f),
                    )
                    TvFocusableSurface(
                        onClick = { actions.onShowSystem(!state.showSystem) },
                        modifier = Modifier.testTag(SplitTunnelTags.SYSTEM_TOGGLE),
                    ) {
                        Text(
                            text = (if (state.showSystem) "✓ " else "") +
                                stringResource(R.string.split_system_apps),
                            style = MaterialTheme.typography.bodyMedium,
                            color = colors.onSurface,
                        )
                    }
                }

                if (self != null && state.mode != SplitMode.INCLUDE) {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .background(colors.surface)
                            .border(1.dp, colors.borderVariant, MaterialTheme.shapes.medium)
                            .padding(TvDimens.Space2)
                            .testTag(SplitTunnelTags.KUMIHO_ROW),
                        horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Column(modifier = Modifier.weight(1f)) {
                            Text(
                                text = self.label,
                                style = MaterialTheme.typography.titleSmall,
                                color = colors.onSurface,
                            )
                            Text(
                                text = self.packageName,
                                style = MaterialTheme.typography.bodySmall,
                                color = colors.onSurfaceVariant,
                            )
                        }
                        Text(
                            text = stringResource(R.string.split_kumiho_locked),
                            style = MaterialTheme.typography.labelLarge,
                            color = colors.onSurfaceVariant,
                            modifier = Modifier.testTag(SplitTunnelTags.KUMIHO_LOCK),
                        )
                    }
                }

                when {
                    state.appsLoading -> Text(
                        text = stringResource(R.string.split_apps_loading),
                        style = MaterialTheme.typography.bodyMedium,
                        color = colors.onSurfaceVariant,
                        modifier = Modifier
                            .padding(TvDimens.Space2)
                            .testTag(SplitTunnelTags.APPS_LOADING),
                    )
                    state.appsError != null -> Column(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(TvDimens.Space2)
                            .testTag(SplitTunnelTags.APPS_ERROR),
                        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
                    ) {
                        Text(
                            text = stringResource(R.string.split_apps_failed_title),
                            style = MaterialTheme.typography.titleMedium,
                            color = colors.onSurface,
                        )
                        Text(
                            text = state.appsError,
                            style = MaterialTheme.typography.bodySmall,
                            color = colors.onSurfaceVariant,
                        )
                        TvButton(
                            label = stringResource(R.string.locations_retry),
                            onClick = actions.onRetryApps,
                            modifier = Modifier.testTag(SplitTunnelTags.APPS_RETRY),
                        )
                    }
                    apps.isEmpty() -> Text(
                        text = if (state.search.isBlank()) {
                            stringResource(R.string.split_apps_empty)
                        } else {
                            stringResource(R.string.split_apps_no_match, state.search.trim())
                        },
                        style = MaterialTheme.typography.bodyMedium,
                        color = colors.onSurfaceVariant,
                        modifier = Modifier
                            .padding(TvDimens.Space2)
                            .testTag(SplitTunnelTags.APPS_EMPTY),
                    )
                    else -> apps.forEach { app ->
                        AppRow(
                            app = app,
                            checked = app.packageName in state.settings.packages,
                            enabled = state.mode != SplitMode.OFF,
                            onToggle = actions.onTogglePackage,
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun ModeCard(
    mode: SplitMode,
    selected: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val colors = MaterialTheme.colorScheme
    TvFocusableSurface(
        onClick = onClick,
        containerColor = if (selected) colors.primary.copy(alpha = 0.16f) else colors.surface,
        modifier = modifier,
    ) {
        Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(
                text = (if (selected) "✓ " else "") + modeTitle(mode),
                style = MaterialTheme.typography.titleSmall,
                color = colors.onSurface,
            )
            Text(
                text = modeDescription(mode),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun modeTitle(mode: SplitMode): String = stringResource(
    when (mode) {
        SplitMode.OFF -> R.string.split_mode_off_title
        SplitMode.EXCLUDE -> R.string.split_mode_exclude_title
        SplitMode.INCLUDE -> R.string.split_mode_include_title
    },
)

@Composable
private fun modeDescription(mode: SplitMode): String = stringResource(
    when (mode) {
        SplitMode.OFF -> R.string.split_mode_off_description
        SplitMode.EXCLUDE -> R.string.split_mode_exclude_description
        SplitMode.INCLUDE -> R.string.split_mode_include_description
    },
)

@Composable
private fun AppRow(
    app: InstalledApp,
    checked: Boolean,
    enabled: Boolean,
    onToggle: (String) -> Unit,
) {
    val colors = MaterialTheme.colorScheme
    TvFocusableSurface(
        onClick = { onToggle(app.packageName) },
        enabled = enabled,
        containerColor = if (checked) colors.primary.copy(alpha = 0.14f) else colors.surface,
        modifier = Modifier
            .fillMaxWidth()
            .testTag(SplitTunnelTags.appRow(app.packageName)),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = if (checked) "✓" else " ",
                style = MaterialTheme.typography.titleSmall,
                color = colors.primary,
                modifier = Modifier.testTag(SplitTunnelTags.appCheck(app.packageName)),
            )
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = app.label,
                    style = MaterialTheme.typography.bodyMedium,
                    color = colors.onSurface,
                )
                Text(
                    text = app.packageName,
                    style = MaterialTheme.typography.bodySmall,
                    color = colors.onSurfaceVariant,
                )
            }
            if (app.isSystem) {
                Text(
                    text = stringResource(R.string.split_system_apps),
                    style = MaterialTheme.typography.bodySmall,
                    color = colors.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun Banner(text: String, tone: androidx.compose.ui.graphics.Color, tag: String) {
    Text(
        text = text,
        style = MaterialTheme.typography.bodyMedium,
        color = MaterialTheme.colorScheme.onSurface,
        modifier = Modifier
            .fillMaxWidth()
            .background(tone.copy(alpha = 0.12f))
            .border(1.dp, tone.copy(alpha = 0.5f), MaterialTheme.shapes.medium)
            .padding(TvDimens.Space2)
            .testTag(tag),
    )
}

@Composable
private fun SplitSearch(
    query: String,
    onQueryChange: (String) -> Unit,
    onClear: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val house = LocalKumihoColors.current
    val focusManager = LocalFocusManager.current
    androidx.compose.material3.MaterialTheme(
        colorScheme = house.toColorScheme(),
        typography = TvM3Typography,
        shapes = KumihoShapes,
    ) {
        OutlinedTextField(
            value = query,
            onValueChange = onQueryChange,
            label = { androidx.compose.material3.Text(stringResource(R.string.split_search_label)) },
            singleLine = true,
            trailingIcon = {
                if (query.isNotEmpty()) {
                    androidx.compose.material3.TextButton(
                        onClick = onClear,
                        modifier = Modifier.testTag(SplitTunnelTags.SEARCH_CLEAR),
                    ) {
                        androidx.compose.material3.Text(stringResource(R.string.split_search_clear))
                    }
                }
            },
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
            keyboardActions = KeyboardActions(onSearch = { focusManager.clearFocus() }),
            modifier = modifier
                .heightIn(min = 64.dp)
                .testTag(SplitTunnelTags.SEARCH),
        )
    }
}
