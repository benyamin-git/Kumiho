package dev.benyamin.kumiho.ui.i18n

import dev.benyamin.kumiho.data.AppLanguage
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PersianFormatTest {

    private val utc = ZoneId.of("UTC")

    @Test
    fun knownGregorianDatesConvertToJalali() {
        assertEquals(JalaliDate(1402, 12, 29), PersianDate.fromGregorian(LocalDate.of(2024, 3, 19)))
        assertEquals(JalaliDate(1403, 1, 1), PersianDate.fromGregorian(LocalDate.of(2024, 3, 20)))
        assertEquals(JalaliDate(1403, 12, 30), PersianDate.fromGregorian(LocalDate.of(2025, 3, 20)))
        assertEquals(JalaliDate(1404, 1, 1), PersianDate.fromGregorian(LocalDate.of(2025, 3, 21)))
        assertEquals(JalaliDate(1405, 8, 19), PersianDate.fromGregorian(LocalDate.of(2026, 11, 10)))
    }

    @Test
    fun leapEsfandHasThirtyDaysOnlyInALeapYear() {
        assertTrue(PersianDate.isLeapYear(1403))
        assertFalse(PersianDate.isLeapYear(1404))
        assertEquals(30, PersianDate.monthLength(1403, 12))
        assertEquals(29, PersianDate.monthLength(1404, 12))
    }

    @Test
    fun numbersUsePersianDigitsForPersian() {
        val persian = PersianFormat.formatNumber(1234)
        assertEquals("۱۲۳۴", persian.filter { it in '۰'..'۹' })
        assertTrue(persian.all { it !in '0'..'9' })
        assertEquals(
            "1234",
            PersianFormat.formatNumber(1234, AppLanguage.ENGLISH).filter { it in '0'..'9' },
        )
    }

    @Test
    fun bytesUsePersianUnitsAndDigits() {
        assertEquals("۰ بایت", PersianFormat.formatBytes(0))
        assertEquals("۱٫۵ کیلوبایت", PersianFormat.formatBytes(1536))
        assertEquals("۱۲٫۳ گیگابایت", PersianFormat.formatBytes(13_207_024_436L))
        assertEquals("1.5 KiB", PersianFormat.formatBytes(1536, AppLanguage.ENGLISH))
    }

    @Test
    fun durationsUsePersianWords() {
        assertEquals("۴۵ ثانیه", PersianFormat.formatDuration(45))
        assertEquals("۳ دقیقه و ۲۰ ثانیه", PersianFormat.formatDuration(200))
        assertEquals("۲ ساعت", PersianFormat.formatDuration(7200))
        assertEquals("3m 20s", PersianFormat.formatDuration(200, AppLanguage.ENGLISH))
    }

    @Test
    fun rttUsesPersianMilliseconds() {
        assertEquals("۱۲۳ میلی‌ثانیه", PersianFormat.formatRtt(123))
        assertEquals("123 ms", PersianFormat.formatRtt(123, AppLanguage.ENGLISH))
    }

    @Test
    fun datesUseTheJalaliCalendarInPersian() {
        val epoch = Instant.parse("2026-11-10T12:00:00Z").epochSecond
        assertEquals("۱۴۰۵/۰۸/۱۹", PersianFormat.formatDate(epoch, utc))
        assertEquals("۱۴۰۵/۰۸/۱۹ ۱۲:۰۰", PersianFormat.formatDateTime(epoch, utc))
        assertEquals("10 Nov 2026", PersianFormat.formatDate(epoch, utc, AppLanguage.ENGLISH))
    }

    @Test
    fun technicalValuesCanBeKeptLatin() {
        assertEquals("123.4", PersianFormat.latinDigits("۱۲۳٫۴"))
        assertEquals("v0.12.0", PersianFormat.latinDigits("v0.12.0"))
    }
}
