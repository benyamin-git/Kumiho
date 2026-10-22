package dev.benyamin.kumiho.ui.shell

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * The five shell destinations. Everything but More maps to a route; More opens
 * a sheet and has no route of its own, so its route is the empty sentinel.
 */
enum class Destination(val route: String, val label: String) {
    Home("home", "Home"),
    Locations("locations", "Locations"),
    Account("account", "Account"),
    Settings("settings", "Settings"),

    /** A sheet trigger, never a tab and never a route: the sentinel is empty. */
    More("", "More"),
    ;

    companion object {
        /** The five items, in shell order. */
        val tabs: List<Destination> = entries.toList()
    }
}

/** The two routes behind the More sheet. They are routes, not nav items. */
enum class MoreRoute(val route: String, val label: String) {
    Logs("logs", "Logs"),
    Diagnostics("diagnostics", "Diagnostics"),
}

/** How the shell lays out navigation for the available width. */
enum class NavMode { BottomBar, Rail }

/** The 600dp breakpoint house rule: bottom bar under it, rail at or above. */
fun navMode(widthDp: Float): NavMode =
    if (widthDp >= 600f) NavMode.Rail else NavMode.BottomBar

/**
 * One navigation state: the selected tab plus an optional More route stacked on
 * top. While a More route is showing, the More item stays marked.
 */
data class ShellNavState(
    val tab: Destination = Destination.Home,
    val moreRoute: MoreRoute? = null,
) {
    /** The nav item that reads as selected. */
    val selectedTab: Destination get() = if (moreRoute != null) Destination.More else tab

    /** The active route: `home` … `diagnostics`. */
    val route: String get() = moreRoute?.route ?: tab.route

    /** The title the top bar shows. */
    val title: String get() = moreRoute?.label ?: tab.label
}

/**
 * Plain-Kotlin shell navigation: a [StateFlow] the shell collects, a small back
 * stack, and no Compose imports, so it is testable on the JVM and fakeable.
 */
class ShellNavigation(initialTab: Destination = Destination.Home) {
    private val stateFlow = MutableStateFlow(ShellNavState(tab = initialTab))
    private val backStack = ArrayDeque<ShellNavState>()

    /** The navigation state. */
    val state: StateFlow<ShellNavState> = stateFlow.asStateFlow()

    /** The current state, for callers that do not need to collect. */
    val current: ShellNavState get() = stateFlow.value

    /**
     * Selects a tab. Selecting More is a no-op here: it opens the sheet. A tab
     * selection clears any More route that was on top.
     */
    fun selectTab(tab: Destination) {
        if (tab == Destination.More) return
        val now = stateFlow.value
        if (now.tab == tab && now.moreRoute == null) return
        backStack.addLast(now)
        stateFlow.value = ShellNavState(tab = tab)
    }

    /** Opens a More route, remembering where to return. */
    fun openMoreRoute(route: MoreRoute) {
        val now = stateFlow.value
        if (now.moreRoute == route) return
        backStack.addLast(now)
        stateFlow.value = now.copy(moreRoute = route)
    }

    /** Returns to the previous state. False when there is nowhere to go. */
    fun back(): Boolean {
        val previous = backStack.removeLastOrNull() ?: return false
        stateFlow.value = previous
        return true
    }
}
