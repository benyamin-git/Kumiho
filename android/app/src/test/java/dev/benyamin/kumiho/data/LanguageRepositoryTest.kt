package dev.benyamin.kumiho.data

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test

class LanguageRepositoryTest {

    private class FakePreferences(initial: String = "") : LanguagePreferences {
        override val language = MutableStateFlow(initial)

        override suspend fun setLanguage(value: String) {
            language.value = value
        }
    }

    private fun repository(
        prefs: LanguagePreferences,
        system: () -> String,
        scope: CoroutineScope,
    ) = LanguageRepository(prefs = prefs, scope = scope, systemLanguage = system)

    private fun scope() = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)

    @Test
    fun `explicit preferences win over the system language`() = runBlocking {
        val prefs = FakePreferences()
        val jobScope = scope()
        try {
            val repo = repository(prefs, system = { "fa" }, scope = jobScope)
            repo.start()
            repo.awaitReady()

            repo.setPreference("en")
            assertEquals("en", repo.preference.value)
            assertEquals(AppLanguage.ENGLISH, repo.effective.value)

            repo.setPreference("fa")
            assertEquals("fa", repo.preference.value)
            assertEquals(AppLanguage.PERSIAN, repo.effective.value)
        } finally {
            jobScope.cancel()
        }
    }

    @Test
    fun `system resolves to Persian only for a Persian OS language`() = runBlocking {
        val jobScope = scope()
        try {
            var system = "fa-IR"
            val repo = repository(FakePreferences(), system = { system }, scope = jobScope)
            repo.start()
            repo.awaitReady()
            assertEquals("system", repo.preference.value)
            assertEquals(AppLanguage.PERSIAN, repo.effective.value)

            system = "en-US"
            repo.setPreference("system")
            assertEquals(AppLanguage.ENGLISH, repo.effective.value)
        } finally {
            jobScope.cancel()
        }
    }

    @Test
    fun `an unknown preference falls back to system`() = runBlocking {
        val prefs = FakePreferences()
        val jobScope = scope()
        try {
            val repo = repository(prefs, system = { "en" }, scope = jobScope)
            repo.start()
            repo.awaitReady()
            repo.setPreference("de")

            assertEquals("system", repo.preference.value)
            assertEquals(AppLanguage.ENGLISH, repo.effective.value)
            assertEquals("system", prefs.language.value)
        } finally {
            jobScope.cancel()
        }
    }

    @Test
    fun `the cached preference is applied before the engine answers`() = runBlocking {
        val jobScope = scope()
        try {
            val repo = repository(FakePreferences("fa"), system = { "en" }, scope = jobScope)
            repo.start()
            repo.awaitReady()

            assertEquals("fa", repo.preference.value)
            assertEquals(AppLanguage.PERSIAN, repo.effective.value)
        } finally {
            jobScope.cancel()
        }
    }

    @Test
    fun `a local switch is not clobbered by a later engine sync`() = runBlocking {
        val prefs = FakePreferences("en")
        val jobScope = scope()
        try {
            val repo = repository(prefs, system = { "en" }, scope = jobScope)
            repo.start()
            repo.awaitReady()

            repo.setPreference("fa")
            repo.syncEngine("en")

            assertEquals("fa", repo.preference.value)
            assertEquals(AppLanguage.PERSIAN, repo.effective.value)
            assertEquals("fa", prefs.language.value)
        } finally {
            jobScope.cancel()
        }
    }

    @Test
    fun `the engine setting wins before any local choice`() = runBlocking {
        val prefs = FakePreferences("en")
        val jobScope = scope()
        try {
            val repo = repository(prefs, system = { "en" }, scope = jobScope)
            repo.start()
            repo.awaitReady()

            repo.syncEngine("fa")

            assertEquals("fa", repo.preference.value)
            assertEquals(AppLanguage.PERSIAN, repo.effective.value)
            assertEquals("fa", prefs.language.value)
        } finally {
            jobScope.cancel()
        }
    }

    @Test
    fun `awaitReady waits for the cached value`() = runBlocking {
        val gate = CompletableDeferred<Unit>()
        val prefs = object : LanguagePreferences {
            override val language: Flow<String> = flow {
                gate.await()
                emit("fa")
            }

            override suspend fun setLanguage(value: String) = Unit
        }
        val jobScope = scope()
        try {
            val repo = repository(prefs, system = { "en" }, scope = jobScope)
            repo.start()
            val waiter = async { repo.awaitReady() }

            assertFalse(waiter.isCompleted)
            gate.complete(Unit)
            waiter.await()
            assertEquals(AppLanguage.PERSIAN, repo.effective.value)
        } finally {
            jobScope.cancel()
        }
    }
}
