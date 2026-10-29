package dev.benyamin.kumiho.ui.locations

/**
 * The live flat-row filter. Matching is trimmed and case-insensitive across
 * country name/code, city name/code, hostname, `host:port` and protocol names.
 * The filter never touches the network and never changes the row order.
 */
object LocationFilter {
    fun apply(rows: List<LocationRow>, query: String): List<LocationRow> {
        if (query.trim().isEmpty()) return rows
        return rows.filter { matches(it, query) }
    }

    fun matches(row: LocationRow, query: String): Boolean {
        val needle = query.trim().lowercase()
        return needle.isEmpty() || row.searchHaystack.contains(needle)
    }
}
