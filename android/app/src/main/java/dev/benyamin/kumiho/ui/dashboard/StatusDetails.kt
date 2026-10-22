package dev.benyamin.kumiho.ui.dashboard

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.rememberVectorPainter
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.components.KumihoCard
import dev.benyamin.kumiho.ui.theme.Dimens
import java.time.ZoneId

/**
 * The details card. Always-on, lockdown and autoconnect are Android's own
 * flags, read-only; the engine's `kill_switch`/`autoconnect` values are never
 * shown or written (A10). Expiry, DNS and IPv6 rows exist only when the engine
 * reported data, with an em dash for a missing sub-value.
 */
@Composable
fun StatusDetails(
    state: DashboardState,
    onOpenVpnSettings: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val status = state.status
    val zone = rememberZone()
    val locale = LocalConfiguration.current.locales[0]
    val tokenExpiry = DashboardFormat.dateLabel(status?.tokenExpires ?: 0L, zone, locale)
    val passExpiry = DashboardFormat.dateLabel(status?.passExpires ?: 0L, zone, locale)
    val dns = status?.dns?.takeIf { it.isNotBlank() }

    KumihoCard(modifier = modifier.testTag(DashboardTags.DETAILS)) {
        Text(
            text = stringResource(R.string.dashboard_details_title),
            style = MaterialTheme.typography.labelLarge,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        DetailRow(
            title = stringResource(R.string.dashboard_details_always_on),
            summary = if (state.alwaysOn) {
                stringResource(R.string.dashboard_details_always_on_restart)
            } else {
                null
            },
            value = onOff(state.alwaysOn),
        )
        DetailRow(
            title = stringResource(R.string.dashboard_details_lockdown),
            value = onOff(state.lockdown),
        )
        DetailRow(
            title = stringResource(R.string.dashboard_details_autoconnect),
            summary = stringResource(R.string.dashboard_details_autoconnect_follows),
            value = onOff(state.alwaysOn),
        )
        DetailRow(
            title = stringResource(R.string.dashboard_details_open_vpn_settings),
            onClick = onOpenVpnSettings,
            modifier = Modifier.testTag(DashboardTags.DETAILS_VPN_SETTINGS),
        )
        if (tokenExpiry != null) {
            DetailRow(
                title = stringResource(R.string.dashboard_details_token),
                value = stringResource(R.string.dashboard_details_expires, tokenExpiry),
            )
        }
        if (passExpiry != null) {
            DetailRow(
                title = stringResource(R.string.dashboard_details_pass),
                value = stringResource(R.string.dashboard_details_expires, passExpiry),
            )
        }
        if (dns != null) {
            DetailRow(
                title = stringResource(R.string.dashboard_details_dns),
                value = dns,
            )
        }
        if (state.phase.active) {
            val ipv6 = when (val display = ipv6Display(status?.ipv6)) {
                Ipv6Display.Blackholed -> stringResource(R.string.dashboard_ipv6_blackholed)
                is Ipv6Display.Value -> display.text
                Ipv6Display.Missing -> DashboardFormat.DASH
            }
            DetailRow(
                title = stringResource(R.string.dashboard_details_ipv6),
                value = ipv6,
            )
        }
    }
}

@Composable
private fun DetailRow(
    title: String,
    modifier: Modifier = Modifier,
    summary: String? = null,
    value: String? = null,
    onClick: (() -> Unit)? = null,
) {
    val base = modifier.fillMaxWidth().defaultMinSize(minHeight = Dimens.TouchTarget)
    Row(
        modifier = if (onClick != null) {
            base.clickable(role = Role.Button, onClick = onClick)
        } else {
            base
        }.padding(vertical = Dimens.Space3),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Dimens.Space3),
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = title,
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurface,
            )
            if (summary != null) {
                Text(
                    text = summary,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        if (value != null) {
            Text(
                text = value,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        if (onClick != null) {
            Icon(
                painter = rememberVectorPainter(Icons.AutoMirrored.Filled.KeyboardArrowRight),
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(24.dp),
            )
        }
    }
}

@Composable
private fun onOff(value: Boolean): String = stringResource(
    if (value) R.string.dashboard_on else R.string.dashboard_off,
)

@Composable
private fun rememberZone(): ZoneId = androidx.compose.runtime.remember { ZoneId.systemDefault() }
