package dev.benyamin.kumiho.ui.tv.dashboard

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.LayoutDirection
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Text
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.dashboard.DashboardFormat
import dev.benyamin.kumiho.ui.dashboard.DashboardState
import dev.benyamin.kumiho.ui.dashboard.DashboardTags
import dev.benyamin.kumiho.ui.tv.components.TvFocusableSurface
import dev.benyamin.kumiho.ui.theme.LocalAppLanguage
import dev.benyamin.kumiho.ui.tv.theme.TvDimens

/**
 * The right column of the TV dashboard: only the cards whose facts are real
 * right now. The location card is the one focusable card and opens the full
 * TV options page, never a bottom sheet.
 */
@Composable
fun TvDashboardCards(
    state: DashboardState,
    onOpenOptions: () -> Unit,
    powerFocus: FocusRequester,
    optionsFocus: FocusRequester,
    locationFocus: FocusRequester,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .verticalScroll(rememberScrollState()),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space2),
    ) {
        if (state.showLocationCard) {
            val rtl = LocalLayoutDirection.current == LayoutDirection.Rtl
            LocationCard(
                state = state,
                onOpenOptions = onOpenOptions,
                powerFocus = powerFocus,
                optionsFocus = optionsFocus,
                locationFocus = locationFocus,
                rtl = rtl,
            )
        }
        if (state.showExitCard) {
            ExitCard(state = state)
        }
        if (state.showSessionCard) {
            SessionCard(state = state)
        }
    }
}

@Composable
private fun LocationCard(
    state: DashboardState,
    onOpenOptions: () -> Unit,
    powerFocus: FocusRequester,
    optionsFocus: FocusRequester,
    locationFocus: FocusRequester,
    rtl: Boolean,
) {
    val status = state.status
    val country = if (state.locationIsRecommended) {
        null
    } else {
        state.countryName(status?.location) ?: status?.location
    }
    val city = state.cityNameForServer(status?.server)
        ?.takeUnless { it.equals(country, ignoreCase = true) }
    val name = when {
        state.locationIsRecommended -> stringResource(R.string.dashboard_location_recommended)
        country != null && city != null ->
            stringResource(R.string.dashboard_location_country_city, country, city)
        else -> country ?: DashboardFormat.DASH
    }
    val server = DashboardFormat.hostOf(status?.server)
    TvFocusableSurface(
        onClick = onOpenOptions,
        modifier = Modifier
            .fillMaxWidth()
            .testTag(DashboardTags.CARD_LOCATION)
            .focusRequester(locationFocus)
            .focusProperties {
                down = optionsFocus
                if (rtl) right = powerFocus else left = powerFocus
            },
    ) {
        Column(verticalArrangement = Arrangement.spacedBy(TvDimens.Space1)) {
            CardTitle(stringResource(R.string.dashboard_location_title))
            Text(
                text = name,
                style = MaterialTheme.typography.titleMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
            if (server != null) {
                Text(
                    text = server,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            if (state.locationIsRecommended) {
                Text(
                    text = stringResource(R.string.dashboard_location_default_hint),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Text(
                text = stringResource(R.string.dashboard_location_change),
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.primary,
            )
        }
    }
}

@Composable
private fun ExitCard(state: DashboardState) {
    Panel(modifier = Modifier.testTag(DashboardTags.CARD_EXIT)) {
        CardTitle(stringResource(R.string.dashboard_exit_title))
        Text(
            text = (state.status?.exitIp ?: DashboardFormat.DASH).replace(":", ":\u200B"),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurface,
        )
        val country = state.status?.exitCountry
        if (!country.isNullOrBlank()) {
            Text(
                text = country,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun SessionCard(state: DashboardState) {
    val status = state.status
    val totals = DashboardFormat.totalsLabel(status?.totals, LocalAppLanguage.current)
    val rates = DashboardFormat.ratesLabel(status?.rates, LocalAppLanguage.current)
    val uptime = DashboardFormat.uptimeLabel(
        since = status?.since ?: 0L,
        nowSeconds = System.currentTimeMillis() / 1000,
        language = LocalAppLanguage.current,
    )
    Panel(modifier = Modifier.testTag(DashboardTags.CARD_SESSION)) {
        CardTitle(stringResource(R.string.dashboard_session_title))
        if (totals != null) {
            Text(
                text = totals,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
        }
        if (rates != null) {
            Text(
                text = rates,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        if (uptime != null) {
            Text(
                text = stringResource(R.string.dashboard_session_uptime, uptime),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun Panel(
    modifier: Modifier = Modifier,
    content: @Composable androidx.compose.foundation.layout.ColumnScope.() -> Unit,
) {
    val colors = MaterialTheme.colorScheme
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(MaterialTheme.shapes.medium)
            .background(colors.surface)
            .padding(TvDimens.CardPadding),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
        content = content,
    )
}

@Composable
private fun CardTitle(text: String) {
    Text(
        text = text,
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
}
