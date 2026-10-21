package dev.benyamin.kumiho.ui.components

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.AccountCircle
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Settings
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.painter.Painter
import androidx.compose.ui.graphics.vector.rememberVectorPainter
import androidx.compose.ui.res.painterResource
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.shell.Destination
import dev.benyamin.kumiho.ui.shell.MoreRoute

/**
 * Icon mapping. Core Material icons cover home/account/settings/more; the
 * glyphs the core set lacks (globe, gauge, power, shield, split, tile) are the
 * house vector drawables. Everything is a [Painter] so components take one type.
 */
@Composable
fun destinationIcon(destination: Destination): Painter = when (destination) {
    Destination.Home -> rememberVectorPainter(Icons.Filled.Home)
    Destination.Locations -> painterResource(R.drawable.ic_globe)
    Destination.Account -> rememberVectorPainter(Icons.Filled.AccountCircle)
    Destination.Settings -> rememberVectorPainter(Icons.Filled.Settings)
    Destination.More -> rememberVectorPainter(Icons.Filled.MoreVert)
}

/** The icon for a More route. */
@Composable
fun moreRouteIcon(route: MoreRoute): Painter = when (route) {
    MoreRoute.Logs -> rememberVectorPainter(Icons.AutoMirrored.Filled.List)
    MoreRoute.Diagnostics -> painterResource(R.drawable.ic_gauge)
}
