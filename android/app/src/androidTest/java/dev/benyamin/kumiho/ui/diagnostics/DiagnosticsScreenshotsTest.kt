package dev.benyamin.kumiho.ui.diagnostics

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
import dev.benyamin.kumiho.diagnostics.AndroidFacts
import dev.benyamin.kumiho.engine.DoctorCheck
import dev.benyamin.kumiho.engine.DoctorReport
import dev.benyamin.kumiho.engine.DoctorStatus
import dev.benyamin.kumiho.ui.components.RateSample
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.vpn.SplitMode
import java.io.File
import kotlin.math.cos
import kotlin.math.sin
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * Renders the Diagnostics charts (throughput + latency) in every theme and
 * writes one PNG per theme to the app's external files directory. Review-only:
 * these captures are not committed.
 * `adb pull /sdcard/Android/data/dev.benyamin.kumiho.debug/files/diagnostics-screenshots`
 */
class DiagnosticsScreenshotsTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private var theme by mutableStateOf(ThemeId.DARK)
    private var state by mutableStateOf(connectedState())

    private val output: File
        get() = InstrumentationRegistry.getInstrumentation()
            .targetContext
            .getExternalFilesDir("diagnostics-screenshots")
            ?: error("the app has no external files directory")

    @Test
    fun captureTheChartsInEveryTheme() {
        rule.setContent {
            KumihoTheme(
                colors = PaletteMapping.fromJson(
                    dev.benyamin.kumiho.go.mobile.Mobile.palette(theme.id, KumihoAccent.BLUE.id),
                    theme,
                    KumihoAccent.BLUE,
                ),
            ) {
                Surface(
                    color = MaterialTheme.colorScheme.background,
                    modifier = Modifier.fillMaxSize(),
                ) {
                    DiagnosticsScreen(
                        state = state,
                        serviceBound = true,
                        splitMode = SplitMode.OFF,
                        splitAppCount = 0,
                        actions = noOpActions(),
                    )
                }
            }
        }
        output.mkdirs()

        for (id in listOf(ThemeId.LIGHT, ThemeId.DARK, ThemeId.OLED)) {
            theme = id
            state = connectedState()
            rule.waitForIdle()
            capture("diagnostics-connected-${id.id}.png")
        }

        state = connectedState().copy(
            trace = emptyList(),
            latency = emptyList(),
            chartNote = DiagnosticsChartNote.NO_SESSION,
        )
        theme = ThemeId.DARK
        rule.waitForIdle()
        capture("diagnostics-no-session-dark.png")
    }

    private fun capture(name: String) {
        val bitmap: Bitmap = rule.onRoot().captureToImage().asAndroidBitmap()
        val file = File(output, name)
        file.outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        assertTrue("a screenshot must not be empty: ${file.absolutePath}", file.length() > 0)
    }

    private fun noOpActions() = DiagnosticsActions(
        onRerun = {},
        onCopyReport = {},
        onDismissMessage = {},
        onOpenVpnSettings = {},
        onOpenNotificationSettings = {},
        onOpenBatterySettings = {},
        onRequestReset = {},
        onDismissReset = {},
        onConfirmReset = {},
    )

    private fun connectedState(): DiagnosticsUiState {
        return DiagnosticsUiState(
            loading = false,
            report = DoctorReport(
                version = "0.11.0",
                host = "android",
                os = "android",
                time = "now",
                checks = listOf(
                    DoctorCheck("vpn-permission", DoctorStatus.OK, "granted", null),
                    DoctorCheck("engine-state", DoctorStatus.OK, "connected", null),
                    DoctorCheck("dns", DoctorStatus.WARN, "system resolver", "open settings"),
                ),
                failed = 0,
                warned = 1,
                skipped = 0,
            ),
            facts = AndroidFacts(
                device = "Google Pixel 8",
                sdk = 36,
                vpnPermission = true,
                dnsServers = listOf("10.8.0.2"),
                engineState = "connected",
            ),
            trace = traceSamples(),
            latency = listOf(
                LatencyBar("edge.fast.example.net", 24.0),
                LatencyBar("edge.ams.example.net", 41.0),
                LatencyBar("edge.fra.example.net", 58.0),
                LatencyBar("edge.lon.example.net", 73.0),
                LatencyBar("edge.nyc.example.net", 112.0),
            ),
            chartNote = null,
        )
    }

    private fun traceSamples(): List<RateSample> {
        val now = System.currentTimeMillis()
        val mib = 1024.0 * 1024.0
        return (0 until 24).map { i ->
            val phase = i / 24.0 * 2.0 * Math.PI
            RateSample(
                atMillis = now - (23 - i) * 2_000L,
                down = (3.1 + 1.6 * sin(phase)) * mib,
                up = (0.9 + 0.5 * cos(phase)) * mib,
            )
        }
    }
}
