package dev.benyamin.kumiho.ui.i18n

import org.junit.Assert.assertEquals
import org.junit.Test

/** The invisible isolate controls around a Latin technical value. */
class BidiTextTest {

    @Test
    fun ltrValuesAreWrappedWithoutChangingTheirBytes() {
        assertEquals("\u2066vpn.example.com\u2069", BidiText.isolateLtr("vpn.example.com"))
    }

    @Test
    fun emptyAndNullStayEmpty() {
        assertEquals("", BidiText.isolateLtr(""))
    }

    @Test
    fun theControlsAreTheUnicodeIsolatePair() {
        assertEquals("\u2066", BidiText.LRI)
        assertEquals("\u2069", BidiText.PDI)
    }
}
