package dev.benyamin.kumiho.ui.tv.dashboard

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Text
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.dashboard.DashboardActions
import dev.benyamin.kumiho.ui.dashboard.DashboardState
import dev.benyamin.kumiho.ui.dashboard.DashboardTags
import dev.benyamin.kumiho.ui.dashboard.LocationChoice
import dev.benyamin.kumiho.ui.dashboard.LocationLoad
import dev.benyamin.kumiho.ui.dashboard.cityTag
import dev.benyamin.kumiho.ui.dashboard.countryTag
import dev.benyamin.kumiho.ui.dashboard.sheetHintText
import dev.benyamin.kumiho.ui.locations.CountryFlags
import dev.benyamin.kumiho.ui.tv.components.TvButton
import dev.benyamin.kumiho.ui.tv.components.TvFocusableSurface
import dev.benyamin.kumiho.ui.tv.components.TvPageHeader
import dev.benyamin.kumiho.ui.tv.theme.TvDimens

/**
 * The TV replacement for the phone's modal options sheet: a full page with the
 * picker on the left and the connection controls on the right. Back closes it.
 * Location changes are still a disconnect-first decision; Connect is disabled
 * while a session is active and the hint says why.
 */
@Composable
fun TvConnectOptionsPage(
    state: DashboardState,
    actions: DashboardActions,
    onClose: () -> Unit,
    modifier: Modifier = Modifier,
) {
    BackHandler(onBack = onClose)
    val firstFocus = remember { FocusRequester() }
    LaunchedEffect(Unit) { runCatching { firstFocus.requestFocus() } }
    val colors = MaterialTheme.colorScheme

    Box(
        modifier = modifier
            .fillMaxSize()
            .background(colors.background)
            .testTag(DashboardTags.SHEET),
    ) {
        Row(
            modifier = Modifier
                .fillMaxSize()
                .padding(TvDimens.Space2),
            horizontalArrangement = Arrangement.spacedBy(TvDimens.Space4),
        ) {
            Column(
                modifier = Modifier
                    .weight(1f)
                    .fillMaxSize(),
                verticalArrangement = Arrangement.spacedBy(TvDimens.Space2),
            ) {
                TvPageHeader(
                    title = stringResource(R.string.dashboard_options_title),
                    subtitle = stringResource(R.string.dashboard_picker_recommended_text),
                )
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .weight(1f)
                        .verticalScroll(rememberScrollState()),
                    verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
                ) {
                    Picker(
                        state = state,
                        actions = actions,
                        firstFocus = firstFocus,
                    )
                }
            }
            Column(
                modifier = Modifier
                    .width(360.dp)
                    .fillMaxSize()
                    .verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(TvDimens.Space2),
            ) {
                Text(
                    text = stringResource(R.string.dashboard_options_proxy_only),
                    style = MaterialTheme.typography.titleMedium,
                    color = colors.onSurface,
                )
                TvFocusableSurface(
                    onClick = { actions.onToggleProxyOnly(!state.options.proxyOnly) },
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(DashboardTags.SHEET_PROXY_ONLY),
                ) {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Column(modifier = Modifier.weight(1f)) {
                            Text(
                                text = stringResource(R.string.dashboard_options_proxy_only),
                                style = MaterialTheme.typography.bodyMedium,
                                color = colors.onSurface,
                            )
                            Text(
                                text = stringResource(R.string.dashboard_options_proxy_only_text),
                                style = MaterialTheme.typography.bodySmall,
                                color = colors.onSurfaceVariant,
                            )
                        }
                        Text(
                            text = stringResource(
                                if (state.options.proxyOnly) R.string.dashboard_on else R.string.dashboard_off,
                            ),
                            style = MaterialTheme.typography.labelLarge,
                            color = if (state.options.proxyOnly) colors.primary else colors.onSurfaceVariant,
                        )
                    }
                }

                Text(
                    text = stringResource(R.string.dashboard_options_lockdown),
                    style = MaterialTheme.typography.titleMedium,
                    color = colors.onSurface,
                )
                TvFocusableSurface(
                    onClick = actions.onOpenVpnSettings,
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(DashboardTags.SHEET_LOCKDOWN),
                ) {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Column(modifier = Modifier.weight(1f)) {
                            Text(
                                text = stringResource(R.string.dashboard_options_lockdown_summary),
                                style = MaterialTheme.typography.bodySmall,
                                color = colors.onSurfaceVariant,
                            )
                        }
                        Text(
                            text = stringResource(
                                if (state.lockdown) R.string.dashboard_on else R.string.dashboard_off,
                            ),
                            style = MaterialTheme.typography.labelLarge,
                            color = colors.onSurface,
                        )
                    }
                }

                val hint = sheetHintText(state.sheetHint)
                if (hint != null) {
                    Text(
                        text = hint,
                        style = MaterialTheme.typography.bodySmall,
                        color = if (state.sheetHint == dev.benyamin.kumiho.ui.dashboard.SheetHint.DISCONNECT_FIRST) {
                            colors.onSurface
                        } else {
                            colors.onSurfaceVariant
                        },
                        modifier = Modifier
                            .padding(vertical = TvDimens.Space1)
                            .testTag(DashboardTags.SHEET_HINT),
                    )
                }
                TvButton(
                    label = stringResource(R.string.dashboard_options_connect),
                    onClick = actions.onConnectFromOptions,
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(DashboardTags.SHEET_CONNECT),
                    enabled = state.optionsConnectEnabled,
                    primary = true,
                )
                Text(
                    text = stringResource(R.string.tv_options_back_hint),
                    style = MaterialTheme.typography.bodySmall,
                    color = colors.onSurfaceVariant,
                    textAlign = TextAlign.Start,
                )
                TvButton(
                    label = stringResource(R.string.tv_back),
                    onClick = onClose,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        }
    }
}

@Composable
private fun Picker(
    state: DashboardState,
    actions: DashboardActions,
    firstFocus: FocusRequester,
) {
    val choice = state.effectiveChoice
    val colors = MaterialTheme.colorScheme
    ChoiceRow(
        title = stringResource(R.string.dashboard_picker_recommended),
        summary = stringResource(R.string.dashboard_picker_recommended_text),
        selected = choice == LocationChoice.Recommended,
        onClick = actions.onSelectRecommended,
        modifier = Modifier
            .testTag(DashboardTags.PICKER_RECOMMENDED)
            .focusRequester(firstFocus),
    )
    if (state.status?.location != null) {
        val saved = if (state.locationIsRecommended) {
            stringResource(R.string.dashboard_location_recommended)
        } else {
            state.countryName(state.status.location) ?: state.status.location
        }
        ChoiceRow(
            title = stringResource(R.string.dashboard_picker_saved),
            summary = saved,
            selected = choice == LocationChoice.Saved,
            onClick = actions.onSelectSaved,
            modifier = Modifier.testTag(DashboardTags.PICKER_SAVED),
        )
    }

    when (val locations = state.locations) {
        LocationLoad.Loading -> Text(
            text = stringResource(R.string.dashboard_picker_loading),
            style = MaterialTheme.typography.bodyMedium,
            color = colors.onSurfaceVariant,
            modifier = Modifier.padding(vertical = TvDimens.Space2),
        )
        is LocationLoad.Failed -> Column(verticalArrangement = Arrangement.spacedBy(TvDimens.Space1)) {
            Text(
                text = stringResource(R.string.dashboard_picker_failed_title),
                style = MaterialTheme.typography.titleSmall,
                color = colors.onSurface,
            )
            Text(
                text = locations.detail,
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurfaceVariant,
            )
            TvButton(
                label = stringResource(R.string.dashboard_notice_retry),
                onClick = actions.onReloadLocations,
                modifier = Modifier.testTag(DashboardTags.PICKER_RETRY),
            )
        }
        is LocationLoad.Ready -> {
            val countries = locations.countries.filterNot {
                it.code.equals(DashboardState.RECOMMENDED_CODE, ignoreCase = true)
            }
            if (countries.isEmpty()) {
                Text(
                    text = stringResource(R.string.dashboard_picker_empty),
                    style = MaterialTheme.typography.bodyMedium,
                    color = colors.onSurfaceVariant,
                    modifier = Modifier.testTag(DashboardTags.PICKER_EMPTY),
                )
            }
            countries.forEach { country ->
                val expanded = country.code in state.options.expanded
                TvFocusableSurface(
                    onClick = { actions.onToggleCountry(country.code) },
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(countryTag(country.code)),
                ) {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
                    ) {
                        Text(
                            text = CountryFlags.emoji(country.code),
                            style = MaterialTheme.typography.titleSmall,
                            color = colors.onSurface,
                        )
                        Text(
                            text = country.name,
                            style = MaterialTheme.typography.titleSmall,
                            color = colors.onSurface,
                            modifier = Modifier.weight(1f),
                        )
                        Text(
                            text = if (expanded) "▾" else "▸",
                            style = MaterialTheme.typography.titleSmall,
                            color = colors.onSurfaceVariant,
                        )
                    }
                }
                if (expanded) {
                    country.cities.forEach { city ->
                        val selected = choice is LocationChoice.Picked &&
                            choice.countryCode.equals(country.code, ignoreCase = true) &&
                            choice.cityCode.equals(city.code, ignoreCase = true)
                        ChoiceRow(
                            title = city.name,
                            summary = null,
                            selected = selected,
                            onClick = { actions.onSelectCity(country.code, city.code) },
                            modifier = Modifier
                                .padding(start = TvDimens.Space4)
                                .testTag(cityTag(country.code, city.code)),
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun ChoiceRow(
    title: String,
    summary: String?,
    selected: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val colors = MaterialTheme.colorScheme
    TvFocusableSurface(onClick = onClick, modifier = modifier.fillMaxWidth()) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
        ) {
            Text(
                text = if (selected) "✓" else " ",
                style = MaterialTheme.typography.titleSmall,
                color = colors.primary,
            )
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = title,
                    style = MaterialTheme.typography.titleSmall,
                    color = colors.onSurface,
                )
                if (summary != null) {
                    Text(
                        text = summary,
                        style = MaterialTheme.typography.bodySmall,
                        color = colors.onSurfaceVariant,
                    )
                }
            }
        }
    }
}
