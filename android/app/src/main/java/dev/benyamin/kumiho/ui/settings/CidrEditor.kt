package dev.benyamin.kumiho.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Info
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.unit.dp
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.theme.Dimens

/**
 * The `exclude_cidrs` editor: one multiline field for a comma-separated IPv4
 * list, an "IPv4 only" hint and the C1 note that the tunnel DNS host stays
 * routed even when its range is excluded.
 *
 * Validation is [CidrValidator]'s; this composable only draws the field and
 * the two notes. The dialog owns the inline error text so every editor reports
 * a rejected value the same way.
 */
@Composable
fun CidrEditor(
    value: String,
    onValueChange: (String) -> Unit,
    enabled: Boolean,
    invalid: SettingInput.Invalid?,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier.fillMaxWidth(),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space2),
    ) {
        OutlinedTextField(
            value = value,
            onValueChange = onValueChange,
            modifier = Modifier
                .fillMaxWidth()
                .testTag(SettingsTags.DIALOG_FIELD),
            label = { Text(stringResource(R.string.settings_cidr_label)) },
            enabled = enabled,
            isError = invalid != null,
            minLines = 3,
            maxLines = 5,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii),
        )
        CidrNote(
            text = stringResource(R.string.settings_cidr_hint),
            tag = SettingsTags.CIDR_HINT,
        )
        CidrNote(
            text = stringResource(R.string.settings_cidr_dns_note),
            tag = SettingsTags.CIDR_DNS_NOTE,
        )
    }
}

@Composable
private fun CidrNote(text: String, tag: String) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .testTag(tag),
        horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        verticalAlignment = Alignment.Top,
    ) {
        Box(modifier = Modifier.padding(top = 2.dp)) {
            Icon(
                imageVector = Icons.Filled.Info,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(16.dp),
            )
        }
        Text(
            text = text,
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}
