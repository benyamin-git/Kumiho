package dev.benyamin.kumiho.ui.tv

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.tv.material3.MaterialTheme
import dev.benyamin.kumiho.ui.shell.Destination
import dev.benyamin.kumiho.ui.shell.MoreRoute
import dev.benyamin.kumiho.ui.shell.ShellNavigation
import dev.benyamin.kumiho.ui.tv.theme.TvDimens

/**
 * The only TV scaffold: overscan-safe frame, left rail, top bar and one content
 * slot per destination. Back from a tab exits to the launcher (no handler);
 * Back from More or a More route returns to the previous TV page.
 */
@Composable
fun TvAppShell(
    nav: ShellNavigation,
    home: @Composable () -> Unit,
    locations: @Composable () -> Unit,
    account: @Composable () -> Unit,
    settings: @Composable () -> Unit,
    logs: @Composable () -> Unit,
    diagnostics: @Composable () -> Unit,
    @Suppress("UNUSED_PARAMETER") tvConnectRequested: Boolean = false,
    modifier: Modifier = Modifier,
) {
    val state by nav.state.collectAsStateWithLifecycle()
    var moreOpen by remember { mutableStateOf(false) }
    val railFocus = remember {
        Destination.tabs.associateWith { FocusRequester() }
    }
    LaunchedEffect(Unit) { runCatching { railFocus.getValue(Destination.Home).requestFocus() } }

    BackHandler(enabled = moreOpen || state.moreRoute != null) {
        when {
            state.moreRoute != null -> {
                nav.back()
                moreOpen = true
                runCatching { railFocus.getValue(Destination.More).requestFocus() }
            }
            moreOpen -> {
                moreOpen = false
                runCatching { railFocus.getValue(state.tab).requestFocus() }
            }
        }
    }

    val colors = MaterialTheme.colorScheme
    Box(
        modifier = modifier
            .fillMaxSize()
            .background(colors.background)
            .padding(
                horizontal = TvDimens.OverscanHorizontal,
                vertical = TvDimens.OverscanVertical,
            ),
    ) {
        Row(modifier = Modifier.fillMaxSize()) {
            TvRail(
                selected = if (moreOpen || state.moreRoute != null) Destination.More else state.selectedTab,
                onSelect = { destination ->
                    if (destination == Destination.More) {
                        moreOpen = true
                    } else {
                        moreOpen = false
                        nav.selectTab(destination)
                    }
                },
                focusRequesters = railFocus,
            )
            Column(modifier = Modifier.fillMaxSize().padding(start = TvDimens.Space2)) {
                TvTopBar(title = if (moreOpen) Destination.More.label else state.title)
                Box(modifier = Modifier.fillMaxSize()) {
                    when {
                        moreOpen -> TvMorePage(
                            onLogs = {
                                moreOpen = false
                                nav.openMoreRoute(MoreRoute.Logs)
                            },
                            onDiagnostics = {
                                moreOpen = false
                                nav.openMoreRoute(MoreRoute.Diagnostics)
                            },
                        )
                        state.moreRoute != null -> when (state.moreRoute) {
                            MoreRoute.Logs -> logs()
                            MoreRoute.Diagnostics -> diagnostics()
                            null -> Unit
                        }
                        else -> when (state.tab) {
                            Destination.Home -> home()
                            Destination.Locations -> locations()
                            Destination.Account -> account()
                            Destination.Settings -> settings()
                            Destination.More -> Unit
                        }
                    }
                }
            }
        }
    }
}
