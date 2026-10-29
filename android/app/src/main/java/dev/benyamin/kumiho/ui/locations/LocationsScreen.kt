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
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.rememberVectorPainter
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
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
import dev.benyamin.kumiho.ui.theme.Dimens
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors

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
)

/**
 * The stateless Locations screen: a pinned search field and toolbar, the
 * current selection, the notice banner and one flat server list.
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
    OutlinedTextField(
        value = query,
        onValueChange = onQueryChange,
        modifier = modifier
            .fillMaxWidth()
            .testTag(LocationsTags.SEARCH),
        label = { Text(stringResource(R.string.locations_search_label)) },
        placeholder = { Text(stringResource(R.string.locations_search_placeholder)) },
        singleLine = true,
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
        horizontalArrangement = Arrangement.spacedBy(Dimens.Space1),
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
        ToolbarAction(
            label = if (state.pingingAll) {
                stringResource(R.string.locations_pinging_all, state.pingAllCount)
            } else {
                stringResource(R.string.locations_ping_all)
            },
            tag = LocationsTags.PING_ALL,
            enabled = state.canPingAll,
            loading = state.pingingAll,
            onClick = actions.onPingAll,
            modifier = Modifier.weight(1f),
        )
    }
}

/** One equal-width action in the toolbar row, with an optional spinner. */
@Composable
private fun ToolbarAction(
    label: String,
    tag: String,
    enabled: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    loading: Boolean = false,
) {
    TextButton(
        onClick = onClick,
        enabled = enabled,
        contentPadding = PaddingValues(horizontal = Dimens.Space2, vertical = Dimens.Space2),
        modifier = modifier
            .heightIn(min = Dimens.TouchTarget)
            .testTag(tag),
    ) {
        if (loading) {
            CircularProgressIndicator(
                modifier = Modifier.size(16.dp),
                strokeWidth = 2.dp,
                color = MaterialTheme.colorScheme.primary,
            )
            Spacer(modifier = Modifier.size(Dimens.Space1))
        }
        Text(
            text = label,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
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
                        width = 20.dp,
                        height = 15.dp,
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

// ---- flat server list ---------------------------------------------------

@Composable
private fun ServerList(
    state: LocationsState,
    actions: LocationsActions,
    listState: LazyListState,
) {
    LazyColumn(
        state = listState,
        modifier = Modifier
            .fillMaxSize()
            .testTag(LocationsTags.LIST),
        contentPadding = PaddingValues(
            start = Dimens.ScreenPadding,
            end = Dimens.ScreenPadding,
            top = Dimens.Space1,
            bottom = Dimens.Space4,
        ),
    ) {
        items(items = state.rows, key = { it.key }) { row ->
            ServerRow(
                row = row,
                latency = state.latencyOf(row),
                selected = state.isSelected(row.country, row.city, row.server),
                saving = state.isSaving(row.server),
                onSelect = actions.onSelect,
            )
        }
    }
}

/**
 * One server: the flag, the full `Country · City`, the second line
 * `hostname:port · protocols`, and the latency/check trailing. The row is
 * intentionally a single tap target; there is nothing to expand.
 */
@Composable
private fun ServerRow(
    row: LocationRow,
    latency: LatencyState,
    selected: Boolean,
    saving: Boolean,
    onSelect: (Country, City, Server) -> Unit,
) {
    val server = row.server
    val enabled = !saving
    val background = if (selected) {
        MaterialTheme.colorScheme.primary.copy(alpha = 0.12f)
    } else {
        MaterialTheme.colorScheme.surface.copy(alpha = 0f)
    }
    Surface(
        color = background,
        modifier = Modifier
            .fillMaxWidth()
            .defaultMinSize(minHeight = Dimens.RowHeight)
            .semantics(mergeDescendants = true) { this.selected = selected }
            .then(
                if (enabled) {
                    Modifier.clickable(role = Role.Button) { onSelect(row.country, row.city, server) }
                } else {
                    Modifier
                },
            )
            .testTag(serverRowTag(row.country.code, row.city.code, row.address)),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(start = Dimens.Space2, end = Dimens.Space4, top = Dimens.Space1, bottom = Dimens.Space1),
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
            }
            if (saving) {
                CircularProgressIndicator(
                    modifier = Modifier.size(16.dp),
                    strokeWidth = 2.dp,
                    color = MaterialTheme.colorScheme.primary,
                )
            } else {
                Text(
                    text = latencyText(latency),
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
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
