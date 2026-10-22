package dev.benyamin.kumiho.ui.settings

import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertContentDescriptionEquals
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.assertTextEquals
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.test.performTextReplacement
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.unit.dp
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.data.InstalledApp
import dev.benyamin.kumiho.ui.theme.KumihoAccent
import dev.benyamin.kumiho.ui.theme.KumihoTheme
import dev.benyamin.kumiho.ui.theme.PaletteMapping
import dev.benyamin.kumiho.ui.theme.ThemeId
import dev.benyamin.kumiho.vpn.SplitMode
import dev.benyamin.kumiho.vpn.SplitSettings
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

/**
 * The split screen as it is drawn: the real Compose tree in the light palette,
 * a mutable fake state, and a deterministic icon. Nothing here reads packages,
 * DataStore, the service or the network.
 */
class SplitTunnelScreenTest {

    @get:Rule
    val rule = createAndroidComposeRule<ComponentActivity>()

    private val calls = mutableListOf<String>()
    private var screenState by mutableStateOf(uiState())

    private fun show(
        state: SplitTunnelUiState = uiState(),
        reconnectRequired: Boolean = false,
    ) {
        screenState = state
        rule.setContent {
            KumihoTheme(
                colors = PaletteMapping.fromJson(
                    dev.benyamin.kumiho.go.mobile.Mobile.palette(
                        ThemeId.LIGHT.id,
                        KumihoAccent.BLUE.id,
                    ),
                    ThemeId.LIGHT,
                    KumihoAccent.BLUE,
                ),
            ) {
                SplitTunnelScreen(
                    state = screenState,
                    reconnectRequired = reconnectRequired,
                    actions = SplitTunnelActions(
                        onModeSelected = { mode ->
                            calls += "mode-${mode.name}"
                            screenState = screenState.copy(
                                settings = screenState.settings.copy(mode = mode),
                            )
                        },
                        onTogglePackage = { packageName ->
                            calls += "toggle-$packageName"
                            val packages = screenState.settings.packages
                            screenState = screenState.copy(
                                settings = screenState.settings.copy(
                                    packages = if (packageName in packages) {
                                        packages - packageName
                                    } else {
                                        packages + packageName
                                    },
                                ),
                            )
                        },
                        onSearchChange = { query ->
                            calls += "search-$query"
                            screenState = screenState.copy(search = query)
                        },
                        onClearSearch = {
                            calls += "clear-search"
                            screenState = screenState.copy(search = "")
                        },
                        onShowSystem = { show ->
                            calls += "system-$show"
                            screenState = screenState.copy(showSystem = show)
                        },
                        onRetryRepository = { calls += "retry-repository" },
                        onRetryApps = { calls += "retry-apps" },
                        onBack = { calls += "back" },
                    ),
                    appIcon = { FakeAppIcon(it) },
                )
            }
        }
        rule.waitForIdle()
    }

    private fun scrollTo(tag: String) {
        rule.onNodeWithTag(SplitTunnelTags.LIST).performScrollToNode(hasTestTag(tag))
        rule.waitForIdle()
    }

    private fun text(id: Int): String = rule.activity.getString(id)

    // ---- modes -----------------------------------------------------------

    @Test
    fun everyModeIsVisibleAndSelectingOneIsARequest() {
        show()

        rule.onNodeWithTag(SplitTunnelTags.mode(SplitMode.OFF)).assertExists()
        rule.onNodeWithTag(SplitTunnelTags.mode(SplitMode.EXCLUDE)).assertExists()
        rule.onNodeWithTag(SplitTunnelTags.mode(SplitMode.INCLUDE)).assertExists()

        rule.onNodeWithTag(SplitTunnelTags.mode(SplitMode.EXCLUDE)).performClick()
        assertEquals(listOf("mode-EXCLUDE"), calls)
        rule.onNodeWithTag(SplitTunnelTags.OFF_HINT).assertDoesNotExist()
    }

    @Test
    fun offExplainsThatEveryAppUsesTheTunnel() {
        show()

        rule.onNodeWithTag(SplitTunnelTags.OFF_HINT).assertExists()
        rule.onNodeWithText(text(R.string.split_off_hint)).assertExists()
        rule.onNodeWithTag(SplitTunnelTags.COUNT)
            .assertTextEquals(text(R.string.split_off_pick_mode))
    }

    @Test
    fun anEmptyIncludeWarnsInsteadOfClaimingNothingTunnels() {
        show(
            uiState(
                settings = SplitSettings(mode = SplitMode.INCLUDE),
            ),
        )

        rule.onNodeWithTag(SplitTunnelTags.EMPTY_INCLUDE).assertExists()
        rule.onNodeWithText(text(R.string.split_empty_include_warning)).assertExists()
    }

    @Test
    fun aNonEmptyIncludeDoesNotWarn() {
        show(
            uiState(
                settings = SplitSettings(SplitMode.INCLUDE, setOf(CHROME.packageName)),
            ),
        )

        rule.onNodeWithTag(SplitTunnelTags.EMPTY_INCLUDE).assertDoesNotExist()
    }

    @Test
    fun theSelectedCountNamesTheSelection() {
        show(
            uiState(
                settings = SplitSettings(
                    SplitMode.INCLUDE,
                    setOf(CHROME.packageName, MAPS.packageName),
                ),
            ),
        )

        scrollTo(SplitTunnelTags.COUNT)
        rule.onNodeWithTag(SplitTunnelTags.COUNT).assertTextEquals(
            rule.activity.resources.getQuantityString(R.plurals.split_selected_apps, 2, 2),
        )
    }

    // ---- picker ----------------------------------------------------------

    @Test
    fun theSearchFieldFiltersAndClears() {
        show(uiState(settings = SplitSettings(SplitMode.EXCLUDE)))

        scrollTo(SplitTunnelTags.SEARCH)
        rule.onNodeWithTag(SplitTunnelTags.SEARCH).performTextReplacement("maps")
        rule.waitForIdle()

        assertTrue(calls.contains("search-maps"))
        rule.onNodeWithTag(SplitTunnelTags.appRow(MAPS.packageName)).assertExists()
        rule.onNodeWithTag(SplitTunnelTags.appRow(CHROME.packageName)).assertDoesNotExist()

        rule.onNodeWithTag(SplitTunnelTags.SEARCH_CLEAR).performClick()
        rule.waitForIdle()
        assertTrue(calls.contains("clear-search"))
        rule.onNodeWithTag(SplitTunnelTags.appRow(CHROME.packageName)).assertExists()
    }

    @Test
    fun theSystemAppsToggleRevealsNonLaunchableApps() {
        show(uiState(settings = SplitSettings(SplitMode.EXCLUDE)))

        scrollTo(SplitTunnelTags.SYSTEM_TOGGLE)
        rule.onNodeWithTag(SplitTunnelTags.SYSTEM_TOGGLE)
            .assertContentDescriptionEquals(text(R.string.split_system_apps))
        rule.onNodeWithTag(SplitTunnelTags.appRow(SETTINGS.packageName)).assertDoesNotExist()
        rule.onNodeWithTag(SplitTunnelTags.SYSTEM_TOGGLE).performClick()
        rule.waitForIdle()

        assertTrue(calls.contains("system-true"))
        rule.onNodeWithTag(SplitTunnelTags.appRow(SETTINGS.packageName)).assertExists()
    }

    @Test
    fun tappingAnAppIsARequestToToggleIt() {
        show(uiState(settings = SplitSettings(SplitMode.EXCLUDE)))

        scrollTo(SplitTunnelTags.appRow(CHROME.packageName))
        rule.onNodeWithTag(SplitTunnelTags.appRow(CHROME.packageName)).performClick()

        assertTrue(calls.contains("toggle-${CHROME.packageName}"))
        rule.onNodeWithTag(SplitTunnelTags.appCheck(CHROME.packageName), useUnmergedTree = true).assertIsEnabled()
    }

    @Test
    fun offShowsTheAppsButDisablesTheirChecks() {
        show()

        scrollTo(SplitTunnelTags.appRow(CHROME.packageName))
        rule.onNodeWithTag(SplitTunnelTags.appRow(CHROME.packageName)).assertIsNotEnabled()
    }

    // ---- Kumiho ----------------------------------------------------------

    @Test
    fun kumihoStaysLockedOutsideInOff() {
        show()
        scrollTo(SplitTunnelTags.KUMIHO_ROW)
        rule.onNodeWithTag(SplitTunnelTags.KUMIHO_ROW).assertExists()
        rule.onNodeWithTag(SplitTunnelTags.KUMIHO_LOCK)
            .assertTextEquals(text(R.string.split_kumiho_locked))
        rule.onNodeWithTag(SplitTunnelTags.appRow(SELF.packageName)).assertDoesNotExist()
    }

    @Test
    fun kumihoStaysLockedOutsideInExclude() {
        show(uiState(settings = SplitSettings(SplitMode.EXCLUDE)))
        scrollTo(SplitTunnelTags.KUMIHO_ROW)
        rule.onNodeWithTag(SplitTunnelTags.KUMIHO_ROW).assertExists()
        rule.onNodeWithTag(SplitTunnelTags.KUMIHO_LOCK)
            .assertTextEquals(text(R.string.split_kumiho_locked))
        rule.onNodeWithTag(SplitTunnelTags.appRow(SELF.packageName)).assertDoesNotExist()
    }

    @Test
    fun kumihoIsOmittedFromInclude() {
        show(
            uiState(
                settings = SplitSettings(SplitMode.INCLUDE, setOf(CHROME.packageName)),
            ),
        )

        rule.onNodeWithTag(SplitTunnelTags.KUMIHO_ROW).assertDoesNotExist()
        rule.onNodeWithTag(SplitTunnelTags.appRow(SELF.packageName)).assertDoesNotExist()
    }

    // ---- banners and navigation -----------------------------------------

    @Test
    fun aLiveTunnelShowsTheReconnectBanner() {
        show(reconnectRequired = true)

        rule.onNodeWithTag(SplitTunnelTags.RECONNECT).assertExists()
        rule.onNodeWithText(text(R.string.split_reconnect)).assertExists()
    }

    @Test
    fun aStoppedTunnelHasNoReconnectBanner() {
        show(reconnectRequired = false)

        rule.onNodeWithTag(SplitTunnelTags.RECONNECT).assertDoesNotExist()
    }

    @Test
    fun theBackRowCallsTheBackCallback() {
        show()

        rule.onNodeWithTag(SplitTunnelTags.BACK).performClick()
        assertEquals(listOf("back"), calls)
    }

    // ---- failure and loading states --------------------------------------

    @Test
    fun aRepositoryFailureIsRetryable() {
        show(
            uiState(
                loading = false,
                repositoryError = "corrupt preferences",
            ),
        )

        rule.onNodeWithTag(SplitTunnelTags.ERROR).assertExists()
        rule.onNodeWithText("corrupt preferences").assertExists()
        rule.onNodeWithText(text(R.string.settings_retry)).performClick()

        assertEquals(listOf("retry-repository"), calls)
    }

    @Test
    fun anAppListFailureIsRetryable() {
        show(
            uiState(
                settings = SplitSettings(SplitMode.EXCLUDE),
                appsLoading = false,
                appsError = "package manager unavailable",
            ),
        )

        scrollTo(SplitTunnelTags.APPS_ERROR)
        rule.onNodeWithTag(SplitTunnelTags.APPS_ERROR).assertExists()
        rule.onNodeWithText(text(R.string.settings_retry)).performClick()

        assertEquals(listOf("retry-apps"), calls)
    }

    @Test
    fun aQueryWithNoAppsSaysSo() {
        show(
            uiState(
                settings = SplitSettings(SplitMode.EXCLUDE),
                search = "zzz",
            ),
        )

        scrollTo(SplitTunnelTags.APPS_EMPTY)
        rule.onNodeWithTag(SplitTunnelTags.APPS_EMPTY)
            .assertTextEquals(rule.activity.getString(R.string.split_apps_no_match, "zzz"))
    }

    @Test
    fun theRepositoryLoadingStateIsNamed() {
        show(uiState(loading = true))

        rule.onNodeWithTag(SplitTunnelTags.LOADING).assertExists()
        rule.onNodeWithTag(SplitTunnelTags.MODE_GROUP).assertDoesNotExist()
    }

    // ---- fixtures --------------------------------------------------------

    private fun uiState(
        settings: SplitSettings = SplitSettings(),
        loading: Boolean = false,
        repositoryError: String? = null,
        appsLoading: Boolean = false,
        appsError: String? = null,
        search: String = "",
        showSystem: Boolean = false,
    ) = SplitTunnelUiState(
        settings = settings,
        loading = loading,
        repositoryError = repositoryError,
        apps = APPS,
        appsLoading = appsLoading,
        appsError = appsError,
        search = search,
        showSystem = showSystem,
    )

    companion object {
        val SELF = InstalledApp(
            packageName = "dev.benyamin.kumiho.debug",
            label = "Kumiho",
            isSystem = false,
            launchable = true,
            isSelf = true,
        )
        val CHROME = InstalledApp(
            packageName = "com.android.chrome",
            label = "Chrome",
            isSystem = false,
            launchable = true,
        )
        val MAPS = InstalledApp(
            packageName = "com.example.maps",
            label = "Maps",
            isSystem = false,
            launchable = true,
        )
        val SETTINGS = InstalledApp(
            packageName = "com.android.settings",
            label = "Settings",
            isSystem = true,
            launchable = false,
        )

        val APPS = listOf(SELF, CHROME, MAPS, SETTINGS)
    }
}

/** A deterministic 40dp icon for tests, using only theme roles. */
@androidx.compose.runtime.Composable
fun FakeAppIcon(app: InstalledApp) {
    Box(
        modifier = Modifier
            .size(40.dp)
            .clip(RoundedCornerShape(8.dp))
            .background(MaterialTheme.colorScheme.surfaceContainerHigh)
            .testTag("fake_icon_${app.packageName}"),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = app.label.take(1).uppercase(),
            style = MaterialTheme.typography.titleMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}
