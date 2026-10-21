package dev.benyamin.kumiho.ui.shell

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The shell's navigation rules, exercised on the plain-Kotlin holder that
 * [AppShell] actually drives. No Compose is involved, so this runs on the JVM:
 * the breakpoint, the five items, the More routes and back all come from the
 * same code the shell renders.
 */
class ShellNavigationTest {

    @Test
    fun `bottom bar under 600dp, rail at and above`() {
        assertEquals(NavMode.BottomBar, navMode(0f))
        assertEquals(NavMode.BottomBar, navMode(320f))
        assertEquals(NavMode.BottomBar, navMode(599f))
        assertEquals(NavMode.Rail, navMode(600f))
        assertEquals(NavMode.Rail, navMode(840f))
    }

    @Test
    fun `exactly five destinations in shell order`() {
        assertEquals(
            listOf(
                Destination.Home,
                Destination.Locations,
                Destination.Account,
                Destination.Settings,
                Destination.More,
            ),
            Destination.tabs,
        )
    }

    @Test
    fun `routes are the shell routes plus the two More routes`() {
        assertEquals(
            listOf("home", "locations", "account", "settings", "more"),
            Destination.tabs.map { it.route },
        )
        // The More routes are routes, not navigation items.
        assertEquals(listOf("logs", "diagnostics"), MoreRoute.entries.map { it.route })
        assertTrue(Destination.tabs.none { it.route == "logs" || it.route == "diagnostics" })
    }

    @Test
    fun `starts on home with no More route`() {
        val nav = ShellNavigation()
        assertEquals(Destination.Home, nav.current.tab)
        assertNull(nav.current.moreRoute)
        assertEquals("home", nav.current.route)
        assertEquals(Destination.Home, nav.current.selectedTab)
        assertEquals("Home", nav.current.title)
    }

    @Test
    fun `every tab is reachable and becomes the active route`() {
        val nav = ShellNavigation()
        listOf(
            Destination.Locations,
            Destination.Account,
            Destination.Settings,
            Destination.Home,
        ).forEach { destination ->
            nav.selectTab(destination)
            assertEquals(destination, nav.current.tab)
            assertEquals(destination, nav.current.selectedTab)
            assertEquals(destination.route, nav.current.route)
            assertNull(nav.current.moreRoute)
        }
    }

    @Test
    fun `selecting More never becomes a tab`() {
        val nav = ShellNavigation()
        nav.selectTab(Destination.Settings)
        nav.selectTab(Destination.More)
        // More is a sheet trigger; the underlying tab is untouched.
        assertEquals(Destination.Settings, nav.current.tab)
        assertEquals("settings", nav.current.route)
    }

    @Test
    fun `More routes open as routes and keep More marked`() {
        val nav = ShellNavigation()
        nav.selectTab(Destination.Locations)

        nav.openMoreRoute(MoreRoute.Logs)
        assertEquals(MoreRoute.Logs, nav.current.moreRoute)
        assertEquals("logs", nav.current.route)
        assertEquals("Logs", nav.current.title)
        // More is the item that reads as selected while a More route is up.
        assertEquals(Destination.More, nav.current.selectedTab)
        // The underlying tab is remembered, not lost.
        assertEquals(Destination.Locations, nav.current.tab)

        nav.openMoreRoute(MoreRoute.Diagnostics)
        assertEquals(MoreRoute.Diagnostics, nav.current.moreRoute)
        assertEquals("diagnostics", nav.current.route)
        assertEquals(Destination.More, nav.current.selectedTab)
    }

    @Test
    fun `back returns from a More route to the tab beneath it`() {
        val nav = ShellNavigation()
        nav.selectTab(Destination.Account)
        nav.openMoreRoute(MoreRoute.Diagnostics)

        assertTrue(nav.back())
        assertNull(nav.current.moreRoute)
        assertEquals(Destination.Account, nav.current.tab)
        assertEquals("account", nav.current.route)
    }

    @Test
    fun `back unwinds the tab stack to home and then stops`() {
        val nav = ShellNavigation()
        nav.selectTab(Destination.Locations)
        nav.selectTab(Destination.Settings)

        assertTrue(nav.back())
        assertEquals(Destination.Locations, nav.current.tab)
        assertTrue(nav.back())
        assertEquals(Destination.Home, nav.current.tab)
        assertFalse("home is the floor", nav.back())
        assertEquals(Destination.Home, nav.current.tab)
    }

    @Test
    fun `reselecting the same tab does not grow the back stack`() {
        val nav = ShellNavigation()
        nav.selectTab(Destination.Locations)
        nav.selectTab(Destination.Locations)
        assertTrue(nav.back())
        assertEquals(Destination.Home, nav.current.tab)
        assertFalse(nav.back())
    }

    @Test
    fun `selecting a tab clears a More route that was on top`() {
        val nav = ShellNavigation()
        nav.openMoreRoute(MoreRoute.Logs)
        nav.selectTab(Destination.Home)
        assertNull(nav.current.moreRoute)
        assertEquals("home", nav.current.route)
    }
}
