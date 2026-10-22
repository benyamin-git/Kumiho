package dev.benyamin.kumiho.ui.logs

import dev.benyamin.kumiho.engine.LogEntry
import dev.benyamin.kumiho.engine.LogLevel
import dev.benyamin.kumiho.test.MainDispatcherRule
import java.time.Instant
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * The logs state machine: subscribe/snapshot while visible, the deduped merge,
 * level/search filtering, Follow and the clear suppression of stale stream
 * entries (the engine client's mirror is not cleared by `logs_clear`).
 */
@OptIn(ExperimentalCoroutinesApi::class)
class LogsViewModelTest {

    @get:Rule
    val mainRule = MainDispatcherRule(StandardTestDispatcher())

    private val gateway = FakeGateway()

    private fun viewModel(): LogsViewModel = LogsViewModel(gateway)

    @Test
    fun `opening subscribes and merges the snapshot with the stream`() =
        runTest(mainRule.testDispatcher) {
            gateway.snapshotResult = { listOf(entry(1), entry(2)) }
            gateway.stream.value = listOf(entry(2), entry(3))
            val viewModel = viewModel()

            viewModel.onVisible()
            runCurrent()

            assertEquals(listOf<String?>(null), gateway.subscribeLevels)
            assertEquals(listOf<Int?>(null), gateway.snapshotLevels)
            assertEquals(listOf(entry(1), entry(2), entry(3)), viewModel.state.value.entries)
            assertFalse(viewModel.state.value.loading)
            assertTrue(viewModel.state.value.follow)
        }

    @Test
    fun `a level change subscribes and re-reads the snapshot at that level`() =
        runTest(mainRule.testDispatcher) {
            gateway.snapshotResult = { level ->
                listOf(entry(1, LogLevel.INFO), entry(2, LogLevel.ERROR))
                    .filter { level == null || it.level.wire == level }
            }
            val viewModel = viewModel()
            viewModel.onVisible()
            runCurrent()

            viewModel.setLevel(LogFilterLevel.ERROR)
            runCurrent()

            assertEquals(listOf(null, "error"), gateway.subscribeLevels)
            assertEquals(listOf(null, "error"), gateway.snapshotLevels)
            assertEquals(listOf(entry(2, LogLevel.ERROR)), viewModel.state.value.entries)
        }

    @Test
    fun `search filters the merged console and Follow returns to the tail`() =
        runTest(mainRule.testDispatcher) {
            gateway.snapshotResult = {
                listOf(
                    entry(1, message = "connected"),
                    entry(2, message = "token rejected"),
                )
            }
            val viewModel = viewModel()
            viewModel.onVisible()
            runCurrent()

            viewModel.setFollow(false)
            viewModel.setSearch("TOKEN")
            runCurrent()

            assertEquals(listOf(entry(2, message = "token rejected")), viewModel.state.value.entries)
            assertTrue("typing must re-enable Follow", viewModel.state.value.follow)

            viewModel.clearSearch()
            assertEquals("", viewModel.state.value.search)
            assertEquals(2, viewModel.state.value.entries.size)
        }

    @Test
    fun `stream updates replace the live source and stay deduped`() =
        runTest(mainRule.testDispatcher) {
            gateway.snapshotResult = { listOf(entry(1)) }
            val viewModel = viewModel()
            viewModel.onVisible()
            runCurrent()

            gateway.stream.value = listOf(entry(1), entry(2))
            runCurrent()
            assertEquals(listOf(entry(1), entry(2)), viewModel.state.value.entries)

            gateway.stream.value = listOf(entry(1), entry(2), entry(3))
            runCurrent()
            assertEquals(3, viewModel.state.value.entries.size)
        }

    @Test
    fun `clear calls the engine and suppresses entries the mirror still holds`() =
        runTest(mainRule.testDispatcher) {
            gateway.snapshotResult = { listOf(entry(1), entry(2)) }
            gateway.stream.value = listOf(entry(1), entry(2))
            val viewModel = viewModel()
            viewModel.onVisible()
            runCurrent()
            assertEquals(2, viewModel.state.value.entries.size)

            viewModel.clear()
            runCurrent()

            assertEquals(1, gateway.clearCalls)
            assertTrue(viewModel.state.value.entries.isEmpty())
            assertEquals(LogsMessage.CLEARED, viewModel.state.value.message)
            assertFalse(viewModel.state.value.clearing)

            // A genuinely new line still appears after the clear.
            gateway.stream.value = listOf(entry(1), entry(2), entry(3))
            runCurrent()
            assertEquals(listOf(entry(3)), viewModel.state.value.entries)
        }

    @Test
    fun `a failed clear reports it and keeps the console`() = runTest(mainRule.testDispatcher) {
        gateway.snapshotResult = { listOf(entry(1)) }
        gateway.clearError = IllegalStateException("the file is read-only")
        val viewModel = viewModel()
        viewModel.onVisible()
        runCurrent()

        viewModel.clear()
        runCurrent()

        assertEquals(LogsMessage.CLEAR_FAILED, viewModel.state.value.message)
        assertEquals("the file is read-only", viewModel.state.value.error)
        assertEquals(1, viewModel.state.value.entries.size)
        assertFalse(viewModel.state.value.clearing)
    }

    @Test
    fun `a failed snapshot reports the failure`() = runTest(mainRule.testDispatcher) {
        gateway.snapshotError = IllegalStateException("the engine is gone")
        val viewModel = viewModel()

        viewModel.onVisible()
        runCurrent()

        assertEquals(LogsMessage.LOAD_FAILED, viewModel.state.value.message)
        assertEquals("the engine is gone", viewModel.state.value.error)
        assertFalse(viewModel.state.value.loading)
    }

    @Test
    fun `hiding unsubscribes and showing again starts a fresh subscription`() =
        runTest(mainRule.testDispatcher) {
            gateway.snapshotResult = { listOf(entry(1)) }
            val viewModel = viewModel()
            viewModel.onVisible()
            runCurrent()

            viewModel.onHidden()
            runCurrent()
            assertEquals(1, gateway.unsubscribeCalls)

            viewModel.onVisible()
            runCurrent()

            assertEquals(listOf(null, null), gateway.subscribeLevels)
            assertEquals(1, gateway.unsubscribeCalls)
        }

    @Test
    fun `copy and export feedback is dismissible`() = runTest(mainRule.testDispatcher) {
        val viewModel = viewModel()
        viewModel.onVisible()
        runCurrent()

        viewModel.notifyCopied()
        assertEquals(LogsMessage.COPIED, viewModel.state.value.message)

        viewModel.notifyExportFailed()
        assertEquals(LogsMessage.EXPORT_FAILED, viewModel.state.value.message)

        viewModel.dismissMessage()
        assertNull(viewModel.state.value.message)
        assertNull(viewModel.state.value.error)
    }

    // ---- fake ------------------------------------------------------------

    private class FakeGateway : LogsGateway {
        override val stream = MutableStateFlow<List<LogEntry>>(emptyList())
        var snapshotResult: suspend (String?) -> List<LogEntry> = { emptyList() }
        var snapshotError: Exception? = null
        var clearError: Exception? = null
        var clearCalls = 0
        var unsubscribeCalls = 0
        val subscribeLevels = mutableListOf<String?>()
        val snapshotLevels = mutableListOf<String?>()

        override suspend fun subscribe(level: String?) {
            subscribeLevels += level
        }

        override suspend fun snapshot(level: String?): List<LogEntry> {
            snapshotLevels += level
            snapshotError?.let { throw it }
            return snapshotResult(level)
        }

        override suspend fun unsubscribe() {
            unsubscribeCalls++
        }

        override suspend fun clear() {
            clearCalls++
            clearError?.let { throw it }
        }
    }

    private companion object {
        fun entry(
            second: Long,
            level: LogLevel = LogLevel.INFO,
            tag: String = "net",
            message: String = "connected",
        ) = LogEntry(
            time = Instant.parse("2026-10-22T12:00:00Z").plusSeconds(second),
            level = level,
            tag = tag,
            message = message,
        )
    }
}
