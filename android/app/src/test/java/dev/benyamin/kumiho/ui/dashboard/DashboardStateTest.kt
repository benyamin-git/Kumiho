package dev.benyamin.kumiho.ui.dashboard

import dev.benyamin.kumiho.engine.ByteCounters
import dev.benyamin.kumiho.engine.ByteRates
import dev.benyamin.kumiho.engine.City
import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.engine.Notice
import dev.benyamin.kumiho.engine.Quota
import dev.benyamin.kumiho.vpn.ConnectRequest
import dev.benyamin.kumiho.vpn.ServiceState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The dashboard's pure state: the ten T9 states plus the LOADING overlay, the
 * power action that follows from them, card visibility, the location choice to
 * request mapping, the options sheet's disabled reason and the notice model.
 *
 * Nothing here touches Android, Compose or the engine, so every rule the screen
 * relies on is asserted on the JVM.
 */
class DashboardStateTest {

    // ---- fixtures --------------------------------------------------------

    private fun status(
        state: String = STATE_IDLE,
        authenticated: Boolean = true,
        location: String? = null,
        server: String? = null,
        exitIp: String? = null,
        quota: Quota? = null,
        totals: ByteCounters? = null,
        rates: ByteRates? = null,
        since: Long = 0L,
    ) = EngineStatus(
        state = state,
        authenticated = authenticated,
        email = if (authenticated) EMAIL else null,
        since = since,
        tokenExpires = 0L,
        passExpires = 0L,
        killSwitch = true,
        autoconnect = true,
        location = location,
        server = server,
        proxyOnly = state == STATE_PROXY_ONLY,
        quota = quota,
        dns = null,
        ipv6 = null,
        exitIp = exitIp,
        exitCountry = null,
        totals = totals,
        rates = rates,
    )

    private fun snapshot(
        state: String = STATE_IDLE,
        authenticated: Boolean = true,
        binding: Boolean = true,
        error: String? = null,
        permissionNeeded: Boolean = false,
        location: String? = null,
        server: String? = null,
        exitIp: String? = null,
        quota: Quota? = null,
        totals: ByteCounters? = null,
        rates: ByteRates? = null,
        since: Long = 0L,
        alwaysOn: Boolean = false,
        lockdown: Boolean = false,
    ) = ServiceState(
        binding = binding,
        engineReady = binding,
        status = status(
            state = state,
            authenticated = authenticated,
            location = location,
            server = server,
            exitIp = exitIp,
            quota = quota,
            totals = totals,
            rates = rates,
            since = since,
        ),
        alwaysOn = alwaysOn,
        lockdown = lockdown,
        error = error,
        permissionNeeded = permissionNeeded,
    )

    private fun connected(
        location: String? = "us",
        server: String? = "edge.example.net:443",
        lockdown: Boolean = false,
    ) = DashboardState.fromService(
        snapshot(
            state = STATE_CONNECTED,
            location = location,
            server = server,
            exitIp = "203.0.113.7",
            quota = Quota(limit = 50L * GIB, remaining = 12L * GIB, reset = 1_792_000_000L),
            totals = ByteCounters(up = 12L * MIB, down = 45L * MIB),
            rates = ByteRates(up = 1.2 * MIB, down = 3.4 * MIB),
            since = 1_791_000_000L,
            lockdown = lockdown,
        ),
    )

    // ---- state mapping ---------------------------------------------------

    @Test
    fun everyEngineWireStateMapsToItsDashboardPhase() {
        val expected = mapOf(
            STATE_IDLE to DashboardPhase.OFF,
            STATE_CONNECTED to DashboardPhase.CONNECTED,
            STATE_PROXY_ONLY to DashboardPhase.PROXY_ONLY,
            STATE_CONNECTING to DashboardPhase.CONNECTING,
            STATE_RECONNECTING to DashboardPhase.RECONNECTING,
            STATE_WAITING to DashboardPhase.WAITING,
            STATE_ERROR to DashboardPhase.ERROR,
            STATE_UNKNOWN to DashboardPhase.ERROR,
        )
        expected.forEach { (wire, phase) ->
            assertEquals(
                "engine $wire",
                phase,
                DashboardPhase.from(snapshot(state = wire)),
            )
        }
    }

    @Test
    fun aBoundServiceWithoutStatusIsLoadingNotOff() {
        val loading = ServiceState(binding = true, engineReady = true)
        assertEquals(DashboardPhase.LOADING, DashboardPhase.from(loading))
        assertFalse("loading must not look like an active session", DashboardPhase.LOADING.active)
    }

    @Test
    fun anUnboundServiceWithoutStatusIsUnavailable() {
        assertEquals(
            DashboardPhase.SERVICE_UNAVAILABLE,
            DashboardPhase.from(ServiceState(binding = false)),
        )
    }

    @Test
    fun permissionAndErrorWinOverAStaleConnectedStatus() {
        assertEquals(
            DashboardPhase.PERMISSION_NEEDED,
            DashboardPhase.from(
                snapshot(state = STATE_CONNECTED, permissionNeeded = true, error = "boom"),
            ),
        )
        assertEquals(
            DashboardPhase.ERROR,
            DashboardPhase.from(
                snapshot(state = STATE_CONNECTED, error = "the engine failed"),
            ),
        )
    }

    @Test
    fun anUnauthenticatedStatusIsNotSignedInEvenWhenConnected() {
        assertEquals(
            DashboardPhase.NOT_SIGNED_IN,
            DashboardPhase.from(snapshot(state = STATE_CONNECTED, authenticated = false)),
        )
        assertEquals(
            DashboardPhase.NOT_SIGNED_IN,
            DashboardPhase.from(
                snapshot(state = STATE_UNAUTHENTICATED, authenticated = false),
            ),
        )
    }

    @Test
    fun thePowerActionFollowsThePhase() {
        val expected = mapOf(
            DashboardPhase.LOADING to PowerAction.NONE,
            DashboardPhase.OFF to PowerAction.CONNECT,
            DashboardPhase.CONNECTED to PowerAction.DISCONNECT,
            DashboardPhase.PROXY_ONLY to PowerAction.DISCONNECT,
            DashboardPhase.CONNECTING to PowerAction.NONE,
            DashboardPhase.RECONNECTING to PowerAction.DISCONNECT,
            DashboardPhase.WAITING to PowerAction.DISCONNECT,
            DashboardPhase.NOT_SIGNED_IN to PowerAction.SIGN_IN_REQUIRED,
            DashboardPhase.ERROR to PowerAction.CONNECT,
            DashboardPhase.SERVICE_UNAVAILABLE to PowerAction.UNAVAILABLE,
            DashboardPhase.PERMISSION_NEEDED to PowerAction.REQUEST_PERMISSION,
        )
        expected.forEach { (phase, action) ->
            assertEquals("phase $phase", action, DashboardState(phase = phase).powerAction)
        }
    }

    @Test
    fun thePowerControlIsDisabledOnlyWhenItCannotAct() {
        assertFalse(DashboardState(phase = DashboardPhase.LOADING).powerEnabled)
        assertFalse(DashboardState(phase = DashboardPhase.SERVICE_UNAVAILABLE).powerEnabled)
        assertFalse(DashboardState(phase = DashboardPhase.NOT_SIGNED_IN).powerEnabled)
        assertFalse(DashboardState(phase = DashboardPhase.OFF, inFlight = true).powerEnabled)
        assertTrue(DashboardState(phase = DashboardPhase.OFF).powerEnabled)
        assertTrue(DashboardState(phase = DashboardPhase.ERROR).powerEnabled)
        assertTrue(DashboardState(phase = DashboardPhase.PERMISSION_NEEDED).powerEnabled)
        assertTrue(DashboardState(phase = DashboardPhase.CONNECTED).powerEnabled)
    }

    @Test
    fun thePowerControlIsBusyWhileConnectingOrSubmitting() {
        assertTrue(DashboardState(phase = DashboardPhase.CONNECTING).powerBusy)
        assertTrue(DashboardState(phase = DashboardPhase.OFF, inFlight = true).powerBusy)
        assertFalse(DashboardState(phase = DashboardPhase.CONNECTED).powerBusy)
        assertTrue(DashboardState(phase = DashboardPhase.CONNECTED).powerConnected)
        assertFalse("proxy only is not a filled accent", DashboardState(phase = DashboardPhase.PROXY_ONLY).powerConnected)
    }

    @Test
    fun lockdownOnlyNeedsConfirmationWhenThereIsASessionToDrop() {
        assertTrue(connected(lockdown = true).requiresLockdownConfirmation)
        assertFalse(connected(lockdown = false).requiresLockdownConfirmation)
        assertFalse(
            DashboardState.fromService(snapshot(state = STATE_IDLE, lockdown = true))
                .requiresLockdownConfirmation,
        )
    }

    // ---- cards -----------------------------------------------------------

    @Test
    fun cardsHideUntilTheirDataExists() {
        val loading = DashboardState(phase = DashboardPhase.LOADING)
        assertFalse("no status, no honest location card", loading.showLocationCard)

        val off = DashboardState.fromService(snapshot(state = STATE_IDLE))
        assertTrue("a status always names a location (Recommended)", off.showLocationCard)
        assertFalse(off.showExitCard)
        assertFalse(off.showQuotaCard)
        assertFalse(off.showSessionCard)

        val connected = connected()
        assertTrue(connected.showExitCard)
        assertTrue(connected.showQuotaCard)
        assertTrue(connected.showSessionCard)
    }

    @Test
    fun aQuotaOfZeroRemainingStaysVisible() {
        val exhausted = DashboardState.fromService(
            snapshot(
                state = STATE_CONNECTED,
                quota = Quota(limit = 50L * GIB, remaining = 0L, reset = null),
            ),
        )
        assertTrue("an exhausted quota is real data", exhausted.showQuotaCard)
    }

    @Test
    fun aMissingSideOfTheQuotaIsNotAFakeZero() {
        val partial = DashboardState.fromService(
            snapshot(state = STATE_CONNECTED, quota = Quota(limit = null, remaining = 5L * GIB, reset = null)),
        )
        assertTrue(partial.showQuotaCard)
        assertNull(partial.status?.quota?.limit)
    }

    @Test
    fun theExitCardNeedsAnIp() {
        assertFalse(
            DashboardState.fromService(snapshot(state = STATE_CONNECTED, exitIp = null)).showExitCard,
        )
        assertFalse(
            DashboardState.fromService(snapshot(state = STATE_CONNECTED, exitIp = "  ")).showExitCard,
        )
    }

    // ---- location --------------------------------------------------------

    @Test
    fun noPersistedSelectionReadsAsRecommended() {
        assertTrue(DashboardState.fromService(snapshot(state = STATE_IDLE)).locationIsRecommended)
        assertTrue(
            DashboardState.fromService(snapshot(state = STATE_CONNECTED, location = "REC"))
                .locationIsRecommended,
        )
        assertFalse(
            DashboardState.fromService(snapshot(state = STATE_CONNECTED, location = "us"))
                .locationIsRecommended,
        )
    }

    @Test
    fun aSavedSelectionStartsTheSheetOnSaved() {
        val unpicked = DashboardState.fromService(snapshot(state = STATE_IDLE))
        assertEquals(LocationChoice.Recommended, unpicked.defaultChoice)
        assertEquals(LocationChoice.Recommended, unpicked.effectiveChoice)

        val saved = DashboardState.fromService(snapshot(state = STATE_IDLE, location = "us"))
        assertEquals(LocationChoice.Saved, saved.defaultChoice)
    }

    @Test
    fun aPickedChoiceOverridesTheSheetDefault() {
        val state = DashboardState.fromService(snapshot(state = STATE_IDLE, location = "us")).copy(
            options = ConnectOptionsState(choice = LocationChoice.Picked("de", "ber")),
        )
        assertEquals(LocationChoice.Picked("de", "ber"), state.effectiveChoice)
        assertEquals(
            ConnectRequest(locationCode = "de", cityCode = "ber", server = null, proxyOnly = false),
            state.connectRequest(),
        )
    }

    @Test
    fun eachLocationChoiceMapsToOneRequest() {
        assertEquals(
            ConnectRequest(locationCode = "rec", cityCode = null, server = null, proxyOnly = false),
            LocationChoice.Recommended.toRequest(proxyOnly = false),
        )
        assertEquals(
            ConnectRequest(locationCode = null, cityCode = null, server = null, proxyOnly = true),
            LocationChoice.Saved.toRequest(proxyOnly = true),
        )
        assertEquals(
            ConnectRequest(locationCode = "us", cityCode = "nyc", server = null, proxyOnly = false),
            LocationChoice.Picked(countryCode = "us", cityCode = "nyc").toRequest(proxyOnly = false),
        )
    }

    @Test
    fun countryAndCityNamesResolveFromTheLoadedTree() {
        val tree = LocationLoad.Ready(COUNTRIES)
        val state = DashboardState.fromService(snapshot(state = STATE_CONNECTED, location = "us"))
            .copy(locations = tree)

        assertEquals("United States", state.countryName("us"))
        assertEquals("United States", state.countryName("US"))
        assertNull("REC is not a country name", state.countryName("rec"))
        assertNull(state.countryName(null))
        assertEquals("New York", state.cityName("us", "nyc"))
        assertEquals("New York", state.cityName("US", "NYC"))
        assertNull(state.cityName("us", "missing"))
        assertNull(state.cityName(null, "nyc"))
        assertEquals(COUNTRIES, state.countries)
    }

    @Test
    fun anUnloadedTreeHasNoNames() {
        val state = DashboardState()
        assertNull(state.countryName("us"))
        assertEquals(emptyList<Country>(), state.countries)
    }

    // ---- options sheet ---------------------------------------------------

    @Test
    fun connectIsRefusedWhileASessionIsActive() {
        val active = connected()
        assertFalse(active.optionsConnectEnabled)
        assertEquals(SheetHint.DISCONNECT_FIRST, active.sheetHint)
    }

    @Test
    fun connectIsRefusedWhileLoadingUnavailableOrSignedOut() {
        mapOf(
            DashboardPhase.LOADING to SheetHint.UNAVAILABLE,
            DashboardPhase.SERVICE_UNAVAILABLE to SheetHint.UNAVAILABLE,
            DashboardPhase.NOT_SIGNED_IN to SheetHint.SIGN_IN,
        ).forEach { (phase, hint) ->
            val state = DashboardState(phase = phase)
            assertFalse("phase $phase", state.optionsConnectEnabled)
            assertEquals("phase $phase", hint, state.sheetHint)
        }
    }

    @Test
    fun connectIsRefusedWhileACommandIsInFlight() {
        val inflight = DashboardState(phase = DashboardPhase.OFF, inFlight = true)
        assertFalse(inflight.optionsConnectEnabled)
        assertEquals(SheetHint.WAITING, inflight.sheetHint)
        assertFalse("a busy off state must not submit twice", inflight.optionsConnectEnabled)
    }

    @Test
    fun connectIsAllowedWhenIdleOrFailed() {
        listOf(DashboardPhase.OFF, DashboardPhase.ERROR, DashboardPhase.PERMISSION_NEEDED)
            .forEach { phase ->
                val state = DashboardState(phase = phase)
                assertTrue("phase $phase", state.optionsConnectEnabled)
                assertEquals(SheetHint.NONE, state.sheetHint)
            }
    }

    // ---- notices ---------------------------------------------------------

    @Test
    fun aServiceErrorBecomesARetryableConnectNotice() {
        val notice = DashboardState.fromService(snapshot(state = STATE_ERROR, error = "the edge refused"))
            .notice
        assertEquals(NoticeText.Raw("the edge refused"), notice?.text)
        assertEquals(DashboardNoticeTone.ERROR, notice?.tone)
        assertEquals(DashboardNoticeAction.RETRY_CONNECT, notice?.action)
    }

    @Test
    fun aFatalEngineNoticeCanBeRetriedAndKeepsTheEngineWords() {
        val notice = DashboardNotice.engineNotice(Notice("fatal", STATE_ERROR, "edge handshake failed"))
        assertEquals(NoticeText.Raw("edge handshake failed"), notice.text)
        assertEquals(DashboardNoticeTone.ERROR, notice.tone)
        assertEquals(DashboardNoticeAction.RETRY_CONNECT, notice.action)
    }

    @Test
    fun anAdvisoryEngineNoticeCannotBeRetriedFromHere() {
        val notice = DashboardNotice.engineNotice(Notice("signed_out", STATE_IDLE, "tokens expired"))
        assertEquals(NoticeText.Raw("tokens expired"), notice.text)
        assertEquals(DashboardNoticeTone.WARNING, notice.tone)
        assertEquals(DashboardNoticeAction.NONE, notice.action)
    }

    @Test
    fun anEngineNoticeWithoutAMessageUsesLocalWords() {
        listOf(null, "", "   ").forEach { message ->
            val notice = DashboardNotice.engineNotice(Notice("fatal", STATE_ERROR, message))
            assertEquals(NoticeText.Local(DashboardMessage.ENGINE_NOTICE), notice.text)
        }
    }

    @Test
    fun aRefreshFailureIsAStatusRefreshNoticeWithItsRetry() {
        val notice = DashboardNotice.statusRefreshFailed("the engine did not answer")
        assertEquals(DashboardMessage.STATUS_REFRESH_FAILED, notice.messageCode())
        assertEquals(DashboardNoticeAction.RETRY_REFRESH, notice.action)
        assertTrue(notice.isStatusRefresh)
        assertFalse(DashboardNotice.settingsUnavailable().isStatusRefresh)
    }

    @Test
    fun aDeniedPermissionOffersTheConsentDialogAgain() {
        val notice = DashboardNotice.permissionDenied()
        assertEquals(DashboardMessage.VPN_PERMISSION_DENIED, notice.messageCode())
        assertEquals(DashboardNoticeAction.GRANT_PERMISSION, notice.action)
    }

    @Test
    fun aPolicyNoticeNeverPretendsToBeRetryable() {
        assertEquals(DashboardNoticeAction.NONE, DashboardNotice.signInRequired().action)
        assertEquals(DashboardNoticeAction.NONE, DashboardNotice.serviceUnavailable().action)
        assertEquals(DashboardNoticeAction.NONE, DashboardNotice.disconnectFailed("no").action)
        assertEquals(DashboardNoticeAction.RELOAD_LOCATIONS, DashboardNotice.locationsFailed("no").action)
    }

    @Test
    fun everyLocalMessageHasAStableCode() {
        // The UI resolves these codes to resources; the state stays Android-free.
        assertEquals(
            DashboardMessage.CONNECT_FAILED,
            DashboardNotice.connectFailed("boom").messageCode(),
        )
        assertEquals(
            DashboardMessage.DISCONNECT_FAILED,
            DashboardNotice.disconnectFailed("boom").messageCode(),
        )
        assertEquals(
            DashboardMessage.LOCATIONS_FAILED,
            DashboardNotice.locationsFailed("boom").messageCode(),
        )
        assertEquals(
            DashboardMessage.SETTINGS_UNAVAILABLE,
            DashboardNotice.settingsUnavailable().messageCode(),
        )
        assertEquals(
            DashboardMessage.SIGN_IN_REQUIRED,
            DashboardNotice.signInRequired().messageCode(),
        )
        assertEquals(
            DashboardMessage.SERVICE_UNAVAILABLE,
            DashboardNotice.serviceUnavailable().messageCode(),
        )
    }

    @Test
    fun fromServiceCarriesAndroidPolicyAndTheStatus() {
        val state = DashboardState.fromService(
            snapshot(state = STATE_CONNECTED, alwaysOn = true, lockdown = true),
        )
        assertTrue(state.alwaysOn)
        assertTrue(state.lockdown)
        assertFalse(state.status == null)
        assertEquals(DashboardPhase.CONNECTED, state.phase)
    }

    // ---- helpers ---------------------------------------------------------

    private fun DashboardNotice.messageCode(): DashboardMessage? =
        (text as? NoticeText.Local)?.message

    private companion object {
        const val EMAIL = "user@example.com"
        const val STATE_IDLE = "IDLE"
        const val STATE_CONNECTED = "CONNECTED"
        const val STATE_PROXY_ONLY = "PROXY_ONLY"
        const val STATE_CONNECTING = "CONNECTING"
        const val STATE_RECONNECTING = "RECONNECTING"
        const val STATE_WAITING = "WAITING_NETWORK"
        const val STATE_ERROR = "FATAL"
        const val STATE_UNAUTHENTICATED = "UNAUTHENTICATED"

        const val GIB = 1024L * 1024L * 1024L
        const val MIB = 1024L * 1024L

        val COUNTRIES = listOf(
            Country(
                code = "rec",
                name = "Recommended",
                cities = emptyList(),
            ),
            Country(
                code = "us",
                name = "United States",
                cities = listOf(
                    City(code = "nyc", name = "New York", servers = emptyList()),
                    City(code = "sfo", name = "San Francisco", servers = emptyList()),
                ),
            ),
            Country(
                code = "de",
                name = "Germany",
                cities = listOf(City(code = "ber", name = "Berlin", servers = emptyList())),
            ),
        )

        const val STATE_UNKNOWN = "SOMETHING_NEW"
    }
}
