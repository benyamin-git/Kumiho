package dev.benyamin.kumiho.ui.settings

import androidx.compose.ui.graphics.Color
import dev.benyamin.kumiho.engine.Settings
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.ThemeId

/** The tone of a screen-level notice; the icon carries the meaning too. */
enum class SettingsNoticeTone { WARNING, ERROR }

/** Copy the view model chooses itself; the screen resolves it to a resource. */
enum class SettingsMessage {
    REFRESH_FAILED,
    REFRESH_AFTER_SAVE_FAILED,
}

/** The one notice a settings load can produce, with its one retry. */
data class SettingsNotice(
    val message: SettingsMessage,
    val detail: String,
    val tone: SettingsNoticeTone,
    val retry: Boolean = true,
)

/**
 * One inline row error: either the engine's own words, or a client validation
 * the screen formats from resources. Meaning is never carried by colour alone.
 */
data class SettingsRowError(
    val key: String,
    val text: String? = null,
    val validation: SettingInput.Invalid? = null,
)

/** The manual update check's visible state. Nothing runs until the user taps. */
sealed interface UpdateState {
    data object Idle : UpdateState

    data object Checking : UpdateState

    data object UpToDate : UpdateState

    data class Available(val version: String, val url: String) : UpdateState

    data class Failed(val message: String) : UpdateState
}

/**
 * The whole Settings screen state. The view model is its only producer; the
 * screen reads it and never writes engine state itself.
 */
data class SettingsState(
    /** The first load is running and there is no snapshot yet. */
    val loading: Boolean = true,
    /** At least one load succeeded. */
    val loaded: Boolean = false,
    /** The last successful engine snapshot; kept through a failed change. */
    val snapshot: Settings? = null,
    /** The first load failed and there is no snapshot to show. */
    val loadError: String? = null,
    /** Advanced is the one collapsed group; it starts collapsed. */
    val advancedExpanded: Boolean = false,
    /** The key whose change (set or reset) is in flight; all engine rows are locked. */
    val applyingKey: String? = null,
    /** The last inline row error; a new edit or reopen clears it. */
    val rowError: SettingsRowError? = null,
    /** A refresh failure that retained the snapshot, with a retry. */
    val notice: SettingsNotice? = null,
    val update: UpdateState = UpdateState.Idle,
    val appVersion: String = "",
    val engineVersion: String = "",
) {
    val hasSnapshot: Boolean get() = snapshot != null

    val showLoading: Boolean get() = loading && !hasSnapshot

    val showError: Boolean get() = loadError != null && !hasSnapshot

    /** One change at a time: every engine row is disabled while this is true. */
    val applying: Boolean get() = applyingKey != null

    fun isOverridden(key: String): Boolean =
        snapshot?.overridden?.contains(key) == true &&
            SettingsCatalog.byKey(key)?.readOnly != true

    fun errorFor(key: String): SettingsRowError? = rowError?.takeIf { it.key == key }

    fun valueOf(key: String): String {
        val spec = SettingsCatalog.byKey(key) ?: return ""
        val current = snapshot ?: return ""
        return SettingsMapper.valueOf(spec, current)
    }
}

/**
 * The Android and appearance facts the screen needs. They are read by the
 * route (never by the view model) so the view model stays a plain JVM machine.
 */
data class SettingsAppState(
    val theme: ThemeId = ThemeId.LIGHT,
    val accent: KumihoAccent = KumihoAccent.Default,
    /** Accent swatches from the Go palette; empty means the names stand alone. */
    val accentPreviews: Map<KumihoAccent, Color> = emptyMap(),
    val alwaysOn: Boolean = false,
    val lockdown: Boolean = false,
    val notificationsAllowed: Boolean = false,
    /** True when Kumiho is exempt from battery optimization. */
    val batteryUnrestricted: Boolean = false,
    /** A failed system-settings launch, shown inline so it can never crash. */
    val launchError: String? = null,
)

/**
 * Every callback the stateless Settings screen needs. [onOpenSplitTunneling] is
 * the deliberate T14 seam: T13 renders no split row and no dead click, and T14
 * wires the real screen here.
 */
data class SettingsActions(
    val onToggleAdvanced: () -> Unit,
    val onToggle: (key: String, checked: Boolean) -> Unit,
    val onApply: (key: String, raw: String) -> Unit,
    val onReset: (key: String) -> Unit,
    val onClearError: (key: String) -> Unit,
    val onRetryLoad: () -> Unit,
    val onRetryNotice: () -> Unit,
    val onDismissNotice: () -> Unit,
    val onThemeSelected: (ThemeId) -> Unit,
    val onAccentSelected: (KumihoAccent) -> Unit,
    val onCheckForUpdates: () -> Unit,
    val onOpenRelease: (url: String) -> Unit,
    val onOpenNotificationSettings: () -> Unit,
    val onOpenBatterySettings: () -> Unit,
    val onOpenVpnSettings: () -> Unit,
    val onDismissLaunchError: () -> Unit,
    val onOpenSplitTunneling: (() -> Unit)? = null,
)

/** Stable test tags for the Compose tests; all live under `settings_`. */
object SettingsTags {
    const val SCREEN = "settings_screen"
    const val LIST = "settings_list"
    const val LOADING = "settings_loading"
    const val ERROR = "settings_error_screen"
    const val NOTICE = "settings_notice"
    const val NOTICE_RETRY = "settings_notice_retry"
    const val NOTICE_DISMISS = "settings_notice_dismiss"
    const val LAUNCH_ERROR = "settings_launch_error"
    const val LAUNCH_ERROR_DISMISS = "settings_launch_error_dismiss"

    const val GROUP_APP = "settings_group_app"
    const val GROUP_CONNECTION = "settings_group_connection"
    const val GROUP_ANDROID_VPN = "settings_group_android_vpn"
    const val GROUP_ADVANCED = "settings_group_advanced"
    const val ADVANCED_TOGGLE = "settings_advanced_toggle"

    const val THEME_CONTROL = "settings_theme_control"
    const val VERSION_APP = "settings_version_app"
    const val VERSION_ENGINE = "settings_version_engine"

    const val UPDATE_CHECK = "settings_update_check"
    const val UPDATE_STATUS = "settings_update_status"
    const val UPDATE_OPEN = "settings_update_open"
    const val UPDATE_RETRY = "settings_update_retry"

    const val NOTIFICATIONS_STATE = "settings_notifications_state"
    const val NOTIFICATIONS_OPEN = "settings_notifications_open"
    const val BATTERY_STATE = "settings_battery_state"
    const val BATTERY_OPEN = "settings_battery_open"
    const val ALWAYS_ON_STATE = "settings_always_on_state"
    const val ALWAYS_ON_OPEN = "settings_always_on_open"
    const val VPN_EXPLANATION = "settings_vpn_explanation"

    fun vpnSettings(key: String): String = "settings_vpn_settings_$key"

    const val DIALOG = "settings_dialog"
    const val DIALOG_FIELD = "settings_dialog_field"
    const val DIALOG_APPLY = "settings_dialog_apply"
    const val DIALOG_CANCEL = "settings_dialog_cancel"
    const val DIALOG_ERROR = "settings_dialog_error"

    fun themeOption(id: String): String = "settings_theme_$id"

    fun accentOption(id: String): String = "settings_accent_$id"

    fun row(key: String): String = "settings_row_$key"

    fun toggle(key: String): String = "settings_toggle_$key"

    fun value(key: String): String = "settings_value_$key"

    fun override(key: String): String = "settings_override_$key"

    fun reset(key: String): String = "settings_reset_$key"

    fun error(key: String): String = "settings_error_$key"

    fun applying(key: String): String = "settings_applying_$key"

    fun option(option: String): String = "settings_option_$option"
}
