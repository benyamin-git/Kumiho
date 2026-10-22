package dev.benyamin.kumiho.ui.locations

import java.util.Locale

/** The ISO 3166-1 alpha-2 country flag mapping shared by every Android surface. */
object CountryFlags {
    /** The ISO 3166-1 alpha-2 flag emoji, or "" for REC, blank or a non-ASCII code. */
    fun emoji(code: String?): String {
        val normalized = code.orEmpty().trim().uppercase(Locale.ROOT)
        if (normalized == "REC") return ""
        if (normalized.length != 2 || normalized.any { it !in 'A'..'Z' }) return ""
        val base = 0x1F1E6
        return String(Character.toChars(base + (normalized[0] - 'A'))) +
            String(Character.toChars(base + (normalized[1] - 'A')))
    }
}
