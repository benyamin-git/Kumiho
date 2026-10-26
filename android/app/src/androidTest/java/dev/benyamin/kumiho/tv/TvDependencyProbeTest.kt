package dev.benyamin.kumiho.tv

import androidx.compose.runtime.Composable
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.tv.material3.MaterialTheme
import androidx.tvprovider.media.tv.TvContractCompat
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith

/**
 * T1 classpath probe: the TV provider contract and Compose for TV artifacts
 * must resolve and compile before any TV UI work starts.
 */
@RunWith(AndroidJUnit4::class)
class TvDependencyProbeTest {

    @Test
    fun tvProviderContractIsAvailable() {
        assertEquals("android.media.tv", TvContractCompat.Channels.CONTENT_URI.authority)
    }

    @Suppress("unused")
    @Composable
    private fun tvMaterialThemeCompiles() {
        MaterialTheme {
            // The call is enough to prove tv-material resolves and compiles.
        }
    }
}
