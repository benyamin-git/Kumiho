package dev.benyamin.kumiho.ui.shell

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.exclude
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.ime
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
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.unit.Dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.benyamin.kumiho.ui.components.destinationIcon
import dev.benyamin.kumiho.ui.components.moreRouteIcon
import dev.benyamin.kumiho.ui.screens.HomePlaceholder
import dev.benyamin.kumiho.ui.screens.PlaceholderScreen
import dev.benyamin.kumiho.ui.theme.Dimens

/**
 * The shell. It owns the only [Scaffold], the 64dp top bar and navigation: a
 * bottom bar under 600dp, a navigation rail at or above it. A bottom bar too
 * narrow for all five labels hands its trailing pages to the More sheet. It
 * applies the system-bar insets once; the keyboard inset is left to the
 * screens' own `imePadding()`, so the bars stay under the keyboard instead of
 * counting it twice.
 */
@Composable
fun AppShell(
    nav: ShellNavigation,
    modifier: Modifier = Modifier,
    home: @Composable () -> Unit = { HomePlaceholder() },
    locations: @Composable () -> Unit = { LocationsPlaceholder() },
    account: @Composable () -> Unit = { AccountPlaceholder() },
    settings: @Composable () -> Unit = { SettingsPlaceholder() },
    logs: @Composable () -> Unit = { MoreRoutePlaceholder(MoreRoute.Logs) },
    diagnostics: @Composable () -> Unit = { MoreRoutePlaceholder(MoreRoute.Diagnostics) },
) {
    val state by nav.state.collectAsStateWithLifecycle()
    AppShell(
        state = state,
        onSelectTab = nav::selectTab,
        onOpenMoreRoute = nav::openMoreRoute,
        onBack = nav::back,
        modifier = modifier,
        home = home,
        locations = locations,
        account = account,
        settings = settings,
        logs = logs,
        diagnostics = diagnostics,
    )
}

/**
 * The stateless shell, so the navigation holder can be faked in tests. Every
 * destination is a slot: [home], [locations], [account], [settings] and the two
 * More routes, [logs] and [diagnostics]. All default to placeholders so the
 * shell stays renderable on its own; the app passes the real screens.
 */
@Composable
fun AppShell(
    state: ShellNavState,
    onSelectTab: (Destination) -> Unit,
    onOpenMoreRoute: (MoreRoute) -> Unit,
    onBack: () -> Boolean,
    modifier: Modifier = Modifier,
    home: @Composable () -> Unit = { HomePlaceholder() },
    locations: @Composable () -> Unit = { LocationsPlaceholder() },
    account: @Composable () -> Unit = { AccountPlaceholder() },
    settings: @Composable () -> Unit = { SettingsPlaceholder() },
    logs: @Composable () -> Unit = { MoreRoutePlaceholder(MoreRoute.Logs) },
    diagnostics: @Composable () -> Unit = { MoreRoutePlaceholder(MoreRoute.Diagnostics) },
) {
    BoxWithConstraints(modifier = modifier.fillMaxSize()) {
        val mode = navMode(maxWidth.value)
        val barInsets = WindowInsets.safeDrawing.exclude(WindowInsets.ime)
        // A narrow bottom bar gives up trailing pages to the More sheet rather
        // than wrapping their labels; the rail always shows all five.
        val labelWidth = widestLabelWidth().value
        val barTabs = if (mode == NavMode.Rail) {
            Destination.tabs
        } else {
            bottomBarTabs(maxWidth.value, labelWidth)
        }
        val overflow = Destination.tabs - barTabs.toSet()
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
                        tabs = barTabs,
                        onSelect = onNavClick,
                        modifier = Modifier.windowInsetsPadding(
                            barInsets.only(WindowInsetsSides.Bottom + WindowInsetsSides.Horizontal),
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
                            barInsets.only(WindowInsetsSides.Start + WindowInsetsSides.Bottom),
                        ),
                    )
                }
                Box(
                    modifier = Modifier
                        .fillMaxHeight()
                        .weight(1f)
                        .windowInsetsPadding(
                            barInsets.only(
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
                        ScreenHost(state, home, locations, account, settings, logs, diagnostics)
                    }
                }
            }
        }

        if (moreSheetOpen) {
            MoreSheet(
                overflow = overflow,
                onSelectTab = { destination ->
                    moreSheetOpen = false
                    onSelectTab(destination)
                },
                onDismiss = { moreSheetOpen = false },
                onOpen = { route ->
                    moreSheetOpen = false
                    onOpenMoreRoute(route)
                },
            )
        }
    }
}

/**
 * The widest bottom-bar label at the bar's own type scale, in dp. Measuring it
 * is what lets the bar react to the system font scale, not just the width.
 */
@Composable
private fun widestLabelWidth(): Dp {
    val measurer = rememberTextMeasurer()
    val style = MaterialTheme.typography.labelMedium
    val widest = Destination.tabs.maxOf { measurer.measure(it.label, style).size.width }
    return with(LocalDensity.current) { widest.toDp() }
}

@Composable
private fun BottomBar(
    selected: Destination,
    tabs: List<Destination>,
    onSelect: (Destination) -> Unit,
    modifier: Modifier = Modifier,
) {
    // A page that has moved into the More sheet keeps More marked.
    val marked = if (selected in tabs) selected else Destination.More
    NavigationBar(modifier = modifier, containerColor = MaterialTheme.colorScheme.surfaceContainer) {
        tabs.forEach { destination ->
            NavigationBarItem(
                selected = destination == marked,
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
private fun ScreenHost(
    state: ShellNavState,
    home: @Composable () -> Unit,
    locations: @Composable () -> Unit,
    account: @Composable () -> Unit,
    settings: @Composable () -> Unit,
    logs: @Composable () -> Unit,
    diagnostics: @Composable () -> Unit,
) {
    val more = state.moreRoute
    if (more != null) {
        when (more) {
            MoreRoute.Logs -> logs()
            MoreRoute.Diagnostics -> diagnostics()
        }
        return
    }
    when (state.tab) {
        Destination.Home -> home()
        Destination.Locations -> locations()
        Destination.Account -> account()
        Destination.Settings -> settings()
        Destination.More -> Unit
    }
}

/** The pre-T15 Account placeholder; the app passes the real screen. */
@Composable
private fun AccountPlaceholder() {
    PlaceholderScreen(
        title = Destination.Account.label,
        message = "Not connected. Account details arrive with the engine client.",
        icon = destinationIcon(Destination.Account),
    )
}

/** The pre-T15 More-route placeholder; the app passes the real screens. */
@Composable
private fun MoreRoutePlaceholder(route: MoreRoute) {
    PlaceholderScreen(
        title = route.label,
        message = "Not connected. This screen arrives with the engine client.",
        icon = moreRouteIcon(route),
    )
}

/** The pre-T13 Locations placeholder; the app passes the real screen. */
@Composable
private fun LocationsPlaceholder() {
    PlaceholderScreen(
        title = Destination.Locations.label,
        message = "Not connected. Location data arrives with the engine client.",
        icon = destinationIcon(Destination.Locations),
    )
}

/** The pre-T13 Settings placeholder; the app passes the real screen. */
@Composable
private fun SettingsPlaceholder() {
    PlaceholderScreen(
        title = Destination.Settings.label,
        message = "Engine settings arrive with the engine client.",
        icon = destinationIcon(Destination.Settings),
    )
}
