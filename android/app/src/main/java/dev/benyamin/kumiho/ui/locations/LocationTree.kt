package dev.benyamin.kumiho.ui.locations

import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.Server
import dev.benyamin.kumiho.ui.dashboard.DashboardFormat

/**
 * The pure location-tree rules: REC first, flatten to visible rows for one
 * expansion state, the ping targets the visible server rows describe, and the
 * current selection resolved from the engine status.
 *
 * Nothing here touches Android, Compose or the engine: the view model owns the
 * state, this file owns the semantics, and a JVM test pins both. The tree comes
 * from `serverlist.Country` through `EngineClient.locations`; REC is a normal
 * record whose code is `rec` (the engine writes `REC` in its own constants, so
 * every comparison here is case-insensitive).
 */
object LocationTree {
    /** The REC pseudo-country, matched case-insensitively. */
    const val RECOMMENDED_CODE = "rec"

    /** True for the engine's REC record, whose display name is always "Recommended". */
    fun isRecommended(country: Country): Boolean =
        country.code.equals(RECOMMENDED_CODE, ignoreCase = true)

    /**
     * REC first, every other country in the engine's order (it sorts by name).
     * The input list is never touched.
     */
    fun ordered(countries: List<Country>): List<Country> {
        val recommended = countries.filter(::isRecommended)
        if (recommended.isEmpty()) return countries
        return recommended + countries.filterNot(::isRecommended)
    }

    /** The address the engine persists and pings: `hostname:port`. */
    fun addressOf(server: Server): String = "${server.hostname}:${server.port}"

    /**
     * The latency map key for one address. Host names compare case-insensitively
     * so a result always lands on the row that asked for it.
     */
    fun addressKey(address: String): String = address.trim().lowercase()

    /** The expansion-set key for a country (normalized, so toggling is stable). */
    fun countryKey(country: Country): String = country.code.trim().lowercase()

    /** The expansion-set key for a city inside a country. */
    fun cityKey(country: Country, city: City): String =
        countryKey(country) + "/" + city.code.trim().lowercase()

    /**
     * The visible rows for one expansion state.
     *
     * A non-blank [query] filters first ([LocationFilter]) and then opens every
     * matching branch, so the results are visible without the user expanding
     * anything; the stored expansion sets are untouched, so clearing the query
     * restores exactly what was open before. The country list is speed-ordered
     * by [latency] ([orderedCountries]); the rows inside a country keep the
     * engine order.
     */
    fun rows(
        countries: List<Country>,
        query: String = "",
        expandedCountries: Set<String> = emptySet(),
        expandedCities: Set<String> = emptySet(),
        latency: Map<String, Double?> = emptyMap(),
    ): List<LocationRow> {
        val searching = query.isNotBlank()
        val filtered = LocationFilter.apply(countries, query)
        val rows = ArrayList<LocationRow>()
        for (country in orderedCountries(filtered, latency)) {
            val countryOpen = searching || countryKey(country) in expandedCountries
            rows += LocationRow.CountryRow(country = country, expanded = countryOpen)
            if (!countryOpen) continue
            for (city in country.cities) {
                val cityOpen = searching || cityKey(country, city) in expandedCities
                rows += LocationRow.CityRow(country = country, city = city, expanded = cityOpen)
                if (!cityOpen) continue
                for (server in city.servers) {
                    rows += LocationRow.ServerRow(country = country, city = city, server = server)
                }
            }
        }
        return rows
    }

    /** Every server row in view order. */
    fun visibleServers(rows: List<LocationRow>): List<Server> =
        rows.mapNotNull { (it as? LocationRow.ServerRow)?.server }

    /**
     * The `host:port` targets a manual Test speed sends: the visible server
     * rows, in order, deduped by address, without quarantined servers (the
     * engine's candidate resolver skips them) or entries without a usable
     * address. The first spelling of an address wins.
     */
    fun pingTargets(rows: List<LocationRow>): List<String> {
        val targets = ArrayList<String>()
        val seen = HashSet<String>()
        for (row in rows) {
            val server = (row as? LocationRow.ServerRow)?.server ?: continue
            if (server.quarantined) continue
            if (server.hostname.isBlank() || server.port <= 0) continue
            val address = addressOf(server)
            if (seen.add(addressKey(address))) targets += address
        }
        return targets
    }

    /**
     * The whole loaded tree's ping targets: every country/city/server, skipping
     * quarantined and unusable entries, deduped by lowercased host:port. The
     * first spelling wins, and the result does not depend on the filter or the
     * stored expansion.
     */
    fun pingAllTargets(countries: List<Country>): List<String> {
        val targets = ArrayList<String>()
        val seen = HashSet<String>()
        for (country in countries) {
            for (city in country.cities) {
                for (server in city.servers) {
                    if (server.quarantined) continue
                    if (server.hostname.isBlank() || server.port <= 0) continue
                    val address = addressOf(server)
                    if (seen.add(addressKey(address))) targets += address
                }
            }
        }
        return targets
    }

    /**
     * The selection the engine reports, resolved against the loaded tree.
     *
     * `status.location` is the persisted country code and `status.server` is the
     * persisted `host:port`; the city is whatever city in the tree owns that
     * server. Either side may be absent (nothing selected yet), and a host the
     * tree no longer lists stays an honest raw address instead of a fake match.
     */
    fun currentSelection(
        countries: List<Country>,
        location: String?,
        server: String?,
    ): LocationSelection? {
        val code = location?.trim().orEmpty()
        val address = server?.trim().orEmpty()
        if (code.isEmpty() && address.isEmpty()) return null

        val reportedCountry = countries.firstOrNull { it.code.equals(code, ignoreCase = true) }
        val host = DashboardFormat.hostOf(address)
        val port = portOf(address)

        var matchedCountry: Country? = null
        var matchedCity: City? = null
        var matchedServer: Server? = null
        if (host != null) {
            val branches = listOfNotNull(reportedCountry) + countries.filterNot { it === reportedCountry }
            search@ for (country in branches) {
                for (city in country.cities) {
                    for (entry in city.servers) {
                        if (!entry.hostname.equals(host, ignoreCase = true)) continue
                        if (port != null && entry.port != port) continue
                        matchedCountry = country
                        matchedCity = city
                        matchedServer = entry
                        break@search
                    }
                }
            }
        }

        return LocationSelection(
            countryCode = code.ifEmpty { matchedCountry?.code ?: "" },
            address = address.ifEmpty { null },
            country = matchedCountry ?: reportedCountry,
            city = matchedCity,
            server = matchedServer,
        )
    }

    /** The port part of an engine `host:port` string, or null. */
    fun portOf(address: String?): Int? {
        val value = address?.trim().orEmpty()
        val colon = value.lastIndexOf(':')
        if (colon <= 0 || colon == value.length - 1) return null
        if (value.indexOf(':') != colon) return null
        return value.substring(colon + 1).toIntOrNull()
    }

    /**
     * A server's latency as the screen shows it: never tested (em dash), the
     * last measurement, or a timeout. A null RTT is an answer from the engine
     * and is deliberately distinct from having no answer at all.
     */
    fun latencyOf(latency: Map<String, Double?>, server: Server): LatencyState {
        val key = addressKey(addressOf(server))
        if (!latency.containsKey(key)) return LatencyState.Untested
        val rtt = latency[key]
        if (rtt == null || rtt.isNaN() || rtt < 0.0) return LatencyState.Timeout
        return LatencyState.Measured(Math.round(rtt))
    }

    /**
     * The LatencyState a country row shows (D9): the fastest measured RTT in
     * whole milliseconds, a timeout when some usable server answered without a
     * number, or untested when nothing answered yet.
     */
    fun countryLatencyOf(latency: Map<String, Double?>, country: Country): LatencyState {
        val best = bestMeasuredRtt(latency, country)
        if (best != null) return LatencyState.Measured(Math.round(best))
        val answered = country.cities.any { city -> city.servers.any { timedOut(latency, it) } }
        return if (answered) LatencyState.Timeout else LatencyState.Untested
    }

    /**
     * A server's usable measurement: the key is present, the value is a real
     * non-negative number, and the server is not quarantined and has a real
     * address. Absent and timeout both return null.
     */
    private fun measuredRtt(latency: Map<String, Double?>, server: Server): Double? {
        if (!usable(server)) return null
        val rtt = latency[addressKey(addressOf(server))]
        if (rtt == null || rtt.isNaN() || rtt < 0.0) return null
        return rtt
    }

    /** The fastest measured RTT of a country's usable servers, or null. */
    private fun bestMeasuredRtt(latency: Map<String, Double?>, country: Country): Double? =
        country.cities.asSequence()
            .flatMap { it.servers.asSequence() }
            .mapNotNull { measuredRtt(latency, it) }
            .minOrNull()

    /** True when a usable server has a present key that is not a measurement. */
    private fun timedOut(latency: Map<String, Double?>, server: Server): Boolean {
        if (!usable(server)) return false
        val key = addressKey(addressOf(server))
        if (!latency.containsKey(key)) return false
        val rtt = latency[key]
        return rtt == null || rtt.isNaN() || rtt < 0.0
    }

    /** A server the screen may ping or measure at all. */
    private fun usable(server: Server): Boolean =
        !server.quarantined && server.hostname.isNotBlank() && server.port > 0

    /**
     * The engine order, speed-ordered: countries with a measurement first
     * (ascending), Recommended pinned before everything, and unmeasured
     * countries keeping their original order. The input list is never touched.
     */
    private fun orderedCountries(
        countries: List<Country>,
        latency: Map<String, Double?>,
    ): List<Country> = countries.withIndex()
        .sortedWith(
            compareBy<IndexedValue<Country>>(
                { if (isRecommended(it.value)) 0 else 1 },
                { if (bestMeasuredRtt(latency, it.value) != null) 0 else 1 },
                { bestMeasuredRtt(latency, it.value) ?: 0.0 },
                { it.index },
            ),
        )
        .map { it.value }
}

/**
 * One visible row of the tree. [key] is stable across recompositions and tree
 * refreshes, so the lazy list keeps its scroll position and row state.
 */
sealed interface LocationRow {
    val key: String

    data class CountryRow(val country: Country, val expanded: Boolean) : LocationRow {
        override val key: String get() = "country:" + LocationTree.countryKey(country)
    }

    data class CityRow(
        val country: Country,
        val city: City,
        val expanded: Boolean,
    ) : LocationRow {
        override val key: String get() = "city:" + LocationTree.cityKey(country, city)
    }

    data class ServerRow(
        val country: Country,
        val city: City,
        val server: Server,
    ) : LocationRow {
        override val key: String
            get() = "server:" + LocationTree.cityKey(country, city) +
                "/" + LocationTree.addressKey(LocationTree.addressOf(server))
    }
}

/** The engine's reported selection, with the tree objects when they resolve. */
data class LocationSelection(
    val countryCode: String,
    val address: String?,
    val country: Country?,
    val city: City?,
    val server: Server?,
) {
    /** The REC pseudo-country is presented as "Recommended". */
    val recommended: Boolean get() = countryCode.equals(LocationTree.RECOMMENDED_CODE, ignoreCase = true)

    /** The country's flag, or "" for Recommended, blank or an unknown code. */
    val flag: String get() = CountryFlags.emoji(countryCode)

    /** The country's display name, or the raw code while the tree does not know it. */
    val countryName: String?
        get() = country?.name ?: countryCode.takeIf { it.isNotBlank() }

    val cityName: String? get() = city?.name

    /** The server's host name, or the raw address when the tree does not list it. */
    val serverName: String?
        get() = server?.hostname ?: DashboardFormat.hostOf(address)

    /** True when this selection marks that server row as current. */
    fun marks(country: Country, city: City, server: Server): Boolean {
        val selected = this.server ?: return false
        return selected.hostname.equals(server.hostname, ignoreCase = true) &&
            selected.port == server.port &&
            (countryCode.isBlank() || countryCode.equals(country.code, ignoreCase = true))
    }
}

/** What a server row's latency slot shows. */
sealed interface LatencyState {
    /** Never tested: an em dash. */
    data object Untested : LatencyState

    /** Tested and the probe failed: "timeout". */
    data object Timeout : LatencyState

    /** The last measured round trip in whole milliseconds. */
    data class Measured(val milliseconds: Long) : LatencyState
}

/**
 * Stable tags for the Compose tests and the screenshot capture, so a copy
 * change never breaks a test and a test never depends on a visible sentence.
 */
object LocationsTags {
    const val SCREEN = "locations_screen"
    const val SEARCH = "locations_search"
    const val SEARCH_CLEAR = "locations_search_clear"
    const val REFRESH = "locations_refresh"
    const val TEST_SPEED = "locations_test_speed"
    const val PING_ALL = "locations_ping_all"
    const val TOGGLE_ALL = "locations_toggle_all"
    const val TARGETS_HELPER = "locations_targets_helper"
    const val SUMMARY = "locations_summary"
    const val CONNECTED_HINT = "locations_connected_hint"
    const val NOTICE = "locations_notice"
    const val NOTICE_ACTION = "locations_notice_action"
    const val NOTICE_DISMISS = "locations_notice_dismiss"
    const val LIST = "locations_list"
    const val LOADING = "locations_loading"
    const val ERROR = "locations_error"
    const val EMPTY = "locations_empty"
    const val NO_MATCH = "locations_no_match"
    const val NO_MATCH_CLEAR = "locations_no_match_clear"

    const val COUNTRY_PREFIX = "locations_country_"
    const val CITY_PREFIX = "locations_city_"
    const val SERVER_PREFIX = "locations_server_"
}

/** The tag for one country row. */
fun countryRowTag(code: String): String = LocationsTags.COUNTRY_PREFIX + code.lowercase()

/** The tag for one city row. */
fun cityRowTag(countryCode: String, cityCode: String): String =
    LocationsTags.CITY_PREFIX + countryCode.lowercase() + "_" + cityCode.lowercase()

/** The tag for one server row; the address keeps its punctuation. */
fun serverRowTag(countryCode: String, cityCode: String, address: String): String =
    LocationsTags.SERVER_PREFIX + countryCode.lowercase() + "_" + cityCode.lowercase() + "_" + address
