package dev.benyamin.kumiho.ui.dashboard

import dev.benyamin.kumiho.engine.ByteCounters
import dev.benyamin.kumiho.engine.ByteRates
import dev.benyamin.kumiho.engine.Quota
import java.time.ZoneId
import java.util.Locale
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * Byte, quota, rate, uptime and date formatting against the desktop's wording
 * (`internal/gui/controller.go`): binary units, `%.1f %ciB`, and one decimal.
 *
 * Missing data is null, never a fake zero: the UI hides the row instead of
 * claiming a quota or an exit IP the engine never reported.
 */
class DashboardFormatTest {

    // ---- bytes -----------------------------------------------------------

    @Test
    fun bytesUseBinaryUnitsAndOneDecimal() {
        assertEquals("0 B", DashboardFormat.humanBytes(0))
        assertEquals("1 B", DashboardFormat.humanBytes(1))
        assertEquals("512 B", DashboardFormat.humanBytes(512))
        assertEquals("1023 B", DashboardFormat.humanBytes(1023))
        assertEquals("1.0 KiB", DashboardFormat.humanBytes(1024))
        assertEquals("1.5 KiB", DashboardFormat.humanBytes(1536))
        assertEquals("1.0 MiB", DashboardFormat.humanBytes(1024L * 1024L))
        assertEquals("1.5 MiB", DashboardFormat.humanBytes(1024L * 1024L * 3 / 2))
        assertEquals("12.3 GiB", DashboardFormat.humanBytes(13_207_024_436L))
        assertEquals("50.0 GiB", DashboardFormat.humanBytes(50L * 1024L * 1024L * 1024L))
        assertEquals("1.0 TiB", DashboardFormat.humanBytes(1024L * 1024L * 1024L * 1024L))
    }

    // ---- quota -----------------------------------------------------------

    @Test
    fun theQuotaSentenceMatchesTheDesktop() {
        assertEquals(
            "12.3 GiB remaining of 50.0 GiB",
            DashboardFormat.quotaLabel(Quota(limit = 50L * GIB, remaining = 13_207_024_436L, reset = null)),
        )
    }

    @Test
    fun aHalfKnownQuotaSaysOnlyWhatItKnows() {
        assertEquals(
            "5.0 GiB remaining",
            DashboardFormat.quotaLabel(Quota(limit = null, remaining = 5L * GIB, reset = null)),
        )
        assertEquals(
            "50.0 GiB",
            DashboardFormat.quotaLabel(Quota(limit = 50L * GIB, remaining = null, reset = null)),
        )
        assertNull("no quota is not zero remaining", DashboardFormat.quotaLabel(null))
        assertNull(DashboardFormat.quotaLabel(Quota(limit = null, remaining = null, reset = 1L)))
    }

    @Test
    fun anExhaustedQuotaIsARealZero() {
        assertEquals(
            "0 B remaining of 50.0 GiB",
            DashboardFormat.quotaLabel(Quota(limit = 50L * GIB, remaining = 0L, reset = null)),
        )
    }

    @Test
    fun theQuotaBarNeedsBothSides() {
        assertEquals(
            0.5f,
            DashboardFormat.quotaFraction(Quota(limit = 100L, remaining = 50L, reset = null))!!,
            0.0001f,
        )
        assertEquals(
            "an over-reported remainder is clamped",
            1f,
            DashboardFormat.quotaFraction(Quota(limit = 100L, remaining = 150L, reset = null))!!,
            0.0001f,
        )
        assertEquals(
            0f,
            DashboardFormat.quotaFraction(Quota(limit = 100L, remaining = 0L, reset = null))!!,
            0.0001f,
        )
        assertNull(DashboardFormat.quotaFraction(Quota(limit = null, remaining = 1L, reset = null)))
        assertNull(DashboardFormat.quotaFraction(Quota(limit = 1L, remaining = null, reset = null)))
        assertNull("a zero limit cannot be a fraction", DashboardFormat.quotaFraction(Quota(limit = 0L, remaining = 0L, reset = null)))
        assertNull(DashboardFormat.quotaFraction(null))
    }

    // ---- rates and totals ------------------------------------------------

    @Test
    fun ratesReadUpThenDown() {
        assertEquals(
            "up 1.2 MiB/s / down 3.4 MiB/s",
            DashboardFormat.ratesLabel(
                ByteRates(up = 1.2 * MIB, down = 3.4 * MIB),
            ),
        )
    }

    @Test
    fun noRatesOrNoTrafficHidesTheLine() {
        assertNull(DashboardFormat.ratesLabel(null))
        assertNull(DashboardFormat.ratesLabel(ByteRates(up = 0.0, down = 0.0)))
    }

    @Test
    fun totalsReadUpThenDown() {
        assertEquals(
            "up 12.3 MiB / down 45.6 MiB",
            DashboardFormat.totalsLabel(ByteCounters(up = 12_897_485L, down = 47_817_523L)),
        )
        assertNull(DashboardFormat.totalsLabel(null))
    }

    @Test
    fun zeroTotalsAreRealAndStayVisible() {
        assertEquals("up 0 B / down 0 B", DashboardFormat.totalsLabel(ByteCounters(up = 0, down = 0)))
    }

    // ---- uptime ----------------------------------------------------------

    @Test
    fun uptimeUsesTheDesktopShape() {
        assertNull("no start time, no uptime", DashboardFormat.uptimeLabel(since = 0L, nowSeconds = 100L))
        assertEquals(
            "a future start reads as zero, never negative",
            "0s",
            DashboardFormat.uptimeLabel(since = 200L, nowSeconds = 100L),
        )
        assertEquals("42s", DashboardFormat.uptimeLabel(since = 100L, nowSeconds = 142L))
        assertEquals("1m 0s", DashboardFormat.uptimeLabel(since = 100L, nowSeconds = 160L))
        assertEquals("3m 20s", DashboardFormat.uptimeLabel(since = 100L, nowSeconds = 300L))
        assertEquals("1h 0m", DashboardFormat.uptimeLabel(since = 100L, nowSeconds = 3700L))
        assertEquals("2h 5m", DashboardFormat.uptimeLabel(since = 100L, nowSeconds = 7600L))
    }

    // ---- dates and hosts -------------------------------------------------

    @Test
    fun aDateIsLocalAndL10nAware() {
        val noon = java.time.Instant.parse("2026-11-12T12:00:00Z").epochSecond
        assertEquals(
            "12 Nov 2026",
            DashboardFormat.dateLabel(noon, ZoneId.of("UTC"), Locale.UK),
        )
        assertEquals(
            "13 Nov 2026",
            DashboardFormat.dateLabel(
                java.time.Instant.parse("2026-11-12T23:30:00Z").epochSecond,
                ZoneId.of("Europe/Berlin"),
                Locale.UK,
            ),
        )
        assertNull("an absent timestamp is not an expiry", DashboardFormat.dateLabel(0L, ZoneId.of("UTC"), Locale.UK))
        assertNull(DashboardFormat.dateLabel(-1L, ZoneId.of("UTC"), Locale.UK))
    }

    @Test
    fun theHostDropsOnlyARealPort() {
        assertEquals("edge.example.net", DashboardFormat.hostOf("edge.example.net:443"))
        assertEquals("edge.example.net", DashboardFormat.hostOf("edge.example.net"))
        assertEquals("edge.example.net", DashboardFormat.hostOf("  edge.example.net:80  "))
        assertNull(DashboardFormat.hostOf(null))
        assertNull(DashboardFormat.hostOf("   "))
        assertEquals(
            "a bare IPv6 address is not a host:port pair",
            "2001:db8::1",
            DashboardFormat.hostOf("2001:db8::1"),
        )
        assertEquals(
            "an IPv6 literal with a port keeps its last colon",
            "2001:db8::1:443",
            DashboardFormat.hostOf("2001:db8::1:443"),
        )
        assertEquals("host:notaport", DashboardFormat.hostOf("host:notaport"))
    }

    @Test
    fun ipv6BlackholedIsRecognisedWithoutGuessing() {
        assertEquals(Ipv6Display.Blackholed, ipv6Display("blackholed"))
        assertEquals(Ipv6Display.Blackholed, ipv6Display(" BLACKHOLED "))
        assertEquals(Ipv6Display.Missing, ipv6Display(null))
        assertEquals(Ipv6Display.Missing, ipv6Display("   "))
        assertEquals(Ipv6Display.Value("2001:db8::1"), ipv6Display(" 2001:db8::1 "))
    }

    @Test
    fun theDashIsOneCharacterAndNotANumber() {
        assertEquals("—", DashboardFormat.DASH)
        assertEquals(1, DashboardFormat.DASH.length)
    }

    private companion object {
        const val GIB = 1024L * 1024L * 1024L
        const val MIB = 1024.0 * 1024.0
    }
}
