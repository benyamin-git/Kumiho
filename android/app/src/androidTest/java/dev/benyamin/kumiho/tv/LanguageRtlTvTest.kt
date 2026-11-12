package dev.benyamin.kumiho.tv

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.requiredSize
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsFocused
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onFirst
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performKeyInput
import androidx.compose.ui.test.requestFocus
import androidx.compose.ui.test.pressKey
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.unit.dp
import dev.benyamin.kumiho.data.AppLanguage
import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.go.mobile.Mobile
import dev.benyamin.kumiho.ui.dashboard.DashboardActions
import dev.benyamin.kumiho.ui.dashboard.DashboardPhase
import dev.benyamin.kumiho.ui.dashboard.DashboardState
import dev.benyamin.kumiho.ui.dashboard.DashboardTags
import dev.benyamin.kumiho.ui.tv.TvAppShell
import dev.benyamin.kumiho.ui.tv.TvShellTags
import dev.benyamin.kumiho.ui.tv.dashboard.TvDashboardScreen
import dev.benyamin.kumiho.ui.tv.tvRailTag
import dev.benyamin.kumiho.ui.shell.Destination
import dev.benyamin.kumiho.ui.shell.ShellNavigation
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.ui.tv.theme.TvKumihoTheme
import java.util.Locale
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** The ten-foot shell in Persian: the rail is on the right and focus starts there. */
class LanguageRtlTvTest : TvTestBase() {

    private var direction by mutableStateOf(LayoutDirection.Ltr)

    private fun show(language: AppLanguage) {
        rule.setContent {
            val base = LocalContext.current
            val baseConfiguration = LocalConfiguration.current
            val localized = remember(language, baseConfiguration) {
                val locale = Locale.forLanguageTag(language.tag)
                val configuration = android.content.res.Configuration(
                    baseConfiguration,
                ).apply {
                    setLocale(locale)
                    setLayoutDirection(locale)
                }
                base.createConfigurationContext(configuration)
            }
            CompositionLocalProvider(
                LocalContext provides localized,
                LocalConfiguration provides localized.resources.configuration,
                LocalLayoutDirection provides if (language == AppLanguage.PERSIAN) {
                    LayoutDirection.Rtl
                } else {
                    LayoutDirection.Ltr
                },
            ) {
                direction = LocalLayoutDirection.current
                TvKumihoTheme(
                    colors = PaletteMapping.fromJson(
                        Mobile.palette(ThemeId.LIGHT.id, KumihoAccent.BLUE.id),
                        ThemeId.LIGHT,
                        KumihoAccent.BLUE,
                    ),
                    language = language,
                ) {
                    Box(modifier = Modifier.requiredSize(960.dp, 540.dp)) {
                        TvAppShell(
                            nav = remember { ShellNavigation() },
                            home = {},
                            locations = {},
                            settings = {},
                            logs = {},
                            diagnostics = {},
                        )
                    }
                }
            }
        }
        rule.waitForIdle()
    }

    @Test
    fun persianMakesTheTvShellRtl() {
        show(AppLanguage.PERSIAN)

        assertEquals(LayoutDirection.Rtl, direction)
        rule.onAllNodesWithText("خانه").onFirst().assertIsDisplayed()
    }

    @Test
    fun persianRailIsAtTheRightEdge() {
        show(AppLanguage.PERSIAN)

        rule.onNodeWithTag(TvShellTags.RAIL).assertExists()
        val rail = rule.onNodeWithTag(TvShellTags.RAIL).fetchSemanticsNode().boundsInRoot
        val root = rule.onRoot().fetchSemanticsNode().boundsInRoot
        assertTrue("The TV rail must be on the right half in RTL", rail.center.x > root.center.x)
    }

    @Test
    fun theFirstRailDestinationIsStillHome() {
        show(AppLanguage.PERSIAN)

        rule.onNodeWithTag(tvRailTag(Destination.Home)).assertExists()
        assertEquals("home", Destination.tabs.first().route)
    }

    @Test
    fun persianDpadLeftMovesFromPowerToTheLocationCard() {
        rule.setContent {
            val base = LocalContext.current
            val locale = Locale.forLanguageTag("fa")
            val configuration = android.content.res.Configuration(
                LocalConfiguration.current,
            ).apply {
                setLocale(locale)
                setLayoutDirection(locale)
            }
            val localized = base.createConfigurationContext(configuration)
            CompositionLocalProvider(
                LocalContext provides localized,
                LocalConfiguration provides localized.resources.configuration,
                LocalLayoutDirection provides LayoutDirection.Rtl,
            ) {
                TvKumihoTheme(
                    colors = PaletteMapping.fromJson(
                        Mobile.palette(ThemeId.LIGHT.id, KumihoAccent.BLUE.id),
                        ThemeId.LIGHT,
                        KumihoAccent.BLUE,
                    ),
                    language = AppLanguage.PERSIAN,
                ) {
                    TvDashboardScreen(
                        state = DashboardState(
                            phase = DashboardPhase.OFF,
                            status = EngineStatus(
                                state = "OFF",
                                authenticated = true,
                                email = null,
                                since = 0,
                                tokenExpires = 0,
                                passExpires = 0,
                                killSwitch = false,
                                autoconnect = false,
                                location = "gb",
                                server = "lon1.example.net:443",
                                proxyOnly = false,
                                quota = null,
                                dns = null,
                                ipv6 = null,
                                exitIp = null,
                                exitCountry = null,
                                totals = null,
                                rates = null,
                            ),
                        ),
                        actions = DashboardActions(
                            onPower = {},
                            onOpenOptions = {},
                            onCloseOptions = {},
                            onSelectRecommended = {},
                            onSelectSaved = {},
                            onToggleCountry = {},
                            onSelectCity = { _, _ -> },
                            onToggleProxyOnly = {},
                            onConnectFromOptions = {},
                            onReloadLocations = {},
                            onOpenVpnSettings = {},
                            onConfirmLockdownDisconnect = {},
                            onCancelLockdown = {},
                            onDismissNotice = {},
                            onRetryNotice = {},
                        ),
                    )
                }
            }
        }
        rule.waitForIdle()

        rule.onNodeWithTag(DashboardTags.POWER).requestFocus()
        rule.waitForIdle()
        rule.onNodeWithTag(DashboardTags.POWER).assertIsFocused()
        rule.onNodeWithTag(DashboardTags.POWER).performKeyInput { pressKey(Key.DirectionLeft) }
        rule.waitForIdle()
        rule.onNodeWithTag(DashboardTags.CARD_LOCATION).assertIsFocused()
    }
}
