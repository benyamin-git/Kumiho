package dev.benyamin.kumiho.data

import dev.benyamin.kumiho.test.MainDispatcherRule
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.emitAll
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.onStart
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
 * The allow-bypass persistence contract: Off by default, a synchronous
 * snapshot for the VPN bridge, a pending value committed only after
 * `establish()`, immediate writes, a failed read that serves Off and stays
 * retryable, and a failed write that restores the last persisted value.
 *
 * [BypassSettingsStore] is an in-memory fake and the scope is the test scope,
 * so every write is deterministic virtual time.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class AllowBypassTest {

    @get:Rule
    val mainRule = MainDispatcherRule(StandardTestDispatcher())

    private class FakeStore(initial: Boolean = false) : BypassSettingsStore {
        val backing = MutableStateFlow(initial)
        val noise = MutableStateFlow(0)
        val writes = mutableListOf<Boolean>()
        var collections = 0
        var failWrites = false

        /** Emits a duplicate allow-bypass value when an unrelated key changes. */
        override val allowBypass: Flow<Boolean> =
            combine(backing, noise) { value, _ -> value }
                .onStart { collections += 1 }

        override suspend fun setAllowBypass(enabled: Boolean) {
            if (failWrites) error("the preferences store is full")
            writes += enabled
            backing.value = enabled
        }
    }

    /** A store that holds the first write until the second one is requested. */
    private class GatedStore(initial: Boolean = false) : BypassSettingsStore {
        val backing = MutableStateFlow(initial)
        val writes = mutableListOf<Boolean>()
        val releaseFirst = CompletableDeferred<Unit>()
        private var gateUsed = false

        override val allowBypass: Flow<Boolean> = backing

        override suspend fun setAllowBypass(enabled: Boolean) {
            if (!gateUsed) {
                gateUsed = true
                releaseFirst.await()
            }
            writes += enabled
            backing.value = enabled
        }
    }

    /** A store whose collection fails until [broken] is cleared. */
    private class BrokenStore : BypassSettingsStore {
        val backing = MutableStateFlow(true)
        var broken = true
        var collections = 0

        override val allowBypass: Flow<Boolean> = flow {
            collections += 1
            if (broken) error("corrupt preferences")
            emitAll(backing)
        }

        override suspend fun setAllowBypass(enabled: Boolean) {
            backing.value = enabled
        }
    }

    private fun repository(store: BypassSettingsStore, scope: kotlinx.coroutines.CoroutineScope) =
        AllowBypassRepository(store = store, scope = scope)

    // ---- defaults and loading -------------------------------------------

    @Test
    fun `a fresh repository is Off and loading`() = runTest(mainRule.testDispatcher) {
        val store = FakeStore()
        val repository = repository(store, backgroundScope)

        assertEquals(AllowBypassState(), repository.state.value)
        assertFalse(repository.snapshot())
    }

    @Test
    fun `start loads the stored value`() = runTest(mainRule.testDispatcher) {
        val store = FakeStore(initial = true)
        val repository = repository(store, backgroundScope)

        repository.start()
        runCurrent()

        assertFalse(repository.state.value.loading)
        assertTrue(repository.state.value.enabled)
        assertNull(repository.state.value.error)
        assertTrue(repository.snapshot())
    }

    // ---- immediate writes ------------------------------------------------

    @Test
    fun `a toggle is mirrored immediately and persisted`() =
        runTest(mainRule.testDispatcher) {
            val store = FakeStore()
            val repository = repository(store, backgroundScope)
            repository.start()
            runCurrent()

            repository.setEnabled(true)

            // The snapshot is optimistic: acquireFD can run before the write lands.
            assertTrue(repository.state.value.enabled)
            runCurrent()
            assertEquals(listOf(true), store.writes)
            assertTrue(store.backing.value)
        }

    @Test
    fun `a slow write cannot overwrite the last toggle`() =
        runTest(mainRule.testDispatcher) {
            val store = GatedStore()
            val repository = repository(store, backgroundScope)
            repository.start()
            runCurrent()

            repository.setEnabled(true)
            repository.setEnabled(false)
            assertFalse(repository.state.value.enabled)

            runCurrent()
            store.releaseFirst.complete(Unit)
            runCurrent()

            assertEquals(listOf(true, false), store.writes)
            assertFalse(store.backing.value)
            assertFalse(repository.state.value.enabled)
        }

    @Test
    fun `an unrelated preferences emission does not clear a save error`() =
        runTest(mainRule.testDispatcher) {
            val store = FakeStore(initial = true)
            val repository = repository(store, backgroundScope)
            repository.start()
            runCurrent()

            store.failWrites = true
            repository.setEnabled(false)
            runCurrent()
            assertEquals(BypassError.SAVE, repository.state.value.error)

            store.noise.value += 1
            runCurrent()

            assertEquals(BypassError.SAVE, repository.state.value.error)
            assertTrue(repository.state.value.enabled)
        }

    @Test
    fun `snapshot records the pending value until markApplied`() =
        runTest(mainRule.testDispatcher) {
            val store = FakeStore(initial = true)
            val repository = repository(store, backgroundScope)
            repository.start()
            runCurrent()

            assertTrue(repository.state.value.enabled)
            assertFalse(repository.state.value.applied)

            assertTrue(repository.snapshot())
            assertFalse(repository.state.value.applied)

            repository.markApplied()
            assertTrue(repository.state.value.applied)
        }

    // ---- failures ---------------------------------------------------------

    @Test
    fun `a failed load serves Off and stays retryable`() = runTest(mainRule.testDispatcher) {
        val store = BrokenStore()
        val repository = repository(store, backgroundScope)

        repository.start()
        runCurrent()

        assertFalse(repository.state.value.enabled)
        assertFalse(repository.state.value.loading)
        assertEquals(BypassError.LOAD, repository.state.value.error)
        assertFalse(repository.snapshot())

        store.broken = false
        store.collections = 0
        repository.start()
        runCurrent()

        assertEquals(1, store.collections)
        assertTrue(repository.state.value.enabled)
        assertFalse(repository.state.value.loading)
        assertNull(repository.state.value.error)
        assertTrue(repository.snapshot())
    }

    @Test
    fun `a failed write restores the persisted value and reports save`() =
        runTest(mainRule.testDispatcher) {
            val store = FakeStore(initial = true)
            val repository = repository(store, backgroundScope)
            repository.start()
            runCurrent()

            store.failWrites = true
            repository.setEnabled(false)
            assertFalse("the toggle is optimistic", repository.state.value.enabled)

            runCurrent()
            assertTrue("the last persisted value is restored", repository.state.value.enabled)
            assertEquals(BypassError.SAVE, repository.state.value.error)

            store.failWrites = false
            repository.setEnabled(false)
            runCurrent()
            assertFalse(repository.state.value.enabled)
            assertNull(repository.state.value.error)
        }

    @Test
    fun `a write after a failed load recovers the stored value`() =
        runTest(mainRule.testDispatcher) {
            val store = BrokenStore()
            val repository = repository(store, backgroundScope)
            repository.start()
            runCurrent()
            assertEquals(BypassError.LOAD, repository.state.value.error)

            repository.setEnabled(true)
            runCurrent()

            assertTrue(repository.state.value.enabled)
            assertTrue(store.backing.value)
            assertNull(repository.state.value.error)
        }

    @Test
    fun `start is idempotent`() = runTest(mainRule.testDispatcher) {
        val store = FakeStore()
        val repository = repository(store, backgroundScope)

        repository.start()
        repository.start()
        runCurrent()

        assertEquals(1, store.collections)
    }
}
