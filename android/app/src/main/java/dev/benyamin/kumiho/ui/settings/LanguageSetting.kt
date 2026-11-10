package dev.benyamin.kumiho.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.data.AppLanguage
import dev.benyamin.kumiho.ui.theme.Dimens

/** The three engine language values, in the order the control shows them. */
internal val LANGUAGE_OPTIONS = listOf("system", "en", "fa")

/**
 * The app-level language row. It is deliberately above the theme because it
 * changes the whole screen on the next composition, not after a restart.
 * `System` shows the language it actually resolves to, so the note never hides
 * the OS decision.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun LanguageSetting(
    preference: String,
    effective: AppLanguage,
    applying: Boolean,
    rawError: String?,
    onSelected: (String) -> Unit,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
    contentPadding: PaddingValues = PaddingValues(
        horizontal = Dimens.ScreenPadding,
        vertical = Dimens.Space2,
    ),
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .padding(contentPadding),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space1),
    ) {
        Text(
            text = stringResource(R.string.language_title),
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Text(
            text = stringResource(R.string.language_hint),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        SingleChoiceSegmentedButtonRow(
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = Dimens.Space1)
                .testTag(SettingsTags.LANGUAGE_CONTROL),
        ) {
            LANGUAGE_OPTIONS.forEachIndexed { index, option ->
                SegmentedButton(
                    selected = option == preference,
                    enabled = !applying,
                    onClick = { onSelected(option) },
                    shape = SegmentedButtonDefaults.itemShape(
                        index = index,
                        count = LANGUAGE_OPTIONS.size,
                    ),
                    label = {
                        Text(
                            text = languageLabel(option),
                            maxLines = 1,
                        )
                    },
                    modifier = Modifier
                        .defaultMinSize(minHeight = Dimens.TouchTarget)
                        .testTag(SettingsTags.languageOption(option)),
                )
            }
        }
        if (preference == "system") {
            Text(
                text = stringResource(
                    R.string.language_system_note,
                    languageLabel(effective.tag),
                ),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.testTag(SettingsTags.LANGUAGE_SYSTEM_NOTE),
            )
        }
        if (rawError != null) {
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .testTag(SettingsTags.LANGUAGE_ERROR),
                verticalArrangement = Arrangement.spacedBy(Dimens.Space1),
            ) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
                ) {
                    Text(
                        text = stringResource(R.string.language_failed),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error,
                        modifier = Modifier.weight(1f),
                    )
                    TextButton(
                        onClick = onRetry,
                        enabled = !applying,
                        modifier = Modifier.testTag(SettingsTags.LANGUAGE_RETRY),
                    ) {
                        Text(stringResource(R.string.settings_retry))
                    }
                }
                Text(
                    text = rawError,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    textAlign = TextAlign.Start,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        }
    }
}

/** The option label shared by phone and TV; unknown values fall back to System. */
@Composable
internal fun languageLabel(id: String): String = stringResource(
    when (id) {
        "en" -> R.string.language_english
        "fa" -> R.string.language_persian
        else -> R.string.language_system
    },
)
