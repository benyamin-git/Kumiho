package dev.benyamin.kumiho.ui.locations

import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import dev.benyamin.kumiho.R

/** The visible country name; the REC pseudo-country is a localized shortcut. */
@Composable
fun locationCountryName(row: LocationRow): String =
    if (row.recommended) stringResource(R.string.locations_recommended) else row.countryName

/** The visible row title; the REC pseudo-country is a localized shortcut. */
@Composable
fun locationTitle(row: LocationRow): String =
    if (row.recommended) {
        stringResource(R.string.locations_recommended)
    } else {
        row.title
    }

/** The visible country name for a resolved engine selection. */
@Composable
fun locationCountryName(selection: LocationSelection): String? =
    if (selection.recommended) stringResource(R.string.locations_recommended) else selection.countryName
