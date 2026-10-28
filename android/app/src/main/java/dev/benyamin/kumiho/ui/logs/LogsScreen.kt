package dev.benyamin.kumiho.ui.logs

import android.content.Intent
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.gestures.detectVerticalDragGestures
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.lazy.LazyListState
import androidx.core.content.FileProvider
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.data.LogExporter
import dev.benyamin.kumiho.engine.EngineClient
import dev.benyamin.kumiho.engine.LogEntry
import dev.benyamin.kumiho.ui.components.KumihoChip
import dev.benyamin.kumiho.ui.components.KumihoEmptyState
import dev.benyamin.kumiho.ui.components.KumihoErrorState
import dev.benyamin.kumiho.ui.components.KumihoLoadingState
import dev.benyamin.kumiho.ui.theme.Dimens
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors
import java.time.Instant

/** Test tags for the logs console. */
object LogsTags {
    const val SCREEN = "logs_screen"
    const val LIST = "logs_list"
    const val SEARCH = "logs_search"
    const val SEARCH_CLEAR = "logs_search_clear"
    const val FOLLOW = "logs_follow"
    const val CLEAR = "logs_clear"
    const val SHARE = "logs_share"
    const val LOADING = "logs_loading"
    const val ERROR = "logs_error"
    const val EMPTY = "logs_empty"
    const val MESSAGE = "logs_message"
    const val MESSAGE_DISMISS = "logs_message_dismiss"
    const val FILTER_PREFIX = "logs_filter_"

    fun filter(level: LogFilterLevel): String = FILTER_PREFIX + level.name.lowercase()
    fun copy(index: Int): String = "logs_copy_$index"
}

/** The production gateway over the already-bound engine client. */
class EngineLogsGateway(private val engine: EngineClient) : LogsGateway {
    override suspend fun subscribe(level: String?) = engine.logsSubscribe(level)

    override suspend fun snapshot(level: String?): List<LogEntry> = engine.logsSnapshot(level)

    override suspend fun unsubscribe() = engine.logsUnsubscribe()

    override suspend fun clear() = engine.logsClear()

    override val stream = engine.logs
}

/**
 * The Logs entry point. The console subscribes while it is visible and
 * unsubscribes when its composition is disposed; the engine snapshot opens the
 * screen with the retained ring, and the share sheet gets a private cache file
 * through a FileProvider URI.
 */
@Suppress("DEPRECATION") // LocalClipboardManager is the non-suspend API this screen needs.
@Composable
fun LogsRoute(
    container: AppContainer,
    modifier: Modifier = Modifier,
    content: @Composable (LogsUiState, LogsActions, String?) -> Unit = { state, actions, launchError ->
        LogsScreen(state = state, actions = actions, modifier = modifier, launchError = launchError)
    },
) {
    val coordinator = container.vpnServiceCoordinator
    val serviceState by coordinator.state.collectAsStateWithLifecycle()
    val engine by coordinator.engine.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val clipboard = LocalClipboardManager.current
    val client = engine
    if (client == null) {
        LogsWaitingSurface(error = serviceState.error, modifier = modifier)
        return
    }

    val gateway = remember(client) { EngineLogsGateway(client) }
    val key = remember(client) { "logs-${System.identityHashCode(client)}" }
    val viewModel: LogsViewModel = viewModel(key = key) {
        LogsViewModel(gateway)
    }
    val state by viewModel.state.collectAsStateWithLifecycle()

    DisposableEffect(viewModel) {
        viewModel.onVisible()
        onDispose { viewModel.onHidden() }
    }

    var launchError by remember { mutableStateOf<String?>(null) }
    val exportFailed = stringResource(R.string.logs_export_failed)
    val shareTitle = stringResource(R.string.logs_share_title)

    val actions = LogsActions(
        onLevelSelected = viewModel::setLevel,
        onSearchChange = viewModel::setSearch,
        onClearSearch = viewModel::clearSearch,
        onFollowChange = viewModel::setFollow,
        onClear = viewModel::clear,
        onCopyLine = { entry ->
            clipboard.setText(AnnotatedString(LogFormat.copyLine(entry)))
            viewModel.notifyCopied()
        },
        onShare = {
            try {
                val file = LogExporter.writeTo(
                    LogExporter.exportsDir(context.cacheDir),
                    state.entries,
                    Instant.now(),
                )
                val uri = FileProvider.getUriForFile(
                    context,
                    "${context.packageName}.fileprovider",
                    file,
                )
                val send = Intent(Intent.ACTION_SEND)
                    .setType("text/plain")
                    .putExtra(Intent.EXTRA_STREAM, uri)
                    .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
                context.startActivity(Intent.createChooser(send, shareTitle))
                launchError = null
            } catch (error: RuntimeException) {
                launchError = error.message?.takeIf { it.isNotBlank() } ?: exportFailed
                viewModel.notifyExportFailed()
            }
        },
        onDismissMessage = {
            launchError = null
            viewModel.dismissMessage()
        },
        onRetry = viewModel::retry,
    )
    content(state, actions, launchError)
}

/** The stateless logs console. */
@Composable
fun LogsScreen(
    state: LogsUiState,
    actions: LogsActions,
    modifier: Modifier = Modifier,
    launchError: String? = null,
) {
    val listState = rememberLazyListState()
    val message = logsMessageText(state.message)
    val messageIsWarning = state.message == LogsMessage.CLEAR_FAILED ||
        state.message == LogsMessage.LOAD_FAILED ||
        state.message == LogsMessage.EXPORT_FAILED

    LaunchedEffect(state.follow, state.entries.size) {
        if (state.follow && state.entries.isNotEmpty()) {
            listState.animateScrollToItem(state.entries.lastIndex)
        }
    }

    Column(modifier = modifier.fillMaxSize().testTag(LogsTags.SCREEN)) {
        if (message != null || launchError != null) {
            LogsBanner(
                title = message.orEmpty(),
                detail = launchError ?: state.error,
                warning = messageIsWarning,
                onDismiss = actions.onDismissMessage,
                modifier = Modifier.padding(
                    start = Dimens.ScreenPadding,
                    end = Dimens.ScreenPadding,
                    top = Dimens.Space3,
                ),
            )
        }
        LogsControls(state = state, actions = actions)
        Box(modifier = Modifier.fillMaxWidth().weight(1f)) {
            when {
                state.loading && state.entries.isEmpty() -> KumihoLoadingState(
                    label = stringResource(R.string.logs_loading),
                    modifier = Modifier.testTag(LogsTags.LOADING),
                )
                state.error != null && state.entries.isEmpty() -> KumihoErrorState(
                    message = state.error,
                    title = stringResource(R.string.logs_load_failed_title),
                    onRetry = actions.onRetry,
                    modifier = Modifier
                        .padding(Dimens.ScreenPadding)
                        .testTag(LogsTags.ERROR),
                )
                state.isEmpty -> KumihoEmptyState(
                    title = stringResource(R.string.logs_empty_title),
                    message = if (state.search.isNotBlank()) {
                        stringResource(R.string.logs_no_match, state.search)
                    } else {
                        stringResource(R.string.logs_empty_message)
                    },
                    modifier = Modifier.testTag(LogsTags.EMPTY),
                )
                else -> LogConsole(
                    entries = state.entries,
                    listState = listState,
                    onCopy = actions.onCopyLine,
                    onUserScroll = { actions.onFollowChange(false) },
                )
            }
        }
    }
}

@Composable
private fun LogsControls(state: LogsUiState, actions: LogsActions) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = Dimens.ScreenPadding, vertical = Dimens.Space2),
        verticalArrangement = Arrangement.spacedBy(Dimens.Space2),
    ) {
        OutlinedTextField(
            value = state.search,
            onValueChange = actions.onSearchChange,
            label = { Text(stringResource(R.string.logs_search_label)) },
            singleLine = true,
            trailingIcon = {
                if (state.search.isNotEmpty()) {
                    TextButton(
                        onClick = actions.onClearSearch,
                        modifier = Modifier.testTag(LogsTags.SEARCH_CLEAR),
                    ) {
                        Text(stringResource(R.string.logs_search_clear))
                    }
                }
            },
            modifier = Modifier
                .fillMaxWidth()
                .testTag(LogsTags.SEARCH),
        )
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .horizontalScroll(rememberScrollState()),
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            LogFilterLevel.entries.forEach { level ->
                KumihoChip(
                    label = stringResource(level.labelRes()),
                    selected = state.level == level,
                    onClick = { actions.onLevelSelected(level) },
                    modifier = Modifier
                        .heightIn(min = Dimens.TouchTarget)
                        .testTag(LogsTags.filter(level)),
                )
            }
        }
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            KumihoChip(
                label = stringResource(R.string.logs_follow),
                selected = state.follow,
                onClick = { actions.onFollowChange(!state.follow) },
                modifier = Modifier
                    .heightIn(min = Dimens.TouchTarget)
                    .testTag(LogsTags.FOLLOW),
            )
            OutlinedButton(
                onClick = actions.onClear,
                enabled = !state.clearing,
                modifier = Modifier
                    .weight(1f)
                    .heightIn(min = Dimens.TouchTarget)
                    .testTag(LogsTags.CLEAR),
            ) {
                Text(
                    if (state.clearing) {
                        stringResource(R.string.logs_clearing)
                    } else {
                        stringResource(R.string.logs_clear)
                    },
                )
            }
            OutlinedButton(
                onClick = actions.onShare,
                enabled = state.entries.isNotEmpty(),
                modifier = Modifier
                    .weight(1f)
                    .heightIn(min = Dimens.TouchTarget)
                    .testTag(LogsTags.SHARE),
            ) {
                Text(stringResource(R.string.logs_share))
            }
        }
    }
}

@Composable
private fun LogConsole(
    entries: List<LogEntry>,
    listState: LazyListState,
    onCopy: (LogEntry) -> Unit,
    onUserScroll: () -> Unit,
) {
    LazyColumn(
        state = listState,
        modifier = Modifier
            .fillMaxSize()
            .pointerInput(Unit) {
                detectVerticalDragGestures { _, _ -> onUserScroll() }
            }
            .testTag(LogsTags.LIST),
        contentPadding = PaddingValues(bottom = Dimens.Space4),
    ) {
        itemsIndexed(entries, key = { index, entry -> "$index-${entry.time}-${entry.tag}" }) { index, entry ->
            LogRow(entry = entry, index = index, onCopy = onCopy)
            HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
        }
        if (entries.size >= LogFilter.MAX_ENTRIES) {
            item(key = "cap") {
                Text(
                    text = stringResource(R.string.logs_cap, LogFilter.MAX_ENTRIES),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    textAlign = TextAlign.Center,
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(Dimens.Space3),
                )
            }
        }
    }
}

@Composable
private fun LogRow(entry: LogEntry, index: Int, onCopy: (LogEntry) -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = Dimens.ScreenPadding, end = Dimens.Space1, top = Dimens.Space2),
        verticalAlignment = Alignment.Top,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Row(
                horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = LogFormat.clock(entry.time),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text(
                    text = entry.level.wire.uppercase(),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text(
                    text = entry.tag,
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.primary,
                )
            }
            Text(
                text = entry.message,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurface,
                modifier = Modifier.padding(bottom = Dimens.Space2),
            )
        }
        TextButton(
            onClick = { onCopy(entry) },
            modifier = Modifier
                .heightIn(min = Dimens.TouchTarget)
                .testTag(LogsTags.copy(index)),
        ) {
            Text(stringResource(R.string.logs_copy))
        }
    }
}

@Composable
private fun LogsBanner(
    title: String,
    detail: String?,
    warning: Boolean,
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val role = if (warning) LocalKumihoColors.current.warning else MaterialTheme.colorScheme.primary
    Surface(
        modifier = modifier.fillMaxWidth(),
        shape = MaterialTheme.shapes.medium,
        color = role.copy(alpha = 0.12f),
        border = BorderStroke(1.dp, role.copy(alpha = 0.5f)),
    ) {
        Row(
            modifier = Modifier.padding(Dimens.Space3),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Dimens.Space2),
        ) {
            if (warning) {
                Icon(
                    imageVector = Icons.Filled.Warning,
                    contentDescription = null,
                    tint = role,
                    modifier = Modifier.size(20.dp),
                )
            }
            Column(modifier = Modifier.weight(1f)) {
                if (title.isNotBlank()) {
                    Text(
                        text = title,
                        style = MaterialTheme.typography.titleSmall,
                        color = role,
                    )
                }
                if (!detail.isNullOrBlank()) {
                    Text(
                        text = detail,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                }
            }
            TextButton(
                onClick = onDismiss,
                modifier = Modifier.testTag(LogsTags.MESSAGE_DISMISS),
            ) {
                Text(stringResource(R.string.logs_dismiss))
            }
        }
    }
}

@Composable
private fun LogsWaitingSurface(error: String?, modifier: Modifier = Modifier) {
    Surface(modifier = modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            if (error == null) {
                KumihoLoadingState(
                    label = stringResource(R.string.logs_loading),
                    modifier = Modifier.testTag(LogsTags.LOADING),
                )
            } else {
                Column(
                    modifier = Modifier.padding(Dimens.Space5),
                    horizontalAlignment = Alignment.CenterHorizontally,
                    verticalArrangement = Arrangement.spacedBy(Dimens.Space3),
                ) {
                    Icon(
                        imageVector = Icons.Filled.Warning,
                        contentDescription = null,
                        tint = MaterialTheme.colorScheme.onSurface,
                        modifier = Modifier.size(40.dp),
                    )
                    Text(
                        text = stringResource(R.string.logs_engine_unavailable_title),
                        style = MaterialTheme.typography.titleLarge,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    Text(
                        text = stringResource(R.string.logs_engine_unavailable),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        textAlign = TextAlign.Center,
                    )
                    Text(
                        text = error,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        textAlign = TextAlign.Center,
                    )
                }
            }
        }
    }
}

@Composable
internal fun logsMessageText(message: LogsMessage?): String? = when (message) {
    null -> null
    LogsMessage.CLEARED -> stringResource(R.string.logs_cleared)
    LogsMessage.COPIED -> stringResource(R.string.logs_copied)
    LogsMessage.CLEAR_FAILED -> stringResource(R.string.logs_clear_failed)
    LogsMessage.LOAD_FAILED -> stringResource(R.string.logs_load_failed_title)
    LogsMessage.EXPORT_FAILED -> stringResource(R.string.logs_export_failed)
}

private fun LogFilterLevel.labelRes(): Int = when (this) {
    LogFilterLevel.ALL -> R.string.logs_filter_all
    LogFilterLevel.DEBUG -> R.string.logs_filter_debug
    LogFilterLevel.INFO -> R.string.logs_filter_info
    LogFilterLevel.WARN -> R.string.logs_filter_warn
    LogFilterLevel.ERROR -> R.string.logs_filter_error
}

/** The callbacks the stateless console needs. */
data class LogsActions(
    val onLevelSelected: (LogFilterLevel) -> Unit,
    val onSearchChange: (String) -> Unit,
    val onClearSearch: () -> Unit,
    val onFollowChange: (Boolean) -> Unit,
    val onClear: () -> Unit,
    val onCopyLine: (LogEntry) -> Unit,
    val onShare: () -> Unit,
    val onDismissMessage: () -> Unit,
    val onRetry: () -> Unit,
)
