package dev.benyamin.kumiho.ui.locations

import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.engine.LocationRef
import dev.benyamin.kumiho.engine.LocationsResult
import dev.benyamin.kumiho.engine.PingDone
import dev.benyamin.kumiho.engine.PingResult
import dev.benyamin.kumiho.engine.Quota
import dev.benyamin.kumiho.vpn.ServiceState
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The switch choreography, driven against a fake gateway with virtual time.
 * No Android, engine or network is involved.
 */
class SwitchLocationTest {

    private val gateway = SwitchFakeGateway()
    private val target = SwitchTarget("us", "nyc", ADDRESS, "United States · New York")

    @Test
    fun `the happy path disconnects waits selects and connects in order`() = runTest {
        gateway.statusStates.addLast("CONNECTED")
        gateway.statusStates.addLast("IDLE")
        val steps = mutableListOf<SwitchStep>()

        val result = SwitchLocation(gateway, timeoutMillis = 5_000, pollMillis = 100)
            .run(target) { steps += it }

        assertTrue(result.isSuccess)
        assertEquals(
            listOf("disconnect", "status", "status", "select", "connect"),
            gateway.calls,
        )
        assertEquals(
            listOf(SwitchStep.Disconnecting, SwitchStep.Connecting(target.label)),
            steps,
        )
        assertEquals(SelectionRequest("us", "nyc", ADDRESS), gateway.selection)
    }

    @Test
    fun `every connected family state is waited out before selecting`() = runTest {
        gateway.statusStates.addAll(listOf("CONNECTED", "PROXY_ONLY", "RECONNECTING", "WAITING_NETWORK", "IDLE"))

        val result = SwitchLocation(gateway, timeoutMillis = 5_000, pollMillis = 100)
            .run(target) {}

        assertTrue(result.isSuccess)
        assertEquals("connect", gateway.calls.last())
        assertEquals("select", gateway.calls[gateway.calls.lastIndex - 1])
        assertEquals(5, gateway.calls.count { it == "status" })
    }

    @Test
    fun `a disconnect failure stops before polling`() = runTest {
        gateway.disconnectError = IllegalStateException("disconnect failed")
        val steps = mutableListOf<SwitchStep>()

        val result = SwitchLocation(gateway).run(target) { steps += it }

        assertFalse(result.isSuccess)
        assertEquals("disconnect failed", result.exceptionOrNull()?.message)
        assertEquals(listOf("disconnect"), gateway.calls)
        assertEquals(listOf(SwitchStep.Disconnecting), steps)
    }

    @Test
    fun `a status failure stops before selecting`() = runTest {
        gateway.statusError = IllegalStateException("status failed")

        val result = SwitchLocation(gateway).run(target) {}

        assertFalse(result.isSuccess)
        assertEquals("status failed", result.exceptionOrNull()?.message)
        assertEquals(listOf("disconnect", "status"), gateway.calls)
    }

    @Test
    fun `a select failure stops before connecting`() = runTest {
        gateway.selectError = IllegalStateException("save failed")

        val result = SwitchLocation(gateway).run(target) {}

        assertFalse(result.isSuccess)
        assertEquals("save failed", result.exceptionOrNull()?.message)
        assertEquals(listOf("disconnect", "status", "select"), gateway.calls)
    }

    @Test
    fun `a connect failure is returned after the target is saved`() = runTest {
        gateway.connectError = IllegalStateException("connect failed")
        val steps = mutableListOf<SwitchStep>()

        val result = SwitchLocation(gateway).run(target) { steps += it }

        assertFalse(result.isSuccess)
        assertEquals("connect failed", result.exceptionOrNull()?.message)
        assertEquals(listOf("disconnect", "status", "select", "connect"), gateway.calls)
        assertEquals(
            listOf(SwitchStep.Disconnecting, SwitchStep.Connecting(target.label)),
            steps,
        )
    }

    @Test
    fun `a session that never settles times out without selecting`() = runTest {
        gateway.alwaysState = "CONNECTED"

        val result = SwitchLocation(gateway, timeoutMillis = 1_000, pollMillis = 250)
            .run(target) {}

        assertFalse(result.isSuccess)
        assertEquals(
            "The session did not stop in time.",
            result.exceptionOrNull()?.message,
        )
        assertTrue(gateway.calls.count { it == "status" } > 1)
        assertFalse(gateway.calls.contains("select"))
        assertFalse(gateway.calls.contains("connect"))
    }

    @Test
    fun `lockdown does not change the sequence`() = runTest {
        gateway.serviceState.value = ServiceState(lockdown = true)
        gateway.statusStates.addLast("WAITING_NETWORK")
        gateway.statusStates.addLast("IDLE")

        val result = SwitchLocation(gateway, timeoutMillis = 5_000, pollMillis = 100)
            .run(target) {}

        assertTrue(result.isSuccess)
        assertEquals(listOf("disconnect", "status", "status", "select", "connect"), gateway.calls)
    }

    private companion object {
        const val ADDRESS = "edge.example.net:2499"
    }
}

private class SwitchFakeGateway : LocationsGateway {
    val calls = mutableListOf<String>()
    val statusStates = ArrayDeque<String>()
    var alwaysState = "IDLE"
    var statusError: Exception? = null
    var selectError: Exception? = null
    var disconnectError: Exception? = null
    var connectError: Exception? = null
    var selection: SelectionRequest? = null

    val serviceState = MutableStateFlow(ServiceState())

    override val service: StateFlow<ServiceState> = serviceState
    override val pings: Flow<PingResult> = emptyFlow()
    override val pingDone: Flow<PingDone> = emptyFlow()

    override suspend fun locations(refresh: Boolean): LocationsResult =
        error("locations is not part of a switch")

    override suspend fun selectLocation(countryCode: String, cityCode: String?, server: String) {
        calls += "select"
        selection = SelectionRequest(countryCode, cityCode, server)
        selectError?.let { throw it }
    }

    override suspend fun selectFastest(): Unit = error("fastest is not part of a switch")

    override suspend fun favorite(ref: LocationRef, favorite: Boolean): Unit =
        error("favorites are not part of a switch")

    override suspend fun clearRecents(): Unit = error("recents are not part of a switch")

    override suspend fun setAllowLocked(enabled: Boolean): Unit =
        error("locked rows are not part of a switch")

    override suspend fun ping(hosts: List<String>): Unit = error("pings are not part of a switch")

    override suspend fun status(): EngineStatus {
        calls += "status"
        statusError?.let { throw it }
        val state = statusStates.removeFirstOrNull() ?: alwaysState
        return status(state)
    }

    override suspend fun disconnect() {
        calls += "disconnect"
        disconnectError?.let { throw it }
    }

    override suspend fun connect() {
        calls += "connect"
        connectError?.let { throw it }
    }

    private fun status(state: String) = EngineStatus(
        state = state,
        authenticated = true,
        email = "user@example.com",
        since = 0L,
        tokenExpires = 0L,
        passExpires = 0L,
        killSwitch = false,
        autoconnect = false,
        location = null,
        server = null,
        proxyOnly = false,
        quota = Quota(limit = null, remaining = null, reset = null),
        dns = null,
        ipv6 = null,
        exitIp = null,
        exitCountry = null,
        totals = null,
        rates = null,
    )
}
