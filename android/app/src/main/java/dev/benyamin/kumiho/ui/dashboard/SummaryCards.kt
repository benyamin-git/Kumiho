package dev.benyamin.kumiho.ui.dashboard

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.components.KumihoCard
import dev.benyamin.kumiho.ui.components.KumihoQuotaBar
import dev.benyamin.kumiho.ui.theme.Dimens
import java.time.ZoneId

/**
 * The four dashboard cards. A card exists only when its data does: Exit IP,
 * Quota and Session are hidden while disconnected, empty or stale. The Location
 * card is the one honest exception: with no persisted selection it says
 * "Recommended", which is the engine's real default, not a placeholder.
 *
 * The grid is one column on a phone (values never split mid-number) and two
 * equal-height columns from the shell's rail breakpoint up. The shell already
 * caps content at a readable measure, so four narrow columns would only wrap.
 */
@Composable
fun SummaryCards(
    state: DashboardState,
    onOpenOptions: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val cards = buildList<@Composable (Modifier) -> Unit> {
        if (state.showLocationCard) {
            add { slot -> LocationCard(state, onOpenOptions, slot) }
        }
        if (state.showExitCard) {
            add { slot -> ExitCard(state, slot) }
        }
        if (state.showQuotaCard) {
            add { slot -> QuotaCard(state, slot) }
        }
        if (state.showSessionCard) {
            add { slot -> SessionCard(state, slot) }
        }
    }
    if (cards.isEmpty()) return

    BoxWithConstraints(modifier = modifier.fillMaxWidth()) {
        val columns = if (maxWidth >= MEDIUM_BREAKPOINT) minOf(2, cards.size) else 1
        Column(verticalArrangement = Arrangement.spacedBy(Dimens.Space3)) {
            cards.chunked(columns).forEach { rowCards ->
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(IntrinsicSize.Max),
                    horizontalArrangement = Arrangement.spacedBy(Dimens.Space3),
                ) {
                    rowCards.forEach { card ->
                        Box(modifier = Modifier.weight(1f).fillMaxHeight()) {
                            card(Modifier.fillMaxWidth())
                        }
                    }
                    repeat(columns - rowCards.size) {
                        Spacer(modifier = Modifier.weight(1f))
                    }
                }
            }
        }
    }
}

@Composable
private fun LocationCard(
    state: DashboardState,
    onOpenOptions: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val status = state.status
    val name = when {
        state.locationIsRecommended -> stringResource(R.string.dashboard_location_recommended)
        else -> state.countryName(status?.location) ?: status?.location
    }
    val server = DashboardFormat.hostOf(status?.server)
    KumihoCard(
        modifier = modifier
            .testTag(DashboardTags.CARD_LOCATION)
            .clip(MaterialTheme.shapes.medium)
            .clickable(role = Role.Button, onClick = onOpenOptions),
    ) {
        CardTitle(stringResource(R.string.dashboard_location_title))
        Text(
            text = name ?: DashboardFormat.DASH,
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
                modifier = Modifier.padding(top = Dimens.Space1),
            )
        }
        Text(
            text = stringResource(R.string.dashboard_location_change),
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.primary,
            modifier = Modifier.padding(top = Dimens.Space2),
        )
    }
}

@Composable
private fun ExitCard(state: DashboardState, modifier: Modifier = Modifier) {
    val status = state.status
    KumihoCard(modifier = modifier.testTag(DashboardTags.CARD_EXIT)) {
        CardTitle(stringResource(R.string.dashboard_exit_title))
        Text(
            text = status?.exitIp ?: DashboardFormat.DASH,
            style = MaterialTheme.typography.titleMedium,
            color = MaterialTheme.colorScheme.onSurface,
        )
        val country = status?.exitCountry
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
private fun QuotaCard(state: DashboardState, modifier: Modifier = Modifier) {
    val quota = state.status?.quota
    val label = DashboardFormat.quotaLabel(quota)
    val fraction = DashboardFormat.quotaFraction(quota)
    val reset = DashboardFormat.dateLabel(
        epochSeconds = quota?.reset ?: 0L,
        zone = ZoneId.systemDefault(),
        locale = LocalConfiguration.current.locales[0],
    )
    KumihoCard(modifier = modifier.testTag(DashboardTags.CARD_QUOTA)) {
        CardTitle(stringResource(R.string.dashboard_quota_title))
        if (label != null) {
            Text(
                text = label,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
        }
        if (fraction != null) {
            KumihoQuotaBar(
                fraction = fraction,
                modifier = Modifier.padding(top = Dimens.Space2),
            )
        }
        if (reset != null) {
            Text(
                text = stringResource(R.string.dashboard_quota_resets, reset),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = Dimens.Space1),
            )
        }
    }
}

@Composable
private fun SessionCard(state: DashboardState, modifier: Modifier = Modifier) {
    val status = state.status
    val totals = DashboardFormat.totalsLabel(status?.totals)
    val rates = DashboardFormat.ratesLabel(status?.rates)
    val uptime = DashboardFormat.uptimeLabel(
        since = status?.since ?: 0L,
        nowSeconds = System.currentTimeMillis() / 1000,
    )
    KumihoCard(modifier = modifier.testTag(DashboardTags.CARD_SESSION)) {
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
private fun CardTitle(text: String) {
    Text(
        text = text,
        style = MaterialTheme.typography.labelMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(bottom = Dimens.Space1),
    )
}

/** The shell's rail breakpoint; below it the cards stack in one column. */
private val MEDIUM_BREAKPOINT = 600.dp
