package dev.benyamin.kumiho.ui.settings

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.components.KumihoStatusChip
import dev.benyamin.kumiho.ui.components.StatusTone
import dev.benyamin.kumiho.ui.theme.Dimens
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors

/**
 * One settable engine row: label, hint, current value, the override chip with
 * its Reset, and an inline error. A boolean toggles itself; every other kind
 * opens [SettingEditorDialog].
 *
 * [locked] is "some change is in flight": every engine row is disabled while
 * any one of them is being written. [applying] says this row owns that change
 * and replaces its value with "Applying…".
 */
@Composable
fun EngineSettingRow(
    spec: SettingSpec,
    value: String,
    overridden: Boolean,
    locked: Boolean,
    applying: Boolean,
    error: SettingsRowError?,
    onToggle: (Boolean) -> Unit,
    onEdit: () -> Unit,
    onReset: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val enabled = !locked
    val boolean = spec.kind == SettingKind.BOOLEAN
    val checked = value == "on"
    val open = if (boolean || !enabled) null else onEdit
    val rowModifier = if (open != null) {
        Modifier.clickable(role = Role.Button, onClick = open)
    } else {
        Modifier
    }
    Surface(
        modifier = modifier.fillMaxWidth(),
        color = Color.Transparent,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space2),
        ) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .defaultMinSize(minHeight = Dimens.TouchTarget)
                    .testTag(SettingsTags.row(spec.key))
                    .then(rowModifier),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(Dimens.Space3),
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = stringResource(spec.labelRes),
                        style = MaterialTheme.typography.bodyLarge,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    Text(
                        text = stringResource(spec.hintRes),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                when {
                    applying -> ApplyingValue(key = spec.key)
                    boolean -> BooleanValue(
                        key = spec.key,
                        checked = checked,
                        enabled = enabled,
                        onToggle = onToggle,
                    )
                    else -> DisplayValue(
                        key = spec.key,
                        text = settingDisplayValue(spec, value),
                    )
                }
            }
            if (overridden) {
                OverrideMark(
                    key = spec.key,
                    label = stringResource(spec.labelRes),
                    enabled = enabled,
                    onReset = onReset,
                )
            }
            if (error != null) {
                SettingRowErrorText(key = spec.key, error = error)
            }
        }
    }
}

/** The in-flight row: the words carry the state, the spinner only repeats it. */
@Composable
private fun ApplyingValue(key: String) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
    ) {
        CircularProgressIndicator(
            modifier = Modifier.size(16.dp),
            strokeWidth = 2.dp,
            color = MaterialTheme.colorScheme.primary,
        )
        Text(
            text = stringResource(R.string.settings_applying),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.testTag(SettingsTags.applying(key)),
        )
    }
}

/** On/off is written out beside the switch, so position is not the only cue. */
@Composable
private fun BooleanValue(
    key: String,
    checked: Boolean,
    enabled: Boolean,
    onToggle: (Boolean) -> Unit,
) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
    ) {
        Text(
            text = stringResource(if (checked) R.string.settings_on else R.string.settings_off),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.testTag(SettingsTags.value(key)),
        )
        Switch(
            checked = checked,
            onCheckedChange = onToggle,
            enabled = enabled,
            modifier = Modifier.testTag(SettingsTags.toggle(key)),
        )
    }
}

/** The current value and the chevron that promises an editor. */
@Composable
private fun DisplayValue(
    key: String,
    text: String,
) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Dimens.Space1),
    ) {
        Text(
            text = text,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.testTag(SettingsTags.value(key)),
        )
        Icon(
            imageVector = Icons.AutoMirrored.Filled.KeyboardArrowRight,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.size(24.dp),
        )
    }
}

/** The override chip and its one action: drop the runtime override. */
@Composable
private fun OverrideMark(
    key: String,
    label: String,
    enabled: Boolean,
    onReset: () -> Unit,
) {
    val resetDescription = stringResource(R.string.settings_reset_label, label)
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = Dimens.Space1),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
    ) {
        KumihoStatusChip(
            tone = StatusTone.Warning,
            label = stringResource(R.string.settings_overridden),
            modifier = Modifier.testTag(SettingsTags.override(key)),
        )
        TextButton(
            onClick = onReset,
            enabled = enabled,
            modifier = Modifier
                .heightIn(min = Dimens.TouchTarget)
                .testTag(SettingsTags.reset(key))
                .semantics { contentDescription = resetDescription },
        ) {
            Text(stringResource(R.string.settings_reset))
        }
    }
}

/** One inline row error, with an icon so meaning is not carried by colour. */
@Composable
private fun SettingRowErrorText(key: String, error: SettingsRowError) {
    val tint = LocalKumihoColors.current.loss
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = Dimens.Space1)
            .testTag(SettingsTags.error(key)),
        verticalAlignment = Alignment.Top,
        horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
    ) {
        Icon(
            imageVector = Icons.Filled.Warning,
            contentDescription = null,
            tint = tint,
            modifier = Modifier
                .padding(top = 2.dp)
                .size(16.dp),
        )
        Column {
            if (error.validation != null) {
                Text(
                    text = settingValidationText(error.validation),
                    style = MaterialTheme.typography.bodySmall,
                    color = tint,
                )
            } else {
                Text(
                    text = stringResource(R.string.settings_validation_apply_failed),
                    style = MaterialTheme.typography.bodySmall,
                    color = tint,
                )
                if (!error.text.isNullOrBlank()) {
                    Text(
                        text = error.text,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

/**
 * The editor for an enum, integer, text or CIDR row. Client validation runs on
 * Apply and keeps the dialog open with the reason; a valid value is handed to
 * the view model, which closes the dialog and writes it.
 *
 * The dialog's 16dp radius is the house dialog radius, never a Material default.
 */
@Composable
fun SettingEditorDialog(
    spec: SettingSpec,
    initialValue: String,
    locked: Boolean,
    onApply: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    var text by remember(spec.key) { mutableStateOf(initialValue) }
    var invalid by remember(spec.key) { mutableStateOf<SettingInput.Invalid?>(null) }

    val apply: () -> Unit = {
        when (val input = SettingsMapper.serialize(spec, text)) {
            is SettingInput.Valid -> onApply(input.value)
            is SettingInput.Invalid -> invalid = input
        }
    }

    AlertDialog(
        onDismissRequest = onDismiss,
        modifier = Modifier.testTag(SettingsTags.DIALOG),
        shape = MaterialTheme.shapes.large,
        title = {
            Text(
                text = stringResource(
                    R.string.settings_editor_title,
                    stringResource(spec.labelRes),
                ),
            )
        },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(Dimens.Space2)) {
                Text(
                    text = stringResource(spec.hintRes),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                if (spec.kind == SettingKind.ENUM) {
                    EnumOptions(
                        spec = spec,
                        selected = text,
                        enabled = !locked,
                        onSelect = {
                            text = it
                            invalid = null
                        },
                    )
                } else {
                    OutlinedTextField(
                        value = text,
                        onValueChange = {
                            text = it
                            invalid = null
                        },
                        modifier = Modifier
                            .fillMaxWidth()
                            .testTag(SettingsTags.DIALOG_FIELD),
                        singleLine = spec.kind != SettingKind.CIDR_LIST,
                        maxLines = if (spec.kind == SettingKind.CIDR_LIST) 3 else 1,
                        enabled = !locked,
                        isError = invalid != null,
                        keyboardOptions = if (spec.kind == SettingKind.INTEGER) {
                            KeyboardOptions(keyboardType = KeyboardType.Number)
                        } else {
                            KeyboardOptions.Default
                        },
                        placeholder = spec.placeholderRes?.let { placeholder ->
                            { Text(stringResource(placeholder)) }
                        },
                    )
                }
                invalid?.let { problem ->
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        verticalAlignment = Alignment.Top,
                        horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
                    ) {
                        Icon(
                            imageVector = Icons.Filled.Warning,
                            contentDescription = null,
                            tint = LocalKumihoColors.current.loss,
                            modifier = Modifier
                                .padding(top = 2.dp)
                                .size(16.dp),
                        )
                        Text(
                            text = settingValidationText(problem),
                            style = MaterialTheme.typography.bodySmall,
                            color = LocalKumihoColors.current.loss,
                            modifier = Modifier.testTag(SettingsTags.DIALOG_ERROR),
                        )
                    }
                }
            }
        },
        confirmButton = {
            TextButton(
                onClick = apply,
                enabled = !locked,
                modifier = Modifier
                    .heightIn(min = Dimens.TouchTarget)
                    .testTag(SettingsTags.DIALOG_APPLY),
            ) {
                Text(stringResource(R.string.settings_apply))
            }
        },
        dismissButton = {
            TextButton(
                onClick = onDismiss,
                modifier = Modifier
                    .heightIn(min = Dimens.TouchTarget)
                    .testTag(SettingsTags.DIALOG_CANCEL),
            ) {
                Text(stringResource(R.string.settings_cancel))
            }
        },
    )
}

/** The enum choices: a radio list, each 48dp tall and named in words. */
@Composable
private fun EnumOptions(
    spec: SettingSpec,
    selected: String,
    enabled: Boolean,
    onSelect: (String) -> Unit,
) {
    Column {
        spec.options.forEach { option ->
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .defaultMinSize(minHeight = Dimens.TouchTarget)
                    .selectable(
                        selected = option == selected,
                        enabled = enabled,
                        role = Role.RadioButton,
                        onClick = { onSelect(option) },
                    )
                    .testTag(SettingsTags.option(option)),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                RadioButton(selected = option == selected, onClick = null, enabled = enabled)
                Text(
                    text = settingOptionLabel(option),
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurface,
                    modifier = Modifier.padding(start = Dimens.Space2),
                )
            }
        }
    }
}

/** The label shown for a value, or the honest placeholder when it is empty. */
@Composable
fun settingDisplayValue(spec: SettingSpec, value: String): String = when {
    value.isBlank() -> spec.placeholderRes?.let { stringResource(it) }
        ?: stringResource(R.string.settings_placeholder_none)
    spec.kind == SettingKind.ENUM -> settingOptionLabel(value)
    else -> value
}

/** A known enum option's name; an unknown engine value stays raw rather than guessed. */
@Composable
fun settingOptionLabel(option: String): String {
    val res = SettingsCatalog.optionLabelRes(option)
    return if (res != null) stringResource(res) else option
}

/** Client validation copy for one rejected value. */
@Composable
fun settingValidationText(invalid: SettingInput.Invalid): String = when (invalid.error) {
    SettingError.NOT_A_NUMBER -> stringResource(R.string.settings_validation_number)
    SettingError.OUT_OF_RANGE -> {
        val range = invalid.range
        if (range != null) {
            stringResource(R.string.settings_validation_range, range.min, range.max)
        } else {
            stringResource(R.string.settings_validation_number)
        }
    }
    SettingError.NOT_AN_OPTION -> stringResource(R.string.settings_validation_option)
    SettingError.INVALID_CIDR -> stringResource(
        R.string.settings_validation_cidr,
        invalid.detail.orEmpty(),
    )
    SettingError.IPV6_NOT_SUPPORTED -> stringResource(R.string.settings_validation_cidr_ipv6)
    SettingError.READ_ONLY -> stringResource(R.string.settings_validation_read_only)
}
