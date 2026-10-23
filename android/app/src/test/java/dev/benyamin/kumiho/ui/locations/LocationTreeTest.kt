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
    fun `a country expands straight to its servers`() {
        val rows = LocationTree.rows(ORDERED, expandedCountries = setOf("us"))

        assertEquals(
            listOf(
                "country:rec",
                "country:us",
                "server:us/nyc/edge.example.net:2499",
                "server:us/nyc/share.example.net:443",
                "server:us/sfo/share.example.net:443",
                "country:de",
            ),
            rows.map { it.key },
        )
    }

    @Test
    fun `server rows keep their city for context`() {
        val servers = LocationTree.rows(ORDERED, expandedCountries = setOf("us"))
            .filterIsInstance<LocationRow.ServerRow>()

        assertEquals(listOf("nyc", "nyc", "sfo"), servers.map { it.city.code })
        assertEquals("us", servers.first().country.code)
    }

    @Test
    fun `row keys are stable and unique`() {
        val rows = LocationTree.rows(ORDERED, expandedCountries = setOf("rec", "us", "de"))

        assertEquals(
            listOf(
                "country:rec",
                "server:rec/rec/auto.example.net:443",
                "country:us",
                "server:us/nyc/edge.example.net:2499",
                "server:us/nyc/share.example.net:443",
                "server:us/sfo/share.example.net:443",
                "country:de",
                "server:de/ber/ber.example.net:443",
                "server:de/ber/quarantined.example.net:443",
            ),
            rows.map { it.key },
        )
        assertEquals(rows.size, rows.map { it.key }.toSet().size)
        assertEquals(
            rows.map { it.key },
            LocationTree.rows(ORDERED, expandedCountries = setOf("rec", "us", "de")).map { it.key },
        )
    }

    @Test
    fun `a query opens the matching branches without touching the expansion`() {
        val rows = LocationTree.rows(ORDERED, query = "edge")

        assertEquals(
            listOf("country:us", "server:us/nyc/edge.example.net:2499"),
            rows.map { it.key },
        )
        // The user's expansion is untouched: clearing the query returns to none.
        assertEquals(3, LocationTree.rows(ORDERED, query = "").size)
    }

    // ---- ping targets ----------------------------------------------------

    @Test
    fun `ping targets are the visible servers deduped by address`() {
        val rows = LocationTree.rows(ORDERED, expandedCountries = setOf("us"))

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

        val rows = LocationTree.rows(listOf(country), expandedCountries = setOf("us"))

        assertEquals(listOf("edge.example.net:2499"), LocationTree.pingTargets(rows))
    }

    @Test
    fun `ping all targets walk every country including REC`() {
        assertEquals(
            listOf(
                "auto.example.net:443",
                "edge.example.net:2499",
                "share.example.net:443",
                "ber.example.net:443",
            ),
            LocationTree.pingAllTargets(ORDERED),
        )
    }

    @Test
    fun `ping all targets skip quarantined and broken addresses and keep the first spelling`() {
        val country = Country(
            code = "us",
            name = "United States",
            cities = listOf(
                City(
                    code = "nyc",
                    name = "New York",
                    servers = listOf(
                        server("EDGE.Example.NET", 2499),
                        server("edge.example.net", 2499),
                        server("quarantined.example.net", quarantined = true),
                        server("", 443),
                        server("noport.example.net", 0),
                    ),
                ),
            ),
        )

        assertEquals(listOf("EDGE.Example.NET:2499"), LocationTree.pingAllTargets(listOf(country)))
    }

    @Test
    fun `ping all targets are independent of expansion and filtering`() {
        assertEquals(
            "a collapsed tree has nothing for Test speed to probe",
            emptyList<String>(),
            LocationTree.pingTargets(LocationTree.rows(ORDERED)),
        )
        assertEquals(4, LocationTree.pingAllTargets(ORDERED).size)
    }

    @Test
    fun `a quarantined server is still visible for context`() {
        val rows = LocationTree.rows(ORDERED, expandedCountries = setOf("de"))

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
    fun `a resolved country selection carries its flag`() {
        val selection = LocationTree.currentSelection(ORDERED, location = "us", server = "edge.example.net:2499")

        requireNotNull(selection)
        assertEquals("🇺🇸", selection.flag)
    }

    @Test
    fun `a recommended selection carries no flag`() {
        val selection = LocationTree.currentSelection(ORDERED, location = "REC", server = null)

        requireNotNull(selection)
        assertEquals("", selection.flag)
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

    // ---- country latency and speed order ---------------------------------

    @Test
    fun `unmeasured rows keep the engine order with Recommended pinned first`() {
        val countries = listOf(US, REC, DE)

        assertEquals(
            listOf("country:rec", "country:us", "country:de"),
            LocationTree.rows(countries).map { it.key },
        )

        val servers = LocationTree.rows(countries, expandedCountries = setOf("us"))
            .filterIsInstance<LocationRow.ServerRow>()

        assertEquals(listOf("nyc", "nyc", "sfo"), servers.map { it.city.code })
        assertEquals(
            listOf(
                "edge.example.net:2499",
                "share.example.net:443",
                "share.example.net:443",
            ),
            servers.map { LocationTree.addressOf(it.server) },
        )
    }

    @Test
    fun `measured countries sort first ascending and unmeasured keep engine order`() {
        val latency = mapOf(
            latencyKey("edge.example.net", 2499) to 80.0,
            latencyKey("ber.example.net") to 20.0,
        )

        assertEquals(
            listOf("country:rec", "country:de", "country:us"),
            LocationTree.rows(listOf(US, REC, DE), latency = latency).map { it.key },
        )
        assertEquals(
            listOf("country:rec", "country:de", "country:us", "country:fr"),
            LocationTree.rows(listOf(US, REC, DE, FR), latency = latency).map { it.key },
        )
    }

    @Test
    fun `Recommended stays pinned first even when others are measured`() {
        val latency = mapOf(latencyKey("edge.example.net", 2499) to 20.0)

        assertEquals(
            listOf("country:rec", "country:us"),
            LocationTree.rows(listOf(US, REC), latency = latency).map { it.key },
        )
    }

    @Test
    fun `timeouts do not count as measurements`() {
        val latency = mapOf(
            latencyKey("edge.example.net", 2499) to null,
            latencyKey("ber.example.net") to 30.0,
        )

        assertEquals(
            listOf("country:rec", "country:de", "country:us"),
            LocationTree.rows(listOf(US, REC, DE), latency = latency).map { it.key },
        )
    }

    @Test
    fun `countryLatencyOf takes the fastest measurement`() {
        val fastest = mapOf(
            latencyKey("edge.example.net", 2499) to 42.4,
            latencyKey("share.example.net") to 9.0,
        )
        assertEquals(LatencyState.Measured(9), LocationTree.countryLatencyOf(fastest, US))

        val timeoutBeside = mapOf(
            latencyKey("edge.example.net", 2499) to 42.4,
            latencyKey("share.example.net") to null,
        )
        assertEquals(LatencyState.Measured(42), LocationTree.countryLatencyOf(timeoutBeside, US))
    }

    @Test
    fun `countryLatencyOf reads timeout when only timeouts answered`() {
        assertEquals(
            LatencyState.Timeout,
            LocationTree.countryLatencyOf(mapOf(latencyKey("edge.example.net", 2499) to null), US),
        )
        assertEquals(LatencyState.Untested, LocationTree.countryLatencyOf(emptyMap(), US))
    }

    @Test
    fun `NaN and negative results are timeouts not measurements`() {
        val latency = mapOf(
            latencyKey("edge.example.net", 2499) to Double.NaN,
            latencyKey("share.example.net") to -1.0,
        )

        assertEquals(LatencyState.Timeout, LocationTree.countryLatencyOf(latency, US))
        assertEquals(
            listOf("country:rec", "country:us", "country:de"),
            LocationTree.rows(listOf(US, REC, DE), latency = latency).map { it.key },
        )
    }

    @Test
    fun `quarantined and unusable servers never set a country latency`() {
        assertEquals(
            LatencyState.Untested,
            LocationTree.countryLatencyOf(mapOf(latencyKey("quarantined.example.net") to 1.0), DE),
        )
        assertEquals(
            LatencyState.Untested,
            LocationTree.countryLatencyOf(mapOf(latencyKey("quarantined.example.net") to null), DE),
        )
        assertEquals(
            LatencyState.Measured(50),
            LocationTree.countryLatencyOf(
                mapOf(
                    latencyKey("quarantined.example.net") to null,
                    latencyKey("ber.example.net") to 50.0,
                ),
                DE,
            ),
        )

        val broken = Country(
            code = "us",
            name = "United States",
            cities = listOf(
                City(
                    code = "nyc",
                    name = "New York",
                    servers = listOf(server("", 443), server("noport.example.net", 0)),
                ),
            ),
        )
        assertEquals(
            LatencyState.Untested,
            LocationTree.countryLatencyOf(
                mapOf(
                    latencyKey(":443") to 1.0,
                    latencyKey("noport.example.net", 0) to 1.0,
                ),
                broken,
            ),
        )
    }

    @Test
    fun `searching sorts the filtered view`() {
        val latency = mapOf(
            latencyKey("edge.example.net", 2499) to 10.0,
            latencyKey("share.example.net") to 200.0,
            latencyKey("par.example.net") to 50.0,
        )

        assertEquals(
            listOf("country:rec", "country:us", "country:fr", "country:de"),
            LocationTree.rows(listOf(US, REC, DE, FR), latency = latency).map { it.key },
        )

        val rows = LocationTree.rows(listOf(US, REC, DE, FR), query = "c", latency = latency)
        assertEquals(
            listOf("country:rec", "country:fr", "country:us"),
            rows.filterIsInstance<LocationRow.CountryRow>().map { it.key },
        )

        val filteredUs = rows
            .filterIsInstance<LocationRow.CountryRow>()
            .first { it.country.code == "us" }
            .country
        assertEquals(listOf("sfo"), filteredUs.cities.map { it.code })
        assertEquals(LatencyState.Measured(200), LocationTree.countryLatencyOf(latency, filteredUs))
        assertEquals(LatencyState.Measured(10), LocationTree.countryLatencyOf(latency, US))
    }

    @Test
    fun `host and city searches still auto-open the matching branch`() {
        assertEquals(
            listOf(
                "country:de",
                "server:de/ber/ber.example.net:443",
                "server:de/ber/quarantined.example.net:443",
            ),
            LocationTree.rows(ORDERED, query = "berlin").map { it.key },
        )
        assertEquals(
            listOf("country:us", "server:us/nyc/edge.example.net:2499"),
            LocationTree.rows(ORDERED, query = "edge.example").map { it.key },
        )
    }

    // ---- server speed order ----------------------------------------------

    @Test
    fun `servers sort measured first then keep input order`() {
        val rows = LocationTree.rows(
            ORDERED,
            expandedCountries = setOf("us"),
            latency = mapOf(latencyKey("share.example.net") to 90.0),
        )

        assertEquals(
            listOf(
                "country:rec",
                "country:us",
                "server:us/nyc/share.example.net:443",
                "server:us/sfo/share.example.net:443",
                "server:us/nyc/edge.example.net:2499",
                "country:de",
            ),
            rows.map { it.key },
        )

        val ascending = LocationTree.rows(
            ORDERED,
            expandedCountries = setOf("us"),
            latency = mapOf(
                latencyKey("share.example.net") to 90.0,
                latencyKey("edge.example.net", 2499) to 10.0,
            ),
        )
        assertEquals(
            listOf(
                "server:us/nyc/edge.example.net:2499",
                "server:us/nyc/share.example.net:443",
                "server:us/sfo/share.example.net:443",
            ),
            ascending.filterIsInstance<LocationRow.ServerRow>().map { it.key },
        )
    }

    @Test
    fun `equal measurements keep input order`() {
        val rows = LocationTree.rows(
            ORDERED,
            expandedCountries = setOf("us"),
            latency = mapOf(
                latencyKey("share.example.net") to 42.0,
                latencyKey("edge.example.net", 2499) to 42.0,
            ),
        )

        assertEquals(
            listOf(
                "server:us/nyc/edge.example.net:2499",
                "server:us/nyc/share.example.net:443",
                "server:us/sfo/share.example.net:443",
            ),
            rows.filterIsInstance<LocationRow.ServerRow>().map { it.key },
        )
    }

    private companion object {
        fun server(host: String, port: Int = 443, quarantined: Boolean = false) = Server(
            hostname = host,
            port = port,
            quarantined = quarantined,
            protocols = listOf(Protocol(name = "connect", host = host, port = port, scheme = "https")),
        )

        /** The latency-map key for one fixture address. */
        fun latencyKey(host: String, port: Int = 443): String =
            LocationTree.addressKey("$host:$port")

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

        val FR = Country(
            code = "fr",
            name = "France",
            cities = listOf(
                City(code = "par", name = "Paris", servers = listOf(server("par.example.net"))),
            ),
        )

        val ORDERED = LocationTree.ordered(listOf(US, REC, DE))
    }
}
