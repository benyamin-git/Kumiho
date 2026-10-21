package dev.benyamin.kumiho.ui.setup

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.selection.toggleable
import androidx.compose.material3.Checkbox
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.theme.Dimens

/**
 * Step 1: the informed-consent screen. It names what the tunnel carries, what
 * never leaves the phone, that there is no telemetry, and that the local
 * proxies are reachable by any app on the device. The tick is required.
 */
@Composable
fun ConsentStep(
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .testTag(SetupTags.CONSENT_STEP),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space4),
    ) {
        Text(
            text = stringResource(R.string.consent_title),
            style = MaterialTheme.typography.headlineMedium,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Text(
            text = stringResource(R.string.consent_intro),
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Text(
            text = stringResource(R.string.consent_future),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        Text(
            text = stringResource(R.string.consent_local_title),
            style = MaterialTheme.typography.titleMedium,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Bullet(stringResource(R.string.consent_local_tokens))
        Bullet(stringResource(R.string.consent_local_data))
        Bullet(stringResource(R.string.consent_local_no_telemetry))

        Text(
            text = stringResource(R.string.consent_proxy_title),
            style = MaterialTheme.typography.titleMedium,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Text(
            text = stringResource(R.string.consent_proxy_text),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurface,
        )
    }
}

/**
 * The required consent tick. It is pinned above the wizard's bottom action, so
 * it is on screen on first paint instead of below the scrollable copy: a
 * disabled Continue must always sit next to the tick that enables it.
 *
 * The whole row is the toggle target (48dp minimum), the box is the visible
 * glyph and the row owns the checkbox semantics.
 */
@Composable
fun ConsentTickRow(
    accepted: Boolean,
    onAcceptedChange: (Boolean) -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .heightIn(min = Dimens.TouchTarget)
            .padding(vertical = Dimens.Space1)
            .toggleable(
                value = accepted,
                role = Role.Checkbox,
                onValueChange = onAcceptedChange,
            )
            .testTag(SetupTags.CONSENT_ACCEPT),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Dimens.Space3),
    ) {
        Checkbox(checked = accepted, onCheckedChange = null)
        Text(
            text = stringResource(R.string.consent_accept),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurface,
        )
    }
}

/** One paragraph behind a bullet, so a list reads as a list without art. */
@Composable
private fun Bullet(text: String) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        verticalAlignment = Alignment.Top,
    ) {
        Text(
            text = "•",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(
            text = text,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.weight(1f),
        )
    }
}
