package dev.benyamin.kumiho.ui.locations

import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country

/**
 * The live location filter. It matches, case-insensitively and after trimming
 * surrounding whitespace:
 *
 * - a country name or country code — the whole country with all its cities and
 *   servers;
 * - a city name — that city with all its servers;
 * - a server hostname — that server, with its country and city kept for
 *   context.
 *
 * City codes, protocol names and ports are deliberately not matched: the user
 * searches for places and hosts they can read in the list. The filter is pure:
 * it reads only its arguments, never the network, and it returns new country
 * objects instead of editing the input, so the loaded tree stays intact.
 */
object LocationFilter {
    fun apply(countries: List<Country>, query: String): List<Country> {
        val needle = query.trim().lowercase()
        if (needle.isEmpty()) return countries
        return countries.mapNotNull { country ->
            if (matches(country.name, needle) || matches(country.code, needle)) {
                // A country match keeps every city and server it has.
                return@mapNotNull country
            }
            val cities = country.cities.mapNotNull { city -> matchCity(city, needle) }
            if (cities.isEmpty()) null else country.copy(cities = cities)
        }
    }

    private fun matchCity(city: City, needle: String): City? {
        if (matches(city.name, needle)) return city
        val servers = city.servers.filter { matches(it.hostname, needle) }
        return if (servers.isEmpty()) null else city.copy(servers = servers)
    }

    private fun matches(value: String?, needle: String): Boolean =
        value?.lowercase()?.contains(needle) == true
}
