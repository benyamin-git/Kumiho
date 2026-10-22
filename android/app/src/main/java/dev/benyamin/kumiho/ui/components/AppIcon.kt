package dev.benyamin.kumiho.ui.components

import android.content.Context
import android.util.LruCache
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.core.graphics.drawable.toBitmap
import dev.benyamin.kumiho.data.InstalledApp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * Loads and caches app icons off the main thread. The cache is small and
 * keyed by package; no image library is used.
 */
class AppIconLoader(context: Context) {
    private val appContext = context.applicationContext
    private val cache = LruCache<String, ImageBitmap>(64)

    suspend fun load(packageName: String, sizePx: Int): ImageBitmap? = withContext(Dispatchers.IO) {
        cache.get(packageName)?.let { return@withContext it }
        val bitmap = try {
            appContext.packageManager.getApplicationIcon(packageName)
                .toBitmap(sizePx, sizePx)
                .asImageBitmap()
        } catch (_: Exception) {
            return@withContext null
        }
        cache.put(packageName, bitmap)
        bitmap
    }
}

/**
 * One app icon: the launcher icon when it loads, otherwise a neutral lettered
 * placeholder using only theme roles. 40dp inside a 48dp row.
 */
@Composable
fun AppIcon(
    packageName: String,
    label: String,
    modifier: Modifier = Modifier,
    size: Dp = 40.dp,
) {
    val context = LocalContext.current
    val loader = remember(context) { AppIconLoader(context) }
    val sizePx = with(androidx.compose.ui.platform.LocalDensity.current) { size.roundToPx() }
    val image by produceState<ImageBitmap?>(initialValue = null, packageName, sizePx) {
        value = loader.load(packageName, sizePx)
    }
    val shape = RoundedCornerShape(8.dp)
    val current = image
    if (current != null) {
        Image(
            bitmap = current,
            contentDescription = null,
            modifier = modifier
                .size(size)
                .clip(shape),
        )
    } else {
        Box(
            modifier = modifier
                .size(size)
                .clip(shape)
                .background(MaterialTheme.colorScheme.surfaceContainerHigh),
            contentAlignment = Alignment.Center,
        ) {
            Text(
                text = label.take(1).uppercase(),
                style = MaterialTheme.typography.titleMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                textAlign = TextAlign.Center,
            )
        }
    }
}

/** Convenience overload used by the default split-screen icon slot. */
@Composable
fun AppIcon(app: InstalledApp, modifier: Modifier = Modifier) {
    AppIcon(
        packageName = app.packageName,
        label = app.label,
        modifier = modifier,
    )
}
