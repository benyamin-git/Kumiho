package dev.benyamin.kumiho.ui.dashboard

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.BottomSheetDefaults
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.components.KumihoSectionHeader
import dev.benyamin.kumiho.ui.components.KumihoSettingRow
import dev.benyamin.kumiho.ui.theme.Dimens

/**
 * The connect options sheet: the compact location picker, the proxy-only
 * switch, the Android lockdown shortcut and the one Connect action.
 *
 * A connect while a session is active is refused by the service and by the
 * dashboard (T12: location changes are an explicit disconnect first), so the
 * sheet disables Connect and says why. The sheet never writes a setting and
 * never changes Android's Always-on/lockdown flags; the lockdown row opens the
 * system screen instead.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ConnectOptionsSheet(
    state: DashboardState,
    onClose: () -> Unit,
    onSelectRecommended: () -> Unit,
    onSelectSaved: () -> Unit,
    onToggleCountry: (String) -> Unit,
    onSelectCity: (String, String) -> Unit,
    onReloadLocations: () -> Unit,
    onToggleProxyOnly: (Boolean) -> Unit,
    onOpenVpnSettings: () -> Unit,
    onConnect: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    val proxyLabel = stringResource(R.string.dashboard_options_proxy_only)
    ModalBottomSheet(
        onDismissRequest = onClose,
        modifier = modifier.testTag(DashboardTags.SHEET),
        sheetState = sheetState,
        shape = MaterialTheme.shapes.large,
        containerColor = MaterialTheme.colorScheme.surfaceContainerLow,
        contentColor = MaterialTheme.colorScheme.onSurface,
        dragHandle = { BottomSheetDefaults.DragHandle() },
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .navigationBarsPadding()
                .padding(bottom = Dimens.Space3)
                .verticalScroll(rememberScrollState())
                .testTag(DashboardTags.SHEET_CONTENT),
        ) {
            KumihoSectionHeader(title = stringResource(R.string.dashboard_options_title))
            CompactLocationPicker(
                state = state,
                onSelectRecommended = onSelectRecommended,
                onSelectSaved = onSelectSaved,
                onToggleCountry = onToggleCountry,
                onSelectCity = onSelectCity,
                onReload = onReloadLocations,
            )
            HorizontalDivider(
                color = MaterialTheme.colorScheme.outlineVariant,
                modifier = Modifier.padding(vertical = Dimens.Space2),
            )
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = Dimens.Space4, vertical = Dimens.Space2),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = proxyLabel,
                        style = MaterialTheme.typography.bodyLarge,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    Text(
                        text = stringResource(R.string.dashboard_options_proxy_only_text),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Switch(
                    checked = state.options.proxyOnly,
                    onCheckedChange = onToggleProxyOnly,
                    modifier = Modifier
                        .testTag(DashboardTags.SHEET_PROXY_ONLY)
                        .semantics { contentDescription = proxyLabel },
                )
            }
            val lockdownValue = stringResource(
                if (state.lockdown) R.string.dashboard_on else R.string.dashboard_off,
            )
            KumihoSettingRow(
                title = stringResource(R.string.dashboard_options_lockdown),
                summary = stringResource(R.string.dashboard_options_lockdown_summary),
                value = lockdownValue,                onClick = onOpenVpnSettings,
                modifier = Modifier.testTag(DashboardTags.SHEET_LOCKDOWN),
            )
            val hint = sheetHintText(state.sheetHint)
            if (hint != null) {
                Text(
                    text = hint,
                    style = MaterialTheme.typography.bodySmall,
                    color = if (state.sheetHint == SheetHint.DISCONNECT_FIRST) {
                        MaterialTheme.colorScheme.onSurface
                    } else {
                        MaterialTheme.colorScheme.onSurfaceVariant
                    },
                    modifier = Modifier
                        .padding(horizontal = Dimens.Space4, vertical = Dimens.Space2)
                        .testTag(DashboardTags.SHEET_HINT),
                )
            }
            Button(
                onClick = onConnect,
                enabled = state.optionsConnectEnabled,
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = Dimens.Space4, vertical = Dimens.Space2)
                    .heightIn(min = Dimens.ControlHeight)
                    .testTag(DashboardTags.SHEET_CONNECT),
            ) {
                Text(stringResource(R.string.dashboard_options_connect))
            }
        }
    }
}

/** The reason the sheet's Connect is disabled, or null when it is enabled. */
@Composable
fun sheetHintText(hint: SheetHint): String? = when (hint) {
    SheetHint.NONE -> null
    SheetHint.DISCONNECT_FIRST -> stringResource(R.string.dashboard_options_disconnect_first)
    SheetHint.WAITING -> stringResource(R.string.dashboard_options_waiting)
    SheetHint.SIGN_IN -> stringResource(R.string.dashboard_options_sign_in)
    SheetHint.UNAVAILABLE -> stringResource(R.string.dashboard_options_unavailable)
}
