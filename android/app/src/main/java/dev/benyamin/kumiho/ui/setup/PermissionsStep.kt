package dev.benyamin.kumiho.ui.setup

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.components.KumihoCard
import dev.benyamin.kumiho.ui.components.KumihoStatusChip
import dev.benyamin.kumiho.ui.components.StatusTone
import dev.benyamin.kumiho.ui.theme.Dimens

/**
 * Step 3: the two Android permissions, each with its real state. A denial is
 * shown in place and never blocks Continue (A13); nothing claims a grant the
 * OS has not given.
 */
@Composable
fun PermissionsStep(
    permissions: PermissionState,
    onRequestVpn: () -> Unit,
    onRequestNotifications: () -> Unit,
    onOpenNotificationSettings: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .testTag(SetupTags.PERMISSIONS_STEP),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space4),
    ) {
        Text(
            text = stringResource(R.string.permissions_title),
            style = MaterialTheme.typography.headlineMedium,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Text(
            text = stringResource(R.string.permissions_intro),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        KumihoCard(modifier = Modifier.fillMaxWidth()) {
            Text(
                text = stringResource(R.string.permissions_vpn_title),
                style = MaterialTheme.typography.titleMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Text(
                text = stringResource(R.string.permissions_vpn_text),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = Dimens.Space2),
            )
            PermissionStatus(
                granted = permissions.vpnConsentGranted,
                grantedLabel = stringResource(R.string.permissions_vpn_granted),
                missingLabel = stringResource(R.string.permissions_vpn_missing),
                modifier = Modifier
                    .padding(top = Dimens.Space3)
                    .testTag(SetupTags.PERMISSIONS_VPN_STATUS),
            )
            if (!permissions.vpnConsentGranted) {
                Button(
                    onClick = onRequestVpn,
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(top = Dimens.Space3)
                        .heightIn(min = Dimens.ControlHeight)
                        .testTag(SetupTags.PERMISSIONS_VPN_ACTION),
                ) {
                    Text(stringResource(R.string.permissions_vpn_action))
                }
                Text(
                    text = stringResource(R.string.permissions_vpn_denied),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(top = Dimens.Space2),
                )
            }
        }

        KumihoCard(modifier = Modifier.fillMaxWidth()) {
            Text(
                text = stringResource(R.string.permissions_notifications_title),
                style = MaterialTheme.typography.titleMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Text(
                text = stringResource(R.string.permissions_notifications_text),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = Dimens.Space2),
            )
            PermissionStatus(
                granted = permissions.notificationsGranted,
                grantedLabel = stringResource(R.string.permissions_notifications_granted),
                missingLabel = stringResource(R.string.permissions_notifications_missing),
                modifier = Modifier
                    .padding(top = Dimens.Space3)
                    .testTag(SetupTags.PERMISSIONS_NOTIFICATIONS_STATUS),
            )
            if (!permissions.notificationsGranted) {
                if (permissions.notificationsRuntimeRequired) {
                    Button(
                        onClick = onRequestNotifications,
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(top = Dimens.Space3)
                            .heightIn(min = Dimens.ControlHeight)
                            .testTag(SetupTags.PERMISSIONS_NOTIFICATIONS_ACTION),
                    ) {
                        Text(stringResource(R.string.permissions_notifications_action))
                    }
                }
                TextButton(
                    onClick = onOpenNotificationSettings,
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(min = Dimens.TouchTarget)
                        .testTag(SetupTags.PERMISSIONS_NOTIFICATIONS_SETTINGS),
                ) {
                    Text(stringResource(R.string.permissions_notifications_settings))
                }
                Text(
                    text = stringResource(R.string.permissions_notifications_denied),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(top = Dimens.Space2),
                )
            }
        }

        Text(
            text = stringResource(R.string.permissions_note),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

/** A permission's real state as a chip: icon, text and tone, never colour alone. */
@Composable
private fun PermissionStatus(
    granted: Boolean,
    grantedLabel: String,
    missingLabel: String,
    modifier: Modifier = Modifier,
) {
    KumihoStatusChip(
        tone = if (granted) StatusTone.Profit else StatusTone.Warning,
        label = if (granted) grantedLabel else missingLabel,
        modifier = modifier,
    )
}
