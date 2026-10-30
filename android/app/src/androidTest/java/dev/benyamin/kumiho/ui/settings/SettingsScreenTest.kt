package dev.benyamin.kumiho.ui.settings

import androidx.activity.ComponentActivity
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertContentDescriptionEquals
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.assertTextEquals
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.test.performTextReplacement
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.data.BypassError
import dev.benyamin.kumiho.engine.Settings
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.vpn.SplitMode
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * The Settings screen as it is drawn: the real Compose tree in the real light
 * palette, driven only by [SettingsState]/[SettingsAppState] and recording
 * callbacks.
 *
 * Every assertion goes through a stable `settings_*` tag or a resource string.
 * Nothing here reaches the engine, the service, the filesystem or the network.
 */
class SettingsScreenTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private val calls = mutableListOf<String>()

    private fun show(
        state: SettingsState = state(),
        appState: SettingsAppState = appState(),
        actions: SettingsActions = actions(),
    ) {
        rule.setContent {
            KumihoTheme(
                colors = PaletteMapping.fromJson(
                    dev.benyamin.kumiho.go.mobile.Mobile.palette(ThemeId.LIGHT.id, KumihoAccent.BLUE.id),
                    ThemeId.LIGHT,
                    KumihoAccent.BLUE,
                ),
            ) {
                SettingsScreen(state = state, appState = appState, actions = actions)
            }
        }
        rule.waitForIdle()
    }

    private fun text(id: Int): String = rule.activity.getString(id)

    private fun scrollTo(tag: String) {
        rule.onNodeWithTag(SettingsTags.LIST).performScrollToNode(hasTestTag(tag))
        rule.waitForIdle()
    }

    // ---- reachability ----------------------------------------------------

    @Test
    fun everyEngineKeyIsReachableExactlyOnce() {
        show(state(advancedExpanded = true))

        for (key in SettingsCatalog.engineKeys) {
            scrollTo(SettingsTags.row(key))
            rule.onAllNodesWithTag(SettingsTags.row(key))
                .assertCountEquals(1)
        }
    }

    @Test
    fun advancedStartsCollapsedAndTheHeaderIsTheOnlyToggle() {
        show()

        rule.onNodeWithTag(SettingsTags.row("edge_address")).assertDoesNotExist()
        rule.onNodeWithTag(SettingsTags.row("log_level")).assertDoesNotExist()

        scrollTo(SettingsTags.ADVANCED_TOGGLE)
        rule.onNodeWithTag(SettingsTags.ADVANCED_TOGGLE).assertExists().performClick()

        assertTrue(calls.contains("toggle-advanced"))
    }

    @Test
    fun expandedAdvancedRevealsItsRows() {
        show(state(advancedExpanded = true))

        for (key in SettingsCatalog.specsIn(SettingsGroup.ADVANCED).map { it.key }) {
            scrollTo(SettingsTags.row(key))
            rule.onNodeWithTag(SettingsTags.row(key)).assertExists()
        }
    }

    // ---- rows and inputs -------------------------------------------------

    @Test
    fun aBooleanRowWritesThroughAndNamesItsState() {
        show()

        scrollTo(SettingsTags.row("exit_check"))
        rule.onNodeWithTag(SettingsTags.value("exit_check")).assertTextEquals(text(R.string.settings_on))
        rule.onNodeWithTag(SettingsTags.toggle("exit_check")).performClick()

        assertEquals(listOf("clear-exit_check", "toggle-exit_check-false"), calls)
    }

    @Test
    fun aBooleanSwitchIsNamedForTalkBack() {
        show()

        scrollTo(SettingsTags.row("exit_check"))
        rule.onNodeWithTag(SettingsTags.toggle("exit_check"))
            .assertContentDescriptionEquals(text(R.string.settings_label_exit_check))
    }

    @Test
    fun aValueRowShowsItsCurrentValueAndOpensAnEditor() {
        show()

        scrollTo(SettingsTags.row("mtu"))
        rule.onNodeWithTag(SettingsTags.value("mtu"), useUnmergedTree = true)
            .assertTextEquals("1500")
        rule.onNodeWithTag(SettingsTags.row("mtu")).performClick()

        rule.onNodeWithTag(SettingsTags.DIALOG).assertExists()
        rule.onNodeWithTag(SettingsTags.DIALOG_FIELD).assertExists()
    }

    @Test
    fun anInvalidNumberStaysInTheEditorWithTheReason() {
        show()

        scrollTo(SettingsTags.row("mtu"))
        rule.onNodeWithTag(SettingsTags.row("mtu")).performClick()
        rule.onNodeWithTag(SettingsTags.DIALOG_FIELD).performTextReplacement("abc")
        rule.onNodeWithTag(SettingsTags.DIALOG_APPLY).performClick()

        rule.onNodeWithTag(SettingsTags.DIALOG).assertExists()
        rule.onNodeWithTag(SettingsTags.DIALOG_ERROR)
            .assertTextEquals(text(R.string.settings_validation_number))
        assertTrue("an invalid value never reaches the engine", calls.none { it.startsWith("apply-") })

        rule.onNodeWithTag(SettingsTags.DIALOG_CANCEL).performClick()
        rule.onNodeWithTag(SettingsTags.DIALOG).assertDoesNotExist()
    }

    @Test
    fun anOutOfRangeNumberNamesItsRange() {
        show()

        scrollTo(SettingsTags.row("socks_port"))
        rule.onNodeWithTag(SettingsTags.row("socks_port")).performClick()
        rule.onNodeWithTag(SettingsTags.DIALOG_FIELD).performTextReplacement("70000")
        rule.onNodeWithTag(SettingsTags.DIALOG_APPLY).performClick()

        rule.onNodeWithTag(SettingsTags.DIALOG_ERROR)
            .assertTextEquals(rule.activity.getString(R.string.settings_validation_range, 1, 65535))
        assertTrue(calls.none { it.startsWith("apply-") })
    }

    @Test
    fun aValidEditIsAppliedWithTheNormalizedValue() {
        show(state(advancedExpanded = true))

        scrollTo(SettingsTags.row("exclude_cidrs"))
        rule.onNodeWithTag(SettingsTags.row("exclude_cidrs")).performClick()
        rule.onNodeWithTag(SettingsTags.DIALOG_FIELD)
            .performTextReplacement(" 10.0.0.0/8 , 192.168.1.5 ")
        rule.onNodeWithTag(SettingsTags.DIALOG_APPLY).performClick()

        assertEquals(listOf("clear-exclude_cidrs", "apply-exclude_cidrs-10.0.0.0/8, 192.168.1.5/32"), calls)
        rule.onNodeWithTag(SettingsTags.DIALOG).assertDoesNotExist()
    }

    @Test
    fun anEnumEditorChoosesOneNamedOption() {
        show(state(advancedExpanded = true))

        scrollTo(SettingsTags.row("log_level"))
        rule.onNodeWithTag(SettingsTags.row("log_level")).performClick()
        rule.onNodeWithTag(SettingsTags.option("warn")).performClick()
        rule.onNodeWithTag(SettingsTags.DIALOG_APPLY).performClick()

        assertEquals(listOf("clear-log_level", "apply-log_level-warn"), calls)
    }

    @Test
    fun anOverriddenRowShowsTheChipAndResets() {
        show(state(snapshot = SETTINGS.copy(overridden = listOf("mtu"))))

        scrollTo(SettingsTags.row("mtu"))
        rule.onNodeWithTag(SettingsTags.override("mtu")).assertExists()
        rule.onNodeWithText(text(R.string.settings_overridden)).assertExists()
        rule.onNodeWithTag(SettingsTags.reset("mtu")).performClick()

        assertEquals(listOf("clear-mtu", "reset-mtu"), calls)
    }

    @Test
    fun theApplyingRowSaysSoAndEveryOtherRowIsLocked() {
        show(state(applyingKey = "mtu"))

        scrollTo(SettingsTags.row("mtu"))
        rule.onNodeWithTag(SettingsTags.applying("mtu"))
            .assertTextEquals(rule.activity.getString(R.string.settings_applying))
        rule.onNodeWithTag(SettingsTags.value("mtu")).assertDoesNotExist()

        scrollTo(SettingsTags.row("exit_check"))
        rule.onNodeWithTag(SettingsTags.toggle("exit_check")).assertIsNotEnabled()
    }

    @Test
    fun anInlineEngineErrorIsVisibleBesideItsRow() {
        show(
            state(
                rowError = SettingsRowError(
                    key = "mtu",
                    text = "mtu: out of range: 10",
                ),
            ),
        )

        scrollTo(SettingsTags.row("mtu"))
        rule.onNodeWithTag(SettingsTags.error("mtu")).assertExists()
        rule.onNodeWithText(text(R.string.settings_validation_apply_failed)).assertExists()
        rule.onNodeWithText("mtu: out of range: 10").assertExists()
    }

    // ---- Android VPN -----------------------------------------------------

    @Test
    fun androidRowsShowLockdownAndAlwaysOnNeverTheEngineValues() {
        show(
            appState = appState(alwaysOn = false, lockdown = true),
            state = state(
                snapshot = SETTINGS.copy(killSwitch = "on", autoconnect = "on"),
            ),
        )

        scrollTo(SettingsTags.row("kill_switch"))
        rule.onNodeWithTag(SettingsTags.value("kill_switch"))
            .assertTextEquals(text(R.string.settings_kill_switch_lockdown))

        scrollTo(SettingsTags.row("autoconnect"))
        rule.onNodeWithTag(SettingsTags.value("autoconnect"))
            .assertTextEquals(text(R.string.settings_autoconnect_off))
    }

    @Test
    fun eachAndroidRowOpensTheSystemVpnSettings() {
        show()

        scrollTo(SettingsTags.row("kill_switch"))
        rule.onNodeWithTag(SettingsTags.vpnSettings("kill_switch")).performClick()
        scrollTo(SettingsTags.row("autoconnect"))
        rule.onNodeWithTag(SettingsTags.vpnSettings("autoconnect")).performClick()

        assertEquals(listOf("vpn-settings", "vpn-settings"), calls)
    }

    // ---- app section -----------------------------------------------------

    @Test
    fun themeAndAccentApplyImmediately() {
        show()

        rule.onNodeWithTag(SettingsTags.themeOption(ThemeId.DARK.id)).performClick()
        rule.onNodeWithTag(SettingsTags.accentOption(KumihoAccent.TEAL.id)).performClick()

        assertEquals(listOf("theme-dark", "accent-teal"), calls)
    }

    @Test
    fun bothVersionsAreVisible() {
        show()

        rule.onNodeWithTag(SettingsTags.VERSION_APP)
            .assertTextEquals(rule.activity.getString(R.string.settings_version_app, "0.5.0-debug"))
        rule.onNodeWithTag(SettingsTags.VERSION_ENGINE)
            .assertTextEquals(rule.activity.getString(R.string.settings_version_engine, "0.5.0"))
    }

    @Test
    fun anAvailableReleaseOnlyOpensItsPage() {
        show(state(update = UpdateState.Available("0.6.0", "https://example.test/release")))

        scrollTo(SettingsTags.UPDATE_STATUS)
        rule.onNodeWithTag(SettingsTags.UPDATE_STATUS)
            .assertTextEquals(rule.activity.getString(R.string.settings_update_available, "0.6.0"))
        rule.onNodeWithTag(SettingsTags.UPDATE_OPEN).performClick()

        assertEquals(listOf("open-release-https://example.test/release"), calls)
    }

    @Test
    fun aCheckingUpdateCannotBeTappedTwice() {
        show(state(update = UpdateState.Checking))

        scrollTo(SettingsTags.UPDATE_CHECK)
        rule.onNodeWithTag(SettingsTags.UPDATE_CHECK).assertIsNotEnabled()
        rule.onNodeWithTag(SettingsTags.UPDATE_STATUS).assertExists()
    }

    @Test
    fun aFailedUpdateOffersARetry() {
        show(state(update = UpdateState.Failed("no route to host")))

        scrollTo(SettingsTags.UPDATE_STATUS)
        rule.onNodeWithText("no route to host").assertExists()
        rule.onNodeWithTag(SettingsTags.UPDATE_RETRY).performClick()

        assertEquals(listOf("check-updates"), calls)
    }

    @Test
    fun theAndroidStateRowsOpenTheirSystemPages() {
        show()

        rule.onNodeWithTag(SettingsTags.NOTIFICATIONS_OPEN).performClick()
        scrollTo(SettingsTags.BATTERY_OPEN)
        rule.onNodeWithTag(SettingsTags.BATTERY_OPEN).performClick()
        scrollTo(SettingsTags.ALWAYS_ON_OPEN)
        rule.onNodeWithTag(SettingsTags.ALWAYS_ON_OPEN).performClick()

        assertEquals(
            listOf("notification-settings", "battery-settings", "vpn-settings"),
            calls,
        )
    }

    @Test
    fun theNotificationBatteryAndAlwaysOnStatesAreNamed() {
        show(
            appState = appState(
                notificationsAllowed = false,
                battery = BatteryOptimization.UNRESTRICTED,
                alwaysOn = true,
            ),
        )

        rule.onNodeWithTag(SettingsTags.NOTIFICATIONS_STATE)
            .assertTextEquals(text(R.string.settings_notifications_missing))
        scrollTo(SettingsTags.BATTERY_STATE)
        rule.onNodeWithTag(SettingsTags.BATTERY_STATE)
            .assertTextEquals(text(R.string.settings_battery_unrestricted))
        scrollTo(SettingsTags.ALWAYS_ON_STATE)
        rule.onNodeWithTag(SettingsTags.ALWAYS_ON_STATE)
            .assertTextEquals(text(R.string.settings_always_on_state_on))
    }

    // ---- split tunneling shortcut ----------------------------------------

    @Test
    fun theSplitRowShowsOffAndOpensTheNestedScreen() {
        show(appState = appState(splitMode = SplitMode.OFF, splitAppCount = 0))

        scrollTo(SettingsTags.SPLIT_ROW)
        rule.onNodeWithTag(SettingsTags.SPLIT_SUMMARY, useUnmergedTree = true)
            .assertTextEquals(text(R.string.settings_split_summary_off))
        rule.onNodeWithTag(SettingsTags.SPLIT_ROW).performClick()

        assertEquals(listOf("split"), calls)
    }

    @Test
    fun theSplitRowSummarizesTheExcludedCount() {
        show(appState = appState(splitMode = SplitMode.EXCLUDE, splitAppCount = 3))
        scrollTo(SettingsTags.SPLIT_ROW)
        rule.onNodeWithTag(SettingsTags.SPLIT_SUMMARY, useUnmergedTree = true).assertTextEquals(
            rule.activity.resources.getQuantityString(
                R.plurals.settings_split_summary_exclude,
                3,
                3,
            ),
        )
    }

    @Test
    fun theSplitRowSummarizesTheIncludedCount() {
        show(appState = appState(splitMode = SplitMode.INCLUDE, splitAppCount = 2))
        scrollTo(SettingsTags.SPLIT_ROW)
        rule.onNodeWithTag(SettingsTags.SPLIT_SUMMARY, useUnmergedTree = true).assertTextEquals(
            rule.activity.resources.getQuantityString(
                R.plurals.settings_split_summary_include,
                2,
                2,
            ),
        )
    }

    @Test
    fun theSplitRowStaysReachableWhileEngineSettingsLoad() {
        show(state(loading = true, loaded = false, snapshot = null))

        scrollTo(SettingsTags.SPLIT_ROW)
        rule.onNodeWithTag(SettingsTags.SPLIT_ROW).assertExists()
        rule.onNodeWithTag(SettingsTags.GROUP_CONNECTION).assertDoesNotExist()
    }

    @Test
    fun theSplitRowStaysReachableWhenEngineSettingsFail() {
        show(state(loading = false, loaded = false, snapshot = null, loadError = "daemon offline"))

        scrollTo(SettingsTags.SPLIT_ROW)
        rule.onNodeWithTag(SettingsTags.SPLIT_ROW).assertExists()
        rule.onNodeWithTag(SettingsTags.GROUP_CONNECTION).assertDoesNotExist()
    }

    // ---- keep connections outside the tunnel ----------------------------

    @Test
    fun theBypassRowShowsOffAndIsEnabled() {
        show()

        scrollTo(SettingsTags.BYPASS_ROW)
        rule.onNodeWithText(text(R.string.settings_bypass_title)).assertExists()
        rule.onNodeWithText(text(R.string.settings_bypass_hint)).assertExists()
        rule.onNodeWithTag(SettingsTags.BYPASS_VALUE)
            .assertTextEquals(text(R.string.settings_off))
        rule.onNodeWithTag(SettingsTags.BYPASS_SWITCH).assertIsEnabled()
    }

    @Test
    fun theBypassSwitchWritesThroughAndNamesOn() {
        show(appState = appState(bypassEnabled = true))

        scrollTo(SettingsTags.BYPASS_ROW)
        rule.onNodeWithTag(SettingsTags.BYPASS_VALUE)
            .assertTextEquals(text(R.string.settings_on))
        rule.onNodeWithTag(SettingsTags.BYPASS_SWITCH).performClick()

        assertEquals(listOf("bypass-false"), calls)
    }

    @Test
    fun theBypassSwitchIsDisabledWhileLoading() {
        show(appState = appState(bypassLoaded = false))

        scrollTo(SettingsTags.BYPASS_ROW)
        rule.onNodeWithTag(SettingsTags.BYPASS_SWITCH).assertIsNotEnabled()
        rule.onNodeWithTag(SettingsTags.BYPASS_VALUE)
            .assertTextEquals(text(R.string.settings_off))
    }

    @Test
    fun theBypassReconnectWarningAppears() {
        show(appState = appState(bypassNeedsReconnect = true))

        scrollTo(SettingsTags.BYPASS_ROW)
        rule.onNodeWithTag(SettingsTags.BYPASS_RECONNECT).assertExists()
        rule.onNodeWithText(text(R.string.settings_bypass_reconnect)).assertExists()
    }

    @Test
    fun theBypassReconnectWarningIsAbsentByDefault() {
        show()

        scrollTo(SettingsTags.BYPASS_ROW)
        rule.onNodeWithTag(SettingsTags.BYPASS_RECONNECT).assertDoesNotExist()
    }

    @Test
    fun theBypassLockdownWarningIsShown() {
        show(appState = appState(lockdown = true))

        scrollTo(SettingsTags.BYPASS_ROW)
        rule.onNodeWithTag(SettingsTags.BYPASS_LOCKDOWN).assertExists()
        rule.onNodeWithText(text(R.string.settings_bypass_lockdown)).assertExists()
        rule.onNodeWithTag(SettingsTags.BYPASS_SWITCH).assertIsEnabled().performClick()
        assertEquals(listOf("bypass-true"), calls)
    }

    @Test
    fun aBypassLoadErrorIsShown() {
        show(appState = appState(bypassError = BypassError.LOAD, bypassLoaded = true))

        scrollTo(SettingsTags.BYPASS_ROW)
        rule.onNodeWithTag(SettingsTags.BYPASS_ERROR).assertExists()
        rule.onNodeWithText(text(R.string.settings_bypass_load_failed)).assertExists()
        rule.onNodeWithTag(SettingsTags.BYPASS_SWITCH).assertIsEnabled()
    }

    @Test
    fun aBypassSaveErrorIsShown() {
        show(appState = appState(bypassError = BypassError.SAVE, bypassEnabled = true))

        scrollTo(SettingsTags.BYPASS_ROW)
        rule.onNodeWithTag(SettingsTags.BYPASS_ERROR).assertExists()
        rule.onNodeWithText(text(R.string.settings_bypass_save_failed)).assertExists()
    }

    @Test
    fun theBypassRowStaysReachableWhenEngineSettingsFail() {
        show(state(loading = false, loaded = false, snapshot = null, loadError = "daemon offline"))

        scrollTo(SettingsTags.BYPASS_ROW)
        rule.onNodeWithTag(SettingsTags.BYPASS_ROW).assertExists()
        rule.onNodeWithTag(SettingsTags.GROUP_CONNECTION).assertDoesNotExist()
    }

    // ---- notices and content states -------------------------------------

    @Test
    fun aRefreshNoticeOffersItsOneRetryAndADismiss() {
        show(
            state(
                notice = SettingsNotice(
                    message = SettingsMessage.REFRESH_FAILED,
                    detail = "the engine did not answer",
                    tone = SettingsNoticeTone.WARNING,
                ),
            ),
        )

        rule.onNodeWithTag(SettingsTags.NOTICE).assertExists()
        rule.onNodeWithText(text(R.string.settings_message_refresh_failed)).assertExists()
        rule.onNodeWithTag(SettingsTags.NOTICE_RETRY).performClick()
        rule.onNodeWithTag(SettingsTags.NOTICE_DISMISS).performClick()

        assertEquals(listOf("retry-notice", "dismiss-notice"), calls)
    }

    @Test
    fun aFailedSystemLaunchIsAnInlineDismissibleMessage() {
        show(appState = appState(launchError = "No activity found"))

        rule.onNodeWithTag(SettingsTags.LAUNCH_ERROR).assertExists()
        rule.onNodeWithText("No activity found").assertExists()
        rule.onNodeWithTag(SettingsTags.LAUNCH_ERROR_DISMISS).performClick()

        assertEquals(listOf("dismiss-launch-error"), calls)
    }

    @Test
    fun theLoadingStateKeepsTheAppSectionAvailable() {
        show(state(loading = true, loaded = false, snapshot = null))

        rule.onNodeWithTag(SettingsTags.LOADING).assertExists()
        rule.onNodeWithTag(SettingsTags.LIST).assertExists()
        rule.onNodeWithTag(SettingsTags.GROUP_APP).assertExists()
        rule.onNodeWithTag(SettingsTags.THEME_CONTROL).assertExists()
        rule.onNodeWithTag(SettingsTags.GROUP_CONNECTION).assertDoesNotExist()
        rule.onNodeWithTag(SettingsTags.GROUP_ADVANCED).assertDoesNotExist()
    }

    @Test
    fun theInitialErrorKeepsTheAppSectionAndOffersARetry() {
        show(state(loading = false, loaded = false, snapshot = null, loadError = "daemon offline"))

        rule.onNodeWithTag(SettingsTags.ERROR).assertExists()
        rule.onNodeWithText("daemon offline").assertExists()
        rule.onNodeWithText(text(R.string.settings_load_failed_title)).assertExists()
        rule.onNodeWithTag(SettingsTags.LIST).assertExists()
        rule.onNodeWithTag(SettingsTags.GROUP_APP).assertExists()
        rule.onNodeWithTag(SettingsTags.THEME_CONTROL).assertExists()
        rule.onNodeWithTag(SettingsTags.GROUP_CONNECTION).assertDoesNotExist()
        rule.onNodeWithText(text(R.string.settings_retry)).performClick()

        assertEquals(listOf("retry-load"), calls)
    }

    // ---- fixtures --------------------------------------------------------

    private fun state(
        loading: Boolean = false,
        loaded: Boolean = true,
        snapshot: Settings? = SETTINGS,
        loadError: String? = null,
        advancedExpanded: Boolean = false,
        applyingKey: String? = null,
        rowError: SettingsRowError? = null,
        notice: SettingsNotice? = null,
        update: UpdateState = UpdateState.Idle,
    ) = SettingsState(
        loading = loading,
        loaded = loaded,
        snapshot = snapshot,
        loadError = loadError,
        advancedExpanded = advancedExpanded,
        applyingKey = applyingKey,
        rowError = rowError,
        notice = notice,
        update = update,
        appVersion = "0.5.0-debug",
        engineVersion = "0.5.0",
    )

    private fun appState(
        alwaysOn: Boolean = true,
        lockdown: Boolean = false,
        notificationsAllowed: Boolean = true,
        battery: BatteryOptimization = BatteryOptimization.UNKNOWN,
        launchError: String? = null,
        splitMode: SplitMode = SplitMode.OFF,
        splitAppCount: Int = 0,
        bypassEnabled: Boolean = false,
        bypassLoaded: Boolean = true,
        bypassNeedsReconnect: Boolean = false,
        bypassError: BypassError? = null,
    ) = SettingsAppState(
        theme = ThemeId.LIGHT,
        accent = KumihoAccent.BLUE,
        accentPreviews = KumihoAccent.entries.associateWith { Color(0xFF3366CC) },
        alwaysOn = alwaysOn,
        lockdown = lockdown,
        notificationsAllowed = notificationsAllowed,
        battery = battery,
        launchError = launchError,
        splitMode = splitMode,
        splitAppCount = splitAppCount,
        bypassEnabled = bypassEnabled,
        bypassLoaded = bypassLoaded,
        bypassNeedsReconnect = bypassNeedsReconnect,
        bypassError = bypassError,
    )

    private fun actions() = SettingsActions(
        onToggleAdvanced = { calls += "toggle-advanced" },
        onToggle = { key, checked -> calls += "toggle-$key-$checked" },
        onApply = { key, raw -> calls += "apply-$key-$raw" },
        onReset = { calls += "reset-$it" },
        onClearError = { calls += "clear-$it" },
        onRetryLoad = { calls += "retry-load" },
        onRetryNotice = { calls += "retry-notice" },
        onDismissNotice = { calls += "dismiss-notice" },
        onThemeSelected = { calls += "theme-${it.id}" },
        onAccentSelected = { calls += "accent-${it.id}" },
        onCheckForUpdates = { calls += "check-updates" },
        onOpenRelease = { calls += "open-release-$it" },
        onOpenNotificationSettings = { calls += "notification-settings" },
        onOpenBatterySettings = { calls += "battery-settings" },
        onOpenVpnSettings = { calls += "vpn-settings" },
        onDismissLaunchError = { calls += "dismiss-launch-error" },
        onToggleBypass = { calls += "bypass-$it" },
        onOpenSplitTunneling = { calls += "split" },
    )

    private companion object {
        fun settings() = Settings(
            socksPort = 1080,
            mtu = 1500,
            exitCheck = true,
            dohProvider = "automatic",
            dnsUpstream = "9.9.9.9",
            edgeAddress = "203.0.113.7",
            upstreamProxy = "http://proxy.example:3128",
            excludeCidrs = listOf("10.0.0.0/8"),
            allowLan = false,
            logLevel = "info",
            redactTargets = true,
            killSwitch = "on",
            autoconnect = "on",
            killSwitchEffective = true,
            autoconnectEffective = true,
            quotaPollMinutes = 30,
            overridden = emptyList(),
            tlsSplit = "first-byte",
            httpProxy = false,
            httpProxyPort = 8080,
            solveChallenges = true,
            keepaliveProfile = "standard",
        )

        val SETTINGS = settings()
    }
}
