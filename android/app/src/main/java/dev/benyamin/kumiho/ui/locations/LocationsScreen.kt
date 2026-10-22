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
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Clear
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.KeyboardArrowDown
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
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.rememberVectorPainter
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
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
import dev.benyamin.kumiho.ui.components.KumihoRow
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

    LocationsScreen(
        state = state,
        actions = LocationsActions(
            onQueryChange = locationsViewModel::onQueryChange,
            onClearQuery = locationsViewModel::clearQuery,
            onToggleCountry = locationsViewModel::toggleCountry,
            onToggleCity = locationsViewModel::toggleCity,
            onSelect = locationsViewModel::select,
            onRefresh = locationsViewModel::refresh,
            onRetryLoad = locationsViewModel::retryLoad,
            onTestSpeed = locationsViewModel::testSpeed,
            onDismissNotice = locationsViewModel::dismissNotice,
            onRetryNotice = locationsViewModel::retryNotice,
        ),
        modifier = modifier,
    )
}

/** Every callback the stateless Locations screen needs; grouped to stay readable. */
data class LocationsActions(
    val onQueryChange: (String) -> Unit,
    val onClearQuery: () -> Unit,
    val onToggleCountry: (String) -> Unit,
    val onToggleCity: (String, String) -> Unit,
    val onSelect: (Country, City, Server) -> Unit,
    val onRefresh: () -> Unit,
    val onRetryLoad: () -> Unit,
    val onTestSpeed: () -> Unit,
    val onDismissNotice: () -> Unit,
    val onRetryNotice: () -> Unit,
)

/**
 * The stateless Locations screen: a pinned search field and toolbar, the
 * current selection, the connected hint, the notice banner and the full
 * country/city/server tree.
 *
 * It makes no engine, service or Android call, so a Compose test renders it
 * from fake state. Nothing here selects or connects on its own: a server row
 * tap is a request, and the check mark only follows the service status.
 */
@Composable
fun LocationsScreen(
    state: LocationsState,
    actions: LocationsActions,
    modifier: Modifier = Modifier,
) {
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
        Toolbar(
            refreshing = state.refreshing,
            testing = state.testing,
            canTest = state.canTest,
            targetCount = state.testTargets.size,
            onRefresh = actions.onRefresh,
            onTestSpeed = actions.onTestSpeed,
        )
        SelectionSummary(
            selection = state.selection,
            modifier = Modifier.padding(
                start = Dimens.ScreenPadding,
                end = Dimens.ScreenPadding,
                top = Dimens.Space2,
            ),
        )
        if (state.sessionActive) {
            InlineBanner(
                icon = Icons.Filled.Info,
                tint = MaterialTheme.colorScheme.primary,
                text = stringResource(R.string.locations_connected_hint),
                modifier = Modifier
                    .padding(
                        start = Dimens.ScreenPadding,
                        end = Dimens.ScreenPadding,
                        top = Dimens.Space2,
                    )
                    .testTag(LocationsTags.CONNECTED_HINT),
            )
        }
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
                else -> Tree(
                    state = state,
                    actions = actions,
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
private fun Toolbar(
    refreshing: Boolean,
    testing: Boolean,
    canTest: Boolean,
    targetCount: Int,
    onRefresh: () -> Unit,
    onTestSpeed: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space1),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        IconButton(
            onClick = onRefresh,
            enabled = !refreshing,
            modifier = Modifier
                .size(Dimens.TouchTarget)
                .testTag(LocationsTags.REFRESH),
        ) {
            if (refreshing) {
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
        Box(modifier = Modifier.weight(1f))
        Column(horizontalAlignment = Alignment.End) {
            TextButton(
                onClick = onTestSpeed,
                enabled = canTest,
                modifier = Modifier
                    .heightIn(min = Dimens.TouchTarget)
                    .testTag(LocationsTags.TEST_SPEED),
            ) {
                if (testing) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(16.dp),
                        strokeWidth = 2.dp,
                        color = MaterialTheme.colorScheme.primary,
                    )
                    Spacer(modifier = Modifier.size(Dimens.Space1))
                }
                Text(
                    text = if (testing) {
                        stringResource(R.string.locations_testing)
                    } else {
                        stringResource(R.string.locations_test_speed)
                    },
                )
            }
            Text(
                text = if (targetCount == 0) {
                    stringResource(R.string.locations_test_helper_none)
                } else {
                    pluralStringResource(R.plurals.locations_test_helper, targetCount, targetCount)
                },
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                textAlign = TextAlign.End,
                modifier = Modifier.testTag(LocationsTags.TEST_HELPER),
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
            val plain = selectionText(selection)
            val flag = selection?.flag.orEmpty()
            Text(
                text = if (flag.isEmpty()) plain else "$flag $plain",
                modifier = if (flag.isEmpty()) {
                    Modifier
                } else {
                    Modifier.clearAndSetSemantics { contentDescription = plain }
                },
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurface,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

/** `Recommended`, or `Country · City · host`; the raw address when unresolved. */
@Composable
private fun selectionText(selection: LocationSelection?): String {
    if (selection == null) return stringResource(R.string.locations_selection_none)
    val separator = stringResource(R.string.locations_selection_separator)
    val parts = mutableListOf<String>()
    if (selection.recommended) {
        parts += stringResource(R.string.locations_recommended)
    } else {
        selection.countryName?.let(parts::add)
        selection.cityName?.let(parts::add)
    }
    selection.serverName?.let(parts::add)
    return parts.joinToString(separator)
}

/** A quiet informational banner: icon, words, no action. */
@Composable
private fun InlineBanner(
    icon: ImageVector,
    tint: Color,
    text: String,
    modifier: Modifier = Modifier,
) {
    Surface(
        modifier = modifier.fillMaxWidth(),
        shape = MaterialTheme.shapes.medium,
        color = tint.copy(alpha = 0.1f),
        border = BorderStroke(1.dp, tint.copy(alpha = 0.5f)),
    ) {
        Row(
            modifier = Modifier.padding(Dimens.Space3),
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
            verticalAlignment = Alignment.Top,
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
            )
        }
    }
}

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
private fun noticeText(text: LocationsNoticeText): String = when (text) {
    is LocationsNoticeText.Raw -> text.text
    is LocationsNoticeText.Local -> stringResource(messageResource(text.message))
}

private fun messageResource(message: LocationsMessage): Int = when (message) {
    LocationsMessage.REFRESH_FAILED -> R.string.locations_message_refresh_failed
    LocationsMessage.SELECT_FAILED -> R.string.locations_message_select_failed
    LocationsMessage.TEST_FAILED -> R.string.locations_message_test_failed
}

// ---- content states -----------------------------------------------------

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

// ---- the tree -----------------------------------------------------------

@Composable
private fun Tree(state: LocationsState, actions: LocationsActions) {
    LazyColumn(
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
            when (row) {
                is LocationRow.CountryRow -> CountryRow(
                    row = row,
                    searching = state.searching,
                    onToggle = actions.onToggleCountry,
                )
                is LocationRow.CityRow -> CityRow(
                    row = row,
                    searching = state.searching,
                    onToggle = actions.onToggleCity,
                )
                is LocationRow.ServerRow -> ServerRow(
                    row = row,
                    latency = state.latencyOf(row.server),
                    selected = state.isSelected(row.country, row.city, row.server),
                    saving = state.isSaving(row.server),
                    onSelect = actions.onSelect,
                )
            }
        }
    }
}

@Composable
private fun CountryRow(
    row: LocationRow.CountryRow,
    searching: Boolean,
    onToggle: (String) -> Unit,
) {
    val name = if (LocationTree.isRecommended(row.country)) {
        stringResource(R.string.locations_recommended)
    } else {
        row.country.name
    }
    val flag = CountryFlags.emoji(row.country.code)
    KumihoRow(
        title = name,
        titlePrefix = flag.takeIf { it.isNotEmpty() },
        modifier = Modifier
            .semantics { heading() }
            .testTag(countryRowTag(row.country.code)),
        onClick = if (searching) null else ({ onToggle(row.country.code) }),
        trailing = if (searching) {
            null
        } else {
            { ExpandGlyph(expanded = row.expanded) }
        },
    )
}

@Composable
private fun CityRow(
    row: LocationRow.CityRow,
    searching: Boolean,
    onToggle: (String, String) -> Unit,
) {
    KumihoRow(
        title = row.city.name,
        subtitle = pluralStringResource(
            R.plurals.locations_city_servers,
            row.city.servers.size,
            row.city.servers.size,
        ),
        modifier = Modifier
            .padding(start = Dimens.Space4)
            .testTag(cityRowTag(row.country.code, row.city.code)),
        onClick = if (searching) null else ({ onToggle(row.country.code, row.city.code) }),
        trailing = if (searching) {
            null
        } else {
            { ExpandGlyph(expanded = row.expanded) }
        },
    )
}

@Composable
private fun ExpandGlyph(expanded: Boolean) {
    Icon(
        imageVector = if (expanded) {
            Icons.Filled.KeyboardArrowDown
        } else {
            Icons.AutoMirrored.Filled.KeyboardArrowRight
        },
        contentDescription = stringResource(
            if (expanded) R.string.locations_collapse else R.string.locations_expand,
        ),
        tint = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.size(24.dp),
    )
}

/**
 * One server: the host name (ellipsized, never taking the latency or the check
 * with it), the protocol/port line, a quarantine mark, and the latency.
 */
@Composable
private fun ServerRow(
    row: LocationRow.ServerRow,
    latency: LatencyState,
    selected: Boolean,
    saving: Boolean,
    onSelect: (Country, City, Server) -> Unit,
) {
    val server = row.server
    val enabled = !server.quarantined && !saving
    val background = if (selected) {
        MaterialTheme.colorScheme.primary.copy(alpha = 0.12f)
    } else {
        Color.Transparent
    }
    Surface(
        color = background,
        modifier = Modifier
            .fillMaxWidth()
            .defaultMinSize(minHeight = Dimens.RowHeight)
            .semantics { this.selected = selected }
            .then(
                if (enabled) {
                    Modifier.clickable(role = Role.Button) { onSelect(row.country, row.city, server) }
                } else {
                    Modifier
                },
            )
            .testTag(serverRowTag(row.country.code, row.city.code, LocationTree.addressOf(server))),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(start = Dimens.Space6, end = Dimens.Space4, top = Dimens.Space2, bottom = Dimens.Space2),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space3),
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = server.hostname,
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurface,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                ServerDetail(server)
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
                        contentDescription = stringResource(R.string.locations_selected),
                        tint = MaterialTheme.colorScheme.primary,
                        modifier = Modifier.size(20.dp),
                    )
                }
            }
        }
    }
}

/** `connect, quic · port 2499`, plus the honest quarantine mark. */
@Composable
private fun ServerDetail(server: Server) {
    val separator = stringResource(R.string.locations_selection_separator)
    val protocols = server.protocols
        .map { it.name.trim() }
        .filter { it.isNotEmpty() }
        .distinct()
        .joinToString(", ")
    val detail = if (protocols.isEmpty()) {
        stringResource(R.string.locations_server_port, server.port)
    } else {
        stringResource(R.string.locations_server_protocols, protocols, server.port)
    }
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Dimens.Space1),
    ) {
        Text(
            text = detail,
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f, fill = false),
        )
        if (server.quarantined) {
            Text(
                text = separator + stringResource(R.string.locations_quarantined),
                style = MaterialTheme.typography.bodySmall,
                color = LocalKumihoColors.current.warning,
                maxLines = 1,
            )
        }
    }
}

@Composable
private fun latencyText(latency: LatencyState): String = when (latency) {
    LatencyState.Untested -> stringResource(R.string.locations_latency_none)
    LatencyState.Timeout -> stringResource(R.string.locations_latency_timeout)
    is LatencyState.Measured -> stringResource(R.string.locations_latency, latency.milliseconds)
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
