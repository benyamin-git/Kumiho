package dev.benyamin.kumiho.ui.shell

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.BottomSheetDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.components.KumihoRow
import dev.benyamin.kumiho.ui.components.KumihoSectionHeader
import dev.benyamin.kumiho.ui.components.destinationIcon
import dev.benyamin.kumiho.ui.components.moreRouteIcon
import dev.benyamin.kumiho.ui.theme.Dimens

/**
 * The More sheet. Pages outside the bottom bar sit here first, so Settings is
 * always reachable on a phone; Logs and Diagnostics follow as routes. Picking
 * anything closes the sheet.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MoreSheet(
    onDismiss: () -> Unit,
    onOpen: (MoreRoute) -> Unit,
    modifier: Modifier = Modifier,
    overflow: List<Destination> = emptyList(),
    onSelectTab: (Destination) -> Unit = {},
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    val localizedContext = LocalContext.current
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        modifier = modifier,
        sheetState = sheetState,
        shape = MaterialTheme.shapes.large,
        containerColor = MaterialTheme.colorScheme.surfaceContainerLow,
        contentColor = MaterialTheme.colorScheme.onSurface,
        dragHandle = { BottomSheetDefaults.DragHandle() },
    ) {
        // The sheet composes in its own window; re-provide the localized
        // context captured above so stringResource stays Persian there.
        CompositionLocalProvider(LocalContext provides localizedContext) {
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .navigationBarsPadding()
                    .padding(bottom = Dimens.Space3),
            ) {
                KumihoSectionHeader(title = stringResource(R.string.more_title))
                overflow.forEach { destination ->
                    KumihoRow(
                        title = stringResource(destination.labelRes),
                        icon = destinationIcon(destination),
                        onClick = { onSelectTab(destination) },
                    )
                }
                MoreRoute.entries.forEach { route ->
                    KumihoRow(
                        title = stringResource(route.labelRes),
                        icon = moreRouteIcon(route),
                        onClick = { onOpen(route) },
                    )
                }
            }
        }
    }
}
