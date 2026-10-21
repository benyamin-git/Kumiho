package dev.benyamin.kumiho

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import dev.benyamin.kumiho.go.mobile.DeviceBridge
import dev.benyamin.kumiho.go.mobile.Listener
import dev.benyamin.kumiho.go.mobile.Mobile
import java.io.File
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

/**
 * T6 AAR probe (plan STOP/GO gate).
 *
 * This is the off-device proof that the gomobile AAR is usable from Kotlin: the
 * generated [Mobile] class loads, its static methods are bound, the engine
 * starts against app-private paths, a command round-trips through
 * Kotlin -> Go -> Kotlin, the [Listener] callback fires back into Kotlin, and
 * close is idempotent.
 *
 * It deliberately does not connect: the fake [DeviceBridge.acquireFD] throws, so
 * a connect would fail loudly instead of silently passing. fd adoption is
 * covered by the Go pipe test in T4; the real establish/DNS check is T11.
 *
 * The five assertions live in one test method because [Mobile] is a
 * process-wide singleton and JUnit gives no ordering guarantee between methods.
 */
@RunWith(AndroidJUnit4::class)
class MobileProbeTest {

    /** Records bridge calls and refuses the fd path the probe must not take. */
    private class RecordingBridge : DeviceBridge {
        val configureCalls = CopyOnWriteArrayList<String>()
        val networkAvailableCalls = AtomicInteger()

        override fun configure(configJSON: String) {
            configureCalls.add(configJSON)
        }

        override fun acquireFD(mtu: Long): Long =
            throw AssertionError("the probe must not call DeviceBridge.acquireFD; fd adoption is T4/T11")

        override fun networkAvailable(): Boolean {
            networkAvailableCalls.incrementAndGet()
            return true
        }
    }

    /** Latches the first Kotlin callback from Go. */
    private class LatchingListener : Listener {
        val statuses = CopyOnWriteArrayList<String>()
        private val firstStatus = CountDownLatch(1)

        override fun onStatus(statusJSON: String) {
            statuses.add(statusJSON)
            firstStatus.countDown()
        }

        override fun onLog(entryJSON: String) = Unit

        override fun onPing(resultJSON: String) = Unit

        override fun onNotice(noticeJSON: String) = Unit

        fun awaitFirstStatus(timeout: Long, unit: TimeUnit): Boolean = firstStatus.await(timeout, unit)
    }

    @Test
    fun aarProbe() {
        // 1. The AAR loads and its static methods are bound.
        val version = Mobile.version()
        assertTrue("Mobile.version() must not be empty", version.isNotBlank())

        // 2. Palette is reachable before the engine starts.
        val palette = Mobile.palette("dark", "blue")
        assertTrue("palette must carry the primary role: $palette", palette.contains("\"primary\""))
        assertTrue("palette must carry the on-surface role: $palette", palette.contains("\"on-surface\""))

        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val dataDir = File(context.filesDir, "aar-probe")
        val tokensDir = File(context.noBackupFilesDir, "aar-probe")
        val runtimeDir = File(context.cacheDir, "aar-probe")
        dataDir.deleteRecursively()
        tokensDir.deleteRecursively()
        runtimeDir.deleteRecursively()

        val paths = JSONObject()
            .put("config", File(dataDir, "kumiho.toml").absolutePath)
            .put("state", File(dataDir, "state.json").absolutePath)
            .put("tokens", tokensDir.absolutePath)
            .put("runtime", runtimeDir.absolutePath)
            .put("logs", File(dataDir, "logs").absolutePath)
            .toString()

        val bridge = RecordingBridge()
        val listener = LatchingListener()

        // 3. Start with app-private paths, a recording fake bridge and a listener.
        Mobile.start(paths, bridge, listener)
        try {
            val status = Mobile.command("""{"type":"status"}""")
            val envelope = JSONObject(status)
            assertEquals("status", envelope.getString("type"))
            assertTrue("the status envelope must carry a payload: $status", envelope.has("payload"))
            val payload = envelope.getJSONObject("payload")
            assertTrue("the status payload must carry a state: $payload", payload.has("state"))
            assertTrue("a fresh probe directory is signed out: $payload", !payload.optBoolean("authenticated"))

            // 4. Kotlin -> Go -> Kotlin: the listener fired back into Kotlin.
            assertTrue(
                "Listener.onStatus must fire within 15s",
                listener.awaitFirstStatus(15, TimeUnit.SECONDS),
            )
            val pushed = JSONObject(listener.statuses.last())
            assertTrue("the pushed status must carry a state: $pushed", pushed.has("state"))
        } finally {
            // 5a. Close succeeds on a live session.
            Mobile.close()
        }

        // 5b. A second close is harmless ...
        Mobile.close()

        // ... and the facade is really gone: commands fail with a Go error.
        var afterClose: Exception? = null
        try {
            Mobile.command("""{"type":"status"}""")
        } catch (e: Exception) {
            afterClose = e
        }
        assertNotNull("command after close must fail with a Go error", afterClose)
    }
}
