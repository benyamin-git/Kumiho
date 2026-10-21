package dev.benyamin.kumiho.ui.dashboard

import dev.benyamin.kumiho.engine.ByteCounters
import dev.benyamin.kumiho.engine.ByteRates
import dev.benyamin.kumiho.engine.Quota
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/**
 * Byte, quota and date formatting with the desktop's wording
 * (`internal/gui/controller.go`): binary units, `%.1f %ciB`, and the same
 * quota/rates/totals sentences. A missing value is null, never a fake zero;
 * callers hide the row or card instead of inventing data.
 *
 * The file is pure: no Android, no Compose, and its date output takes the zone
 * and locale from the caller so a JVM test is deterministic.
 */
object DashboardFormat {
    /** The one missing-value glyph the UI shows for a missing sub-value. */
    const val DASH = "—"

    private const val UNIT = 1024L

    /**
     * A byte count in binary units, exactly as the desktop's `humanBytes`:
     * `0 B`, `512 B`, `1.0 KiB`, `1.5 MiB`, `12.3 GiB`.
     */
    fun humanBytes(bytes: Long): String {
        if (bytes < UNIT) return "$bytes B"
        var divisor = UNIT
        var exponent = 0
        var value = bytes / UNIT
        while (value >= UNIT) {
            divisor *= UNIT
            exponent++
            value /= UNIT
        }
        val scaled = bytes.toDouble() / divisor.toDouble()
        val suffix = "KMGTPE"[exponent]
        return String.format(Locale.ROOT, "%.1f %ciB", scaled, suffix)
    }

    /**
     * The quota sentence: `12.3 GiB remaining of 50.0 GiB` when both sides are
     * known, a half sentence for one side, and null when there is nothing.
     * Zero remaining is a real, exhausted quota and stays visible.
     */
    fun quotaLabel(quota: Quota?): String? {
        if (quota == null) return null
        val remaining = quota.remaining
        val limit = quota.limit
        return when {
            remaining != null && limit != null ->
                "${humanBytes(remaining)} remaining of ${humanBytes(limit)}"
            remaining != null -> "${humanBytes(remaining)} remaining"
            limit != null -> humanBytes(limit)
            else -> null
        }
    }

    /** The bar fraction, or null when either side is missing or nonsensical. */
    fun quotaFraction(quota: Quota?): Float? {
        val remaining = quota?.remaining ?: return null
        val limit = quota.limit ?: return null
        if (limit <= 0L) return null
        return (remaining.toDouble() / limit.toDouble()).toFloat().coerceIn(0f, 1f)
    }

    /**
     * `up 1.2 MiB/s / down 3.4 MiB/s`; null when there are no rates yet. Both
     * directions at zero describe no traffic, which the desktop also hides.
     */
    fun ratesLabel(rates: ByteRates?): String? {
        if (rates == null || (rates.up == 0.0 && rates.down == 0.0)) return null
        return "up ${humanBytes(rates.up.toLong())}/s / down ${humanBytes(rates.down.toLong())}/s"
    }

    /** `up 12.3 MiB / down 45.6 MiB`; null when the engine has no totals. */
    fun totalsLabel(totals: ByteCounters?): String? {
        if (totals == null) return null
        return "up ${humanBytes(totals.up)} / down ${humanBytes(totals.down)}"
    }

    /**
     * Session uptime from the engine's `since` (Unix seconds): `42s`,
     * `3m 20s`, `2h 5m`. Null when the engine has no start time; a start time
     * in the future reads as zero rather than a negative duration.
     */
    fun uptimeLabel(since: Long, nowSeconds: Long): String? {
        if (since <= 0L) return null
        val elapsed = (nowSeconds - since).coerceAtLeast(0L)
        return when {
            elapsed < 60 -> "${elapsed}s"
            elapsed < 3600 -> "${elapsed / 60}m ${elapsed % 60}s"
            else -> "${elapsed / 3600}h ${(elapsed % 3600) / 60}m"
        }
    }

    /**
     * A date for an expiry or reset timestamp in Unix seconds, e.g.
     * `12 Nov 2026`. Null for an absent (0) timestamp, so no row claims an
     * expiry the engine never reported.
     */
    fun dateLabel(
        epochSeconds: Long,
        zone: ZoneId = ZoneId.systemDefault(),
        locale: Locale = Locale.getDefault(),
    ): String? {
        if (epochSeconds <= 0L) return null
        val formatter = DateTimeFormatter.ofPattern("d MMM yyyy", locale).withZone(zone)
        return formatter.format(Instant.ofEpochSecond(epochSeconds))
    }

    /** The host part of the engine's `host:port` server string. */
    fun hostOf(server: String?): String? {
        val value = server?.trim().orEmpty()
        if (value.isEmpty()) return null
        val colon = value.lastIndexOf(':')
        if (colon > 0 && value.indexOf(':') == colon) {
            val port = value.substring(colon + 1)
            if (port.isNotEmpty() && port.all { it.isDigit() }) {
                return value.substring(0, colon)
            }
        }
        return value
    }
}

/** How the IPv6 row presents the engine's `ipv6` value. */
sealed interface Ipv6Display {
    /** The engine reports the whole family blackholed (C1's only mode). */
    data object Blackholed : Ipv6Display

    data class Value(val text: String) : Ipv6Display

    /** No value at all: the row shows an em dash, never a fake "none". */
    data object Missing : Ipv6Display
}

/** Classifies the engine's IPv6 value without baking display copy into state. */
fun ipv6Display(value: String?): Ipv6Display {
    val text = value?.trim().orEmpty()
    return when {
        text.equals("blackholed", ignoreCase = true) -> Ipv6Display.Blackholed
        text.isEmpty() -> Ipv6Display.Missing
        else -> Ipv6Display.Value(text)
    }
}
