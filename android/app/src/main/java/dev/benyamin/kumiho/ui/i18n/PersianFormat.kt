package dev.benyamin.kumiho.ui.i18n

import dev.benyamin.kumiho.data.AppLanguage
import java.text.NumberFormat
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/**
 * The one display-formatting path for Persian. Technical values (IPs, ports,
 * versions, emails, URLs and raw log text) never go through here.
 *
 * English keeps the desktop's binary-unit wording; Persian uses Persian digits,
 * Persian units and the Jalali calendar. All functions are pure and JVM-tested.
 */
object PersianFormat {
    private const val UNIT = 1024L
    private val byteUnits = listOf("بایت", "کیلوبایت", "مگابایت", "گیگابایت", "ترابایت", "پتابایت", "اگزابایت")

    fun formatNumber(value: Long, language: AppLanguage = AppLanguage.PERSIAN): String =
        NumberFormat.getIntegerInstance(locale(language)).format(value)

    fun formatNumber(
        value: Double,
        decimals: Int = 0,
        language: AppLanguage = AppLanguage.PERSIAN,
    ): String {
        val format = NumberFormat.getNumberInstance(locale(language)).apply {
            minimumFractionDigits = decimals
            maximumFractionDigits = decimals
            isGroupingUsed = false
        }
        return format.format(value)
    }

    fun formatBytes(bytes: Long, language: AppLanguage = AppLanguage.PERSIAN): String {
        if (language == AppLanguage.ENGLISH) {
            if (bytes < UNIT) return "$bytes B"
            var divisor = UNIT
            var exponent = 0
            var value = bytes / UNIT
            while (value >= UNIT) {
                divisor *= UNIT
                exponent++
                value /= UNIT
            }
            return String.format(Locale.ROOT, "%.1f %ciB", bytes.toDouble() / divisor, "KMGTPE"[exponent])
        }
        if (bytes < UNIT) return "${formatNumber(bytes)} ${byteUnits[0]}"
        var divisor = UNIT
        var exponent = 1
        var value = bytes / UNIT
        while (value >= UNIT && exponent < byteUnits.lastIndex) {
            divisor *= UNIT
            exponent++
            value /= UNIT
        }
        val scaled = bytes.toDouble() / divisor.toDouble()
        val decimals = if (scaled == scaled.toLong().toDouble()) 0 else 1
        return "${formatNumber(scaled, decimals)} ${byteUnits[exponent]}"
    }

    /** `42s`, `3m 20s`, `2h 5m` in English; `۴۲ ثانیه` etc. in Persian. */
    fun formatDuration(seconds: Long, language: AppLanguage = AppLanguage.PERSIAN): String {
        val elapsed = seconds.coerceAtLeast(0L)
        return if (language == AppLanguage.ENGLISH) {
            when {
                elapsed < 60 -> "${elapsed}s"
                elapsed < 3600 -> "${elapsed / 60}m ${elapsed % 60}s"
                else -> "${elapsed / 3600}h ${(elapsed % 3600) / 60}m"
            }
        } else {
            when {
                elapsed < 60 -> "${formatNumber(elapsed)} ثانیه"
                elapsed < 3600 -> buildString {
                    append(formatNumber(elapsed / 60))
                    append(" دقیقه")
                    if (elapsed % 60 != 0L) {
                        append(" و ")
                        append(formatNumber(elapsed % 60))
                        append(" ثانیه")
                    }
                }
                else -> buildString {
                    append(formatNumber(elapsed / 3600))
                    append(" ساعت")
                    if ((elapsed % 3600) / 60 != 0L) {
                        append(" و ")
                        append(formatNumber((elapsed % 3600) / 60))
                        append(" دقیقه")
                    }
                }
            }
        }
    }

    fun formatRtt(milliseconds: Long, language: AppLanguage = AppLanguage.PERSIAN): String =
        if (language == AppLanguage.ENGLISH) {
            "$milliseconds ms"
        } else {
            "${formatNumber(milliseconds)} میلی‌ثانیه"
        }

    /**
     * `۱۲ آبان ۱۴۰۵` is intentionally avoided: the short numeric Jalali form is
     * used consistently in cards and rows. English keeps `12 Nov 2026`.
     */
    fun formatDate(
        epochSeconds: Long,
        zone: ZoneId = ZoneId.systemDefault(),
        language: AppLanguage = AppLanguage.PERSIAN,
    ): String? {
        if (epochSeconds <= 0L) return null
        return if (language == AppLanguage.ENGLISH) {
            DateTimeFormatter.ofPattern("d MMM yyyy", Locale.ENGLISH)
                .withZone(zone)
                .format(Instant.ofEpochSecond(epochSeconds))
        } else {
            val jalali = PersianDate.fromEpochSeconds(epochSeconds, zone)
            toPersianDigits(
                String.format(
                    Locale.ROOT,
                    "%04d/%02d/%02d",
                    jalali.year,
                    jalali.month,
                    jalali.day,
                ),
            )
        }
    }

    fun formatDateTime(
        epochSeconds: Long,
        zone: ZoneId = ZoneId.systemDefault(),
        language: AppLanguage = AppLanguage.PERSIAN,
    ): String? {
        if (epochSeconds <= 0L) return null
        val date = formatDate(epochSeconds, zone, language) ?: return null
        val time = DateTimeFormatter.ofPattern("HH:mm", Locale.ROOT)
            .withZone(zone)
            .format(Instant.ofEpochSecond(epochSeconds))
        return if (language == AppLanguage.ENGLISH) {
            "$date $time"
        } else {
            "$date ${toPersianDigits(time)}"
        }
    }

    /** Keeps technical values copyable while showing Persian prose around them. */
    fun latinDigits(value: String): String = buildString(value.length) {
        value.forEach { char ->
            append(
                when (char) {
                    '۰' -> '0'
                    '۱' -> '1'
                    '۲' -> '2'
                    '۳' -> '3'
                    '۴' -> '4'
                    '۵' -> '5'
                    '۶' -> '6'
                    '۷' -> '7'
                    '۸' -> '8'
                    '۹' -> '9'
                    '٠' -> '0'
                    '١' -> '1'
                    '٢' -> '2'
                    '٣' -> '3'
                    '٤' -> '4'
                    '٥' -> '5'
                    '٦' -> '6'
                    '٧' -> '7'
                    '٨' -> '8'
                    '٩' -> '9'
                    '٫' -> '.'
                    '٬' -> ','
                    else -> char
                },
            )
        }
    }

    private fun locale(language: AppLanguage): Locale =
        if (language == AppLanguage.PERSIAN) Locale.forLanguageTag("fa-IR") else Locale.ENGLISH

    private fun toPersianDigits(value: String): String = buildString(value.length) {
        value.forEach { char ->
            append(
                when (char) {
                    '0' -> '۰'
                    '1' -> '۱'
                    '2' -> '۲'
                    '3' -> '۳'
                    '4' -> '۴'
                    '5' -> '۵'
                    '6' -> '۶'
                    '7' -> '۷'
                    '8' -> '۸'
                    '9' -> '۹'
                    else -> char
                },
            )
        }
    }
}
