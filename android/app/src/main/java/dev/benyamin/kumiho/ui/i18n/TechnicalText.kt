package dev.benyamin.kumiho.ui.i18n

import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextDirection

/**
 * Text fields that carry a URL, token, host, port, IP, CIDR or email stay
 * left-to-right and left-aligned even in the Persian layout. Labels and errors
 * around them remain localized; only the value itself is technical.
 */
fun TextStyle.ltrTechnical(): TextStyle = copy(
    textAlign = TextAlign.Start,
    textDirection = TextDirection.Ltr,
)
