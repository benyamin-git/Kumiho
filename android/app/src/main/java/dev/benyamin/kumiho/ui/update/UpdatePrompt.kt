package dev.benyamin.kumiho.ui.update

import android.content.Context
import android.content.Intent
import android.provider.Settings
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.DialogProperties
import androidx.lifecycle.compose.LifecycleResumeEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.benyamin.kumiho.AppContainer
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.settings.UpdateState
import dev.benyamin.kumiho.ui.settings.updateFailureText
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors
import dev.benyamin.kumiho.update.UpdateInstaller

/**
 * The one launch-time consent dialog, shared by phone and TV. Settings owns
 * its own update row; this overlay only appears when the app-scoped manager
 * says a newer release was found or the user started an install there.
 */
@Composable
fun UpdatePromptHost(container: AppContainer) {
    val manager = container.updateManager
    val state by manager.state.collectAsStateWithLifecycle()
    val prompt by manager.prompt.collectAsStateWithLifecycle()
    if (!prompt) return
    if (state is UpdateState.Idle ||
        state is UpdateState.Checking ||
        state is UpdateState.UpToDate
    ) {
        return
    }

    val context = LocalContext.current
    val launchFailed = stringResource(R.string.update_prompt_launch_failed)
    var canInstall by remember { mutableStateOf(UpdateInstaller.canInstall(context)) }
    var launchError by remember { mutableStateOf<String?>(null) }
    val focus = remember { FocusRequester() }
    val current = state

    LifecycleResumeEffect(Unit) {
        canInstall = UpdateInstaller.canInstall(context)
        manager.onResume(installedVersion(context))
        onPauseOrDispose { }
    }
    LaunchedEffect(current, prompt) {
        runCatching { focus.requestFocus() }
    }

    val openRelease: (String) -> Unit = { url ->
        if (url.startsWith("https://")) {
            try {
                context.startActivity(
                    Intent(Intent.ACTION_VIEW, android.net.Uri.parse(url))
                        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
                )
                launchError = null
            } catch (_: Exception) {
                launchError = launchFailed
            }
        }
    }
    val allowInstalls: () -> Unit = {
        try {
            context.startActivity(
                UpdateInstaller.permissionIntent(context)
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
            )
            launchError = null
        } catch (_: Exception) {
            try {
                context.startActivity(
                    Intent(Settings.ACTION_SECURITY_SETTINGS)
                        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
                )
                launchError = null
            } catch (_: Exception) {
                launchError = launchFailed
            }
        }
    }
    val install: () -> Unit = {
        val file = manager.readyFile()
        if (file == null) {
            launchError = null
            manager.install()
        } else {
            try {
                UpdateInstaller.install(context, file)
                manager.onInstallerLaunched()
                launchError = null
            } catch (_: Exception) {
                launchError = launchFailed
            }
        }
    }

    val dismiss = { manager.dismissPrompt() }
    val dismissible = current !is UpdateState.Installing
    val failure = (current as? UpdateState.Available)?.installError

    AlertDialog(
        onDismissRequest = { if (dismissible) dismiss() },
        modifier = Modifier.testTag("update_prompt"),
        properties = DialogProperties(
            dismissOnBackPress = dismissible,
            dismissOnClickOutside = false,
        ),
        title = {
            Text(
                when (current) {
                    is UpdateState.ReadyToInstall ->
                        stringResource(R.string.update_prompt_ready_title)
                    is UpdateState.Installing ->
                        stringResource(R.string.update_prompt_installing_title)
                    is UpdateState.Installed ->
                        stringResource(R.string.update_prompt_installed_title)
                    is UpdateState.Failed ->
                        stringResource(R.string.settings_update_failed)
                    else -> stringResource(
                        R.string.update_prompt_title,
                        versionOf(current),
                    )
                },
            )
        },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                when (current) {
                    is UpdateState.Available -> {
                        Text(
                            stringResource(
                                if (current.installable) {
                                    R.string.update_prompt_body
                                } else {
                                    R.string.update_prompt_release_body
                                },
                            ),
                        )
                        failure?.let {
                            Text(
                                text = updateFailureText(it),
                                color = LocalKumihoColors.current.loss,
                            )
                        }
                    }
                    is UpdateState.Downloading -> {
                        LinearProgressIndicator(
                            progress = { current.progress / 100f },
                            modifier = Modifier
                                .fillMaxWidth()
                                .testTag("update_prompt_progress"),
                        )
                        Text(stringResource(R.string.update_prompt_downloading, current.progress))
                    }
                    is UpdateState.ReadyToInstall -> Text(
                        stringResource(
                            if (canInstall) {
                                R.string.update_prompt_ready_body
                            } else {
                                R.string.update_prompt_allow_body
                            },
                        ),
                    )
                    is UpdateState.Installing ->
                        Text(stringResource(R.string.update_prompt_installing_body))
                    is UpdateState.Installed -> Text(
                        stringResource(R.string.update_prompt_installed_body, current.version),
                    )
                    is UpdateState.Failed -> Text(updateFailureText(current.failure))
                    else -> Unit
                }
                launchError?.let { error ->
                    Text(
                        text = error,
                        color = LocalKumihoColors.current.loss,
                    )
                }
            }
        },
        confirmButton = {
            when (current) {
                is UpdateState.Available -> {
                    if (current.installable) {
                        TextButton(
                            onClick = manager::install,
                            modifier = Modifier
                                .focusRequester(focus)
                                .testTag("update_prompt_confirm"),
                        ) {
                            Text(stringResource(R.string.update_prompt_update))
                        }
                    } else {
                        TextButton(
                            onClick = { openRelease(current.url) },
                            modifier = Modifier
                                .focusRequester(focus)
                                .testTag("update_prompt_confirm"),
                        ) {
                            Text(stringResource(R.string.update_prompt_open))
                        }
                    }
                }
                is UpdateState.Downloading -> TextButton(
                    onClick = dismiss,
                    modifier = Modifier
                        .focusRequester(focus)
                        .testTag("update_prompt_confirm"),
                ) {
                    Text(stringResource(R.string.update_prompt_cancel))
                }
                is UpdateState.ReadyToInstall -> TextButton(
                    onClick = if (canInstall) install else allowInstalls,
                    modifier = Modifier
                        .focusRequester(focus)
                        .testTag("update_prompt_confirm"),
                ) {
                    Text(
                        stringResource(
                            if (canInstall) {
                                R.string.update_prompt_install
                            } else {
                                R.string.update_prompt_allow
                            },
                        ),
                    )
                }
                is UpdateState.Installing -> TextButton(
                    onClick = dismiss,
                    modifier = Modifier
                        .focusRequester(focus)
                        .testTag("update_prompt_confirm"),
                ) {
                    Text(stringResource(R.string.update_prompt_close))
                }
                is UpdateState.Installed -> TextButton(
                    onClick = dismiss,
                    modifier = Modifier
                        .focusRequester(focus)
                        .testTag("update_prompt_confirm"),
                ) {
                    Text(stringResource(R.string.update_prompt_close))
                }
                is UpdateState.Failed -> TextButton(
                    onClick = dismiss,
                    modifier = Modifier
                        .focusRequester(focus)
                        .testTag("update_prompt_confirm"),
                ) {
                    Text(stringResource(R.string.update_prompt_close))
                }
                else -> Unit
            }
        },
        dismissButton = {
            if (current is UpdateState.Available ||
                current is UpdateState.ReadyToInstall
            ) {
                TextButton(
                    onClick = dismiss,
                    modifier = Modifier.testTag("update_prompt_dismiss"),
                ) {
                    Text(stringResource(R.string.update_prompt_later))
                }
            }
        },
    )
}

private fun versionOf(state: UpdateState): String = when (state) {
    is UpdateState.Available -> state.version
    is UpdateState.Downloading -> state.version
    is UpdateState.ReadyToInstall -> state.version
    is UpdateState.Installing -> state.version
    is UpdateState.Installed -> state.version
    else -> ""
}

private fun installedVersion(context: Context): String = try {
    context.packageManager.getPackageInfo(context.packageName, 0).versionName.orEmpty()
} catch (_: Exception) {
    ""
}
