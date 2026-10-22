package dev.benyamin.kumiho.ui.dashboard

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
import dev.benyamin.kumiho.engine.ByteCounters
import dev.benyamin.kumiho.engine.ByteRates
import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.engine.Quota
import dev.benyamin.kumiho.engine.Server
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.vpn.ServiceState
import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * Renders the dashboard in the three house themes with sample data and writes
 * one PNG per (screen, theme) pair to the app's external files directory, so
 * the design self-check looks at what the emulator actually drew.
 *
 * Pull the files with:
 * `adb pull /sdcard/Android/data/dev.benyamin.kumiho.debug/files/dashboard-screenshots`
 */
class DashboardScreenshotsTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private var theme by mutableStateOf(ThemeId.LIGHT)
    private var state by mutableStateOf(connectedState())

    @Test
    fun captureTheDashboardInEveryTheme() {
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
                    DashboardScreen(state = state, actions = noOpActions())
                }
            }
        }
        val output = InstrumentationRegistry.getInstrumentation()
            .targetContext
            .getExternalFilesDir("dashboard-screenshots")
            ?: error("the app has no external files directory")
        output.mkdirs()

        capture(output, "connected")

        rule.runOnIdle {
            state = DashboardState.fromService(
                ServiceState(
                    binding = true,
                    engineReady = true,
                    status = status(state = STATE_IDLE),
                ),
            ).copy(
                locations = LocationLoad.Ready(COUNTRIES),
                notice = DashboardNotice.statusRefreshFailed("the engine did not answer"),
            )
        }
        capture(output, "off-notice")

        rule.runOnIdle {
            state = DashboardState.fromService(
                ServiceState(
                    binding = true,
                    engineReady = true,
                    status = status(state = STATE_IDLE),
                    permissionNeeded = true,
                ),
            ).copy(locations = LocationLoad.Ready(COUNTRIES))
        }
        capture(output, "permission")
    }

    private fun capture(output: File, screen: String) {
        for (entry in ThemeId.entries) {
            rule.runOnIdle { theme = entry }
            rule.waitForIdle()
            val file = File(output, "t11-$screen-${entry.id}.png")
            file.outputStream().use { stream ->
                rule.onRoot().captureToImage().asAndroidBitmap()
                    .compress(Bitmap.CompressFormat.PNG, 100, stream)
            }
            assertTrue("a screenshot must not be empty: ${file.absolutePath}", file.length() > 0)
        }
    }

    private fun noOpActions() = DashboardActions(
        onPower = {},
        onOpenOptions = {},
        onCloseOptions = {},
        onSelectRecommended = {},
        onSelectSaved = {},
        onToggleCountry = {},
        onSelectCity = { _, _ -> },
        onToggleProxyOnly = {},
        onConnectFromOptions = {},
        onReloadLocations = {},
        onOpenVpnSettings = {},
        onConfirmLockdownDisconnect = {},
        onCancelLockdown = {},
        onDismissNotice = {},
        onRetryNotice = {},
    )

    private fun connectedState(): DashboardState = DashboardState.fromService(
        ServiceState(
            binding = true,
            engineReady = true,
            status = status(
                state = STATE_CONNECTED,
                location = "us",
                server = "edge.example.net:443",
                exitIp = "203.0.113.7",
            ),
        ),
    ).copy(locations = LocationLoad.Ready(COUNTRIES))

    private fun status(
        state: String,
        location: String? = null,
        server: String? = null,
        exitIp: String? = null,
    ) = EngineStatus(
        state = state,
        authenticated = true,
        email = "user@example.com",
        since = System.currentTimeMillis() / 1000 - 4_123,
        tokenExpires = 1_806_000_000L,
        passExpires = 1_812_000_000L,
        killSwitch = true,
        autoconnect = true,
        location = location,
        server = server,
        proxyOnly = false,
        quota = Quota(limit = 50L * GIB, remaining = 13_207_024_436L, reset = 1_792_000_000L),
        dns = "10.8.0.2",
        ipv6 = "blackholed",
        exitIp = exitIp,
        exitCountry = if (exitIp == null) null else "Netherlands",
        totals = ByteCounters(up = 12_897_485L, down = 47_817_523L),
        rates = ByteRates(up = 1.2 * MIB, down = 3.4 * MIB),
    )

    private companion object {
        const val STATE_IDLE = "IDLE"
        const val STATE_CONNECTED = "CONNECTED"
        const val GIB = 1024L * 1024L * 1024L
        const val MIB = 1024.0 * 1024.0

        val SERVER = Server(
            hostname = "edge.example.net",
            port = 443,
            quarantined = false,
            protocols = emptyList(),
        )

        val COUNTRIES = listOf(
            Country(
                code = "us",
                name = "United States",
                cities = listOf(
                    City(
                        code = "nyc",
                        name = "New York",
                        servers = listOf(SERVER),
                    ),
                ),
            ),
            Country(
                code = "de",
                name = "Germany",
                cities = listOf(City(code = "ber", name = "Berlin", servers = emptyList())),
            ),
        )
    }
}
