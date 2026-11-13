package dev.benyamin.kumiho.ui.settings

import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import dev.benyamin.kumiho.R

/** The localized reason shown under the update check's failure headline. */
@Composable
fun updateFailureText(failure: UpdateFailure): String = when (failure) {
    UpdateFailure.InstalledVersionInvalid ->
        stringResource(R.string.settings_update_failure_installed)
    is UpdateFailure.Network -> stringResource(R.string.settings_update_failure_network)
    is UpdateFailure.Http ->
        stringResource(R.string.settings_update_failure_http, failure.code)
    UpdateFailure.Malformed -> stringResource(R.string.settings_update_failure_malformed)
    UpdateFailure.NoRelease -> stringResource(R.string.settings_update_failure_no_release)
    UpdateFailure.MissingTag -> stringResource(R.string.settings_update_failure_missing_tag)
    UpdateFailure.InvalidTag -> stringResource(R.string.settings_update_failure_invalid_tag)
    is UpdateFailure.Download -> stringResource(R.string.settings_update_failure_download)
    UpdateFailure.Checksum -> stringResource(R.string.settings_update_failure_checksum)
    UpdateFailure.InvalidApk -> stringResource(R.string.settings_update_failure_apk)
    is UpdateFailure.Install -> stringResource(R.string.settings_update_failure_install)
}
