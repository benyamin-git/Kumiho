package dev.benyamin.kumiho.ui.locations

import dev.benyamin.kumiho.R
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** The generated bundle: only ISO alpha-2 codes resolve to a real flag. */
class CountryFlagsTest {

    @Test
    fun `known ISO codes resolve case-insensitively and ignore surrounding space`() {
        assertEquals(R.drawable.flag_de, CountryFlags.resourceFor("DE"))
        assertEquals(R.drawable.flag_de, CountryFlags.resourceFor("de"))
        assertEquals(R.drawable.flag_de, CountryFlags.resourceFor(" de "))
        assertEquals(R.drawable.flag_us, CountryFlags.resourceFor("US"))
    }

    @Test
    fun `Recommended unknown non-ISO and malformed codes have no flag`() {
        assertNull(CountryFlags.resourceFor("REC"))
        assertNull(CountryFlags.resourceFor("rec"))
        assertNull(CountryFlags.resourceFor("xx"))
        assertNull(CountryFlags.resourceFor("xk"))
        assertNull(CountryFlags.resourceFor("eu"))
        assertNull(CountryFlags.resourceFor("USA"))
        assertNull(CountryFlags.resourceFor("D1"))
        assertNull(CountryFlags.resourceFor(""))
        assertNull(CountryFlags.resourceFor(null))
    }

    @Test
    fun `the generated map contains every ISO alpha-2 flag exactly once`() {
        assertEquals(249, COUNTRY_FLAG_RESOURCES.size)
        assertTrue(COUNTRY_FLAG_RESOURCES.keys.all { it.matches(Regex("[a-z]{2}")) })
        assertEquals(249, COUNTRY_FLAG_RESOURCES.values.toSet().size)
        assertNotNull(COUNTRY_FLAG_RESOURCES["gb"])
        assertNotNull(COUNTRY_FLAG_RESOURCES["us"])
    }
}
