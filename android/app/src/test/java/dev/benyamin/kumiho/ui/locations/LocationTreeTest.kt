package dev.benyamin.kumiho.ui.locations

import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.Protocol
import dev.benyamin.kumiho.engine.Server
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The pure tree rules: REC first, the visible rows for one expansion state,
 * the ping targets those rows describe, and the current selection resolved from
 * the engine's `location` + `server`. No Android, no engine.
 */
class LocationTreeTest {

    // ---- ordering --------------------------------------------------------

    @Test
    fun `REC is first and every other country keeps the engine order`() {
        val ordered = LocationTree.ordered(listOf(US, REC, DE))

        assertEquals(listOf("rec", "us", "de"), ordered.map { it.code })
    }

    @Test
    fun `a tree without REC is returned as it is`() {
        val countries = listOf(US, DE)

        assertSame(countries, LocationTree.ordered(countries))
    }

    @Test
    fun `ordering never mutates the loaded tree`() {
        val countries = listOf(US, REC, DE)

        LocationTree.ordered(countries)

        assertEquals(listOf("us", "rec", "de"), countries.map { it.code })
    }

    @Test
    fun `REC matches the engine's uppercase constant too`() {
        assertTrue(LocationTree.isRecommended(REC))
        assertTrue(LocationTree.isRecommended(REC.copy(code = "REC")))
        assertFalse(LocationTree.isRecommended(US))
    }

    // ---- rows ------------------------------------------------------------

    @Test
    fun `a collapsed tree shows only its countries`() {
        val rows = LocationTree.rows(ORDERED)

        assertEquals(3, rows.size)
        assertTrue(rows.all { it is LocationRow.CountryRow })
        assertEquals(listOf("rec", "us", "de"), rows.map { (it as LocationRow.CountryRow).country.code })
    }

    @Test
    fun `a country expands to its cities but not yet to its servers`() {
        val rows = LocationTree.rows(ORDERED, expandedCountries = setOf("us"))

        assertEquals(
            listOf("country:rec", "country:us", "city:us/nyc", "city:us/sfo", "country:de"),
            rows.map { it.key },
        )
    }

    @Test
    fun `a city expands to its servers`() {
        val rows = LocationTree.rows(
            ORDERED,
            expandedCountries = setOf("us"),
            expandedCities = setOf("us/nyc"),
        )

        val servers = rows.filterIsInstance<LocationRow.ServerRow>()
        assertEquals(listOf("edge.example.net:2499", "share.example.net:443"), servers.map { LocationTree.addressOf(it.server) })
        assertEquals("us", servers.first().country.code)
        assertEquals("nyc", servers.first().city.code)
    }

    @Test
    fun `row keys are stable and unique`() {
        val rows = LocationTree.rows(
            ORDERED,
            expandedCountries = setOf("rec", "us", "de"),
            expandedCities = setOf("rec/rec", "us/nyc", "us/sfo", "de/ber"),
        )

        assertEquals(rows.size, rows.map { it.key }.toSet().size)
        assertEquals(rows.map { it.key }, LocationTree.rows(
            ORDERED,
            expandedCountries = setOf("rec", "us", "de"),
            expandedCities = setOf("rec/rec", "us/nyc", "us/sfo", "de/ber"),
        ).map { it.key })
    }

    @Test
    fun `a query opens the matching branches without touching the expansion`() {
        val rows = LocationTree.rows(ORDERED, query = "edge")

        assertEquals(
            listOf("country:us", "city:us/nyc", "server:us/nyc/edge.example.net:2499"),
            rows.map { it.key },
        )
        // The user's expansion is untouched: clearing the query returns to none.
        assertEquals(3, LocationTree.rows(ORDERED, query = "").size)
    }

    // ---- ping targets ----------------------------------------------------

    @Test
    fun `ping targets are the visible servers deduped by address`() {
        val rows = LocationTree.rows(
            ORDERED,
            expandedCountries = setOf("us"),
            expandedCities = setOf("us/nyc", "us/sfo"),
        )

        // share.example.net:443 appears under both cities; it is probed once.
        assertEquals(
            listOf("edge.example.net:2499", "share.example.net:443"),
            LocationTree.pingTargets(rows),
        )
    }

    @Test
    fun `ping targets skip quarantined servers and broken addresses`() {
        val country = Country(
            code = "us",
            name = "United States",
            cities = listOf(
                City(
                    code = "nyc",
                    name = "New York",
                    servers = listOf(
                        server("edge.example.net", 2499),
                        server("quarantined.example.net", quarantined = true),
                        server("", 443),
                        server("noport.example.net", 0),
                    ),
                ),
            ),
        )

        val rows = LocationTree.rows(
            listOf(country),
            expandedCountries = setOf("us"),
            expandedCities = setOf("us/nyc"),
        )

        assertEquals(listOf("edge.example.net:2499"), LocationTree.pingTargets(rows))
    }

    @Test
    fun `a quarantined server is still visible for context`() {
        val rows = LocationTree.rows(
            ORDERED,
            expandedCountries = setOf("de"),
            expandedCities = setOf("de/ber"),
        )

        assertTrue(LocationTree.visibleServers(rows).any { it.quarantined })
        assertFalse(LocationTree.pingTargets(rows).any { it.contains("quarantined") })
    }

    // ---- current selection ----------------------------------------------

    @Test
    fun `the selection resolves the country from location and the city from the server`() {
        val selection = LocationTree.currentSelection(ORDERED, location = "us", server = "edge.example.net:2499")

        requireNotNull(selection)
        assertEquals("us", selection.country?.code)
        assertEquals("United States", selection.countryName)
        assertEquals("New York", selection.cityName)
        assertEquals("edge.example.net", selection.serverName)
        assertFalse(selection.recommended)
    }

    @Test
    fun `no location and no server is no selection`() {
        assertNull(LocationTree.currentSelection(ORDERED, location = null, server = null))
        assertNull(LocationTree.currentSelection(ORDERED, location = " ", server = ""))
    }

    @Test
    fun `the comparison is case-insensitive on both sides`() {
        val selection = LocationTree.currentSelection(
            ORDERED,
            location = "US",
            server = "EDGE.Example.NET:2499",
        )

        requireNotNull(selection)
        assertEquals("New York", selection.cityName)
        assertTrue(selection.marks(US, US.cities.first(), US.cities.first().servers.first()))
    }

    @Test
    fun `a recommended selection is recognised`() {
        val selection = LocationTree.currentSelection(ORDERED, location = "REC", server = null)

        requireNotNull(selection)
        assertTrue(selection.recommended)
        assertEquals("Recommended", selection.serverName ?: "Recommended")
    }

    @Test
    fun `a server the tree no longer lists stays an honest raw address`() {
        val selection = LocationTree.currentSelection(
            ORDERED,
            location = "us",
            server = "gone.example.net:443",
        )

        requireNotNull(selection)
        assertEquals("us", selection.country?.code)
        assertNull(selection.city)
        assertEquals("gone.example.net", selection.serverName)
        assertFalse(
            "an unknown host must not mark any row",
            selection.marks(US, US.cities.first(), US.cities.first().servers.first()),
        )
    }

    @Test
    fun `a selection marks only its own server row`() {
        val selection = LocationTree.currentSelection(ORDERED, location = "us", server = "edge.example.net:2499")
        requireNotNull(selection)
        val nyc = US.cities.first()

        assertTrue(selection.marks(US, nyc, nyc.servers.first()))
        assertFalse(selection.marks(US, nyc, nyc.servers[1]))
        assertFalse(selection.marks(DE, DE.cities.first(), DE.cities.first().servers.first()))
    }

    @Test
    fun `a port difference is not a match`() {
        val selection = LocationTree.currentSelection(ORDERED, location = "us", server = "edge.example.net:443")
        requireNotNull(selection)

        assertNull(selection.server)
        assertFalse(selection.marks(US, US.cities.first(), US.cities.first().servers.first()))
    }

    // ---- latency ---------------------------------------------------------

    @Test
    fun `latency separates untested from a timeout and a measurement`() {
        val edge = US.cities.first().servers.first()
        val key = LocationTree.addressKey(LocationTree.addressOf(edge))

        assertEquals(LatencyState.Untested, LocationTree.latencyOf(emptyMap(), edge))
        assertEquals(LatencyState.Timeout, LocationTree.latencyOf(mapOf(key to null), edge))
        assertEquals(LatencyState.Measured(42), LocationTree.latencyOf(mapOf(key to 42.4), edge))
    }

    @Test
    fun `a latency key is case-insensitive and trimmed`() {
        val edge = server("EDGE.Example.NET", 2499)

        assertEquals(
            LatencyState.Measured(9),
            LocationTree.latencyOf(
                mapOf(LocationTree.addressKey(" edge.example.net:2499 ") to 9.0),
                edge,
            ),
        )
    }

    private companion object {
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
                    servers = listOf(
                        server("edge.example.net", 2499),
                        server("share.example.net"),
                    ),
                ),
                City(code = "sfo", name = "San Francisco", servers = listOf(server("share.example.net"))),
            ),
        )

        val DE = Country(
            code = "de",
            name = "Germany",
            cities = listOf(
                City(
                    code = "ber",
                    name = "Berlin",
                    servers = listOf(server("ber.example.net"), server("quarantined.example.net", quarantined = true)),
                ),
            ),
        )

        val ORDERED = LocationTree.ordered(listOf(US, REC, DE))
    }
}
