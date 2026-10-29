package dev.benyamin.kumiho.ui.locations

import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.Server
import dev.benyamin.kumiho.ui.dashboard.DashboardFormat

/**
 * The pure flat-list rules: one usable row per server, the country and speed
 * orders, the row text, the all-servers ping targets and the current
 * selection. No Android, Compose or engine call belongs here.
 *
 * `serverlist.Country` still arrives as a country/city tree, but the screen is
 * flat: [rows] omits quarantined and targetless servers, and no row has an
 * expansion state.
 */
object LocationTree {
    /** The REC pseudo-country, matched case-insensitively. */
    const val RECOMMENDED_CODE = "rec"

    /** The visible separator for compound display strings and summaries. */
    const val SEPARATOR = " · "

    /** True for the engine's REC record, whose display name is always "Recommended". */
    fun isRecommended(country: Country): Boolean =
        country.code.equals(RECOMMENDED_CODE, ignoreCase = true)

    /** A server the list may show, select or ping. */
    fun isUsable(server: Server): Boolean =
        !server.quarantined &&
            server.hostname.isNotBlank() &&
            server.port in 1..65_535

    /** The address the engine persists and pings: `hostname:port`. */
    fun addressOf(server: Server): String = "${server.hostname}:${server.port}"

    /**
     * The latency map key for one address. Host names compare case-insensitively
     * so a result always lands on the row that asked for it.
     */
    fun addressKey(address: String): String = address.trim().lowercase()

    /** The country's display name; REC is never a country name. */
    fun displayCountryName(country: Country): String =
        if (isRecommended(country)) {
            "Recommended"
        } else {
            country.name.trim().ifBlank { country.code.trim().uppercase() }
        }

    /** A distinct, non-empty protocol name list in engine order. */
    fun protocolNames(server: Server): List<String> =
        server.protocols
            .map { it.name.trim() }
            .filter { it.isNotEmpty() }
            .distinct()

    /**
     * The visible server rows in the requested order.
     *
     * A non-blank [query] filters the flat rows ([LocationFilter]) without
     * changing the order. [latency] is the map shown in the latency cells and is
     * used by speed order; during an in-flight run the caller passes the frozen
     * map so rows never jump under the user.
     */
    fun rows(
        countries: List<Country>,
        query: String = "",
        latency: Map<String, Double?> = emptyMap(),
        sortMode: LocationSortMode = LocationSortMode.COUNTRY,
    ): List<LocationRow> {
        val flattened = ArrayList<LocationRow>()
        for (country in countries) {
            for (city in country.cities) {
                for (server in city.servers) {
                    if (isUsable(server)) {
                        flattened += LocationRow(country, city, server)
                    }
                }
            }
        }
        val filtered = LocationFilter.apply(flattened, query)
        return filtered.withIndex()
            .sortedWith(
                compareBy<IndexedValue<LocationRow>>(
                    { if (isRecommended(it.value.country)) 0 else 1 },
                    {
                        if (sortMode == LocationSortMode.SPEED) {
                            if (measuredRtt(latency, it.value.server) == null) 1 else 0
                        } else {
                            0
                        }
                    },
                    {
                        if (sortMode == LocationSortMode.SPEED) {
                            measuredRtt(latency, it.value.server) ?: 0.0
                        } else {
                            0.0
                        }
                    },
                    { it.value.countryName.lowercase() },
                    { it.value.cityName.orEmpty().lowercase() },
                    { it.value.server.hostname.lowercase() },
                    { it.index },
                ),
            )
            .map { it.value }
    }

    /**
     * The whole loaded tree's ping targets: every usable server, deduped by
     * lowercased host:port. The first spelling wins, and the result does not
     * depend on the filter or the order.
     */
    fun pingAllTargets(countries: List<Country>): List<String> {
        val targets = ArrayList<String>()
        val seen = HashSet<String>()
        for (country in countries) {
            for (city in country.cities) {
                for (server in city.servers) {
                    if (!isUsable(server)) continue
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
     * A quarantined current server stays resolvable for the summary even when
     * its row is hidden as required by the flat list.
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

    /** A finite measured RTT used for speed ordering, or null. */
    private fun measuredRtt(latency: Map<String, Double?>, server: Server): Double? {
        val rtt = latency[addressKey(addressOf(server))] ?: return null
        if (rtt.isNaN() || rtt.isInfinite() || rtt < 0.0) return null
        return rtt
    }
}

/** Which order the flat rows use for this visit. */
enum class LocationSortMode {
    COUNTRY,
    SPEED,
}

/**
 * One visible server row. The fields are enough for the row, the filter, the
 * summary and a stable LazyColumn key; there is no parent or expansion state.
 */
data class LocationRow(
    val country: Country,
    val city: City,
    val server: Server,
) {
    val countryCode: String get() = country.code.trim().lowercase()

    /** Full country name, or "Recommended"; never a country code unless unknown. */
    val countryName: String get() = LocationTree.displayCountryName(country)

    /** The full city name unless it is blank or repeats the country name. */
    val cityName: String?
        get() = city.name.trim()
            .takeIf { it.isNotEmpty() && !it.equals(countryName, ignoreCase = true) }

    /** `Country · City`, or just the country when the city adds nothing. */
    val title: String
        get() = listOfNotNull(countryName, cityName).joinToString(LocationTree.SEPARATOR)

    val address: String get() = LocationTree.addressOf(server)

    /** Distinct protocol names in engine order. */
    val protocolNames: List<String> get() = LocationTree.protocolNames(server)

    /** `hostname:port · protocol, protocol`; the detail tail is omitted when empty. */
    val detail: String
        get() = if (protocolNames.isEmpty()) {
            address
        } else {
            address + LocationTree.SEPARATOR + protocolNames.joinToString(", ")
        }

    /** Stable identity across refreshes for the lazy list. */
    val key: String
        get() = countryCode + "/" + city.code.trim().lowercase() + "/" +
            LocationTree.addressKey(address)

    /** Everything the flat search may match, normalized once. */
    val searchHaystack: String
        get() = buildList {
            add(country.name)
            add(countryName)
            add(country.code)
            add(city.name)
            add(city.code)
            add(server.hostname)
            add(address)
            addAll(protocolNames)
        }.joinToString(" ").lowercase()
}

/**
 * The engine's reported selection, with the tree objects when they resolve.
 * The summary uses [serverAddress] and [protocolNames] so a graphical client
 * can render `Country · City · host:port · protocols`.
 */
data class LocationSelection(
    val countryCode: String,
    val address: String?,
    val country: Country?,
    val city: City?,
    val server: Server?,
) {
    /** The REC pseudo-country is presented as "Recommended". */
    val recommended: Boolean
        get() = countryCode.equals(LocationTree.RECOMMENDED_CODE, ignoreCase = true)

    /** The country's display name, or the raw code while the tree does not know it. */
    val countryName: String?
        get() = country?.let(LocationTree::displayCountryName)
            ?: countryCode.takeIf { it.isNotBlank() }

    /** The full city name unless it is blank or repeats the country name. */
    val cityName: String?
        get() = city?.name?.trim()
            ?.takeIf { it.isNotEmpty() && !it.equals(countryName, ignoreCase = true) }

    /** The canonical `host:port`, or the engine's raw address when unresolved. */
    val serverAddress: String?
        get() = server?.let(LocationTree::addressOf)
            ?: address?.takeIf { it.isNotBlank() }

    /** The matched server's distinct protocol names; empty for a raw address. */
    val protocolNames: List<String>
        get() = server?.let(LocationTree::protocolNames).orEmpty()

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
    const val PING_ALL = "locations_ping_all"
    const val SUMMARY = "locations_summary"
    const val NOTICE = "locations_notice"
    const val NOTICE_ACTION = "locations_notice_action"
    const val NOTICE_DISMISS = "locations_notice_dismiss"
    const val LIST = "locations_list"
    const val LOADING = "locations_loading"
    const val ERROR = "locations_error"
    const val EMPTY = "locations_empty"
    const val NO_MATCH = "locations_no_match"
    const val NO_MATCH_CLEAR = "locations_no_match_clear"

    const val SERVER_PREFIX = "locations_server_"
}

/** The tag for one server row; the address keeps its punctuation. */
fun serverRowTag(countryCode: String, cityCode: String, address: String): String =
    LocationsTags.SERVER_PREFIX + countryCode.lowercase() + "_" + cityCode.lowercase() + "_" + address
