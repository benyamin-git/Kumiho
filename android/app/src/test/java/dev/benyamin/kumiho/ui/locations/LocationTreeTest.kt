package dev.benyamin.kumiho.ui.locations

import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.Protocol
import dev.benyamin.kumiho.engine.Server
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The pure flat-list rules: one usable row per server, the country and speed
 * orders, the row text, the all-servers ping targets and the current selection.
 * No Android, no Compose, no engine.
 */
class LocationTreeTest {

    // ---- rows and display text ------------------------------------------

    @Test
    fun `rows are usable servers only and keep country city server identity`() {
        val rows = LocationTree.rows(listOf(US))

        assertEquals(
            listOf(
                "us/nyc/edge.example.net:2499",
                "us/nyc/share.example.net:443",
                "us/sfo/west.example.net:443",
            ),
            rows.map { it.key },
        )
        assertTrue(rows.first().country === US)
        assertTrue(rows.first().city === NYC)
        assertTrue(rows.first().server === EDGE)
    }

    @Test
    fun `the primary line is the full country and city`() {
        val rows = LocationTree.rows(listOf(US))

        assertEquals("United States · New York", rows.first().title)
        assertEquals(
            "edge.example.net:2499 · connect, quic",
            rows.first().detail,
        )
    }

    @Test
    fun `the city is omitted when missing or identical to the country`() {
        val singapore = Country(
            code = "sg",
            name = "Singapore",
            cities = listOf(City("sin", "Singapore", listOf(server("sin.example.net")))),
        )
        val noCityName = Country(
            code = "xx",
            name = "Example",
            cities = listOf(City("", "", listOf(server("example.net")))),
        )

        assertEquals("Singapore", LocationTree.rows(listOf(singapore)).single().title)
        assertEquals("Example", LocationTree.rows(listOf(noCityName)).single().title)
    }

    @Test
    fun `Recommended uses the display name Recommended`() {
        val row = LocationTree.rows(listOf(REC)).single()

        assertEquals("Recommended", row.title)
        assertEquals("Recommended", row.countryName)
    }

    @Test
    fun `the detail line omits an empty protocol list`() {
        val plain = server("plain.example.net", protocols = emptyList())

        val row = LocationTree.rows(
            listOf(Country("us", "United States", listOf(City("nyc", "New York", listOf(plain))))),
        ).single()

        assertEquals("plain.example.net:443", row.detail)
        assertTrue(row.protocolNames.isEmpty())
    }

    @Test
    fun `duplicate protocol names are shown once in engine order`() {
        val row = LocationTree.rows(listOf(US)).first { it.server === EDGE }

        assertEquals(listOf("connect", "quic"), row.protocolNames)
        assertEquals("edge.example.net:2499 · connect, quic", row.detail)
    }

    @Test
    fun `quarantined and targetless servers are hidden everywhere`() {
        val badHost = server("")
        val badPort = server("bad.example.net", port = 0)
        val hugePort = server("huge.example.net", port = 70_000)
        val country = Country(
            code = "us",
            name = "United States",
            cities = listOf(
                City(
                    "nyc",
                    "New York",
                    listOf(QUARANTINED, badHost, badPort, hugePort, EDGE),
                ),
            ),
        )

        assertEquals(listOf("us/nyc/edge.example.net:2499"), LocationTree.rows(listOf(country)).map { it.key })
        assertEquals(listOf(EDGE_ADDRESS), LocationTree.pingAllTargets(listOf(country)))
    }

    // ---- default order ---------------------------------------------------

    @Test
    fun `default order is Recommended then country city host`() {
        val rows = LocationTree.rows(listOf(US, REC, DE))

        assertEquals(
            listOf(
                "rec/rec/auto.example.net:443",
                "de/ber/ber.example.net:443",
                "us/nyc/edge.example.net:2499",
                "us/nyc/share.example.net:443",
                "us/sfo/west.example.net:443",
            ),
            rows.map { it.key },
        )
    }

    @Test
    fun `default order ignores latency`() {
        val latency = mapOf(
            LocationTree.addressKey(EDGE_ADDRESS) to 1.0,
            LocationTree.addressKey(BER_ADDRESS) to 2.0,
        )

        assertEquals(
            listOf("rec/rec/auto.example.net:443", "de/ber/ber.example.net:443", "us/nyc/edge.example.net:2499"),
            LocationTree.rows(listOf(US, REC, DE), latency = latency).take(3).map { it.key },
        )
    }

    // ---- speed order -----------------------------------------------------

    @Test
    fun `speed order keeps Recommended first then finite timeouts or unpinged last`() {
        val unpinged = server("z-unpinged.example.net")
        val germany = Country(
            "de",
            "Germany",
            listOf(City("ber", "Berlin", listOf(BER, unpinged))),
        )
        val latency = mapOf(
            LocationTree.addressKey(EDGE_ADDRESS) to 80.0,
            LocationTree.addressKey(SHARE_ADDRESS) to 20.0,
            LocationTree.addressKey(WEST_ADDRESS) to 10.0,
            LocationTree.addressKey(BER_ADDRESS) to null,
        )

        val rows = LocationTree.rows(
            countries = listOf(US, REC, germany),
            latency = latency,
            sortMode = LocationSortMode.SPEED,
        )

        assertEquals(
            listOf(
                "rec/rec/auto.example.net:443",
                "us/sfo/west.example.net:443",
                "us/nyc/share.example.net:443",
                "us/nyc/edge.example.net:2499",
                "de/ber/ber.example.net:443",
                "de/ber/z-unpinged.example.net:443",
            ),
            rows.map { it.key },
        )
    }

    @Test
    fun `speed ties fall back to country city and host`() {
        val latency = mapOf(
            LocationTree.addressKey(EDGE_ADDRESS) to 20.0,
            LocationTree.addressKey(SHARE_ADDRESS) to 20.0,
            LocationTree.addressKey(WEST_ADDRESS) to 20.0,
            LocationTree.addressKey(BER_ADDRESS) to 20.0,
        )

        assertEquals(
            listOf(
                "de/ber/ber.example.net:443",
                "us/nyc/edge.example.net:2499",
                "us/nyc/share.example.net:443",
                "us/sfo/west.example.net:443",
            ),
            LocationTree.rows(
                countries = listOf(US, REC, DE).filterNot(LocationTree::isRecommended),
                latency = latency,
                sortMode = LocationSortMode.SPEED,
            ).map { it.key },
        )
    }

    // ---- search and targets ---------------------------------------------

    @Test
    fun `a query filters rows without changing order`() {
        assertEquals(
            listOf("us/nyc/edge.example.net:2499"),
            LocationTree.rows(listOf(US, REC, DE), query = "  QUIC ").map { it.key },
        )
    }

    @Test
    fun `ping all targets every usable server and de-duplicates addresses`() {
        val duplicate = City("other", "Other", listOf(server("EDGE.example.net", 2499)))
        val country = Country("us", "United States", listOf(NYC, duplicate))

        assertEquals(listOf(EDGE_ADDRESS, SHARE_ADDRESS), LocationTree.pingAllTargets(listOf(country)))
    }

    // ---- selection -------------------------------------------------------

    @Test
    fun `current selection resolves the country city server and summary detail`() {
        val selection = LocationTree.currentSelection(
            countries = listOf(US),
            location = "us",
            server = EDGE_ADDRESS,
        )

        assertEquals("United States", selection?.countryName)
        assertEquals("New York", selection?.cityName)
        assertEquals(EDGE_ADDRESS, selection?.serverAddress)
        assertEquals(listOf("connect", "quic"), selection?.protocolNames)
        assertTrue(selection!!.marks(US, NYC, EDGE))
        assertFalse(selection.marks(US, NYC, SHARE))
    }

    @Test
    fun `an unresolved selection keeps the reported country and raw address`() {
        val selection = LocationTree.currentSelection(
            countries = listOf(DE),
            location = "de",
            server = "raw.example.net:443",
        )

        assertEquals("Germany", selection?.countryName)
        assertNull(selection?.cityName)
        assertEquals("raw.example.net:443", selection?.serverAddress)
        assertTrue(selection!!.protocolNames.isEmpty())
    }

    @Test
    fun `a quarantined current server stays in the summary even when its row is hidden`() {
        val selection = LocationTree.currentSelection(
            countries = listOf(US),
            location = "us",
            server = QUARANTINED_ADDRESS,
        )

        assertEquals(QUARANTINED_ADDRESS, selection?.serverAddress)
        assertTrue(LocationTree.rows(listOf(US)).none { it.server === QUARANTINED })
    }

    @Test
    fun `Recommended is not treated as a country flag`() {
        val selection = LocationTree.currentSelection(listOf(REC), "rec", AUTO_ADDRESS)

        assertTrue(selection!!.recommended)
        assertEquals("Recommended", selection.countryName)
        assertEquals(AUTO_ADDRESS, selection.serverAddress)
    }

    // ---- latency ---------------------------------------------------------

    @Test
    fun `latency is untested timeout or measured by address key`() {
        val key = LocationTree.addressKey(EDGE_ADDRESS)

        assertEquals(LatencyState.Untested, LocationTree.latencyOf(emptyMap(), EDGE))
        assertEquals(LatencyState.Timeout, LocationTree.latencyOf(mapOf(key to null), EDGE))
        assertEquals(LatencyState.Measured(42), LocationTree.latencyOf(mapOf(key to 42.4), EDGE))
        assertEquals(LatencyState.Timeout, LocationTree.latencyOf(mapOf(key to -1.0), EDGE))
    }

    private companion object {
        const val EDGE_ADDRESS = "edge.example.net:2499"
        const val SHARE_ADDRESS = "share.example.net:443"
        const val WEST_ADDRESS = "west.example.net:443"
        const val BER_ADDRESS = "ber.example.net:443"
        const val QUARANTINED_ADDRESS = "quarantined.example.net:443"
        const val AUTO_ADDRESS = "auto.example.net:443"

        fun server(
            host: String,
            port: Int = 443,
            protocols: List<String> = listOf("connect"),
            quarantined: Boolean = false,
        ) = Server(
            hostname = host,
            port = port,
            quarantined = quarantined,
            protocols = protocols.map { Protocol(it, host, port, "https") },
        )

        val EDGE = server("edge.example.net", 2499, protocols = listOf("connect", "quic", "connect"))
        val SHARE = server("share.example.net")
        val WEST = server("west.example.net")
        val BER = server("ber.example.net")
        val QUARANTINED = server("quarantined.example.net", quarantined = true)
        val AUTO = server("auto.example.net")

        val NYC = City("nyc", "New York", listOf(EDGE, SHARE, QUARANTINED))
        val SFO = City("sfo", "San Francisco", listOf(WEST))
        val US = Country("us", "United States", listOf(NYC, SFO))
        val DE = Country("de", "Germany", listOf(City("ber", "Berlin", listOf(BER))))
        val REC = Country(
            "rec",
            "Recommended Location",
            listOf(City("rec", "Recommended", listOf(AUTO))),
        )
    }
}
