package dev.benyamin.kumiho.ui.setup

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.components.KumihoCard
import dev.benyamin.kumiho.ui.theme.Dimens

/**
 * Step 4: Always-on VPN and lockdown, as Android owns them. The state is read
 * from the service and refreshed on resume (R16); Kumiho explains the two
 * flags, opens the system screen and never turns either one on itself.
 *
 * "Connect now" is offered only when it can work: signed in and VPN consent
 * granted. Otherwise the step finishes without connecting.
 */
@Composable
fun AlwaysOnStep(
    alwaysOn: Boolean,
    lockdown: Boolean,
    canConnectNow: Boolean,
    signedIn: Boolean,
    vpnConsentGranted: Boolean,
    onOpenVpnSettings: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .testTag(SetupTags.ALWAYS_ON_STEP),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space4),
    ) {
        Text(
            text = stringResource(R.string.always_on_intro),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        KumihoCard(modifier = Modifier.fillMaxWidth()) {
            Text(
                text = stringResource(R.string.always_on_always_title),
                style = MaterialTheme.typography.titleMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Text(
                text = stringResource(R.string.always_on_always_text),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = Dimens.Space2),
            )
            OsStateRow(
                title = stringResource(R.string.always_on_row_state),
                on = alwaysOn,
                modifier = Modifier
                    .padding(top = Dimens.Space3)
                    .testTag(SetupTags.ALWAYS_ON_STATUS),
            )
        }

        KumihoCard(modifier = Modifier.fillMaxWidth()) {
            Text(
                text = stringResource(R.string.always_on_lockdown_title),
                style = MaterialTheme.typography.titleMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Text(
                text = stringResource(R.string.always_on_lockdown_text),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = Dimens.Space2),
            )
            OsStateRow(
                title = stringResource(R.string.always_on_row_state),
                on = lockdown,
                modifier = Modifier
                    .padding(top = Dimens.Space3)
                    .testTag(SetupTags.LOCKDOWN_STATUS),
            )
        }

        OutlinedButton(
            onClick = onOpenVpnSettings,
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = Dimens.ControlHeight)
                .testTag(SetupTags.ALWAYS_ON_SETTINGS),
        ) {
            Text(stringResource(R.string.always_on_open_settings))
        }

        Text(
            text = stringResource(connectHint(canConnectNow, signedIn, vpnConsentGranted)),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.testTag(SetupTags.ALWAYS_ON_HINT),
        )
        Text(
            text = stringResource(R.string.always_on_finish_note),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

/** The honest reason a Finish button can or cannot connect straight away. */
private fun connectHint(canConnectNow: Boolean, signedIn: Boolean, vpnConsentGranted: Boolean): Int =
    when {
        canConnectNow -> R.string.always_on_connect_hint
        !signedIn && !vpnConsentGranted -> R.string.always_on_connect_hint_needs_both
        !vpnConsentGranted -> R.string.always_on_connect_hint_needs_vpn
        else -> R.string.always_on_connect_hint_needs_sign_in
    }

/**
 * One OS-owned flag: a label and On/Off as text, merged into a single
 * accessible node so a screen reader reads "Always-on VPN, Off".
 */
@Composable
private fun OsStateRow(
    title: String,
    on: Boolean,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .semantics(mergeDescendants = true) {},
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Text(
            text = title,
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Text(
            text = stringResource(if (on) R.string.always_on_state_on else R.string.always_on_state_off),
            style = MaterialTheme.typography.titleSmall,
            color = MaterialTheme.colorScheme.onSurface,
        )
    }
}
