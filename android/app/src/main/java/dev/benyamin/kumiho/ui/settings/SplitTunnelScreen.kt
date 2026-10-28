package dev.benyamin.kumiho.ui.settings

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Clear
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.Checkbox
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.data.InstalledApp
import dev.benyamin.kumiho.data.InstalledAppsFilter
import dev.benyamin.kumiho.ui.components.AppIcon
import dev.benyamin.kumiho.ui.components.KumihoErrorState
import dev.benyamin.kumiho.ui.components.KumihoLoadingState
import dev.benyamin.kumiho.ui.theme.Dimens
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors
import dev.benyamin.kumiho.vpn.ServiceState
import dev.benyamin.kumiho.vpn.SplitMode
import dev.benyamin.kumiho.vpn.VpnStateReducer
import dev.benyamin.kumiho.vpn.VpnUiState

/**
 * The split-tunnel entry point. It owns one rotation-safe
 * [SplitTunnelViewModel] over the app-scoped repository, reads the service
 * state for the reconnect banner and hands everything to the stateless screen.
 *
 * The route never connects, disconnects or applies settings: a change is
 * persisted and takes effect on the next connect.
 */
@Composable
fun SplitTunnelRoute(
    container: AppContainer,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
    content: @Composable (SplitTunnelUiState, Boolean, SplitTunnelActions) -> Unit = { state, reconnect, actions ->
        SplitTunnelScreen(state = state, reconnectRequired = reconnect, actions = actions, modifier = modifier)
    },
) {
    val repository = container.splitTunnelRepository
    val appsSource = container.installedAppsRepository
    val splitViewModel: SplitTunnelViewModel = viewModel(key = "split-tunnel") {
        SplitTunnelViewModel(repository = repository, appsSource = appsSource)
    }
    val state by splitViewModel.state.collectAsStateWithLifecycle()
    val serviceState by container.vpnServiceCoordinator.state.collectAsStateWithLifecycle()
    val reconnectRequired = remember(serviceState) { splitReconnectRequired(serviceState) }

    BackHandler(onBack = onBack)

    val splitActions = SplitTunnelActions(
        onModeSelected = splitViewModel::setMode,
        onTogglePackage = splitViewModel::togglePackage,
        onSearchChange = splitViewModel::onSearchChange,
        onClearSearch = splitViewModel::clearSearch,
        onShowSystem = splitViewModel::setShowSystem,
        onRetryRepository = splitViewModel::retryRepository,
        onRetryApps = splitViewModel::retryApps,
        onBack = onBack,
    )
    content(state, reconnectRequired, splitActions)
}

/**
 * Whether the live session is a full tunnel that would need reconnecting to
 * pick up a split change. A proxy-only session has no app split at all, so it
 * never claims a reconnect is needed.
 */
internal fun splitReconnectRequired(state: ServiceState): Boolean =
    when (VpnStateReducer.reduce(state)) {
        VpnUiState.CONNECTED,
        VpnUiState.CONNECTING,
        VpnUiState.RECONNECTING,
        VpnUiState.WAITING,
        -> true
        else -> false
    }

/** Every callback the stateless split screen needs. */
data class SplitTunnelActions(
    val onModeSelected: (SplitMode) -> Unit,
    val onTogglePackage: (String) -> Unit,
    val onSearchChange: (String) -> Unit,
    val onClearSearch: () -> Unit,
    val onShowSystem: (Boolean) -> Unit,
    val onRetryRepository: () -> Unit,
    val onRetryApps: () -> Unit,
    val onBack: () -> Unit,
)

/** Stable test tags for the split screen; all live under `split_`. */
object SplitTunnelTags {
    const val SCREEN = "split_screen"
    const val TITLE = "split_title"
    const val BACK = "split_back"
    const val LIST = "split_list"
    const val MODE_GROUP = "split_mode_group"
    const val RECONNECT = "split_reconnect"
    const val APPLY_NOTE = "split_apply_note"
    const val OFF_HINT = "split_off_hint"
    const val EMPTY_INCLUDE = "split_empty_include_warning"
    const val COUNT = "split_selected_count"
    const val SEARCH = "split_search"
    const val SEARCH_CLEAR = "split_search_clear"
    const val SYSTEM_TOGGLE = "split_system_toggle"
    const val KUMIHO_ROW = "split_kumiho_row"
    const val KUMIHO_LOCK = "split_kumiho_lock"
    const val LOADING = "split_loading"
    const val ERROR = "split_error"
    const val APPS_LOADING = "split_apps_loading"
    const val APPS_ERROR = "split_apps_error"
    const val APPS_RETRY = "split_apps_retry"
    const val APPS_EMPTY = "split_apps_empty"

    fun mode(mode: SplitMode): String = "split_mode_${mode.name.lowercase()}"

    fun appRow(packageName: String): String = "split_app_row_$packageName"

    fun appCheck(packageName: String): String = "split_app_check_$packageName"
}

/**
 * The stateless split screen: its own back row and title (the shell top bar
 * still says Settings), the three mode cards, the state banners, the search
 * and System-apps controls, then the app list with Kumiho pinned outside it.
 *
 * [appIcon] is injectable so Compose and screenshot tests draw a deterministic
 * icon instead of asking the emulator's PackageManager.
 */
@Composable
fun SplitTunnelScreen(
    state: SplitTunnelUiState,
    reconnectRequired: Boolean,
    actions: SplitTunnelActions,
    modifier: Modifier = Modifier,
    appIcon: @Composable (InstalledApp) -> Unit = { AppIcon(it) },
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .testTag(SplitTunnelTags.SCREEN),
    ) {
        Header(onBack = actions.onBack)
        when {
            state.loading -> LoadingContent()
            state.repositoryError != null -> RepositoryErrorContent(
                error = state.repositoryError,
                onRetry = actions.onRetryRepository,
            )
            else -> SplitContent(
                state = state,
                reconnectRequired = reconnectRequired,
                actions = actions,
                appIcon = appIcon,
            )
        }
    }
}

@Composable
private fun Header(onBack: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = Dimens.Space1, end = Dimens.ScreenPadding, top = Dimens.Space1),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        IconButton(
            onClick = onBack,
            modifier = Modifier
                .size(Dimens.TouchTarget)
                .testTag(SplitTunnelTags.BACK),
        ) {
            Icon(
                imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                contentDescription = stringResource(R.string.split_back),
                tint = MaterialTheme.colorScheme.onSurface,
            )
        }
        Column(
            modifier = Modifier
                .weight(1f)
                .padding(start = Dimens.Space1),
            verticalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            Text(
                text = stringResource(R.string.settings_split_title),
                style = MaterialTheme.typography.titleLarge,
                color = MaterialTheme.colorScheme.onSurface,
                modifier = Modifier
                    .semantics { heading() }
                    .testTag(SplitTunnelTags.TITLE),
            )
            Text(
                text = stringResource(R.string.settings_split_hint),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun SplitContent(
    state: SplitTunnelUiState,
    reconnectRequired: Boolean,
    actions: SplitTunnelActions,
    appIcon: @Composable (InstalledApp) -> Unit,
) {
    val apps = remember(state.apps, state.search, state.showSystem) {
        InstalledAppsFilter.filter(
            apps = state.apps,
            query = state.search,
            showSystem = state.showSystem,
        )
    }
    val self = remember(state.apps) { state.apps.firstOrNull { it.isSelf } }
    val appsEnabled = state.mode != SplitMode.OFF

    LazyColumn(
        modifier = Modifier
            .fillMaxSize()
            .testTag(SplitTunnelTags.LIST),
        contentPadding = PaddingValues(top = Dimens.Space2, bottom = Dimens.Space6),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space2),
    ) {
        if (reconnectRequired) {
            item(key = "reconnect") {
                SplitBanner(
                    icon = Icons.Filled.Info,
                    tint = MaterialTheme.colorScheme.primary,
                    text = stringResource(R.string.split_reconnect),
                    tag = SplitTunnelTags.RECONNECT,
                )
            }
        }

        item(key = "modes") { ModeSelector(mode = state.mode, onSelect = actions.onModeSelected) }

        item(key = "apply_note") {
            Text(
                text = stringResource(R.string.split_apply_note),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = Dimens.ScreenPadding)
                    .testTag(SplitTunnelTags.APPLY_NOTE),
            )
        }

        if (state.mode == SplitMode.OFF) {
            item(key = "off_hint") {
                SplitBanner(
                    icon = Icons.Filled.Info,
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    text = stringResource(R.string.split_off_hint),
                    tag = SplitTunnelTags.OFF_HINT,
                )
            }
        }

        if (state.warnEmptyInclude) {
            item(key = "empty_include") {
                SplitBanner(
                    icon = Icons.Filled.Warning,
                    tint = LocalKumihoColors.current.warning,
                    text = stringResource(R.string.split_empty_include_warning),
                    tag = SplitTunnelTags.EMPTY_INCLUDE,
                )
            }
        }

        item(key = "controls") {
            PickerControls(
                state = state,
                onSearchChange = actions.onSearchChange,
                onClearSearch = actions.onClearSearch,
                onShowSystem = actions.onShowSystem,
            )
        }

        if (self != null && state.mode != SplitMode.INCLUDE) {
            item(key = "kumiho") {
                KumihoLockedRow(
                    app = self,
                    appIcon = appIcon,
                    modifier = Modifier.padding(horizontal = Dimens.ScreenPadding),
                )
            }
        }

        when {
            state.appsLoading -> item(key = "apps_loading") { AppsLoadingContent() }
            state.appsError != null -> item(key = "apps_error") {
                AppsErrorContent(error = state.appsError, onRetry = actions.onRetryApps)
            }
            apps.isEmpty() -> item(key = "apps_empty") {
                AppListEmpty(query = state.search)
            }
            else -> items(items = apps, key = { it.packageName }) { app ->
                AppRow(
                    app = app,
                    checked = app.packageName in state.settings.packages,
                    enabled = appsEnabled,
                    onToggle = actions.onTogglePackage,
                    appIcon = appIcon,
                )
            }
        }
    }
}

// ---- mode selector -------------------------------------------------------

@Composable
private fun ModeSelector(mode: SplitMode, onSelect: (SplitMode) -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = Dimens.ScreenPadding)
            .testTag(SplitTunnelTags.MODE_GROUP),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space2),
    ) {
        for (candidate in SplitMode.entries) {
            ModeCard(
                mode = candidate,
                selected = candidate == mode,
                onSelect = onSelect,
            )
        }
    }
}

/** One mode card: a radio row whose selection is carried by border and chip. */
@Composable
private fun ModeCard(
    mode: SplitMode,
    selected: Boolean,
    onSelect: (SplitMode) -> Unit,
) {
    val border = if (selected) {
        BorderStroke(1.dp, MaterialTheme.colorScheme.primary)
    } else {
        BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant)
    }
    Surface(
        modifier = Modifier
            .fillMaxWidth()
            .defaultMinSize(minHeight = Dimens.TouchTarget)
            .selectable(
                selected = selected,
                role = Role.RadioButton,
                onClick = { onSelect(mode) },
            )
            .testTag(SplitTunnelTags.mode(mode)),
        shape = MaterialTheme.shapes.medium,
        color = if (selected) {
            MaterialTheme.colorScheme.primary.copy(alpha = 0.08f)
        } else {
            MaterialTheme.colorScheme.surfaceContainer
        },
        border = border,
    ) {
        Row(
            modifier = Modifier.padding(horizontal = Dimens.Space3, vertical = Dimens.Space2),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            RadioButton(selected = selected, onClick = null)
            Column(
                modifier = Modifier.weight(1f),
                verticalArrangement = Arrangement.spacedBy(2.dp),
            ) {
                Text(
                    text = stringResource(modeTitle(mode)),
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Text(
                    text = stringResource(modeDescription(mode)),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

private fun modeTitle(mode: SplitMode): Int = when (mode) {
    SplitMode.OFF -> R.string.split_mode_off_title
    SplitMode.EXCLUDE -> R.string.split_mode_exclude_title
    SplitMode.INCLUDE -> R.string.split_mode_include_title
}

private fun modeDescription(mode: SplitMode): Int = when (mode) {
    SplitMode.OFF -> R.string.split_mode_off_description
    SplitMode.EXCLUDE -> R.string.split_mode_exclude_description
    SplitMode.INCLUDE -> R.string.split_mode_include_description
}

// ---- banners -------------------------------------------------------------

/** One informational or warning banner: icon, tone, words, no action. */
@Composable
private fun SplitBanner(
    icon: ImageVector,
    tint: Color,
    text: String,
    tag: String,
) {
    Surface(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = Dimens.ScreenPadding)
            .testTag(tag),
        shape = MaterialTheme.shapes.medium,
        color = tint.copy(alpha = 0.12f),
        border = BorderStroke(1.dp, tint.copy(alpha = 0.5f)),
    ) {
        Row(
            modifier = Modifier.padding(Dimens.Space3),
            verticalAlignment = Alignment.Top,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            Icon(
                imageVector = icon,
                contentDescription = null,
                tint = tint,
                modifier = Modifier
                    .padding(top = 2.dp)
                    .size(20.dp),
            )
            Text(
                text = text,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurface,
                modifier = Modifier.weight(1f),
            )
        }
    }
}

// ---- picker controls -----------------------------------------------------

@Composable
private fun PickerControls(
    state: SplitTunnelUiState,
    onSearchChange: (String) -> Unit,
    onClearSearch: () -> Unit,
    onShowSystem: (Boolean) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = Dimens.ScreenPadding),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space2),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .defaultMinSize(minHeight = Dimens.TouchTarget),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            Text(
                text = if (state.mode == SplitMode.OFF) {
                    stringResource(R.string.split_off_pick_mode)
                } else {
                    pluralStringResource(
                        R.plurals.split_selected_apps,
                        state.selectedCount,
                        state.selectedCount,
                    )
                },
                style = MaterialTheme.typography.labelLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier
                    .weight(1f)
                    .testTag(SplitTunnelTags.COUNT),
            )
            val systemLabel = stringResource(R.string.split_system_apps)
            Text(
                text = systemLabel,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Switch(
                checked = state.showSystem,
                onCheckedChange = onShowSystem,
                modifier = Modifier
                    .testTag(SplitTunnelTags.SYSTEM_TOGGLE)
                    .semantics { contentDescription = systemLabel },
            )
        }
        SearchField(
            query = state.search,
            onQueryChange = onSearchChange,
            onClear = onClearSearch,
        )
    }
}

@Composable
private fun SearchField(
    query: String,
    onQueryChange: (String) -> Unit,
    onClear: () -> Unit,
) {
    val focusManager = LocalFocusManager.current
    OutlinedTextField(
        value = query,
        onValueChange = onQueryChange,
        modifier = Modifier
            .fillMaxWidth()
            .testTag(SplitTunnelTags.SEARCH),
        label = { Text(stringResource(R.string.split_search_label)) },
        singleLine = true,
        trailingIcon = {
            if (query.isNotEmpty()) {
                IconButton(
                    onClick = onClear,
                    modifier = Modifier
                        .size(Dimens.TouchTarget)
                        .testTag(SplitTunnelTags.SEARCH_CLEAR),
                ) {
                    Icon(
                        imageVector = Icons.Filled.Clear,
                        contentDescription = stringResource(R.string.split_search_clear),
                    )
                }
            }
        },
        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
        keyboardActions = KeyboardActions(onSearch = { focusManager.clearFocus() }),
    )
}

// ---- app rows ------------------------------------------------------------

/** Kumiho is never part of a selection; it bypasses its own tunnel always. */
@Composable
private fun KumihoLockedRow(
    app: InstalledApp,
    appIcon: @Composable (InstalledApp) -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .defaultMinSize(minHeight = Dimens.RowHeight)
            .testTag(SplitTunnelTags.KUMIHO_ROW),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Dimens.Space3),
    ) {
        appIcon(app)
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = app.label,
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Text(
                text = app.packageName,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space1),
        ) {
            Icon(
                imageVector = Icons.Filled.Lock,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(16.dp),
            )
            Text(
                text = stringResource(R.string.split_kumiho_locked),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.testTag(SplitTunnelTags.KUMIHO_LOCK),
            )
        }
    }
}

@Composable
private fun AppRow(
    app: InstalledApp,
    checked: Boolean,
    enabled: Boolean,
    onToggle: (String) -> Unit,
    appIcon: @Composable (InstalledApp) -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .defaultMinSize(minHeight = Dimens.RowHeight)
            .alpha(if (enabled) 1f else 0.38f)
            .toggleable(
                value = checked,
                enabled = enabled,
                role = Role.Checkbox,
                onValueChange = { onToggle(app.packageName) },
            )
            .testTag(SplitTunnelTags.appRow(app.packageName))
            .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space2),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Dimens.Space3),
    ) {
        appIcon(app)
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = app.label,
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Text(
                text = app.packageName,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Checkbox(
            checked = checked,
            onCheckedChange = null,
            enabled = enabled,
            modifier = Modifier.testTag(SplitTunnelTags.appCheck(app.packageName)),
        )
    }
}

// ---- content states ------------------------------------------------------

@Composable
private fun AppsLoadingContent() {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = Dimens.Space4),
        contentAlignment = Alignment.Center,
    ) {
        KumihoLoadingState(
            label = stringResource(R.string.split_apps_loading),
            modifier = Modifier.testTag(SplitTunnelTags.APPS_LOADING),
        )
    }
}

@Composable
private fun AppsErrorContent(error: String, onRetry: () -> Unit) {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = Dimens.ScreenPadding),
    ) {
        KumihoErrorState(
            title = stringResource(R.string.split_apps_failed_title),
            message = error,
            onRetry = onRetry,
            modifier = Modifier.testTag(SplitTunnelTags.APPS_ERROR),
        )
    }
}

@Composable
private fun AppListEmpty(query: String) {
    Text(
        text = if (query.isBlank()) {
            stringResource(R.string.split_apps_empty)
        } else {
            stringResource(R.string.split_apps_no_match, query.trim())
        },
        style = MaterialTheme.typography.bodyMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        textAlign = TextAlign.Center,
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space4)
            .testTag(SplitTunnelTags.APPS_EMPTY),
    )
}

@Composable
private fun LoadingContent() {
    Box(
        modifier = Modifier
            .fillMaxSize()
            .padding(Dimens.ScreenPadding),
        contentAlignment = Alignment.Center,
    ) {
        KumihoLoadingState(
            label = stringResource(R.string.split_loading),
            modifier = Modifier.testTag(SplitTunnelTags.LOADING),
        )
    }
}

@Composable
private fun RepositoryErrorContent(error: String, onRetry: () -> Unit) {
    Box(
        modifier = Modifier
            .fillMaxSize()
            .padding(Dimens.ScreenPadding),
        contentAlignment = Alignment.TopCenter,
    ) {
        Column(verticalArrangement = Arrangement.spacedBy(Dimens.Space2)) {
            KumihoErrorState(
                title = stringResource(R.string.split_load_failed_title),
                message = error,
                onRetry = onRetry,
                modifier = Modifier.testTag(SplitTunnelTags.ERROR),
            )
            Text(
                text = stringResource(R.string.split_load_failed_hint),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}
