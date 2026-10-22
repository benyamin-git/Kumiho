package dev.benyamin.kumiho.ui.locations

import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.Protocol
import dev.benyamin.kumiho.engine.Server
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The live filter on the JVM: country name/code, city name and server hostname,
 * trimmed and case-insensitive, with the parent path kept for context. The
 * filter never touches the network and never edits the loaded tree.
 */
class LocationFilterTest {

    @Test
    fun `a blank query returns the same tree`() {
        assertSame(COUNTRIES, LocationFilter.apply(COUNTRIES, "   "))
        assertSame(COUNTRIES, LocationFilter.apply(COUNTRIES, ""))
    }

    @Test
    fun `a country name matches case-insensitively and keeps every child`() {
        val result = LocationFilter.apply(COUNTRIES, "  united STATES ")

        assertEquals(listOf("us"), result.map { it.code })
        assertEquals(listOf("nyc", "sfo"), result.single().cities.map { it.code })
        assertEquals(
            listOf("edge.example.net", "share.example.net", "west.example.net"),
            result.single().cities.flatMap { city -> city.servers.map { it.hostname } },
        )
    }

    @Test
    fun `a country code matches`() {
        val result = LocationFilter.apply(COUNTRIES, "us")

        assertEquals(listOf("us"), result.map { it.code })
        assertEquals(listOf("nyc", "sfo"), result.single().cities.map { it.code })
    }

    @Test
    fun `a city name keeps that city and all of its servers`() {
        val result = LocationFilter.apply(COUNTRIES, "new y")

        assertEquals(listOf("us"), result.map { it.code })
        assertEquals(listOf("nyc"), result.single().cities.map { it.code })
        assertEquals(2, result.single().cities.single().servers.size)
    }

    @Test
    fun `a server hostname keeps the server with its parent path`() {
        val result = LocationFilter.apply(COUNTRIES, "west.example")

        assertEquals(listOf("us"), result.map { it.code })
        assertEquals(listOf("sfo"), result.single().cities.map { it.code })
        assertEquals(
            listOf("west.example.net"),
            result.single().cities.single().servers.map { it.hostname },
        )
    }

    @Test
    fun `a city code is not a search term`() {
        // "nyc" is the city code; the city is named "New York". The filter
        // matches what the row shows, not the wire code.
        assertEquals(emptyList<Country>(), LocationFilter.apply(COUNTRIES, "nyc"))
    }

    @Test
    fun `a query with no match is an empty list`() {
        assertEquals(emptyList<Country>(), LocationFilter.apply(COUNTRIES, "atlantis"))
    }

    @Test
    fun `the loaded tree is never mutated`() {
        val before = COUNTRIES.toString()
        LocationFilter.apply(COUNTRIES, "edge")

        assertEquals(before, COUNTRIES.toString())
        assertEquals(3, COUNTRIES.size)
        assertEquals(2, COUNTRIES.first().cities.size)
    }

    @Test
    fun `REC stays first after a filter that keeps several countries`() {
        val filtered = LocationTree.ordered(LocationFilter.apply(COUNTRIES, "e"))

        assertTrue("REC must sort first", LocationTree.isRecommended(filtered.first()))
        assertEquals("rec", filtered.first().code)
    }

    @Test
    fun `a match that keeps a country keeps its empty cities too`() {
        val withEmptyCity = Country(
            code = "fr",
            name = "France",
            cities = listOf(
                City(code = "par", name = "Paris", servers = emptyList()),
                City(code = "mrs", name = "Marseille", servers = EMPTY_SERVERS),
            ),
        )

        assertEquals(
            listOf("par", "mrs"),
            LocationFilter.apply(listOf(withEmptyCity), "france").single().cities.map { it.code },
        )
    }

    private companion object {
        val EMPTY_SERVERS = emptyList<Server>()

        fun server(host: String, port: Int = 443, quarantined: Boolean = false) = Server(
            hostname = host,
            port = port,
            quarantined = quarantined,
            protocols = listOf(Protocol(name = "connect", host = host, port = port, scheme = "https")),
        )

        val REC = Country(
            code = "rec",
            name = "Recommended Location",
            cities = listOf(
                City(code = "rec", name = "Recommended", servers = listOf(server("auto.example.net"))),
            ),
        )

        val US = Country(
            code = "us",
            name = "United States",
            cities = listOf(
                City(
                    code = "nyc",
                    name = "New York",
                    servers = listOf(server("edge.example.net", 2499), server("share.example.net")),
                ),
                City(code = "sfo", name = "San Francisco", servers = listOf(server("west.example.net"))),
            ),
        )

        val DE = Country(
            code = "de",
            name = "Germany",
            cities = listOf(City(code = "ber", name = "Berlin", servers = listOf(server("ber.example.net")))),
        )

        /** Deliberately not in REC-first order, so ordering is the tree's job. */
        val COUNTRIES = listOf(US, REC, DE)
    }
}
