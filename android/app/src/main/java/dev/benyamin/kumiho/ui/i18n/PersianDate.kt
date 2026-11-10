package dev.benyamin.kumiho.ui.i18n

import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/**
 * The pure Gregorian-to-Jalali conversion. It matches the arithmetic used by
 * the common Jalali libraries and keeps the Android UI free of an ICU-only
 * dependency, so the edge dates are JVM-testable.
 */
data class JalaliDate(val year: Int, val month: Int, val day: Int)

object PersianDate {
    private val monthLengths = intArrayOf(31, 31, 31, 31, 31, 31, 30, 30, 30, 30, 30, 29)
    private val gregorianMonthOffsets = intArrayOf(0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334)

    fun fromGregorian(date: LocalDate): JalaliDate {
        val gy = date.year
        val gm = date.monthValue
        val gd = date.dayOfMonth
        val gy2 = if (gm > 2) gy + 1 else gy
        var days = 355666 + (365 * gy) + ((gy2 + 3) / 4) - ((gy2 + 99) / 100) +
            ((gy2 + 399) / 400) + gd + gregorianMonthOffsets[gm - 1]
        var jy = -1595 + (33 * (days / 12053))
        days %= 12053
        jy += 4 * (days / 1461)
        days %= 1461
        if (days > 365) {
            jy += (days - 1) / 365
            days = (days - 1) % 365
        }
        return if (days < 186) {
            JalaliDate(jy, 1 + days / 31, 1 + days % 31)
        } else {
            JalaliDate(jy, 7 + (days - 186) / 30, 1 + (days - 186) % 30)
        }
    }

    fun fromEpochSeconds(epochSeconds: Long, zone: ZoneId = ZoneId.systemDefault()): JalaliDate =
        fromGregorian(Instant.ofEpochSecond(epochSeconds).atZone(zone).toLocalDate())

    fun isLeapYear(year: Int): Boolean {
        val remainder = year % 33
        return remainder in intArrayOf(1, 5, 9, 13, 17, 22, 26, 30)
    }

    fun monthLength(year: Int, month: Int): Int =
        if (month == 12 && isLeapYear(year)) 30 else monthLengths[month - 1]
}
