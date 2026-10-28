package dev.benyamin.kumiho.ui.tv.locations

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
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Text
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Server
import dev.benyamin.kumiho.ui.locations.CountryFlags
import dev.benyamin.kumiho.ui.locations.LatencyState
import dev.benyamin.kumiho.ui.locations.LocationRow
import dev.benyamin.kumiho.ui.locations.LocationTree
import dev.benyamin.kumiho.ui.locations.LocationsActions
import dev.benyamin.kumiho.ui.locations.LocationsNotice
import dev.benyamin.kumiho.ui.locations.LocationsNoticeAction
import dev.benyamin.kumiho.ui.locations.LocationsNoticeTone
import dev.benyamin.kumiho.ui.locations.LocationsRoute
import dev.benyamin.kumiho.ui.locations.LocationsState
import dev.benyamin.kumiho.ui.locations.LocationsTags
import dev.benyamin.kumiho.ui.locations.countryRowTag
import dev.benyamin.kumiho.ui.locations.latencyText
import dev.benyamin.kumiho.ui.locations.noticeText
import dev.benyamin.kumiho.ui.locations.selectionText
import dev.benyamin.kumiho.ui.locations.serverRowTag
import dev.benyamin.kumiho.ui.theme.KumihoShapes
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors
import dev.benyamin.kumiho.ui.tv.components.TvButton
import dev.benyamin.kumiho.ui.tv.components.TvFocusableSurface
import dev.benyamin.kumiho.ui.tv.components.TvPageHeader
import dev.benyamin.kumiho.ui.tv.theme.TvDimens
import dev.benyamin.kumiho.ui.tv.theme.TvM3Typography

/** The television Locations entry; the phone route owns the engine and view model. */
@Composable
fun TvLocationsRoute(container: AppContainer, modifier: Modifier = Modifier) {
    LocationsRoute(container = container, modifier = modifier) { state, actions ->
        TvLocationsScreen(state = state, actions = actions, modifier = modifier)
    }
}

/**
 * The ten-foot location tree: a search row, refresh/ping/expand controls, the
 * saved selection and the whole country/server list as focusable rows.
 */
@Composable
fun TvLocationsScreen(
    state: LocationsState,
    actions: LocationsActions,
    modifier: Modifier = Modifier,
) {
    val colors = MaterialTheme.colorScheme
    val firstFocus = remember { FocusRequester() }
    LaunchedEffect(state.rows.firstOrNull()?.key, state.showLoading, state.showError) {
        if (state.rows.isNotEmpty()) {
            runCatching { firstFocus.requestFocus() }
        }
    }

    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(TvDimens.Space2)
            .testTag(LocationsTags.SCREEN),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
    ) {
        TvPageHeader(title = stringResource(R.string.tv_locations_title))
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            SearchField(
                query = state.query,
                onQueryChange = actions.onQueryChange,
                onClear = actions.onClearQuery,
                modifier = Modifier.weight(1f),
            )
            TvButton(
                label = stringResource(R.string.locations_refresh),
                onClick = actions.onRefresh,
                modifier = Modifier.testTag(LocationsTags.REFRESH),
                enabled = !state.refreshing,
            )
            TvButton(
                label = if (state.pingingAll) {
                    stringResource(R.string.locations_pinging_all, state.pingAllCount)
                } else {
                    stringResource(R.string.locations_ping_all)
                },
                onClick = actions.onPingAll,
                modifier = Modifier.testTag(LocationsTags.PING_ALL),
                enabled = state.canPingAll,
            )
            TvButton(
                label = stringResource(
                    if (state.anyExpanded) R.string.locations_collapse_all else R.string.locations_expand_all,
                ),
                onClick = actions.onToggleAll,
                modifier = Modifier.testTag(LocationsTags.TOGGLE_ALL),
                enabled = state.canToggleAll,
            )
        }

        state.notice?.let { notice ->
            NoticeBanner(
                notice = notice,
                onAction = actions.onRetryNotice,
                onDismiss = actions.onDismissNotice,
            )
        }

        SelectionSummary(state = state)
        if (state.sessionActive) {
            Text(
                text = stringResource(R.string.locations_connected_hint),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
                modifier = Modifier.testTag(LocationsTags.CONNECTED_HINT),
            )
        }

        when {
            state.showLoading -> CenterMessage(
                text = stringResource(R.string.locations_loading),
                tag = LocationsTags.LOADING,
                modifier = Modifier.weight(1f),
            )
            state.showError -> Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(TvDimens.Space2)
                    .testTag(LocationsTags.ERROR),
                verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
            ) {
                Text(
                    text = stringResource(R.string.locations_load_failed_title),
                    style = MaterialTheme.typography.titleLarge,
                    color = colors.onSurface,
                )
                Text(
                    text = state.loadError.orEmpty(),
                    style = MaterialTheme.typography.bodyMedium,
                    color = colors.onSurfaceVariant,
                )
                TvButton(
                    label = stringResource(R.string.locations_retry),
                    onClick = actions.onRetryLoad,
                    modifier = Modifier.testTag(LocationsTags.ERROR),
                )
            }
            state.showEmpty -> Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(TvDimens.Space2)
                    .testTag(LocationsTags.EMPTY),
                verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
            ) {
                Text(
                    text = stringResource(R.string.locations_empty_title),
                    style = MaterialTheme.typography.titleLarge,
                    color = colors.onSurface,
                )
                Text(
                    text = stringResource(R.string.locations_empty_message),
                    style = MaterialTheme.typography.bodyMedium,
                    color = colors.onSurfaceVariant,
                )
                TvButton(
                    label = stringResource(R.string.locations_refresh_action),
                    onClick = actions.onRefresh,
                )
            }
            state.showNoMatch -> Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(TvDimens.Space2)
                    .testTag(LocationsTags.NO_MATCH),
                verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
            ) {
                Text(
                    text = stringResource(R.string.locations_no_match_title),
                    style = MaterialTheme.typography.titleLarge,
                    color = colors.onSurface,
                )
                Text(
                    text = stringResource(R.string.locations_no_match_message, state.query.trim()),
                    style = MaterialTheme.typography.bodyMedium,
                    color = colors.onSurfaceVariant,
                )
                TvButton(
                    label = stringResource(R.string.locations_search_clear),
                    onClick = actions.onClearQuery,
                    modifier = Modifier.testTag(LocationsTags.NO_MATCH_CLEAR),
                )
            }
            else -> Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f)
                    .verticalScroll(rememberScrollState())
                    .testTag(LocationsTags.LIST),
                verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
            ) {
                state.rows.forEachIndexed { index, row ->
                    when (row) {
                        is LocationRow.CountryRow -> CountryRow(
                            row = row,
                            searching = state.searching,
                            latency = state.countryLatency(row.country),
                            onToggle = actions.onToggleCountry,
                            modifier = if (index == 0) {
                                Modifier.focusRequester(firstFocus)
                            } else {
                                Modifier
                            },
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
    }
}

@Composable
private fun SearchField(
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
            label = { androidx.compose.material3.Text(stringResource(R.string.locations_search_label)) },
            singleLine = true,
            trailingIcon = {
                if (query.isNotEmpty()) {
                    androidx.compose.material3.TextButton(
                        onClick = onClear,
                        modifier = Modifier.testTag(LocationsTags.SEARCH_CLEAR),
                    ) {
                        androidx.compose.material3.Text(stringResource(R.string.locations_search_clear))
                    }
                }
            },
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
            keyboardActions = KeyboardActions(onSearch = { focusManager.clearFocus() }),
            modifier = modifier
                .heightIn(min = 64.dp)
                .testTag(LocationsTags.SEARCH),
        )
    }
}

@Composable
private fun SelectionSummary(state: LocationsState) {
    val colors = MaterialTheme.colorScheme
    val selection = state.selection
    val text = selectionText(selection)
    val flag = selection?.flag.orEmpty()
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(colors.surface)
            .border(1.dp, colors.borderVariant, MaterialTheme.shapes.medium)
            .padding(horizontal = TvDimens.Space2, vertical = TvDimens.Space1)
            .testTag(LocationsTags.SUMMARY),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Text(
            text = stringResource(R.string.locations_summary_label),
            style = MaterialTheme.typography.bodySmall,
            color = colors.onSurfaceVariant,
        )
        Text(
            text = if (flag.isEmpty()) text else "$flag $text",
            style = MaterialTheme.typography.titleSmall,
            color = colors.onSurface,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

@Composable
private fun CountryRow(
    row: LocationRow.CountryRow,
    searching: Boolean,
    latency: LatencyState,
    onToggle: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    val colors = MaterialTheme.colorScheme
    val name = if (LocationTree.isRecommended(row.country)) {
        stringResource(R.string.locations_recommended)
    } else {
        row.country.name
    }
    val flag = CountryFlags.emoji(row.country.code)
    val serverCount = row.country.cities.sumOf { it.servers.size }
    TvFocusableSurface(
        onClick = { onToggle(row.country.code) },
        enabled = !searching,
        modifier = modifier
            .fillMaxWidth()
            .testTag(countryRowTag(row.country.code)),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
        ) {
            Text(
                text = flag,
                style = MaterialTheme.typography.titleSmall,
                color = colors.onSurface,
            )
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = name,
                    style = MaterialTheme.typography.titleSmall,
                    color = colors.onSurface,
                )
                Text(
                    text = pluralStringResource(R.plurals.locations_country_servers, serverCount, serverCount),
                    style = MaterialTheme.typography.bodySmall,
                    color = colors.onSurfaceVariant,
                )
            }
            Text(
                text = latencyText(latency),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
            if (!searching) {
                Text(
                    text = if (row.expanded) "▾" else "▸",
                    style = MaterialTheme.typography.titleSmall,
                    color = colors.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun ServerRow(
    row: LocationRow.ServerRow,
    latency: LatencyState,
    selected: Boolean,
    saving: Boolean,
    onSelect: (dev.benyamin.kumiho.engine.Country, City, Server) -> Unit,
) {
    val colors = MaterialTheme.colorScheme
    val enabled = !row.server.quarantined && !saving
    TvFocusableSurface(
        onClick = { onSelect(row.country, row.city, row.server) },
        enabled = enabled,
        containerColor = if (selected) colors.primary.copy(alpha = 0.16f) else colors.surface,
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = TvDimens.Space4)
            .testTag(
                serverRowTag(
                    row.country.code,
                    row.city.code,
                    LocationTree.addressOf(row.server),
                ),
            ),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = row.server.hostname,
                    style = MaterialTheme.typography.bodyMedium,
                    color = colors.onSurface,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    text = row.city.name + " · " + row.server.port,
                    style = MaterialTheme.typography.bodySmall,
                    color = colors.onSurfaceVariant,
                )
                if (row.server.quarantined) {
                    Text(
                        text = stringResource(R.string.locations_quarantined),
                        style = MaterialTheme.typography.bodySmall,
                        color = LocalKumihoColors.current.warning,
                    )
                }
            }
            Text(
                text = latencyText(latency),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
            Text(
                text = when {
                    saving -> "…"
                    selected -> "✓"
                    else -> " "
                },
                style = MaterialTheme.typography.titleSmall,
                color = colors.primary,
            )
        }
    }
}

@Composable
private fun NoticeBanner(
    notice: LocationsNotice,
    onAction: () -> Unit,
    onDismiss: () -> Unit,
) {
    val house = LocalKumihoColors.current
    val tint = when (notice.tone) {
        LocationsNoticeTone.WARNING -> house.warning
        LocationsNoticeTone.ERROR -> house.loss
    }
    val detail = (notice.text as? dev.benyamin.kumiho.ui.locations.LocationsNoticeText.Local)?.detail
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(tint.copy(alpha = 0.12f))
            .border(1.dp, tint.copy(alpha = 0.5f), MaterialTheme.shapes.medium)
            .padding(TvDimens.Space2)
            .testTag(LocationsTags.NOTICE),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
    ) {
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
        Row(horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1)) {
            if (notice.action != LocationsNoticeAction.NONE) {
                TvButton(
                    label = stringResource(R.string.locations_retry),
                    onClick = onAction,
                    modifier = Modifier.testTag(LocationsTags.NOTICE_ACTION),
                )
            }
            TvButton(
                label = stringResource(R.string.locations_dismiss),
                onClick = onDismiss,
                modifier = Modifier.testTag(LocationsTags.NOTICE_DISMISS),
            )
        }
    }
}

@Composable
private fun CenterMessage(text: String, tag: String, modifier: Modifier = Modifier) {
    Box(
        modifier = modifier
            .fillMaxWidth()
            .testTag(tag),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = text,
            style = MaterialTheme.typography.titleMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}
