package dev.benyamin.kumiho.ui.theme

import android.animation.ValueAnimator
import android.content.Context
import android.provider.Settings
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.FiniteAnimationSpec
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.snap
import androidx.compose.animation.core.spring
import androidx.compose.animation.core.tween
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Stable
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext

/**
 * Motion, house rule: one expressive spring for the power control, restrained
 * springs and tweens everywhere else. When the system disables animators the
 * whole set collapses to snap, so nothing moves.
 */
object KumihoMotionSpec {
    /** The expressive spring behind the power control. */
    val Expressive: FiniteAnimationSpec<Float> = spring(
        dampingRatio = 0.34f,
        stiffness = Spring.StiffnessMediumLow,
        visibilityThreshold = 0.0001f,
    )

    /** A restrained spring for state changes. */
    val Restrained: FiniteAnimationSpec<Float> = spring(
        dampingRatio = 0.8f,
        stiffness = Spring.StiffnessMediumLow,
    )

    /** A short tween for cross-fades and small transitions. */
    val Quick: FiniteAnimationSpec<Float> = tween(
        durationMillis = 150,
        easing = FastOutSlowInEasing,
    )

    /** No animation at all. */
    val Snap: FiniteAnimationSpec<Float> = snap()
}

/** The motion specs for the current system setting. */
@Stable
class KumihoMotion internal constructor(val animationsEnabled: Boolean) {
    /** The power control spring, or snap under reduced motion. */
    val expressive: FiniteAnimationSpec<Float>
        get() = if (animationsEnabled) KumihoMotionSpec.Expressive else KumihoMotionSpec.Snap

    /** A restrained spring, or snap under reduced motion. */
    val restrained: FiniteAnimationSpec<Float>
        get() = if (animationsEnabled) KumihoMotionSpec.Restrained else KumihoMotionSpec.Snap

    /** A short tween, or snap under reduced motion. */
    val quick: FiniteAnimationSpec<Float>
        get() = if (animationsEnabled) KumihoMotionSpec.Quick else KumihoMotionSpec.Snap
}

/**
 * True when the system wants animations. [ValueAnimator.areAnimatorsEnabled]
 * already reflects the animator duration scale on API 26+; the explicit
 * duration-scale read keeps the intent obvious and covers a scale of zero.
 */
fun systemAnimationsEnabled(context: Context): Boolean {
    if (!ValueAnimator.areAnimatorsEnabled()) return false
    val scale = Settings.Global.getFloat(
        context.contentResolver,
        Settings.Global.ANIMATOR_DURATION_SCALE,
        1f,
    )
    return scale != 0f
}

/** Reads the system reduced-motion setting once per composition. */
@Composable
fun rememberMotion(): KumihoMotion {
    val context = LocalContext.current
    val enabled = remember(context) { systemAnimationsEnabled(context) }
    return remember(enabled) { KumihoMotion(enabled) }
}
