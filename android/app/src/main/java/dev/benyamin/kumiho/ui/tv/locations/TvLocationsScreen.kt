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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.outlined.Star
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedTextField
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
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
 * The ten-foot location list: a search row, refresh and ping controls, the
 * saved selection and one flat list of focusable server cards.
 */
@Composable
fun TvLocationsScreen(
    state: LocationsState,
    actions: LocationsActions,
    modifier: Modifier = Modifier,
) {
    val colors = MaterialTheme.colorScheme
    val firstFocus = remember { FocusRequester() }
    val listScroll = rememberScrollState()
    LaunchedEffect(state.rows.firstOrNull()?.key, state.listResetToken) {
        if (state.rows.isNotEmpty()) {
            runCatching { firstFocus.requestFocus() }
        }
    }
    LaunchedEffect(state.listResetToken) {
        if (state.rows.isNotEmpty()) {
            listScroll.scrollTo(0)
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
        SearchField(
            query = state.query,
            onQueryChange = actions.onQueryChange,
            onClear = actions.onClearQuery,
            modifier = Modifier.fillMaxWidth(),
        )
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
            verticalAlignment = Alignment.CenterVertically,
        ) {
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
        }

        state.notice?.let { notice ->
            NoticeBanner(
                notice = notice,
                onAction = actions.onRetryNotice,
                onDismiss = actions.onDismissNotice,
            )
        }

        SelectionSummary(state = state)

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
            else -> {
                val sections = state.sections
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .weight(1f)
                        .verticalScroll(listScroll)
                        .testTag(LocationsTags.LIST),
                    verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
                ) {
                    FastestRow(
                        fastest = sections.fastest,
                        saving = state.savingFastest,
                        enabled = state.canSelectFastest,
                        onSelect = actions.onSelectFastest,
                    )
                    if (sections.favorites.isNotEmpty()) {
                        TvSectionHeading(stringResource(R.string.locations_favorites))
                        sections.favorites.forEach { row ->
                            ServerRow(
                                row = row,
                                latency = state.latencyOf(row),
                                selected = state.isSelected(row.country, row.city, row.server),
                                saving = state.isSaving(row.server),
                                favorite = true,
                                onSelect = actions.onSelect,
                                onFavorite = { actions.onToggleFavorite(row) },
                            )
                        }
                    }
                    if (sections.recents.isNotEmpty()) {
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
                        ) {
                            TvSectionHeading(
                                text = stringResource(R.string.locations_recent),
                                modifier = Modifier.weight(1f),
                            )
                            TvButton(
                                label = stringResource(R.string.locations_clear_recents),
                                onClick = actions.onClearRecents,
                                modifier = Modifier.testTag(LocationsTags.CLEAR_RECENTS),
                            )
                        }
                        sections.recents.forEach { row ->
                            ServerRow(
                                row = row,
                                latency = state.latencyOf(row),
                                selected = state.isSelected(row.country, row.city, row.server),
                                saving = state.isSaving(row.server),
                                favorite = state.isFavorite(row),
                                onSelect = actions.onSelect,
                                onFavorite = { actions.onToggleFavorite(row) },
                            )
                        }
                    }
                    TvSectionHeading(stringResource(R.string.locations_full_list))
                    sections.rows.forEachIndexed { index, row ->
                        ServerRow(
                            row = row,
                            latency = state.latencyOf(row),
                            selected = state.isSelected(row.country, row.city, row.server),
                            saving = state.isSaving(row.server),
                            favorite = state.isFavorite(row),
                            onSelect = actions.onSelect,
                            onFavorite = { actions.onToggleFavorite(row) },
                            modifier = if (index == 0) {
                                Modifier.focusRequester(firstFocus)
                            } else {
                                Modifier
                            },
                        )
                    }
                    TvButton(
                        label = stringResource(
                            if (state.allowLocked) {
                                R.string.locations_hide_locked
                            } else {
                                R.string.locations_show_locked
                            },
                        ),
                        onClick = actions.onToggleLocked,
                        modifier = Modifier.testTag(LocationsTags.LOCKED_TOGGLE),
                        enabled = !state.savingLocked,
                    )
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
            placeholder = {
                androidx.compose.material3.Text(stringResource(R.string.locations_search_placeholder))
            },
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
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(MaterialTheme.shapes.medium)
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
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
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
                style = MaterialTheme.typography.titleSmall,
                color = colors.onSurface,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

@Composable
private fun TvSectionHeading(text: String, modifier: Modifier = Modifier) {
    Text(
        text = text,
        style = MaterialTheme.typography.titleMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = modifier
            .fillMaxWidth()
            .padding(top = TvDimens.Space2),
    )
}

@Composable
private fun FastestRow(
    fastest: dev.benyamin.kumiho.ui.locations.FastestLocation?,
    saving: Boolean,
    enabled: Boolean,
    onSelect: () -> Unit,
) {
    val colors = MaterialTheme.colorScheme
    TvFocusableSurface(
        onClick = onSelect,
        enabled = enabled && !saving,
        containerColor = if (fastest != null) colors.primary.copy(alpha = 0.10f) else colors.surface,
        modifier = Modifier
            .fillMaxWidth()
            .testTag(LocationsTags.FASTEST),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
        ) {
            if (fastest != null) {
                CountryFlags.Flag(
                    code = fastest.row.country.code,
                    recommended = LocationTree.isRecommended(fastest.row.country),
                    width = 32.dp,
                    height = 24.dp,
                )
            }
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = stringResource(R.string.locations_fastest),
                    style = MaterialTheme.typography.bodyMedium,
                    color = colors.onSurface,
                )
                Text(
                    text = if (fastest != null) {
                        fastest.row.title + LocationTree.SEPARATOR + fastest.row.address
                    } else {
                        stringResource(R.string.locations_fastest_hint)
                    },
                    style = MaterialTheme.typography.bodySmall,
                    color = colors.onSurfaceVariant,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            if (saving) {
                Text(
                    text = "…",
                    style = MaterialTheme.typography.titleSmall,
                    color = colors.primary,
                )
            } else if (fastest != null) {
                Text(
                    text = stringResource(R.string.locations_latency, fastest.milliseconds),
                    style = MaterialTheme.typography.bodySmall,
                    color = colors.onSurfaceVariant,
                )
            }
        }
    }
}

/**
 * One server in the ten-foot list, drawn as a house card. The row body and the
 * trailing star are two separate focus targets: D-pad right reaches the star
 * without selecting. The body and star share the card's outline so only the
 * focus ring is drawn inside it.
 */
@Composable
private fun ServerRow(
    row: LocationRow,
    latency: LatencyState,
    selected: Boolean,
    saving: Boolean,
    favorite: Boolean,
    onSelect: (dev.benyamin.kumiho.engine.Country, City, Server) -> Unit,
    onFavorite: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val colors = MaterialTheme.colorScheme
    val shape = MaterialTheme.shapes.medium
    val favoriteDescription = stringResource(
        if (favorite) R.string.locations_remove_favorite else R.string.locations_favorite,
        row.title,
    )
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(shape)
            .background(if (selected) colors.primary.copy(alpha = 0.16f) else colors.surface)
            .border(
                width = 1.dp,
                color = if (selected) colors.primary.copy(alpha = 0.5f) else colors.borderVariant,
                shape = shape,
            )
            .padding(TvDimens.Space1),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
    ) {
        TvFocusableSurface(
            onClick = { onSelect(row.country, row.city, row.server) },
            enabled = !saving,
            shape = shape,
            containerColor = Color.Transparent,
            restingBorder = false,
            scaleOnFocus = false,
            contentPadding = TvDimens.Space1,
            modifier = modifier
                .weight(1f)
                .semantics(mergeDescendants = true) { this.selected = selected }
                .testTag(serverRowTag(row.country.code, row.city.code, row.address)),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
            ) {
                CountryFlags.Flag(
                    code = row.country.code,
                    recommended = LocationTree.isRecommended(row.country),
                    width = 32.dp,
                    height = 24.dp,
                )
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = row.title,
                        style = MaterialTheme.typography.bodyMedium,
                        color = colors.onSurface,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Text(
                        text = row.detail,
                        style = MaterialTheme.typography.bodySmall,
                        color = colors.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    if (row.country.locked) {
                        Row(
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
                        ) {
                            Icon(
                                imageVector = Icons.Filled.Lock,
                                contentDescription = null,
                                tint = colors.onSurfaceVariant,
                                modifier = Modifier.padding(end = 2.dp),
                            )
                            Text(
                                text = stringResource(R.string.locations_locked),
                                style = MaterialTheme.typography.bodySmall,
                                color = colors.onSurfaceVariant,
                            )
                        }
                    }
                }
                Text(
                    text = latencyText(latency),
                    style = MaterialTheme.typography.bodySmall,
                    color = colors.onSurfaceVariant,
                )
                Box(
                    modifier = Modifier.size(20.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    when {
                        saving -> Text(
                            text = "…",
                            style = MaterialTheme.typography.titleSmall,
                            color = colors.primary,
                        )
                        selected -> Icon(
                            imageVector = Icons.Filled.Check,
                            contentDescription = null,
                            tint = colors.primary,
                            modifier = Modifier.size(20.dp),
                        )
                    }
                }
            }
        }
        TvFocusableSurface(
            onClick = onFavorite,
            enabled = !saving,
            shape = shape,
            containerColor = Color.Transparent,
            restingBorder = false,
            contentPadding = 0.dp,
            modifier = Modifier
                .size(TvDimens.MinTarget)
                .semantics {
                    contentDescription = favoriteDescription
                    this.selected = favorite
                }
                .testTag(LocationsTags.favorite(row)),
        ) {
            Icon(
                imageVector = if (favorite) Icons.Filled.Star else Icons.Outlined.Star,
                contentDescription = null,
                tint = if (favorite) colors.primary else colors.onSurfaceVariant,
                modifier = Modifier.size(24.dp),
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
            .clip(MaterialTheme.shapes.medium)
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
