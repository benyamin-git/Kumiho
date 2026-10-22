package dev.benyamin.kumiho.ui.settings

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.theme.Dimens
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.ThemeId

/**
 * The Theme row: a three-way segmented control. Light, Dark and OLED are the
 * only options; Material You is never offered. Each choice carries its name and
 * a selected mark, not colour alone.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ThemeSetting(
    theme: ThemeId,
    onThemeSelected: (ThemeId) -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space2),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space1),
    ) {
        Text(
            text = stringResource(R.string.settings_theme_title),
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Text(
            text = stringResource(R.string.settings_theme_hint),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        SingleChoiceSegmentedButtonRow(
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = Dimens.Space1)
                .testTag(SettingsTags.THEME_CONTROL),
        ) {
            ThemeId.entries.forEachIndexed { index, option ->
                SegmentedButton(
                    selected = option == theme,
                    onClick = { onThemeSelected(option) },
                    shape = SegmentedButtonDefaults.itemShape(index = index, count = ThemeId.entries.size),
                    label = {
                        Text(
                            text = themeLabel(option),
                            maxLines = 1,
                        )
                    },
                    modifier = Modifier
                        .defaultMinSize(minHeight = Dimens.TouchTarget)
                        .testTag(SettingsTags.themeOption(option.id)),
                )
            }
        }
    }
}

/**
 * The Accent row: the six house accents in a two-column grid. Each option shows
 * its name, its swatch from the Go palette when available, and a check plus the
 * Selected label when chosen, so colour is never the only cue.
 */
@Composable
fun AccentSetting(
    accent: KumihoAccent,
    previews: Map<KumihoAccent, Color>,
    onAccentSelected: (KumihoAccent) -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space2),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space1),
    ) {
        Text(
            text = stringResource(R.string.settings_accent_title),
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Text(
            text = stringResource(R.string.settings_accent_hint),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Column(
            modifier = Modifier.padding(top = Dimens.Space1),
            verticalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            KumihoAccent.entries.chunked(2).forEach { pair ->
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
                ) {
                    pair.forEach { option ->
                        AccentChoice(
                            accent = option,
                            swatch = previews[option],
                            selected = option == accent,
                            onSelect = onAccentSelected,
                            modifier = Modifier.weight(1f),
                        )
                    }
                    if (pair.size == 1) {
                        Box(modifier = Modifier.weight(1f))
                    }
                }
            }
        }
    }
}

@Composable
private fun AccentChoice(
    accent: KumihoAccent,
    swatch: Color?,
    selected: Boolean,
    onSelect: (KumihoAccent) -> Unit,
    modifier: Modifier = Modifier,
) {
    val border = if (selected) {
        MaterialTheme.colorScheme.primary
    } else {
        MaterialTheme.colorScheme.outlineVariant
    }
    Surface(
        modifier = modifier
            .defaultMinSize(minHeight = Dimens.TouchTarget)
            .testTag(SettingsTags.accentOption(accent.id))
            .semantics { this.selected = selected },
        shape = MaterialTheme.shapes.medium,
        color = if (selected) {
            MaterialTheme.colorScheme.primary.copy(alpha = 0.12f)
        } else {
            MaterialTheme.colorScheme.surfaceContainer
        },
        border = BorderStroke(if (selected) 2.dp else 1.dp, border),
    ) {
        Row(
            modifier = Modifier
                .selectable(
                    selected = selected,
                    role = Role.RadioButton,
                    onClick = { onSelect(accent) },
                )
                .padding(horizontal = Dimens.Space3, vertical = Dimens.Space2)
                .defaultMinSize(minHeight = Dimens.TouchTarget),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            if (swatch != null) {
                Box(
                    modifier = Modifier
                        .size(20.dp)
                        .background(swatch, CircleShape)
                        .border(1.dp, MaterialTheme.colorScheme.outline, CircleShape),
                )
            }
            Text(
                text = accentLabel(accent),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurface,
                maxLines = 1,
                modifier = Modifier.weight(1f, fill = false),
            )
            if (selected) {
                Icon(
                    imageVector = Icons.Filled.Check,
                    contentDescription = stringResource(R.string.settings_selected),
                    tint = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.size(20.dp),
                )
            }
        }
    }
}

/** The theme's user-visible name; a future id falls back to its label. */
@Composable
private fun themeLabel(theme: ThemeId): String {
    val res = SettingsCatalog.themeLabelRes(theme.id)
    return if (res != null) stringResource(res) else theme.label
}

/** The accent's user-visible name; a future id falls back to its label. */
@Composable
private fun accentLabel(accent: KumihoAccent): String {
    val res = SettingsCatalog.accentLabelRes(accent.id)
    return if (res != null) stringResource(res) else accent.label
}
