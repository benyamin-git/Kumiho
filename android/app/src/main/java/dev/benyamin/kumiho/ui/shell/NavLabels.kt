package dev.benyamin.kumiho.ui.shell

import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource

/** The localized title of the active shell route. */
@Composable
fun ShellNavState.screenTitle(): String =
    stringResource(moreRoute?.labelRes ?: tab.labelRes)
