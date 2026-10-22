package dev.benyamin.kumiho.ui.settings

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * The strict `exclude_cidrs` table. The Go `computeRoutes` stays authoritative
 * at connect; this pins the client pre-check that keeps the engine row from
 * writing an exclusion Go would reject or silently widen.
 */
class CidrValidatorTest {

    private fun valid(raw: String, expected: String) {
        assertEquals("input: $raw", SettingInput.Valid(expected), CidrValidator.validate(raw))
    }

    private fun invalid(
        raw: String,
        error: SettingError,
        detail: String? = null,
    ) {
        assertEquals("input: $raw", SettingInput.Invalid(error, detail = detail), CidrValidator.validate(raw))
    }

    // ---- accepted forms --------------------------------------------------

    @Test
    fun `an empty list is valid and stays empty`() {
        valid("", "")
        valid("   ", "")
        valid(" , , ", "")
    }

    @Test
    fun `canonical CIDRs are preserved`() {
        valid("10.0.0.0/8", "10.0.0.0/8")
        valid("192.168.1.0/24", "192.168.1.0/24")
        valid("10.8.0.2/32", "10.8.0.2/32")
        valid("0.0.0.0/1", "0.0.0.0/1")
        valid("128.0.0.0/1", "128.0.0.0/1")
    }

    @Test
    fun `a bare address becomes a host route`() {
        valid("10.8.0.2", "10.8.0.2/32")
        valid("0.0.0.0", "0.0.0.0/32")
        valid("255.255.255.255", "255.255.255.255/32")
    }

    @Test
    fun `host bits are masked like the Go side`() {
        valid("10.1.2.3/8", "10.0.0.0/8")
        valid("192.168.1.77/24", "192.168.1.0/24")
        valid("203.0.113.255/25", "203.0.113.128/25")
    }

    @Test
    fun `spaces and empty items are dropped and entries are normalized`() {
        valid(
            " 10.0.0.0/8 ,, 192.168.1.5 ",
            "10.0.0.0/8, 192.168.1.5/32",
        )
    }

    @Test
    fun `the DNS host and ranges containing it are allowed`() {
        valid("10.8.0.2/32", "10.8.0.2/32")
        valid("10.8.0.0/24", "10.8.0.0/24")
        valid("10.0.0.0/8", "10.0.0.0/8")
    }

    // ---- whole-internet exclusions --------------------------------------

    @Test
    fun `a zero-bit prefix is rejected`() {
        invalid("0.0.0.0/0", SettingError.CIDR_TOO_BROAD, "0.0.0.0/0")
        invalid("10.0.0.0/0", SettingError.CIDR_TOO_BROAD, "0.0.0.0/0")
        invalid("255.255.255.255/0", SettingError.CIDR_TOO_BROAD, "0.0.0.0/0")
    }

    @Test
    fun `a complementary half-space pair is rejected`() {
        invalid("0.0.0.0/1, 128.0.0.0/1", SettingError.CIDR_TOO_BROAD)
    }

    @Test
    fun `a union that covers the whole space through overlaps is rejected`() {
        invalid("0.0.0.0/1, 100.0.0.0/8, 128.0.0.0/1", SettingError.CIDR_TOO_BROAD)
        invalid("0.0.0.0/2, 64.0.0.0/2, 128.0.0.0/1", SettingError.CIDR_TOO_BROAD)
    }

    @Test
    fun `a near miss stays valid`() {
        valid("0.0.0.0/1, 128.0.0.0/2", "0.0.0.0/1, 128.0.0.0/2")
        valid("0.0.0.0/1, 10.0.0.0/8, 128.0.0.0/2", "0.0.0.0/1, 10.0.0.0/8, 128.0.0.0/2")
    }

    // ---- rejected forms --------------------------------------------------

    @Test
    fun `IPv6 is always rejected`() {
        invalid("fd00::/8", SettingError.IPV6_NOT_SUPPORTED)
        invalid("::/0", SettingError.IPV6_NOT_SUPPORTED)
        invalid("2001:db8::1/128", SettingError.IPV6_NOT_SUPPORTED)
        invalid("10.0.0.0/8, fd00::/8", SettingError.IPV6_NOT_SUPPORTED)
    }

    @Test
    fun `malformed addresses are rejected`() {
        for (raw in listOf(
            "10.0.0",
            "10.0.0.0.0",
            "10..0.0/8",
            "10.0.0.256/24",
            "10.0.0.0/-1",
            "10.0.0.0/33",
            "10.0.0.0/8/9",
            "10.0.0.0/",
            "10.0.0.0/ 8",
            "10.0.0.0/08",
            "010.0.0.0/8",
            "10.0.0.01/32",
            "10.0.0.0/255.0.0.0",
            "10.0.0.0/+8",
            "example.com",
            "10.0.0.0 /8",
            "/8",
        )) {
            assertEquals(
                "input: $raw",
                SettingInput.Invalid(SettingError.INVALID_CIDR, detail = raw.trim()),
                CidrValidator.validate(raw),
            )
        }
    }
}
