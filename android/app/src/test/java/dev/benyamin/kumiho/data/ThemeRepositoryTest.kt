package dev.benyamin.kumiho.data

import dev.benyamin.kumiho.ui.theme.ThemeId
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The palette pipeline's failure semantics. [ThemePreferences] and
 * `loadPaletteJson` are the two seams, so this runs on the JVM without a
 * DataStore or the Go library.
 */
class ThemeRepositoryTest {

    private class FakePreferences(
        theme: String? = null,
        accent: String? = null,
    ) : ThemePreferences {
        override val theme = MutableStateFlow(theme)
        override val accent = MutableStateFlow(accent)
        override suspend fun setTheme(id: String) {
            theme.value = id
        }
        override suspend fun setAccent(id: String) {
            accent.value = id
        }
    }

    @Test
    fun `a cancelled palette load never publishes an error`() = runBlocking {
        val started = CompletableDeferred<Unit>()
        val gate = CompletableDeferred<Unit>()
        val scope = CoroutineScope(Job() + Dispatchers.Default)
        val repository = ThemeRepository(
            prefs = FakePreferences(),
            scope = scope,
            defaultTheme = { ThemeId.LIGHT },
            loadPaletteJson = { _, _ ->
                started.complete(Unit)
                gate.await()
                "unreachable"
            },
        )
        try {
            repository.start()
            started.await()

            // collectLatest cancels a superseded load; that must stay pending,
            // not surface "coroutine was cancelled" as a palette failure.
            val job = requireNotNull(scope.coroutineContext[Job])
            job.cancel()
            job.join()

            assertTrue(
                "cancellation published ${repository.state.value}",
                repository.state.value is PaletteState.Loading,
            )
        } finally {
            scope.cancel()
        }
    }

    @Test
    fun `a real palette failure still publishes an error`() = runBlocking {
        val scope = CoroutineScope(Job() + Dispatchers.Default)
        val repository = ThemeRepository(
            prefs = FakePreferences(),
            scope = scope,
            defaultTheme = { ThemeId.LIGHT },
            loadPaletteJson = { _, _ -> error("no palette") },
        )
        try {
            repository.start()
            repository.awaitReady()

            val state = repository.state.value
            assertTrue("expected an error state, got $state", state is PaletteState.Error)
            assertEquals("no palette", (state as PaletteState.Error).message)
        } finally {
            scope.cancel()
        }
    }
}
