package dev.benyamin.kumiho.ui.locations

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Clear
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material.icons.outlined.Star
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.TextField
import androidx.compose.material3.TextFieldDefaults
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.rememberVectorPainter
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.disabled
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.Server
import dev.benyamin.kumiho.ui.components.KumihoEmptyState
import dev.benyamin.kumiho.ui.components.KumihoErrorState
import dev.benyamin.kumiho.ui.components.KumihoLoadingState
import dev.benyamin.kumiho.ui.components.KumihoSectionHeader
import dev.benyamin.kumiho.ui.components.KumihoStatusChip
import dev.benyamin.kumiho.ui.components.StatusTone
import dev.benyamin.kumiho.ui.theme.Dimens
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors
import dev.benyamin.kumiho.ui.theme.PillShape

/**
 * The Locations entry point. It waits for the app-scoped coordinator to bind
 * the service and start the engine, then owns one rotation-safe
 * [LocationsViewModel] keyed by that engine. The route is the only Android-aware
 * part; the view model and [LocationsScreen] stay pure.
 */
@Composable
fun LocationsRoute(
    container: AppContainer,
    modifier: Modifier = Modifier,
    content: @Composable (LocationsState, LocationsActions) -> Unit = { state, actions ->
        LocationsScreen(state = state, actions = actions, modifier = modifier)
    },
) {
    val coordinator = container.vpnServiceCoordinator
    val serviceState by coordinator.state.collectAsStateWithLifecycle()
    val engine by coordinator.engine.collectAsStateWithLifecycle()
    val client = engine

    if (client == null) {
        LocationsWaitingSurface(error = serviceState.error, modifier = modifier)
        return
    }

    val gateway = remember(coordinator) { CoordinatorLocationsGateway(coordinator) }
    val key = remember(client) { "locations-${System.identityHashCode(client)}" }
    val locationsViewModel: LocationsViewModel = viewModel(key = key) {
        LocationsViewModel(gateway = gateway)
    }
    val state by locationsViewModel.state.collectAsStateWithLifecycle()

    // A tab switch leaves this composition and a fresh true resets speed order
    // for the new visit. Rotation restores the saved flag, so it does not.
    var entered by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(locationsViewModel) {
        if (!entered) {
            locationsViewModel.beginVisit()
            entered = true
        }
    }

    val actions = LocationsActions(
        onQueryChange = locationsViewModel::onQueryChange,
        onClearQuery = locationsViewModel::clearQuery,
        onSelect = locationsViewModel::select,
        onRefresh = locationsViewModel::refresh,
        onRetryLoad = locationsViewModel::retryLoad,
        onPingAll = locationsViewModel::pingAll,
        onDismissNotice = locationsViewModel::dismissNotice,
        onRetryNotice = locationsViewModel::retryNotice,
        onSelectFastest = locationsViewModel::selectFastest,
        onToggleFavorite = locationsViewModel::toggleFavorite,
        onClearRecents = locationsViewModel::clearRecents,
        onToggleLocked = locationsViewModel::toggleLocked,
        onConfirmSwitch = locationsViewModel::confirmSwitch,
        onDismissSwitch = locationsViewModel::dismissSwitch,
    )
    content(state, actions)
}

/** Every callback the stateless Locations screen needs; grouped to stay readable. */
data class LocationsActions(
    val onQueryChange: (String) -> Unit,
    val onClearQuery: () -> Unit,
    val onSelect: (Country, City, Server) -> Unit,
    val onRefresh: () -> Unit,
    val onRetryLoad: () -> Unit,
    val onPingAll: () -> Unit,
    val onDismissNotice: () -> Unit,
    val onRetryNotice: () -> Unit,
    val onSelectFastest: () -> Unit = {},
    val onToggleFavorite: (LocationRow) -> Unit = {},
    val onClearRecents: () -> Unit = {},
    val onToggleLocked: () -> Unit = {},
    val onConfirmSwitch: () -> Unit = {},
    val onDismissSwitch: () -> Unit = {},
)

/**
 * The stateless Locations screen: a pinned search field and toolbar, the
 * current selection, the notice banner and one flat server list of house
 * cards.
 *
 * It makes no engine, service or Android call, so a Compose test renders it
 * from fake state. A row tap is a request, and the check mark only follows the
 * service status.
 */
@Composable
fun LocationsScreen(
    state: LocationsState,
    actions: LocationsActions,
    modifier: Modifier = Modifier,
) {
    val listState = rememberLazyListState()
    LaunchedEffect(state.listResetToken) {
        if (state.rows.isNotEmpty()) {
            listState.scrollToItem(0)
        }
    }

    Column(
        modifier = modifier
            .fillMaxSize()
            .imePadding()
            .testTag(LocationsTags.SCREEN),
    ) {
        SearchField(
            query = state.query,
            onQueryChange = actions.onQueryChange,
            onClear = actions.onClearQuery,
            modifier = Modifier.padding(
                start = Dimens.ScreenPadding,
                end = Dimens.ScreenPadding,
                top = Dimens.Space4,
            ),
        )
        Toolbar(state = state, actions = actions)
        SelectionSummary(
            selection = state.selection,
            modifier = Modifier.padding(
                start = Dimens.ScreenPadding,
                end = Dimens.ScreenPadding,
                top = Dimens.Space2,
            ),
        )
        state.notice?.let { notice ->
            NoticeBanner(
                notice = notice,
                onAction = actions.onRetryNotice,
                onDismiss = actions.onDismissNotice,
                modifier = Modifier.padding(
                    start = Dimens.ScreenPadding,
                    end = Dimens.ScreenPadding,
                    top = Dimens.Space2,
                ),
            )
        }

        Box(modifier = Modifier.fillMaxWidth().weight(1f)) {
            when {
                state.showLoading -> LoadingContent()
                state.showError -> ErrorContent(error = state.loadError.orEmpty(), onRetry = actions.onRetryLoad)
                state.showEmpty -> EmptyContent(onRefresh = actions.onRefresh)
                state.showNoMatch -> NoMatchContent(query = state.query, onClear = actions.onClearQuery)
                else -> ServerList(
                    state = state,
                    actions = actions,
                    listState = listState,
                )
            }
        }
    }

    state.pending?.let { target ->
        SwitchLocationDialog(
            target = target,
            step = state.switchStep,
            lockdown = state.lockdown,
            onConfirm = actions.onConfirmSwitch,
            onDismiss = actions.onDismissSwitch,
        )
    }
}

/**
 * The confirmation and progress dialog for a location change while a session
 * is up. It never runs the choreography itself; every button is an action.
 */
@Composable
private fun SwitchLocationDialog(
    target: SwitchTarget,
    step: SwitchStep,
    lockdown: Boolean,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
) {
    val running = step.isRunning
    AlertDialog(
        onDismissRequest = { if (!running) onDismiss() },
        modifier = Modifier.testTag(LocationsTags.SWITCH_DIALOG),
        title = { Text(stringResource(R.string.locations_switch_title)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(Dimens.Space2)) {
                when (step) {
                    SwitchStep.Idle -> {
                        Text(
                            text = stringResource(R.string.locations_switch_target, target.label),
                            style = MaterialTheme.typography.bodyLarge,
                            color = MaterialTheme.colorScheme.onSurface,
                        )
                        Text(
                            text = target.server,
                            style = MaterialTheme.typography.bodySmall.copy(
                                fontFamily = FontFamily.Monospace,
                            ),
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        Text(
                            text = stringResource(R.string.locations_switch_body),
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        if (lockdown) {
                            Row(
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
                            ) {
                                Icon(
                                    imageVector = Icons.Filled.Warning,
                                    contentDescription = null,
                                    tint = LocalKumihoColors.current.warning,
                                    modifier = Modifier.size(18.dp),
                                )
                                Text(
                                    text = stringResource(R.string.locations_switch_lockdown),
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurface,
                                )
                            }
                        }
                    }

                    SwitchStep.Disconnecting -> SwitchProgress(
                        target = target,
                        label = stringResource(R.string.locations_switch_ending),
                    )

                    is SwitchStep.Connecting -> SwitchProgress(
                        target = target,
                        label = stringResource(R.string.locations_switch_connecting, step.target),
                    )

                    is SwitchStep.Failed -> {
                        Text(
                            text = stringResource(R.string.locations_switch_failed),
                            style = MaterialTheme.typography.bodyLarge,
                            color = MaterialTheme.colorScheme.onSurface,
                        )
                        Text(
                            text = step.message,
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
        },
        confirmButton = {
            when (step) {
                SwitchStep.Idle -> Button(
                    onClick = onConfirm,
                    modifier = Modifier.testTag(LocationsTags.SWITCH_CONFIRM),
                ) {
                    Text(stringResource(R.string.locations_switch_confirm))
                }

                is SwitchStep.Failed -> Button(
                    onClick = onConfirm,
                    modifier = Modifier.testTag(LocationsTags.SWITCH_RETRY),
                ) {
                    Text(stringResource(R.string.locations_switch_retry))
                }

                else -> Unit
            }
        },
        dismissButton = {
            when (step) {
                SwitchStep.Idle -> TextButton(
                    onClick = onDismiss,
                    modifier = Modifier.testTag(LocationsTags.SWITCH_DISMISS),
                ) {
                    Text(stringResource(R.string.locations_switch_cancel))
                }

                is SwitchStep.Failed -> TextButton(
                    onClick = onDismiss,
                    modifier = Modifier.testTag(LocationsTags.SWITCH_DISMISS),
                ) {
                    Text(stringResource(R.string.locations_switch_close))
                }

                else -> Unit
            }
        },
    )
}

@Composable
private fun SwitchProgress(target: SwitchTarget, label: String) {
    Text(
        text = target.label,
        style = MaterialTheme.typography.bodyLarge,
        color = MaterialTheme.colorScheme.onSurface,
    )
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
    ) {
        CircularProgressIndicator(
            modifier = Modifier.size(20.dp),
            strokeWidth = 2.dp,
        )
        Text(
            text = label,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

// ---- pinned header ------------------------------------------------------

@Composable
private fun SearchField(
    query: String,
    onQueryChange: (String) -> Unit,
    onClear: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val focusManager = LocalFocusManager.current
    TextField(
        value = query,
        onValueChange = onQueryChange,
        modifier = modifier
            .fillMaxWidth()
            .heightIn(min = Dimens.ControlHeight)
            .testTag(LocationsTags.SEARCH),
        label = { Text(stringResource(R.string.locations_search_label)) },
        placeholder = { Text(stringResource(R.string.locations_search_placeholder)) },
        leadingIcon = {
            Icon(
                imageVector = Icons.Filled.Search,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        },
        trailingIcon = {
            if (query.isNotEmpty()) {
                IconButton(
                    onClick = onClear,
                    modifier = Modifier.testTag(LocationsTags.SEARCH_CLEAR),
                ) {
                    Icon(
                        imageVector = Icons.Filled.Clear,
                        contentDescription = stringResource(R.string.locations_search_clear),
                    )
                }
            }
        },
        singleLine = true,
        shape = MaterialTheme.shapes.medium,
        colors = TextFieldDefaults.colors(
            focusedContainerColor = MaterialTheme.colorScheme.surfaceContainer,
            unfocusedContainerColor = MaterialTheme.colorScheme.surfaceContainer,
            focusedIndicatorColor = Color.Transparent,
            unfocusedIndicatorColor = Color.Transparent,
        ),
        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
        keyboardActions = KeyboardActions(onSearch = { focusManager.clearFocus() }),
    )
}

@Composable
private fun Toolbar(state: LocationsState, actions: LocationsActions) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space1),
        horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        IconButton(
            onClick = actions.onRefresh,
            enabled = !state.refreshing,
            modifier = Modifier
                .size(Dimens.TouchTarget)
                .testTag(LocationsTags.REFRESH),
        ) {
            if (state.refreshing) {
                CircularProgressIndicator(
                    modifier = Modifier.size(20.dp),
                    strokeWidth = 2.dp,
                    color = MaterialTheme.colorScheme.primary,
                )
            } else {
                Icon(
                    imageVector = Icons.Filled.Refresh,
                    contentDescription = stringResource(R.string.locations_refresh),
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        Spacer(modifier = Modifier.weight(1f))
        FilledTonalButton(
            onClick = actions.onPingAll,
            enabled = state.canPingAll,
            modifier = Modifier
                .heightIn(min = Dimens.TouchTarget)
                .testTag(LocationsTags.PING_ALL),
        ) {
            if (state.pingingAll) {
                CircularProgressIndicator(
                    modifier = Modifier.size(16.dp),
                    strokeWidth = 2.dp,
                    color = MaterialTheme.colorScheme.primary,
                )
                Spacer(modifier = Modifier.size(Dimens.Space1))
            }
            Text(
                text = if (state.pingingAll) {
                    stringResource(R.string.locations_pinging_all, state.pingAllCount)
                } else {
                    stringResource(R.string.locations_ping_all)
                },
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

/**
 * The selection the engine reports. Names come from the tree; an unresolved
 * host or code stays a raw value rather than a guess.
 */
@Composable
private fun SelectionSummary(
    selection: LocationSelection?,
    modifier: Modifier = Modifier,
) {
    Surface(
        modifier = modifier
            .fillMaxWidth()
            .testTag(LocationsTags.SUMMARY),
        shape = MaterialTheme.shapes.medium,
        color = MaterialTheme.colorScheme.surfaceContainer,
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
    ) {
        Column(modifier = Modifier.padding(horizontal = Dimens.Space4, vertical = Dimens.Space3)) {
            Text(
                text = stringResource(R.string.locations_summary_label),
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
            ) {
                if (selection != null) {
                    CountryFlags.Flag(
                        code = selection.countryCode,
                        recommended = selection.recommended,
                    )
                }
                Text(
                    text = selectionText(selection),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

/**
 * `Recommended`, or `Country · City · host:port · protocols`; the raw address
 * when unresolved. City and protocols are omitted when they add nothing.
 */
@Composable
internal fun selectionText(selection: LocationSelection?): String {
    if (selection == null) return stringResource(R.string.locations_selection_none)
    val separator = stringResource(R.string.locations_selection_separator)
    val parts = mutableListOf<String>()
    if (selection.recommended) {
        parts += stringResource(R.string.locations_recommended)
    } else {
        selection.countryName?.let(parts::add)
        selection.cityName?.let(parts::add)
    }
    selection.serverAddress?.let(parts::add)
    if (selection.protocolNames.isNotEmpty()) {
        parts += selection.protocolNames.joinToString(", ")
    }
    return parts.joinToString(separator)
}

// ---- sections and server cards ------------------------------------------

@Composable
private fun ServerList(
    state: LocationsState,
    actions: LocationsActions,
    listState: LazyListState,
) {
    val sections = state.sections
    LazyColumn(
        state = listState,
        modifier = Modifier
            .fillMaxSize()
            .testTag(LocationsTags.LIST),
        contentPadding = PaddingValues(
            start = Dimens.ScreenPadding,
            end = Dimens.ScreenPadding,
            top = Dimens.Space2,
            bottom = Dimens.Space4,
        ),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space2),
    ) {
        item(key = "fastest") {
            FastestRow(
                fastest = sections.fastest,
                saving = state.savingFastest,
                enabled = state.canSelectFastest,
                onSelect = actions.onSelectFastest,
            )
        }
        if (sections.favorites.isNotEmpty()) {
            item(key = "favorites_heading") {
                SectionHeading(
                    text = stringResource(R.string.locations_favorites),
                    tag = LocationsTags.FAVORITES_HEADING,
                )
            }
            items(items = sections.favorites, key = { "favorite_${it.key}" }) { row ->
                ServerRow(
                    row = row,
                    latency = state.latencyOf(row),
                    selected = state.isSelected(row.country, row.city, row.server),
                    saved = state.isSaving(row.server),
                    favorite = true,
                    onSelect = actions.onSelect,
                    onFavorite = { actions.onToggleFavorite(row) },
                )
            }
        }
        if (sections.recents.isNotEmpty()) {
            item(key = "recent_heading") {
                RecentHeading(onClear = actions.onClearRecents)
            }
            items(items = sections.recents, key = { "recent_${it.key}" }) { row ->
                ServerRow(
                    row = row,
                    latency = state.latencyOf(row),
                    selected = state.isSelected(row.country, row.city, row.server),
                    saved = state.isSaving(row.server),
                    favorite = state.isFavorite(row),
                    onSelect = actions.onSelect,
                    onFavorite = { actions.onToggleFavorite(row) },
                )
            }
        }
        item(key = "locations_heading") {
            SectionHeading(
                text = stringResource(R.string.locations_full_list),
                tag = LocationsTags.LIST_HEADING,
            )
        }
        items(items = sections.rows, key = { "row_${it.key}" }) { row ->
            ServerRow(
                row = row,
                latency = state.latencyOf(row),
                selected = state.isSelected(row.country, row.city, row.server),
                saved = state.isSaving(row.server),
                favorite = state.isFavorite(row),
                onSelect = actions.onSelect,
                onFavorite = { actions.onToggleFavorite(row) },
            )
        }
        item(key = "locked_footer") {
            LockedFooter(
                allowLocked = state.allowLocked,
                saving = state.savingLocked,
                onToggle = actions.onToggleLocked,
            )
        }
    }
}

@Composable
private fun SectionHeading(text: String, tag: String? = null) {
    KumihoSectionHeader(
        title = text,
        modifier = Modifier
            .padding(top = Dimens.Space4)
            .semantics { heading() }
            .then(if (tag != null) Modifier.testTag(tag) else Modifier),
        contentPadding = PaddingValues(horizontal = 0.dp, vertical = Dimens.Space2),
    )
}

@Composable
private fun RecentHeading(onClear: () -> Unit) {
    KumihoSectionHeader(
        title = stringResource(R.string.locations_recent),
        modifier = Modifier
            .padding(top = Dimens.Space4)
            .semantics { heading() }
            .testTag(LocationsTags.RECENT_HEADING),
        contentPadding = PaddingValues(horizontal = 0.dp, vertical = Dimens.Space2),
        trailing = {
            TextButton(
                onClick = onClear,
                modifier = Modifier
                    .heightIn(min = Dimens.TouchTarget)
                    .testTag(LocationsTags.CLEAR_RECENTS),
            ) {
                Text(stringResource(R.string.locations_clear_recents))
            }
        },
    )
}

/** Latency as a tonal pill; the word or number stays the only signal. */
@Composable
private fun LatencyChip(text: String, modifier: Modifier = Modifier) {
    Surface(
        modifier = modifier,
        shape = PillShape,
        color = MaterialTheme.colorScheme.surfaceContainerHigh,
    ) {
        Text(
            text = text,
            style = MaterialTheme.typography.labelMedium.copy(fontFeatureSettings = "tnum"),
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(horizontal = Dimens.Space2, vertical = 2.dp),
        )
    }
}

/**
 * The pinned Fastest shortcut: the lowest fresh finite RTT, or an honest
 * disabled hint until a Ping all has produced one.
 */
@Composable
private fun FastestRow(
    fastest: FastestLocation?,
    saving: Boolean,
    enabled: Boolean,
    onSelect: () -> Unit,
) {
    val shape = MaterialTheme.shapes.medium
    Surface(
        shape = shape,
        color = MaterialTheme.colorScheme.surfaceContainer,
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
        modifier = Modifier
            .fillMaxWidth()
            .defaultMinSize(minHeight = Dimens.RowHeight)
            .then(
                if (enabled) {
                    Modifier.clickable(role = Role.Button, onClick = onSelect)
                } else {
                    Modifier
                },
            )
            .semantics { if (!enabled) disabled() }
            .testTag(LocationsTags.FASTEST),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = Dimens.Space4, vertical = Dimens.Space2),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space3),
        ) {
            if (fastest != null) {
                CountryFlags.Flag(
                    code = fastest.row.country.code,
                    recommended = LocationTree.isRecommended(fastest.row.country),
                )
            } else {
                Spacer(modifier = Modifier.size(24.dp, 18.dp))
            }
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = stringResource(R.string.locations_fastest),
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Text(
                    text = if (fastest != null) {
                        fastest.row.title + LocationTree.SEPARATOR + fastest.row.address
                    } else {
                        stringResource(R.string.locations_fastest_hint)
                    },
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            when {
                saving -> CircularProgressIndicator(
                    modifier = Modifier.size(16.dp),
                    strokeWidth = 2.dp,
                    color = MaterialTheme.colorScheme.primary,
                )
                fastest != null -> LatencyChip(
                    text = stringResource(R.string.locations_latency, fastest.milliseconds),
                )
            }
        }
    }
}

@Composable
private fun LockedFooter(
    allowLocked: Boolean,
    saving: Boolean,
    onToggle: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = Dimens.Space3),
        horizontalArrangement = Arrangement.Center,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        TextButton(
            onClick = onToggle,
            enabled = !saving,
            modifier = Modifier
                .heightIn(min = Dimens.TouchTarget)
                .testTag(LocationsTags.LOCKED_TOGGLE),
        ) {
            if (saving) {
                CircularProgressIndicator(
                    modifier = Modifier.size(16.dp),
                    strokeWidth = 2.dp,
                    color = MaterialTheme.colorScheme.primary,
                )
                Spacer(modifier = Modifier.size(Dimens.Space1))
            }
            Text(
                stringResource(
                    if (allowLocked) {
                        R.string.locations_hide_locked
                    } else {
                        R.string.locations_show_locked
                    },
                ),
            )
        }
    }
}

/**
 * One server card: the flag, the full `Country · City`, the second line
 * `hostname:port · protocols`, the locked badge, the latency/check trailing,
 * and the separate 48 dp favorite star. The row body remains single-tap.
 */
@Composable
private fun ServerRow(
    row: LocationRow,
    latency: LatencyState,
    selected: Boolean,
    saved: Boolean,
    favorite: Boolean,
    onSelect: (Country, City, Server) -> Unit,
    onFavorite: () -> Unit,
) {
    val server = row.server
    val favoriteDescription = stringResource(
        if (favorite) R.string.locations_remove_favorite else R.string.locations_favorite,
        row.title,
    )
    val scheme = MaterialTheme.colorScheme
    Surface(
        shape = MaterialTheme.shapes.medium,
        color = if (selected) {
            lerp(scheme.surfaceContainer, scheme.primaryContainer, 0.22f)
        } else {
            scheme.surfaceContainer
        },
        border = BorderStroke(
            1.dp,
            if (selected) scheme.primary else scheme.outlineVariant,
        ),
        modifier = Modifier.fillMaxWidth(),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Row(
                modifier = Modifier
                    .weight(1f)
                    .defaultMinSize(minHeight = Dimens.RowHeight)
                    .semantics(mergeDescendants = true) { this.selected = selected }
                    .then(
                        if (!saved) {
                            Modifier.clickable(role = Role.Button) {
                                onSelect(row.country, row.city, server)
                            }
                        } else {
                            Modifier
                        },
                    )
                    .testTag(serverRowTag(row.country.code, row.city.code, row.address))
                    .padding(
                        start = Dimens.Space4,
                        end = Dimens.Space1,
                        top = Dimens.Space2,
                        bottom = Dimens.Space2,
                    ),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(Dimens.Space3),
            ) {
                CountryFlags.Flag(
                    code = row.country.code,
                    recommended = LocationTree.isRecommended(row.country),
                )
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = row.title,
                        style = MaterialTheme.typography.bodyLarge,
                        color = MaterialTheme.colorScheme.onSurface,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Text(
                        text = row.detail,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    if (row.country.locked) {
                        KumihoStatusChip(
                            tone = StatusTone.Warning,
                            label = stringResource(R.string.locations_locked),
                        )
                    }
                }
                if (saved) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(16.dp),
                        strokeWidth = 2.dp,
                        color = MaterialTheme.colorScheme.primary,
                    )
                } else {
                    LatencyChip(text = latencyText(latency))
                    if (selected) {
                        Icon(
                            imageVector = Icons.Filled.Check,
                            contentDescription = null,
                            tint = MaterialTheme.colorScheme.primary,
                            modifier = Modifier.size(20.dp),
                        )
                    }
                }
            }
            IconButton(
                onClick = onFavorite,
                enabled = !saved,
                modifier = Modifier
                    .size(Dimens.TouchTarget)
                    .semantics {
                        contentDescription = favoriteDescription
                        this.selected = favorite
                    }
                    .testTag(LocationsTags.favorite(row)),
            ) {
                Icon(
                    imageVector = if (favorite) Icons.Filled.Star else Icons.Outlined.Star,
                    contentDescription = null,
                    tint = if (favorite) {
                        MaterialTheme.colorScheme.primary
                    } else {
                        MaterialTheme.colorScheme.onSurfaceVariant
                    },
                )
            }
        }
    }
}

@Composable
internal fun latencyText(latency: LatencyState): String = when (latency) {
    LatencyState.Untested -> stringResource(R.string.locations_latency_none)
    LatencyState.Timeout -> stringResource(R.string.locations_latency_timeout)
    is LatencyState.Measured -> stringResource(R.string.locations_latency, latency.milliseconds)
}

// ---- notices and content states ----------------------------------------

/** The one notice with its tone, words, detail and the one retry it offers. */
@Composable
private fun NoticeBanner(
    notice: LocationsNotice,
    onAction: () -> Unit,
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val colors = LocalKumihoColors.current
    val tint = when (notice.tone) {
        LocationsNoticeTone.WARNING -> colors.warning
        LocationsNoticeTone.ERROR -> colors.loss
    }
    val glyph = when (notice.tone) {
        LocationsNoticeTone.WARNING -> Icons.Filled.Warning
        LocationsNoticeTone.ERROR -> Icons.Filled.Clear
    }
    val detail = (notice.text as? LocationsNoticeText.Local)?.detail
    Surface(
        modifier = modifier
            .fillMaxWidth()
            .testTag(LocationsTags.NOTICE),
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
                    imageVector = glyph,
                    contentDescription = null,
                    tint = tint,
                    modifier = Modifier
                        .padding(top = 2.dp)
                        .size(20.dp),
                )
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = noticeText(notice.text),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    if (!detail.isNullOrBlank()) {
                        Text(
                            text = detail,
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
                if (notice.action != LocationsNoticeAction.NONE) {
                    TextButton(
                        onClick = onAction,
                        modifier = Modifier
                            .heightIn(min = Dimens.TouchTarget)
                            .testTag(LocationsTags.NOTICE_ACTION),
                    ) {
                        Text(stringResource(R.string.locations_retry))
                    }
                }
                TextButton(
                    onClick = onDismiss,
                    modifier = Modifier
                        .heightIn(min = Dimens.TouchTarget)
                        .testTag(LocationsTags.NOTICE_DISMISS),
                ) {
                    Text(stringResource(R.string.locations_dismiss))
                }
            }
        }
    }
}

@Composable
internal fun noticeText(text: LocationsNoticeText): String = when (text) {
    is LocationsNoticeText.Raw -> text.text
    is LocationsNoticeText.Local -> stringResource(messageResource(text.message))
}

private fun messageResource(message: LocationsMessage): Int = when (message) {
    LocationsMessage.REFRESH_FAILED -> R.string.locations_message_refresh_failed
    LocationsMessage.SELECT_FAILED -> R.string.locations_message_select_failed
    LocationsMessage.PING_ALL_FAILED -> R.string.locations_message_ping_all_failed
    LocationsMessage.PING_ALL_NO_RESULTS -> R.string.locations_message_ping_all_no_results
}

@Composable
private fun LoadingContent() {
    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        KumihoLoadingState(
            label = stringResource(R.string.locations_loading),
            modifier = Modifier.testTag(LocationsTags.LOADING),
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
            title = stringResource(R.string.locations_load_failed_title),
            message = error,
            onRetry = onRetry,
            modifier = Modifier.testTag(LocationsTags.ERROR),
        )
    }
}

@Composable
private fun EmptyContent(onRefresh: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .testTag(LocationsTags.EMPTY),
        verticalArrangement = Arrangement.Center,
    ) {
        KumihoEmptyState(
            title = stringResource(R.string.locations_empty_title),
            message = stringResource(R.string.locations_empty_message),
            icon = painterResource(R.drawable.ic_globe),
            action = {
                TextButton(
                    onClick = onRefresh,
                    modifier = Modifier.heightIn(min = Dimens.TouchTarget),
                ) {
                    Text(stringResource(R.string.locations_refresh_action))
                }
            },
        )
    }
}

@Composable
private fun NoMatchContent(query: String, onClear: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .testTag(LocationsTags.NO_MATCH),
        verticalArrangement = Arrangement.Center,
    ) {
        KumihoEmptyState(
            title = stringResource(R.string.locations_no_match_title),
            message = stringResource(R.string.locations_no_match_message, query.trim()),
            icon = rememberVectorPainter(Icons.Filled.Search),
            action = {
                TextButton(
                    onClick = onClear,
                    modifier = Modifier
                        .heightIn(min = Dimens.TouchTarget)
                        .testTag(LocationsTags.NO_MATCH_CLEAR),
                ) {
                    Text(stringResource(R.string.locations_search_clear))
                }
            },
        )
    }
}

/** Themed loading while the service binds, or the engine error it reported. */
@Composable
private fun LocationsWaitingSurface(error: String?, modifier: Modifier = Modifier) {
    Surface(
        modifier = modifier.fillMaxSize(),
        color = MaterialTheme.colorScheme.background,
    ) {
        Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            if (error == null) {
                KumihoLoadingState(
                    label = stringResource(R.string.locations_loading),
                    modifier = Modifier.testTag(LocationsTags.LOADING),
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
                        text = stringResource(R.string.locations_engine_unavailable_title),
                        style = MaterialTheme.typography.titleLarge,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    Text(
                        text = stringResource(R.string.locations_engine_unavailable),
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
