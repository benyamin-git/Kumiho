package dev.benyamin.kumiho.data

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * The picker's pure filter. It hides non-launchable apps by default, reveals
 * them with the System-apps toggle, searches label and package without regard
 * to case, never offers Kumiho as a selectable row, and sorts deterministically.
 */
class InstalledAppsFilterTest {

    private val self = InstalledApp(
        packageName = "dev.benyamin.kumiho.debug",
        label = "Kumiho",
        isSystem = false,
        launchable = true,
        isSelf = true,
    )

    private val chrome = InstalledApp(
        packageName = "com.android.chrome",
        label = "Chrome",
        isSystem = false,
        launchable = true,
    )

    private val maps = InstalledApp(
        packageName = "com.example.maps",
        label = "Maps",
        isSystem = false,
        launchable = true,
    )

    private val settings = InstalledApp(
        packageName = "com.android.settings",
        label = "Settings",
        isSystem = true,
        launchable = false,
    )

    private val hiddenService = InstalledApp(
        packageName = "com.example.hidden",
        label = "Hidden service",
        isSystem = true,
        launchable = false,
    )

    private val apps = listOf(self, chrome, maps, settings, hiddenService)

    private fun filter(query: String = "", showSystem: Boolean = false) =
        InstalledAppsFilter.filter(apps = apps, query = query, showSystem = showSystem)

    // ---- visibility ------------------------------------------------------

    @Test
    fun `the default list is launchable apps without Kumiho`() {
        assertEquals(
            listOf("Chrome", "Maps"),
            filter().map { it.label },
        )
    }

    @Test
    fun `Kumiho is never a selectable row, even when it is not launchable`() {
        val hiddenSelf = self.copy(launchable = false)
        for (showSystem in listOf(false, true)) {
            val result = InstalledAppsFilter.filter(
                apps = listOf(hiddenSelf, chrome),
                query = "",
                showSystem = showSystem,
            )
            assertEquals(listOf("Chrome"), result.map { it.label })
        }
    }

    @Test
    fun `the System-apps toggle reveals system and non-launchable apps`() {
        val result = filter(showSystem = true)
        assertEquals(
            listOf("Chrome", "Hidden service", "Maps", "Settings"),
            result.map { it.label },
        )
    }

    // ---- search ----------------------------------------------------------

    @Test
    fun `search matches labels case-insensitively`() {
        assertEquals(listOf("Maps"), filter(query = "ma").map { it.label })
        assertEquals(listOf("Maps"), filter(query = "MAPS").map { it.label })
    }

    @Test
    fun `search matches package names`() {
        assertEquals(
            listOf("Chrome"),
            filter(query = "android.chrome").map { it.label },
        )
        assertEquals(
            emptyList<InstalledApp>(),
            filter(query = "kumiho.debug"),
        )
    }

    @Test
    fun `search trims surrounding spaces and ignores case`() {
        assertEquals(listOf("Maps"), filter(query = "  maps  ").map { it.label })
    }

    @Test
    fun `search does not override the launchable default`() {
        assertEquals(emptyList<InstalledApp>(), filter(query = "Settings"))
        assertEquals(
            listOf("Settings"),
            filter(query = "Settings", showSystem = true).map { it.label },
        )
    }

    @Test
    fun `a query with no match is empty, never a fabricated row`() {
        assertEquals(emptyList<InstalledApp>(), filter(query = "does-not-exist"))
    }

    // ---- ordering --------------------------------------------------------

    @Test
    fun `results sort by lowercased label and then package`() {
        val upper = InstalledApp("com.example.z", "alpha", isSystem = false, launchable = true)
        val lower = InstalledApp("com.example.a", "Alpha", isSystem = false, launchable = true)
        val sameLabel = InstalledApp("com.example.b", "Alpha", isSystem = false, launchable = true)
        val result = InstalledAppsFilter.filter(
            apps = listOf(upper, lower, sameLabel),
            query = "",
            showSystem = false,
        )
        assertEquals(
            listOf("com.example.a", "com.example.b", "com.example.z"),
            result.map { it.packageName },
        )
    }
}
