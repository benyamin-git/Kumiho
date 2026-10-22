package dev.benyamin.kumiho.ui.settings

/**
 * The strict client-side validator behind the `exclude_cidrs` row.
 *
 * The Go `computeRoutes` (`mobile/routes.go`) remains authoritative at connect.
 * This validator exists so the engine row can never write an exclusion the Go
 * side would reject, and so a whole-IPv4 union (which the Go complement would
 * silently turn into a DNS-only tunnel) is caught before it is saved. It
 * mirrors the Go rules:
 *
 * - a bare IPv4 address becomes `/32`;
 * - host bits are masked (`10.1.2.3/8` -> `10.0.0.0/8`);
 * - IPv6 is rejected, because IPv6 always stays blackholed;
 * - malformed values, leading-zero octets and out-of-range prefixes are
 *   rejected;
 * - `/0` and any union of entries that covers the whole IPv4 space are
 *   rejected;
 * - `10.8.0.2` is accepted: the DNS host stays routed even inside an excluded
 *   range (C1), and the UI says so.
 */
object CidrValidator {

    private const val MAX_IPV4 = 0xFFFFFFFFL

    /** Parses and normalizes a comma-separated list, or explains why not. */
    fun validate(raw: String): SettingInput {
        val parts = raw.split(',').map { it.trim() }.filter { it.isNotEmpty() }
        if (parts.isEmpty()) return SettingInput.Valid("")

        val normalized = ArrayList<String>(parts.size)
        val ranges = ArrayList<LongRange>(parts.size)
        for (part in parts) {
            if (part.contains(':')) {
                return SettingInput.Invalid(SettingError.IPV6_NOT_SUPPORTED)
            }
            val parsed = parse(part)
                ?: return SettingInput.Invalid(SettingError.INVALID_CIDR, detail = part)
            if (parsed.bits == 0) {
                return SettingInput.Invalid(SettingError.CIDR_TOO_BROAD, detail = parsed.text)
            }
            normalized += parsed.text
            ranges += parsed.start..parsed.end
        }
        if (coversWholeIpv4(ranges)) {
            return SettingInput.Invalid(SettingError.CIDR_TOO_BROAD)
        }
        return SettingInput.Valid(normalized.joinToString(", "))
    }

    /**
     * Parses one entry into its canonical text and inclusive range, or null
     * when the entry is not a strict IPv4 CIDR.
     */
    private fun parse(raw: String): ParsedCidr? {
        val slash = raw.indexOf('/')
        if (slash >= 0 && raw.indexOf('/', slash + 1) >= 0) return null
        val host = if (slash >= 0) raw.substring(0, slash) else raw
        if (host.isBlank()) return null
        val bits = if (slash >= 0) {
            val prefix = raw.substring(slash + 1)
            if (prefix.isBlank()) return null
            if (prefix.length > 1 && prefix.startsWith('0')) return null
            if (prefix.any { !it.isDigit() }) return null
            prefix.toIntOrNull() ?: return null
        } else {
            32
        }
        if (bits !in 0..32) return null

        val octets = parseIpv4(host) ?: return null
        val address = (octets[0].toLong() shl 24) or
            (octets[1].toLong() shl 16) or
            (octets[2].toLong() shl 8) or
            octets[3].toLong()
        val mask = if (bits == 0) 0L else (MAX_IPV4 shl (32 - bits)) and MAX_IPV4
        val start = address and mask
        val size = 1L shl (32 - bits)
        val end = start + size - 1
        return ParsedCidr(
            text = "${renderIpv4(start)}/$bits",
            start = start,
            end = end,
            bits = bits,
        )
    }

    /** Strict dotted quad: four decimal octets, no shorthand, no leading zeros. */
    private fun parseIpv4(text: String): List<Int>? {
        val parts = text.split('.')
        if (parts.size != 4) return null
        val octets = ArrayList<Int>(4)
        for (part in parts) {
            if (part.isEmpty() || part.length > 3) return null
            if (part.length > 1 && part.startsWith('0')) return null
            if (part.any { !it.isDigit() }) return null
            val value = part.toIntOrNull() ?: return null
            if (value !in 0..255) return null
            octets += value
        }
        return octets
    }

    /**
     * True when the union of [ranges] is the whole IPv4 space. Adjacent and
     * overlapping ranges are merged first, so `0.0.0.0/1 + 128.0.0.0/1` is
     * caught even though neither entry is `/0`.
     */
    private fun coversWholeIpv4(ranges: List<LongRange>): Boolean {
        if (ranges.isEmpty()) return false
        val sorted = ranges.sortedBy { it.first }
        var start = sorted.first().first
        var end = sorted.first().last
        for (range in sorted.drop(1)) {
            if (range.first <= end + 1) {
                if (range.last > end) end = range.last
            } else {
                start = range.first
                end = range.last
            }
        }
        return start == 0L && end == MAX_IPV4
    }

    private fun renderIpv4(value: Long): String = listOf(
        (value shr 24) and 0xFF,
        (value shr 16) and 0xFF,
        (value shr 8) and 0xFF,
        value and 0xFF,
    ).joinToString(".")

    private data class ParsedCidr(
        val text: String,
        val start: Long,
        val end: Long,
        val bits: Int,
    )
}
