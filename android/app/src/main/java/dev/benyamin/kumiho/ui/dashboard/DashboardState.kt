package dev.benyamin.kumiho.ui.dashboard

import dev.benyamin.kumiho.engine.Country
import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.engine.Notice
import dev.benyamin.kumiho.vpn.ConnectRequest
import dev.benyamin.kumiho.vpn.ServiceState
import dev.benyamin.kumiho.vpn.VpnStateReducer
import dev.benyamin.kumiho.vpn.VpnUiState

/**
 * The dashboard's view of the service: the ten [VpnUiState] values from T9 plus
 * a dashboard-only [LOADING] overlay.
 *
 * [LOADING] is not a fourth mapping of engine states: a bound service whose
 * engine has not published a status yet and has no error or permission problem
 * is starting, never "Off". [DashboardPhase.from] delegates the rest to
 * [VpnStateReducer], so the engine-state mapping exists exactly once.
 */
enum class DashboardPhase(val active: Boolean) {
    /** Bound, but the engine has not reported yet. */
    LOADING(false),
    OFF(false),
    CONNECTED(true),
    PROXY_ONLY(true),
    CONNECTING(false),
    RECONNECTING(true),
    WAITING(true),
    NOT_SIGNED_IN(false),
    ERROR(false),
    SERVICE_UNAVAILABLE(false),
    PERMISSION_NEEDED(false),
    ;

    /** True while a tunnel or proxy session exists or is coming back. */
    val isSessionUp: Boolean get() = this == CONNECTED || this == PROXY_ONLY

    companion object {
        /** Maps one service snapshot; LOADING wins over the raw reducer. */
        fun from(state: ServiceState): DashboardPhase {
            if (state.binding &&
                state.status == null &&
                !state.permissionNeeded &&
                state.error == null
            ) {
                return LOADING
            }
            return of(VpnStateReducer.reduce(state))
        }

        /** The ten T9 values, one-to-one. */
        fun of(state: VpnUiState): DashboardPhase = when (state) {
            VpnUiState.OFF -> OFF
            VpnUiState.CONNECTED -> CONNECTED
            VpnUiState.PROXY_ONLY -> PROXY_ONLY
            VpnUiState.CONNECTING -> CONNECTING
            VpnUiState.RECONNECTING -> RECONNECTING
            VpnUiState.WAITING -> WAITING
            VpnUiState.NOT_SIGNED_IN -> NOT_SIGNED_IN
            VpnUiState.ERROR -> ERROR
            VpnUiState.SERVICE_UNAVAILABLE -> SERVICE_UNAVAILABLE
            VpnUiState.PERMISSION_NEEDED -> PERMISSION_NEEDED
        }
    }
}

/** What a tap on the power control means in the current phase. */
enum class PowerAction {
    /** Nothing: the phase is loading or already connecting. */
    NONE,
    CONNECT,
    DISCONNECT,
    REQUEST_PERMISSION,
    SIGN_IN_REQUIRED,
    UNAVAILABLE,
}

/** Why the options sheet's Connect button is disabled. */
enum class SheetHint {
    NONE,

    /** A tunnel is up: the hero is the disconnect control (T12 ruling). */
    DISCONNECT_FIRST,
    WAITING,
    SIGN_IN,
    UNAVAILABLE,
}

/**
 * The compact picker's choices. Every choice maps to a [ConnectRequest]:
 * Recommended sends the REC pseudo-country, Saved sends no location at all (the
 * engine keeps its persisted selection), and a Picked city sends its codes with
 * no server so the engine resolves and persists the first candidate.
 */
sealed interface LocationChoice {
    data object Recommended : LocationChoice

    data object Saved : LocationChoice

    data class Picked(val countryCode: String, val cityCode: String?) : LocationChoice

    /** The request this choice issues. Tested one-to-one in T11. */
    fun toRequest(proxyOnly: Boolean): ConnectRequest = when (this) {
        Recommended -> ConnectRequest(locationCode = "rec", proxyOnly = proxyOnly)
        Saved -> ConnectRequest(proxyOnly = proxyOnly)
        is Picked -> ConnectRequest(
            locationCode = countryCode,
            cityCode = cityCode,
            proxyOnly = proxyOnly,
        )
    }
}

/** The location tree load, with its honest loading and failure states. */
sealed interface LocationLoad {
    data object Loading : LocationLoad

    data class Failed(val detail: String) : LocationLoad

    data class Ready(val countries: List<Country>) : LocationLoad
}

/** The options sheet's own state. */
data class ConnectOptionsState(
    val open: Boolean = false,
    /** Null until the sheet opens; then Saved or Recommended by default. */
    val choice: LocationChoice? = null,
    val proxyOnly: Boolean = false,
    /** Country codes whose cities are visible. */
    val expanded: Set<String> = emptySet(),
)

/** Copy the view model generates itself; the UI resolves it to a resource. */
enum class DashboardMessage {
    SIGN_IN_REQUIRED,
    SERVICE_UNAVAILABLE,
    VPN_PERMISSION_DENIED,
    STATUS_REFRESH_FAILED,
    CONNECT_FAILED,
    DISCONNECT_FAILED,
    LOCATIONS_FAILED,
    SETTINGS_UNAVAILABLE,
    ENGINE_NOTICE,
}

/** Notice text: either the engine's own words or a local message code. */
sealed interface NoticeText {
    data class Raw(val text: String) : NoticeText

    data class Local(val message: DashboardMessage, val detail: String? = null) : NoticeText
}

/** The notice row's tone; meaning is always also carried by an icon. */
enum class DashboardNoticeTone { INFO, WARNING, ERROR }

/** The one meaningful retry a notice can offer. */
enum class DashboardNoticeAction { NONE, RETRY_REFRESH, RETRY_CONNECT, GRANT_PERMISSION, RELOAD_LOCATIONS }

/** The latest advisory message, from the service, the engine or a local call. */
data class DashboardNotice(
    val text: NoticeText,
    val tone: DashboardNoticeTone,
    val action: DashboardNoticeAction = DashboardNoticeAction.NONE,
) {
    /** True when this notice is the status-refresh failure it can retry. */
    val isStatusRefresh: Boolean
        get() = (text as? NoticeText.Local)?.message == DashboardMessage.STATUS_REFRESH_FAILED

    companion object {
        fun serviceError(detail: String): DashboardNotice = DashboardNotice(
            text = NoticeText.Raw(detail),
            tone = DashboardNoticeTone.ERROR,
            action = DashboardNoticeAction.RETRY_CONNECT,
        )

        /**
         * Maps one advisory engine notice. A fatal transition can be retried;
         * a signed-out session cannot be fixed from T11 (no sign-in flow yet),
         * so it is a warning without an action. The engine's message is kept as
         * written rather than rewritten.
         */
        fun engineNotice(notice: Notice): DashboardNotice {
            val text = notice.message?.takeIf { it.isNotBlank() }
                ?.let { NoticeText.Raw(it) }
                ?: NoticeText.Local(DashboardMessage.ENGINE_NOTICE)
            return when (notice.type) {
                NOTICE_FATAL -> DashboardNotice(
                    text = text,
                    tone = DashboardNoticeTone.ERROR,
                    action = DashboardNoticeAction.RETRY_CONNECT,
                )
                else -> DashboardNotice(text = text, tone = DashboardNoticeTone.WARNING)
            }
        }

        fun statusRefreshFailed(detail: String): DashboardNotice = DashboardNotice(
            text = NoticeText.Local(DashboardMessage.STATUS_REFRESH_FAILED, detail),
            tone = DashboardNoticeTone.WARNING,
            action = DashboardNoticeAction.RETRY_REFRESH,
        )

        fun permissionDenied(): DashboardNotice = DashboardNotice(
            text = NoticeText.Local(DashboardMessage.VPN_PERMISSION_DENIED),
            tone = DashboardNoticeTone.WARNING,
            action = DashboardNoticeAction.GRANT_PERMISSION,
        )

        fun signInRequired(): DashboardNotice = DashboardNotice(
            text = NoticeText.Local(DashboardMessage.SIGN_IN_REQUIRED),
            tone = DashboardNoticeTone.INFO,
        )

        fun serviceUnavailable(): DashboardNotice = DashboardNotice(
            text = NoticeText.Local(DashboardMessage.SERVICE_UNAVAILABLE),
            tone = DashboardNoticeTone.ERROR,
        )

        fun connectFailed(detail: String): DashboardNotice = DashboardNotice(
            text = NoticeText.Local(DashboardMessage.CONNECT_FAILED, detail),
            tone = DashboardNoticeTone.ERROR,
            action = DashboardNoticeAction.RETRY_CONNECT,
        )

        fun disconnectFailed(detail: String): DashboardNotice = DashboardNotice(
            text = NoticeText.Local(DashboardMessage.DISCONNECT_FAILED, detail),
            tone = DashboardNoticeTone.ERROR,
        )

        fun locationsFailed(detail: String): DashboardNotice = DashboardNotice(
            text = NoticeText.Local(DashboardMessage.LOCATIONS_FAILED, detail),
            tone = DashboardNoticeTone.WARNING,
            action = DashboardNoticeAction.RELOAD_LOCATIONS,
        )

        fun settingsUnavailable(): DashboardNotice = DashboardNotice(
            text = NoticeText.Local(DashboardMessage.SETTINGS_UNAVAILABLE),
            tone = DashboardNoticeTone.WARNING,
        )

        private const val NOTICE_FATAL = "fatal"
    }
}

/** The dashboard's whole state; the view model is its only producer. */
data class DashboardState(
    val phase: DashboardPhase = DashboardPhase.LOADING,
    val status: EngineStatus? = null,
    /** Android's Always-on VPN flag, from the bound service (never the engine). */
    val alwaysOn: Boolean = false,
    /** Android's lockdown flag, from the bound service. */
    val lockdown: Boolean = false,
    /** A user connect/disconnect is in flight; the button must not submit twice. */
    val inFlight: Boolean = false,
    val notice: DashboardNotice? = null,
    val options: ConnectOptionsState = ConnectOptionsState(),
    val locations: LocationLoad = LocationLoad.Loading,
    /** The lockdown disconnect explanation is on screen. */
    val lockdownPrompt: Boolean = false,
) {
    /** The power control's visible state word and its accessible description. */
    val powerAction: PowerAction
        get() = when (phase) {
            DashboardPhase.CONNECTED,
            DashboardPhase.PROXY_ONLY,
            DashboardPhase.RECONNECTING,
            DashboardPhase.WAITING,
            -> PowerAction.DISCONNECT
            DashboardPhase.OFF, DashboardPhase.ERROR -> PowerAction.CONNECT
            DashboardPhase.PERMISSION_NEEDED -> PowerAction.REQUEST_PERMISSION
            DashboardPhase.NOT_SIGNED_IN -> PowerAction.SIGN_IN_REQUIRED
            DashboardPhase.SERVICE_UNAVAILABLE -> PowerAction.UNAVAILABLE
            DashboardPhase.LOADING, DashboardPhase.CONNECTING -> PowerAction.NONE
        }

    /** Disabled while loading, unavailable, signed out or already submitting. */
    val powerEnabled: Boolean
        get() = !inFlight &&
            phase != DashboardPhase.LOADING &&
            phase != DashboardPhase.SERVICE_UNAVAILABLE &&
            phase != DashboardPhase.NOT_SIGNED_IN

    /** A spinner, not a tap target: connecting or a user command in flight. */
    val powerBusy: Boolean
        get() = inFlight || phase == DashboardPhase.CONNECTING

    /** The button's filled (accent) appearance only for a real tunnel. */
    val powerConnected: Boolean get() = phase == DashboardPhase.CONNECTED

    /** A disconnect under lockdown is allowed, but only with the explanation. */
    val requiresLockdownConfirmation: Boolean get() = lockdown && phase.active

    // ---- cards ----------------------------------------------------------

    /**
     * The location card needs a status to name a location; before the engine
     * reports, there is nothing honest to show, so the card stays hidden.
     */
    val showLocationCard: Boolean get() = status != null

    val showExitCard: Boolean get() = phase.active && !status?.exitIp.isNullOrBlank()

    val showQuotaCard: Boolean
        get() {
            val quota = status?.quota ?: return false
            if (!phase.active) return false
            return quota.limit != null || quota.remaining != null
        }

    val showSessionCard: Boolean get() = phase.active && status?.totals != null

    // ---- location -------------------------------------------------------

    /** True when the engine's real default (REC) is the selection in effect. */
    val locationIsRecommended: Boolean
        get() = status?.location == null || status.location.equals(RECOMMENDED_CODE, ignoreCase = true)

    /** Every country the loaded tree knows, empty until it loads. */
    val countries: List<Country>
        get() = (locations as? LocationLoad.Ready)?.countries.orEmpty()

    fun countryName(code: String?): String? = code
        ?.takeUnless { it.equals(RECOMMENDED_CODE, ignoreCase = true) }
        ?.let { wanted -> countries.firstOrNull { it.code.equals(wanted, ignoreCase = true) }?.name }

    /**
     * The city whose tree entry lists this server host, or null before the tree
     * loads, when there is no server, or when no city claims it. The engine
     * status carries only `host:port`, so the port is dropped first.
     */
    fun cityNameForServer(server: String?): String? {
        val host = DashboardFormat.hostOf(server) ?: return null
        return countries
            .asSequence()
            .flatMap { it.cities.asSequence() }
            .firstOrNull { city -> city.servers.any { it.hostname.equals(host, ignoreCase = true) } }
            ?.name
    }

    fun cityName(countryCode: String?, cityCode: String?): String? {
        if (countryCode == null || cityCode == null) return null
        return countries
            .firstOrNull { it.code.equals(countryCode, ignoreCase = true) }
            ?.cities
            ?.firstOrNull { it.code.equals(cityCode, ignoreCase = true) }
            ?.name
    }

    /** The initial sheet choice: Saved when the engine has a selection. */
    val defaultChoice: LocationChoice
        get() = if (status?.location != null) LocationChoice.Saved else LocationChoice.Recommended

    /** The choice a connect would use right now. */
    val effectiveChoice: LocationChoice get() = options.choice ?: defaultChoice

    /** The request the next connect issues, from the current choice. */
    fun connectRequest(): ConnectRequest = effectiveChoice.toRequest(options.proxyOnly)

    // ---- options sheet --------------------------------------------------

    /** Connect is refused while a session is active (the T12 disconnect-first rule). */
    val optionsConnectEnabled: Boolean
        get() = !inFlight &&
            !phase.active &&
            phase != DashboardPhase.LOADING &&
            phase != DashboardPhase.SERVICE_UNAVAILABLE &&
            phase != DashboardPhase.NOT_SIGNED_IN &&
            phase != DashboardPhase.CONNECTING

    val sheetHint: SheetHint
        get() = when {
            phase.active -> SheetHint.DISCONNECT_FIRST
            inFlight || phase == DashboardPhase.CONNECTING -> SheetHint.WAITING
            phase == DashboardPhase.LOADING || phase == DashboardPhase.SERVICE_UNAVAILABLE ->
                SheetHint.UNAVAILABLE
            phase == DashboardPhase.NOT_SIGNED_IN -> SheetHint.SIGN_IN
            else -> SheetHint.NONE
        }

    companion object {
        /** The engine's REC pseudo-country (`serverlist.RecommendedCode`). */
        const val RECOMMENDED_CODE = "rec"

        /** The initial/updated state for one service snapshot. */
        fun fromService(snapshot: ServiceState): DashboardState = DashboardState(
            phase = DashboardPhase.from(snapshot),
            status = snapshot.status,
            alwaysOn = snapshot.alwaysOn,
            lockdown = snapshot.lockdown,
            notice = snapshot.error?.let { DashboardNotice.serviceError(it) },
        )
    }
}

/**
 * Stable tags for the Compose tests and the screenshot capture, so a copy
 * change never breaks a test and a test never depends on a visible sentence.
 */
object DashboardTags {
    const val SCREEN = "dashboard_screen"
    const val POWER = "dashboard_power"
    const val STATE_WORD = "dashboard_state_word"
    const val CONNECT_OPTIONS = "dashboard_connect_options"

    const val NOTICE = "dashboard_notice"
    const val NOTICE_ACTION = "dashboard_notice_action"
    const val NOTICE_DISMISS = "dashboard_notice_dismiss"

    const val CARD_LOCATION = "dashboard_card_location"
    const val CARD_EXIT = "dashboard_card_exit"
    const val CARD_QUOTA = "dashboard_card_quota"
    const val CARD_SESSION = "dashboard_card_session"

    const val DETAILS = "dashboard_details"
    const val DETAILS_VPN_SETTINGS = "dashboard_details_vpn_settings"

    const val SHEET = "dashboard_options_sheet"
    const val SHEET_CONTENT = "dashboard_options_content"
    const val SHEET_PROXY_ONLY = "dashboard_options_proxy_only"
    const val SHEET_LOCKDOWN = "dashboard_options_lockdown"
    const val SHEET_CONNECT = "dashboard_options_connect"
    const val SHEET_HINT = "dashboard_options_hint"

    const val PICKER_RECOMMENDED = "dashboard_picker_recommended"
    const val PICKER_SAVED = "dashboard_picker_saved"
    const val PICKER_RETRY = "dashboard_picker_retry"
    const val PICKER_EMPTY = "dashboard_picker_empty"
    const val PICKER_COUNTRY_PREFIX = "dashboard_picker_country_"
    const val PICKER_CITY_PREFIX = "dashboard_picker_city_"

    const val LOCKDOWN_DIALOG = "dashboard_lockdown_dialog"
    const val LOCKDOWN_CONFIRM = "dashboard_lockdown_confirm"
    const val LOCKDOWN_CANCEL = "dashboard_lockdown_cancel"

    const val LOADING = "dashboard_loading"
}

/** The tag for one country row. */
fun countryTag(code: String): String = DashboardTags.PICKER_COUNTRY_PREFIX + code

/** The tag for one city row. */
fun cityTag(countryCode: String, cityCode: String): String =
    DashboardTags.PICKER_CITY_PREFIX + countryCode + "_" + cityCode
