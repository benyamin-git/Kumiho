package dev.benyamin.kumiho.ui.locations

import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.Protocol
import dev.benyamin.kumiho.engine.Server
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** The flat row matcher: every visible identity and protocol is searchable. */
class LocationFilterTest {

    private val row = LocationTree.rows(listOf(US)).first()

    @Test
    fun `country name code city name and city code match case-insensitively`() {
        assertTrue(LocationFilter.matches(row, "united states"))
        assertTrue(LocationFilter.matches(row, "US"))
        assertTrue(LocationFilter.matches(row, "new york"))
        assertTrue(LocationFilter.matches(row, "NYC"))
        assertFalse(LocationFilter.matches(row, "  germany  "))
    }

    @Test
    fun `hostname host port and protocol names match`() {
        assertTrue(LocationFilter.matches(row, "edge.example.net"))
        assertTrue(LocationFilter.matches(row, "EDGE.EXAMPLE.NET:2499"))
        assertTrue(LocationFilter.matches(row, "2499"))
        assertTrue(LocationFilter.matches(row, "quic"))
        assertTrue(LocationFilter.matches(row, "connect"))
    }

    @Test
    fun `a blank query keeps every row and no match returns none`() {
        val rows = LocationTree.rows(listOf(US))

        assertEquals(rows, LocationFilter.apply(rows, "   "))
        assertEquals(emptyList<LocationRow>(), LocationFilter.apply(rows, "not-a-server"))
    }

    @Test
    fun `a query returns only the matching flat rows`() {
        val rows = LocationTree.rows(listOf(US))

        assertEquals(
            setOf(
                LocationTree.addressKey(EDGE_ADDRESS),
                LocationTree.addressKey("share.example.net:443"),
            ),
            LocationFilter.apply(rows, "quic").map {
                LocationTree.addressKey(it.address)
            }.toSet(),
        )
        assertFalse(LocationFilter.matches(row, "port 2499"))
    }

    private companion object {
        const val EDGE_ADDRESS = "edge.example.net:2499"

        fun server(host: String, port: Int = 443) = Server(
            hostname = host,
            port = port,
            quarantined = false,
            protocols = listOf(
                Protocol("connect", host, port, "https"),
                Protocol("quic", host, port, "https"),
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
            ),
        )
    }
}
