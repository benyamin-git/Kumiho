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
import androidx.compose.ui.Modifier
import dev.benyamin.kumiho.ui.components.KumihoRow
import dev.benyamin.kumiho.ui.components.KumihoSectionHeader
import dev.benyamin.kumiho.ui.components.moreRouteIcon
import dev.benyamin.kumiho.ui.theme.Dimens

/**
 * The More sheet. Logs and Diagnostics live here as routes rather than as
 * bottom-bar destinations; picking one closes the sheet and opens the route.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MoreSheet(
    onDismiss: () -> Unit,
    onOpen: (MoreRoute) -> Unit,
    modifier: Modifier = Modifier,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        modifier = modifier,
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
                .padding(bottom = Dimens.Space3),
        ) {
            KumihoSectionHeader(title = "More")
            MoreRoute.entries.forEach { route ->
                KumihoRow(
                    title = route.label,
                    icon = moreRouteIcon(route),
                    onClick = { onOpen(route) },
                )
            }
        }
    }
}
