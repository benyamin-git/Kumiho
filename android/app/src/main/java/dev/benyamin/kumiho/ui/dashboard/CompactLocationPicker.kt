package dev.benyamin.kumiho.ui.dashboard

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.components.KumihoErrorState
import dev.benyamin.kumiho.ui.components.KumihoLoadingState
import dev.benyamin.kumiho.ui.components.KumihoRow
import dev.benyamin.kumiho.ui.locations.CountryFlags
import dev.benyamin.kumiho.ui.theme.Dimens

/**
 * The options sheet's compact picker: Recommended first, then the engine's
 * saved location when it has one, then countries that expand to cities.
 *
 * Recommended and Saved never call `select_location`: Recommended sends `rec`
 * and Saved sends no location fields, so the engine keeps its persisted
 * selection. A picked city sends its codes with no server; the engine resolves
 * the first candidate and persists it. The full tree (servers, filter, speed
 * test) stays in Locations (T12).
 */
@Composable
fun CompactLocationPicker(
    state: DashboardState,
    onSelectRecommended: () -> Unit,
    onSelectSaved: () -> Unit,
    onToggleCountry: (String) -> Unit,
    onSelectCity: (String, String) -> Unit,
    onReload: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val choice = state.effectiveChoice
    Column(modifier = modifier) {
        ChoiceRow(
            title = stringResource(R.string.dashboard_picker_recommended),
            summary = stringResource(R.string.dashboard_picker_recommended_text),
            selected = choice == LocationChoice.Recommended,
            onClick = onSelectRecommended,
            modifier = Modifier.testTag(DashboardTags.PICKER_RECOMMENDED),
        )
        if (state.status?.location != null) {
            val saved = when {
                state.locationIsRecommended -> stringResource(R.string.dashboard_location_recommended)
                else -> state.countryName(state.status.location) ?: state.status.location
            }
            ChoiceRow(
                title = stringResource(R.string.dashboard_picker_saved),
                summary = saved,
                selected = choice == LocationChoice.Saved,
                onClick = onSelectSaved,
                modifier = Modifier.testTag(DashboardTags.PICKER_SAVED),
            )
        }

        when (val locations = state.locations) {
            LocationLoad.Loading -> KumihoLoadingState(
                label = stringResource(R.string.dashboard_picker_loading),
            )
            is LocationLoad.Failed -> KumihoErrorState(
                title = stringResource(R.string.dashboard_picker_failed_title),
                message = locations.detail,
                onRetry = onReload,
                modifier = Modifier.testTag(DashboardTags.PICKER_RETRY),
            )
            is LocationLoad.Ready -> {
                val countries = locations.countries.filterNot {
                    it.code.equals(DashboardState.RECOMMENDED_CODE, ignoreCase = true)
                }
                if (countries.isEmpty()) {
                    Text(
                        text = stringResource(R.string.dashboard_picker_empty),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier
                            .padding(horizontal = Dimens.Space4, vertical = Dimens.Space3)
                            .testTag(DashboardTags.PICKER_EMPTY),
                    )
                }
                countries.forEach { country ->
                    val expanded = country.code in state.options.expanded
                    CountryRow(
                        name = country.name,
                        countryCode = country.code,
                        expanded = expanded,
                        onClick = { onToggleCountry(country.code) },
                        modifier = Modifier.testTag(countryTag(country.code)),
                    )
                    if (expanded) {
                        country.cities.forEach { city ->
                            val selected = choice is LocationChoice.Picked &&
                                choice.countryCode.equals(country.code, ignoreCase = true) &&
                                choice.cityCode.equals(city.code, ignoreCase = true)
                            ChoiceRow(
                                title = city.name,
                                selected = selected,
                                onClick = { onSelectCity(country.code, city.code) },
                                modifier = Modifier
                                    .padding(start = Dimens.Space4)
                                    .testTag(cityTag(country.code, city.code)),
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun CountryRow(
    name: String,
    countryCode: String,
    expanded: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    KumihoRow(
        title = name,
        modifier = modifier,
        onClick = onClick,
        leading = {
            CountryFlags.Flag(
                code = countryCode,
                width = 20.dp,
                height = 15.dp,
                modifier = Modifier.testTag(countryFlagTag(countryCode)),
            )
        },
        trailing = {
            Icon(
                imageVector = if (expanded) {
                    Icons.Filled.KeyboardArrowDown
                } else {
                    Icons.AutoMirrored.Filled.KeyboardArrowRight
                },
                contentDescription = stringResource(
                    if (expanded) R.string.dashboard_collapse else R.string.dashboard_expand,
                ),
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(24.dp),
            )
        },
    )
}

/** One selectable row; the selected choice carries a checkmark and a word. */
@Composable
private fun ChoiceRow(
    title: String,
    selected: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    summary: String? = null,
) {
    KumihoRow(
        title = title,
        subtitle = summary,
        modifier = modifier,
        onClick = onClick,
        trailing = if (selected) {
            {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(Dimens.Space1),
                ) {
                    Text(
                        text = stringResource(R.string.dashboard_selected),
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.primary,
                    )
                    Icon(
                        imageVector = Icons.Filled.Check,
                        contentDescription = null,
                        tint = MaterialTheme.colorScheme.primary,
                        modifier = Modifier.size(20.dp),
                    )
                }
            }
        } else {
            null
        },
    )
}
