package dev.benyamin.kumiho.ui.locations

import org.junit.Assert.assertEquals
import org.junit.Test

/** The ISO country-code to flag-emoji mapping, independent of the device locale. */
class CountryFlagsTest {

    @Test
    fun `two ASCII letters map to their regional indicators`() {
        assertEquals("🇩🇪", CountryFlags.emoji("DE"))
        assertEquals("🇩🇪", CountryFlags.emoji("de"))
        assertEquals("🇩🇪", CountryFlags.emoji(" DE "))
        assertEquals("🇺🇸", CountryFlags.emoji("US"))
    }

    @Test
    fun `the recommended pseudo-country has no flag`() {
        assertEquals("", CountryFlags.emoji("REC"))
        assertEquals("", CountryFlags.emoji("rec"))
    }

    @Test
    fun `blank and malformed codes have no flag`() {
        assertEquals("", CountryFlags.emoji(""))
        assertEquals("", CountryFlags.emoji(null))
        assertEquals("", CountryFlags.emoji("USA"))
        assertEquals("", CountryFlags.emoji("D1"))
        assertEquals("", CountryFlags.emoji("D E"))
    }
}
