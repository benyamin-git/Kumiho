package dev.benyamin.kumiho.ui.tv.logs

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.OutlinedTextField
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Text
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.engine.LogEntry
import dev.benyamin.kumiho.ui.logs.LogFilterLevel
import dev.benyamin.kumiho.ui.logs.LogFormat
import dev.benyamin.kumiho.ui.logs.LogsActions
import dev.benyamin.kumiho.ui.logs.LogsMessage
import dev.benyamin.kumiho.ui.logs.LogsRoute
import dev.benyamin.kumiho.ui.logs.LogsTags
import dev.benyamin.kumiho.ui.logs.LogsUiState
import dev.benyamin.kumiho.ui.logs.logsMessageText
import dev.benyamin.kumiho.ui.theme.KumihoShapes
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors
import dev.benyamin.kumiho.ui.tv.components.TvButton
import dev.benyamin.kumiho.ui.tv.components.TvFocusableSurface
import dev.benyamin.kumiho.ui.tv.components.TvPageHeader
import dev.benyamin.kumiho.ui.tv.theme.TvDimens
import dev.benyamin.kumiho.ui.tv.theme.TvM3Typography

/** The television Logs entry; the phone route owns the subscription and export. */
@Composable
fun TvLogsRoute(container: AppContainer, modifier: Modifier = Modifier) {
    LogsRoute(container = container, modifier = modifier) { state, actions, launchError ->
        TvLogsScreen(state = state, actions = actions, launchError = launchError, modifier = modifier)
    }
}

/**
 * The ten-foot log console: the level filters as buttons, the search and
 * follow controls, and one focusable row per entry (select a row to copy it).
 */
@Composable
fun TvLogsScreen(
    state: LogsUiState,
    actions: LogsActions,
    modifier: Modifier = Modifier,
    launchError: String? = null,
) {
    val colors = MaterialTheme.colorScheme
    val listState = rememberLazyListState()
    val message = logsMessageText(state.message)

    LaunchedEffect(state.follow, state.entries.size) {
        if (state.follow && state.entries.isNotEmpty()) {
            listState.animateScrollToItem(state.entries.lastIndex)
        }
    }

    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(TvDimens.Space2)
            .testTag(LogsTags.SCREEN),
        verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
    ) {
        TvPageHeader(title = stringResource(R.string.tv_logs_title))
        if (message != null || launchError != null) {
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .background(colors.surfaceVariant)
                    .border(1.dp, colors.borderVariant, MaterialTheme.shapes.medium)
                    .padding(TvDimens.Space2)
                    .testTag(LogsTags.MESSAGE),
                verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
            ) {
                Text(
                    text = message.orEmpty(),
                    style = MaterialTheme.typography.bodyMedium,
                    color = colors.onSurface,
                )
                val detail = launchError ?: state.error
                if (!detail.isNullOrBlank()) {
                    Text(
                        text = detail,
                        style = MaterialTheme.typography.bodySmall,
                        color = colors.onSurfaceVariant,
                    )
                }
                TvButton(
                    label = stringResource(R.string.account_dismiss),
                    onClick = actions.onDismissMessage,
                    modifier = Modifier.testTag(LogsTags.MESSAGE_DISMISS),
                )
            }
        }

        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            LogFilterLevel.entries.forEach { level ->
                TvButton(
                    label = levelLabel(level),
                    onClick = { actions.onLevelSelected(level) },
                    modifier = Modifier.testTag(LogsTags.filter(level)),
                    primary = state.level == level,
                )
            }
            TvFocusableSurface(
                onClick = { actions.onFollowChange(!state.follow) },
                modifier = Modifier.testTag(LogsTags.FOLLOW),
            ) {
                Text(
                    text = (if (state.follow) "✓ " else "") + stringResource(R.string.logs_follow),
                    style = MaterialTheme.typography.bodyMedium,
                    color = colors.onSurface,
                )
            }
        }

        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            LogSearch(
                query = state.search,
                onQueryChange = actions.onSearchChange,
                onClear = actions.onClearSearch,
                modifier = Modifier.weight(1f),
            )
            TvButton(
                label = stringResource(R.string.logs_clear),
                onClick = actions.onClear,
                modifier = Modifier.testTag(LogsTags.CLEAR),
                enabled = !state.clearing,
            )
            TvButton(
                label = stringResource(R.string.logs_share),
                onClick = actions.onShare,
                modifier = Modifier.testTag(LogsTags.SHARE),
            )
        }

        when {
            state.loading -> Text(
                text = stringResource(R.string.logs_loading),
                style = MaterialTheme.typography.titleMedium,
                color = colors.onSurfaceVariant,
                modifier = Modifier
                    .padding(TvDimens.Space2)
                    .testTag(LogsTags.LOADING),
            )
            state.error != null && state.entries.isEmpty() -> Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(TvDimens.Space2)
                    .testTag(LogsTags.ERROR),
                verticalArrangement = Arrangement.spacedBy(TvDimens.Space1),
            ) {
                Text(
                    text = state.error,
                    style = MaterialTheme.typography.bodyMedium,
                    color = colors.onSurface,
                )
                TvButton(
                    label = stringResource(R.string.locations_retry),
                    onClick = actions.onRetry,
                )
            }
            state.isEmpty -> Text(
                text = stringResource(R.string.tv_logs_empty),
                style = MaterialTheme.typography.titleMedium,
                color = colors.onSurfaceVariant,
                modifier = Modifier
                    .padding(TvDimens.Space2)
                    .testTag(LogsTags.EMPTY),
            )
            else -> LazyColumn(
                state = listState,
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f)
                    .testTag(LogsTags.LIST),
                verticalArrangement = Arrangement.spacedBy(2.dp),
            ) {
                itemsIndexed(state.entries) { index, entry ->
                    LogRow(
                        entry = entry,
                        index = index,
                        onClick = { actions.onCopyLine(entry) },
                    )
                }
            }
        }
    }
}

@Composable
private fun LogRow(entry: LogEntry, index: Int, onClick: () -> Unit) {
    val colors = MaterialTheme.colorScheme
    TvFocusableSurface(
        onClick = onClick,
        modifier = Modifier
            .fillMaxWidth()
            .testTag(LogsTags.copy(index)),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(TvDimens.Space1),
            verticalAlignment = Alignment.Top,
        ) {
            Text(
                text = entry.level.name.padEnd(5),
                style = MaterialTheme.typography.bodySmall,
                color = when (entry.level) {
                    dev.benyamin.kumiho.engine.LogLevel.ERROR -> LocalKumihoColors.current.loss
                    dev.benyamin.kumiho.engine.LogLevel.WARN -> LocalKumihoColors.current.warning
                    else -> colors.onSurfaceVariant
                },
            )
            Text(
                text = LogFormat.line(entry),
                style = MaterialTheme.typography.bodySmall,
                color = colors.onSurface,
                modifier = Modifier.weight(1f),
            )
        }
    }
}

@Composable
private fun LogSearch(
    query: String,
    onQueryChange: (String) -> Unit,
    onClear: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val house = LocalKumihoColors.current
    val focusManager = LocalFocusManager.current
    androidx.compose.material3.MaterialTheme(
        colorScheme = house.toColorScheme(),
        typography = TvM3Typography,
        shapes = KumihoShapes,
    ) {
        OutlinedTextField(
            value = query,
            onValueChange = onQueryChange,
            label = { androidx.compose.material3.Text(stringResource(R.string.logs_search_label)) },
            singleLine = true,
            trailingIcon = {
                if (query.isNotEmpty()) {
                    androidx.compose.material3.TextButton(
                        onClick = onClear,
                        modifier = Modifier.testTag(LogsTags.SEARCH_CLEAR),
                    ) {
                        androidx.compose.material3.Text(stringResource(R.string.logs_search_clear))
                    }
                }
            },
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
            keyboardActions = KeyboardActions(onSearch = { focusManager.clearFocus() }),
            modifier = modifier
                .heightIn(min = 64.dp)
                .testTag(LogsTags.SEARCH),
        )
    }
}

@Composable
private fun levelLabel(level: LogFilterLevel): String = when (level) {
    LogFilterLevel.ALL -> stringResource(R.string.logs_filter_all)
    LogFilterLevel.DEBUG -> stringResource(R.string.logs_filter_debug)
    LogFilterLevel.INFO -> stringResource(R.string.logs_filter_info)
    LogFilterLevel.WARN -> stringResource(R.string.logs_filter_warn)
    LogFilterLevel.ERROR -> stringResource(R.string.logs_filter_error)
}
