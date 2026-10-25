package dev.benyamin.kumiho.ui.account

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.SideEffect
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.compose.ui.window.DialogWindowProvider
import androidx.core.view.WindowCompat
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors
import dev.benyamin.kumiho.ui.theme.ThemeId

/** Stable test tags for the shared full-screen browser dialog host. */
object MozillaAccountBrowserDialogTags {
    const val HOST = "account_webview_dialog_host"
}

/**
 * Hosts the Mozilla account browser outside the app shell chrome.
 *
 * A full-screen dialog is deliberate: the Account and Setup routes live inside
 * the shell's 64dp top bar and bottom navigation, while the browser needs the
 * whole window and its own safe-area handling. The dialog owns no engine state;
 * closing it only closes the browser and leaves the flow active.
 */
@Composable
fun MozillaAccountBrowserDialog(
    loginId: String,
    url: String,
    onRedirect: (String) -> Unit,
    onClose: () -> Unit,
    onUnavailable: () -> Unit,
) {
    Dialog(
        onDismissRequest = onClose,
        properties = DialogProperties(
            dismissOnBackPress = true,
            dismissOnClickOutside = false,
            usePlatformDefaultWidth = false,
            decorFitsSystemWindows = false,
        ),
    ) {
        ApplyDialogSystemBars()
        Surface(
            modifier = Modifier
                .fillMaxSize()
                .testTag(MozillaAccountBrowserDialogTags.HOST),
            color = MaterialTheme.colorScheme.background,
        ) {
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    .windowInsetsPadding(WindowInsets.safeDrawing),
            ) {
                MozillaAccountBrowserScreen(
                    loginId = loginId,
                    url = url,
                    onRedirect = onRedirect,
                    onClose = onClose,
                    onUnavailable = onUnavailable,
                    modifier = Modifier.fillMaxSize(),
                )
            }
        }
    }
}

/**
 * The dialog is its own window, so the theme's status-bar appearance applied
 * around the Activity does not reach it. Match the house Light/Dark/OLED choice
 * here, or dark system icons would sit on a near-black dialog in Dark and OLED.
 */
@Composable
private fun ApplyDialogSystemBars() {
    val view = LocalView.current
    val dark = LocalKumihoColors.current.theme != ThemeId.LIGHT
    SideEffect {
        val window = (view.parent as? DialogWindowProvider)?.window ?: return@SideEffect
        val controller = WindowCompat.getInsetsController(window, view)
        controller.isAppearanceLightStatusBars = !dark
        controller.isAppearanceLightNavigationBars = !dark
    }
}
