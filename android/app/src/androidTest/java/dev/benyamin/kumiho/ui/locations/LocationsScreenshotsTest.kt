package dev.benyamin.kumiho.ui.locations

import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onRoot
import androidx.test.platform.app.InstrumentationRegistry
import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.Protocol
import dev.benyamin.kumiho.engine.Server
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * Renders the Locations screen in the three house themes with a realistic tree
 * and writes one PNG per (screen, theme) pair to the app's external files
 * directory, so the design self-check looks at what the emulator actually drew.
 *
 * This is an extra, like the dashboard capture: it asserts nothing about
 * behaviour and is not part of the connected suite's product coverage. Pull the
 * files with:
 * `adb pull /sdcard/Android/data/dev.benyamin.kumiho.debug/files/locations-screenshots`
 */
class LocationsScreenshotsTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private var theme by mutableStateOf(ThemeId.LIGHT)
    private var state by mutableStateOf(treeState())

    @Test
    fun captureTheLocationsScreensInEveryTheme() {
        rule.setContent {
            KumihoTheme(
                colors = PaletteMapping.fromJson(
                    dev.benyamin.kumiho.go.mobile.Mobile.palette(theme.id, KumihoAccent.BLUE.id),
                    theme,
                    KumihoAccent.BLUE,
                ),
            ) {
                // The app shell owns the Scaffold background; the harness adds
                // one so a dark theme is not drawn on the window's white.
                Surface(
                    color = MaterialTheme.colorScheme.background,
                    modifier = Modifier.fillMaxSize(),
                ) {
                    LocationsScreen(state = state, actions = noOpActions())
                }
            }
        }
        val output = InstrumentationRegistry.getInstrumentation()
            .targetContext
            .getExternalFilesDir("locations-screenshots")
            ?: error("the app has no external files directory")
        output.mkdirs()

        capture(output, "tree")

        rule.runOnIdle { state = noticeState() }
        capture(output, "notice")

        rule.runOnIdle { state = searchState() }
        capture(output, "filter")
    }

    private fun capture(output: File, screen: String) {
        for (entry in ThemeId.entries) {
            rule.runOnIdle { theme = entry }
            rule.waitForIdle()
            val file = File(output, "t12-$screen-${entry.id}.png")
            file.outputStream().use { stream ->
                rule.onRoot().captureToImage().asAndroidBitmap()
                    .compress(Bitmap.CompressFormat.PNG, 100, stream)
            }
            assertTrue("a screenshot must not be empty: ${file.absolutePath}", file.length() > 0)
        }
    }

    private fun treeState() = LocationsState(
        loading = false,
        loaded = true,
        countries = COUNTRIES,
        selection = LocationTree.currentSelection(COUNTRIES, "us", EDGE_ADDRESS),
        latency = mapOf(
            LocationTree.addressKey(EDGE_ADDRESS) to 42.4,
            LocationTree.addressKey(WEST_ADDRESS) to null,
            LocationTree.addressKey(LONG_ADDRESS) to 128.0,
            LocationTree.addressKey(BER_ADDRESS) to 17.0,
        ),
    )

    private fun searchState() = treeState().copy(query = "ber")

    private fun noticeState() = treeState().copy(
        notice = LocationsNotice.refreshFailed("the engine did not answer"),
    )

    private fun noOpActions() = LocationsActions(
        onQueryChange = {},
        onClearQuery = {},
        onSelect = { _, _, _ -> },
        onRefresh = {},
        onRetryLoad = {},
        onPingAll = {},
        onDismissNotice = {},
        onRetryNotice = {},
    )

    private companion object {
        fun server(
            host: String,
            port: Int = 443,
            quarantined: Boolean = false,
            protocols: List<String> = listOf("connect"),
        ) = Server(
            hostname = host,
            port = port,
            quarantined = quarantined,
            protocols = protocols.map { Protocol(name = it, host = host, port = port, scheme = "https") },
        )

        val EDGE_SERVER = server("edge.example.net", 2499, protocols = listOf("connect", "quic"))
        val SHARE_SERVER = server("share.example.net")
        val LONG_SERVER = server("a-very-long-edge-hostname-that-needs-truncation.example.net")
        val WEST_SERVER = server("west.example.net")
        val QUARANTINED_SERVER = server("quarantined.example.net", quarantined = true)
        val AUTO_SERVER = server("auto.example.net")
        val BER_SERVER = server("ber.example.net")

        const val EDGE_ADDRESS = "edge.example.net:2499"
        const val LONG_ADDRESS = "a-very-long-edge-hostname-that-needs-truncation.example.net:443"
        const val WEST_ADDRESS = "west.example.net:443"
        const val BER_ADDRESS = "ber.example.net:443"

        val REC = Country(
            code = "rec",
            name = "Recommended Location",
            cities = listOf(City(code = "rec", name = "Recommended", servers = listOf(AUTO_SERVER))),
        )

        val US = Country(
            code = "us",
            name = "United States",
            cities = listOf(
                City(
                    code = "nyc",
                    name = "New York",
                    servers = listOf(EDGE_SERVER, SHARE_SERVER, LONG_SERVER, QUARANTINED_SERVER),
                ),
                City(code = "sfo", name = "San Francisco", servers = listOf(WEST_SERVER)),
            ),
        )

        val DE = Country(
            code = "de",
            name = "Germany",
            cities = listOf(City(code = "ber", name = "Berlin", servers = listOf(BER_SERVER))),
        )

        val COUNTRIES = listOf(US, REC, DE)
    }
}
