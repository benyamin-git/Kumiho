package dev.benyamin.kumiho.data

import dev.benyamin.kumiho.test.MainDispatcherRule
import dev.benyamin.kumiho.vpn.SplitMode
import dev.benyamin.kumiho.vpn.SplitSettings
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.emitAll
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.map
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
 * The split tunnel's persistence contract: Off by default, immediate writes,
 * uninstalled-package cleanup, a failed package query that guesses nothing, the
 * synchronous provider snapshot the VPN bridge reads, and retry after a broken
 * store.
 *
 * [SplitTunnelStore] is an in-memory fake and the scope is the test scope, so
 * every write is deterministic virtual time.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class SplitTunnelTest {

    @get:Rule
    val mainRule = MainDispatcherRule(StandardTestDispatcher())

    private class FakeStore(initial: SplitSettings = SplitSettings()) : SplitTunnelStore {
        val flow = MutableStateFlow(initial)
        override val splitSettings: Flow<SplitSettings> = flow

        val modes = mutableListOf<SplitMode>()
        val packageWrites = mutableListOf<Set<String>>()

        var failWrites = false

        override suspend fun setSplitMode(mode: SplitMode) {
            if (failWrites) error("the preferences store is full")
            modes += mode
            flow.value = flow.value.copy(mode = mode)
        }

        override suspend fun setSplitPackages(packages: Set<String>) {
            if (failWrites) error("the preferences store is full")
            packageWrites += packages
            flow.value = flow.value.copy(packages = packages)
        }
    }

    /** A store whose collection fails once, then works after [repaired]. */
    private class BrokenStore(
        private val backing: MutableStateFlow<SplitSettings> = MutableStateFlow(SplitSettings()),
    ) : SplitTunnelStore {
        var broken = true
        var collections = 0

        override val splitSettings: Flow<SplitSettings> = flow {
            collections += 1
            if (broken) error("corrupt preferences")
            emitAll(backing.map { it })
        }

        override suspend fun setSplitMode(mode: SplitMode) {
            backing.value = backing.value.copy(mode = mode)
        }

        override suspend fun setSplitPackages(packages: Set<String>) {
            backing.value = backing.value.copy(packages = packages)
        }
    }

    private fun repository(
        store: SplitTunnelStore,
        installed: suspend () -> Set<String>,
        scope: kotlinx.coroutines.CoroutineScope,
    ) = SplitTunnelRepository(store = store, scope = scope, installedPackages = installed)

    // ---- defaults and loading -------------------------------------------

    @Test
    fun `a fresh install is Off with an empty selection`() = runTest(mainRule.testDispatcher) {
        val store = FakeStore()
        val repository = repository(store, { emptySet() }, backgroundScope)

        assertEquals(SplitSettings(), repository.snapshot())
        assertTrue(repository.state.value.loading)

        repository.start()
        runCurrent()

        assertFalse(repository.state.value.loading)
        assertNull(repository.state.value.error)
        assertEquals(SplitMode.OFF, repository.state.value.settings.mode)
        assertEquals(emptySet<String>(), repository.state.value.settings.packages)
        assertEquals(SplitSettings(), repository.snapshot())
        assertTrue("an empty selection needs no package query", store.packageWrites.isEmpty())
    }

    @Test
    fun `the provider serves the stored selection synchronously after load`() =
        runTest(mainRule.testDispatcher) {
            val stored = SplitSettings(
                mode = SplitMode.EXCLUDE,
                packages = setOf("com.example.a", "com.example.b"),
            )
            val store = FakeStore(stored)
            val repository = repository(store, { stored.packages }, backgroundScope)

            repository.start()
            runCurrent()

            assertEquals(stored, repository.snapshot())
            assertEquals(stored, repository.state.value.settings)
        }

    // ---- immediate writes ------------------------------------------------

    @Test
    fun `a mode change is mirrored at once and persisted`() = runTest(mainRule.testDispatcher) {
        val store = FakeStore()
        val repository = repository(store, { emptySet() }, backgroundScope)
        repository.start()
        runCurrent()

        repository.setMode(SplitMode.INCLUDE)

        // The snapshot is optimistic: acquireFD can run before the write lands.
        assertEquals(SplitMode.INCLUDE, repository.snapshot().mode)
        runCurrent()
        assertEquals(listOf(SplitMode.INCLUDE), store.modes)
        assertEquals(SplitMode.INCLUDE, store.flow.value.mode)
    }

    @Test
    fun `toggling a package persists the whole set each time`() = runTest(mainRule.testDispatcher) {
        val store = FakeStore()
        val installed = setOf("com.example.a", "com.example.b")
        val repository = repository(store, { installed }, backgroundScope)
        repository.start()
        runCurrent()

        repository.togglePackage("com.example.a")
        runCurrent()
        repository.togglePackage("com.example.b")
        runCurrent()

        assertEquals(
            listOf(setOf("com.example.a"), setOf("com.example.a", "com.example.b")),
            store.packageWrites,
        )
        assertEquals(installed, repository.snapshot().packages)

        repository.togglePackage("com.example.a")
        runCurrent()
        assertEquals(setOf("com.example.b"), store.packageWrites.last())
        assertEquals(setOf("com.example.b"), repository.snapshot().packages)
    }

    @Test
    fun `blank package names are never stored`() = runTest(mainRule.testDispatcher) {
        val store = FakeStore()
        val repository = repository(store, { setOf("com.example.a") }, backgroundScope)
        repository.start()
        runCurrent()

        repository.setPackages(setOf("com.example.a", "", "  "))
        runCurrent()

        assertEquals(setOf("com.example.a"), store.packageWrites.single())
        assertEquals(setOf("com.example.a"), repository.snapshot().packages)
    }

    // ---- uninstall cleanup ----------------------------------------------

    @Test
    fun `an uninstalled selection is dropped on load and persisted`() =
        runTest(mainRule.testDispatcher) {
            val store = FakeStore(
                SplitSettings(
                    mode = SplitMode.EXCLUDE,
                    packages = setOf("com.example.gone", "com.example.here"),
                ),
            )
            val repository = repository(store, { setOf("com.example.here") }, backgroundScope)

            repository.start()
            runCurrent()

            assertEquals(setOf("com.example.here"), repository.snapshot().packages)
            assertEquals(listOf(setOf("com.example.here")), store.packageWrites)
            assertEquals(setOf("com.example.here"), store.flow.value.packages)
        }

    @Test
    fun `a failed package query keeps the stored selection`() = runTest(mainRule.testDispatcher) {
        val stored = SplitSettings(
            mode = SplitMode.INCLUDE,
            packages = setOf("com.example.a"),
        )
        val store = FakeStore(stored)
        val repository = repository(store, { error("package manager unavailable") }, backgroundScope)

        repository.start()
        runCurrent()

        assertEquals(stored, repository.snapshot())
        assertTrue("a failed query is not proof of an uninstall", store.packageWrites.isEmpty())
        assertNull(repository.state.value.error)
    }

    // ---- failures ---------------------------------------------------------

    @Test
    fun `a failed write surfaces an error and keeps the optimistic value`() =
        runTest(mainRule.testDispatcher) {
            val store = FakeStore()
            val repository = repository(store, { emptySet() }, backgroundScope)
            repository.start()
            runCurrent()

            store.failWrites = true
            repository.setMode(SplitMode.EXCLUDE)
            runCurrent()

            assertEquals(SplitMode.EXCLUDE, repository.snapshot().mode)
            assertEquals("the preferences store is full", repository.state.value.error)
        }

    @Test
    fun `retry restarts observation after a broken store`() = runTest(mainRule.testDispatcher) {
        val store = BrokenStore()
        val repository = repository(store, { emptySet() }, backgroundScope)

        repository.start()
        runCurrent()

        assertEquals("corrupt preferences", repository.state.value.error)
        assertFalse(repository.state.value.loading)

        store.broken = false
        store.collections = 0
        repository.retry()
        runCurrent()

        assertEquals(1, store.collections)
        assertNull(repository.state.value.error)
        assertEquals(SplitSettings(), repository.snapshot())
    }
}
