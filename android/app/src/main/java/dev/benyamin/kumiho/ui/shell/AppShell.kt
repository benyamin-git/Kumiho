package dev.benyamin.kumiho.ui.shell

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationRail
import androidx.compose.material3.NavigationRailItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.benyamin.kumiho.ui.components.destinationIcon
import dev.benyamin.kumiho.ui.components.moreRouteIcon
import dev.benyamin.kumiho.ui.screens.HomePlaceholder
import dev.benyamin.kumiho.ui.screens.PlaceholderScreen
import dev.benyamin.kumiho.ui.theme.Dimens

/**
 * The shell. It owns the only [Scaffold], the 64dp top bar and navigation: a
 * bottom bar under 600dp, a navigation rail at or above it. Insets are applied
 * here once (logical start/end only); screens never add their own.
 */
@Composable
fun AppShell(
    nav: ShellNavigation,
    modifier: Modifier = Modifier,
) {
    val state by nav.state.collectAsStateWithLifecycle()
    AppShell(
        state = state,
        onSelectTab = nav::selectTab,
        onOpenMoreRoute = nav::openMoreRoute,
        onBack = nav::back,
        modifier = modifier,
    )
}

/** The stateless shell, so the navigation holder can be faked in tests. */
@Composable
fun AppShell(
    state: ShellNavState,
    onSelectTab: (Destination) -> Unit,
    onOpenMoreRoute: (MoreRoute) -> Unit,
    onBack: () -> Boolean,
    modifier: Modifier = Modifier,
) {
    BoxWithConstraints(modifier = modifier.fillMaxSize()) {
        val mode = navMode(maxWidth.value)
        var moreSheetOpen by remember { mutableStateOf(false) }
        val back = rememberUpdatedState(onBack)
        val onNavClick: (Destination) -> Unit = { destination ->
            if (destination == Destination.More) moreSheetOpen = true else onSelectTab(destination)
        }

        BackHandler(enabled = moreSheetOpen || state.moreRoute != null) {
            if (moreSheetOpen) moreSheetOpen = false else back.value()
        }

        Scaffold(
            modifier = Modifier.fillMaxSize(),
            containerColor = MaterialTheme.colorScheme.background,
            contentWindowInsets = WindowInsets(0, 0, 0, 0),
            topBar = {
                KumihoTopBar(
                    title = state.title,
                    onBack = if (state.moreRoute != null) {
                        { back.value() }
                    } else {
                        null
                    },
                    modifier = Modifier.windowInsetsPadding(
                        WindowInsets.safeDrawing.only(WindowInsetsSides.Top + WindowInsetsSides.Horizontal),
                    ),
                )
            },
            bottomBar = {
                if (mode == NavMode.BottomBar) {
                    BottomBar(
                        selected = state.selectedTab,
                        onSelect = onNavClick,
                        modifier = Modifier.windowInsetsPadding(
                            WindowInsets.safeDrawing.only(WindowInsetsSides.Bottom + WindowInsetsSides.Horizontal),
                        ),
                    )
                }
            },
        ) { innerPadding ->
            Row(modifier = Modifier.fillMaxSize().padding(innerPadding)) {
                if (mode == NavMode.Rail) {
                    NavRail(
                        selected = state.selectedTab,
                        onSelect = onNavClick,
                        modifier = Modifier.windowInsetsPadding(
                            WindowInsets.safeDrawing.only(WindowInsetsSides.Start + WindowInsetsSides.Bottom),
                        ),
                    )
                }
                Box(
                    modifier = Modifier
                        .fillMaxHeight()
                        .weight(1f)
                        .windowInsetsPadding(
                            WindowInsets.safeDrawing.only(
                                if (mode == NavMode.Rail) {
                                    WindowInsetsSides.End + WindowInsetsSides.Bottom
                                } else {
                                    WindowInsetsSides.Horizontal
                                },
                            ),
                        ),
                    contentAlignment = Alignment.TopCenter,
                ) {
                    Box(
                        modifier = Modifier
                            .widthIn(max = Dimens.ContentMaxWidth)
                            .fillMaxSize(),
                    ) {
                        ScreenHost(state)
                    }
                }
            }
        }

        if (moreSheetOpen) {
            MoreSheet(
                onDismiss = { moreSheetOpen = false },
                onOpen = { route ->
                    moreSheetOpen = false
                    onOpenMoreRoute(route)
                },
            )
        }
    }
}

@Composable
private fun BottomBar(
    selected: Destination,
    onSelect: (Destination) -> Unit,
    modifier: Modifier = Modifier,
) {
    NavigationBar(modifier = modifier, containerColor = MaterialTheme.colorScheme.surfaceContainer) {
        Destination.tabs.forEach { destination ->
            NavigationBarItem(
                selected = destination == selected,
                onClick = { onSelect(destination) },
                icon = { Icon(painter = destinationIcon(destination), contentDescription = null) },
                label = { Text(destination.label) },
                alwaysShowLabel = true,
            )
        }
    }
}

@Composable
private fun NavRail(
    selected: Destination,
    onSelect: (Destination) -> Unit,
    modifier: Modifier = Modifier,
) {
    NavigationRail(modifier = modifier, containerColor = MaterialTheme.colorScheme.surfaceContainer) {
        Destination.tabs.forEach { destination ->
            NavigationRailItem(
                selected = destination == selected,
                onClick = { onSelect(destination) },
                icon = { Icon(painter = destinationIcon(destination), contentDescription = null) },
                label = { Text(destination.label) },
                alwaysShowLabel = true,
            )
        }
    }
}

@Composable
private fun ScreenHost(state: ShellNavState) {
    val more = state.moreRoute
    if (more != null) {
        PlaceholderScreen(
            title = more.label,
            message = "Not connected. This screen arrives with the engine client.",
            icon = moreRouteIcon(more),
        )
        return
    }
    when (state.tab) {
        Destination.Home -> HomePlaceholder()
        Destination.Locations -> PlaceholderScreen(
            title = Destination.Locations.label,
            message = "Not connected. Location data arrives with the engine client.",
            icon = destinationIcon(Destination.Locations),
        )
        Destination.Account -> PlaceholderScreen(
            title = Destination.Account.label,
            message = "Not connected. Account details arrive with the engine client.",
            icon = destinationIcon(Destination.Account),
        )
        Destination.Settings -> PlaceholderScreen(
            title = Destination.Settings.label,
            message = "Engine settings arrive with the engine client.",
            icon = destinationIcon(Destination.Settings),
        )
        Destination.More -> Unit
    }
}
