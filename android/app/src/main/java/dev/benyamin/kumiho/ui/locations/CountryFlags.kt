package dev.benyamin.kumiho.ui.locations

import androidx.compose.foundation.Image
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors
import java.util.Locale

/** The ISO 3166-1 alpha-2 flag mapping shared by every Android surface. */
object CountryFlags {
    private val flagShape = RoundedCornerShape(2.dp)

    /** The bundled drawable for an ISO alpha-2 code, or null for REC/unknown. */
    fun resourceFor(code: String?): Int? {
        val normalized = code.orEmpty().trim().lowercase(Locale.ROOT)
        if (normalized.length != 2 || normalized.any { it !in 'a'..'z' }) return null
        return COUNTRY_FLAG_RESOURCES[normalized]
    }

    /**
     * A decorative 4:3 flag, star for Recommended or globe fallback. The
     * surrounding country/city text carries the meaning, so the image has no
     * content description (D38).
     */
    @Composable
    fun Flag(
        code: String?,
        modifier: Modifier = Modifier,
        recommended: Boolean = false,
        width: Dp = 24.dp,
        height: Dp = 18.dp,
    ) {
        val colors = LocalKumihoColors.current
        val resource = if (recommended) null else resourceFor(code)
        if (recommended) {
            Box(modifier = modifier.size(width, height), contentAlignment = Alignment.Center) {
                Icon(
                    painter = painterResource(R.drawable.ic_recommended_star),
                    contentDescription = null,
                    tint = colors.primary,
                    modifier = Modifier.size(height),
                )
            }
        } else if (resource == null) {
            Box(modifier = modifier.size(width, height), contentAlignment = Alignment.Center) {
                Icon(
                    painter = painterResource(R.drawable.ic_globe),
                    contentDescription = null,
                    tint = colors.onSurfaceVariant,
                    modifier = Modifier.size(height),
                )
            }
        } else {
            Image(
                painter = painterResource(resource),
                contentDescription = null,
                contentScale = ContentScale.FillBounds,
                modifier = modifier
                    .size(width, height)
                    .clip(flagShape)
                    .border(1.dp, colors.outlineVariant, flagShape),
            )
        }
    }
}
