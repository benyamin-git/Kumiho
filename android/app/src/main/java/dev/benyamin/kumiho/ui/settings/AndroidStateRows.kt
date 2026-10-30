package dev.benyamin.kumiho.ui.settings

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.components.KumihoStatusChip
import dev.benyamin.kumiho.ui.components.StatusTone
import dev.benyamin.kumiho.ui.theme.Dimens

/**
 * Android's lockdown state as the kill-switch row's value. Lockdown wins over
 * Always-on because it is the stricter setting; both are OS facts, never the
 * engine's `kill_switch` value (A10).
 */
enum class KillSwitchDisplay(@StringRes val labelRes: Int) {
    LOCKDOWN(R.string.settings_kill_switch_lockdown),
    ALWAYS_ON_ONLY(R.string.settings_kill_switch_always_on),
    OFF(R.string.settings_kill_switch_off),
}

fun killSwitchDisplay(alwaysOn: Boolean, lockdown: Boolean): KillSwitchDisplay = when {
    lockdown -> KillSwitchDisplay.LOCKDOWN
    alwaysOn -> KillSwitchDisplay.ALWAYS_ON_ONLY
    else -> KillSwitchDisplay.OFF
}

/** Android autoconnect is Always-on VPN: there is no separate flag. */
enum class AutoconnectDisplay(@StringRes val labelRes: Int) {
    ON(R.string.settings_autoconnect_on),
    OFF(R.string.settings_autoconnect_off),
}

fun autoconnectDisplay(alwaysOn: Boolean): AutoconnectDisplay =
    if (alwaysOn) AutoconnectDisplay.ON else AutoconnectDisplay.OFF

/** A read-only Android state row: value, explanation, and a real settings link. */
@Composable
fun AndroidStateRow(
    @StringRes titleRes: Int,
    @StringRes hintRes: Int,
    value: String,
    valueTag: String,
    @StringRes actionLabelRes: Int,
    actionTag: String,
    onAction: () -> Unit,
    modifier: Modifier = Modifier,
    tone: StatusTone = StatusTone.Neutral,
) {
    Surface(modifier = modifier.fillMaxWidth(), color = Color.Transparent) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space2),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
            ) {
                Text(
                    text = stringResource(titleRes),
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurface,
                    modifier = Modifier.weight(1f),
                )
                KumihoStatusChip(
                    tone = tone,
                    label = value,
                    modifier = Modifier
                        .testTag(valueTag)
                        .semantics(mergeDescendants = true) {},
                )
            }
            Text(
                text = stringResource(hintRes),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = Dimens.Space1),
            )
            TextButton(
                onClick = onAction,
                modifier = Modifier
                    .align(Alignment.End)
                    .heightIn(min = Dimens.TouchTarget)
                    .testTag(actionTag),
            ) {
                Text(stringResource(actionLabelRes))
            }
        }
    }
}

/** The notification permission row, with its system-settings action. */
@Composable
fun NotificationStateRow(
    allowed: Boolean,
    onOpenSettings: () -> Unit,
    modifier: Modifier = Modifier,
) {
    AndroidStateRow(
        titleRes = R.string.settings_notifications_title,
        hintRes = R.string.settings_notifications_hint,
        value = stringResource(
            if (allowed) R.string.settings_notifications_allowed else R.string.settings_notifications_missing,
        ),
        valueTag = SettingsTags.NOTIFICATIONS_STATE,
        actionLabelRes = R.string.settings_notifications_open,
        actionTag = SettingsTags.NOTIFICATIONS_OPEN,
        onAction = onOpenSettings,
        modifier = modifier,
        tone = if (allowed) StatusTone.Profit else StatusTone.Warning,
    )
}

/** The label and tone for the battery-optimization row. */
@StringRes
fun batteryLabelRes(state: BatteryOptimization): Int = when (state) {
    BatteryOptimization.UNRESTRICTED -> R.string.settings_battery_unrestricted
    BatteryOptimization.OPTIMIZED -> R.string.settings_battery_optimized
    BatteryOptimization.RESTRICTED -> R.string.settings_battery_restricted
    BatteryOptimization.UNKNOWN -> R.string.settings_battery_unknown
}

fun batteryStatusTone(state: BatteryOptimization): StatusTone = when (state) {
    BatteryOptimization.UNRESTRICTED -> StatusTone.Profit
    BatteryOptimization.OPTIMIZED -> StatusTone.Warning
    BatteryOptimization.RESTRICTED -> StatusTone.Loss
    BatteryOptimization.UNKNOWN -> StatusTone.Neutral
}

/** The battery-optimization row; Kumiho never asks to be exempted itself. */
@Composable
fun BatteryStateRow(
    state: BatteryOptimization,
    onOpenSettings: () -> Unit,
    modifier: Modifier = Modifier,
) {
    AndroidStateRow(
        titleRes = R.string.settings_battery_title,
        hintRes = R.string.settings_battery_hint,
        value = stringResource(batteryLabelRes(state)),
        valueTag = SettingsTags.BATTERY_STATE,
        actionLabelRes = R.string.settings_battery_open,
        actionTag = SettingsTags.BATTERY_OPEN,
        onAction = onOpenSettings,
        modifier = modifier,
        tone = batteryStatusTone(state),
    )
}

/** The Always-on shortcut in the app group; the OS VPN settings own the flag. */
@Composable
fun AlwaysOnStateRow(
    alwaysOn: Boolean,
    onOpenSettings: () -> Unit,
    modifier: Modifier = Modifier,
) {
    AndroidStateRow(
        titleRes = R.string.settings_always_on_title,
        hintRes = R.string.settings_always_on_hint,
        value = stringResource(
            if (alwaysOn) R.string.settings_always_on_state_on else R.string.settings_always_on_state_off,
        ),
        valueTag = SettingsTags.ALWAYS_ON_STATE,
        actionLabelRes = R.string.settings_always_on_open,
        actionTag = SettingsTags.ALWAYS_ON_OPEN,
        onAction = onOpenSettings,
        modifier = modifier,
    )
}
