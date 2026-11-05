package dev.benyamin.kumiho.ui.components

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.rotate
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.currentStateAsState
import dev.benyamin.kumiho.R
import dev.benyamin.kumiho.ui.components.GaugeGeometry.ARC_STROKE_DP
import dev.benyamin.kumiho.ui.components.GaugeGeometry.START_DEGREES
import dev.benyamin.kumiho.ui.components.GaugeGeometry.TRACE_BAND_DP
import dev.benyamin.kumiho.ui.components.GaugeGeometry.quotaSweep
import dev.benyamin.kumiho.ui.components.GaugeGeometry.radiusFraction
import dev.benyamin.kumiho.ui.components.GaugeGeometry.sampleSweep
import dev.benyamin.kumiho.ui.theme.LocalKumihoColors
import dev.benyamin.kumiho.ui.theme.PillShape
import dev.benyamin.kumiho.ui.theme.rememberMotion
import kotlin.math.cos
import kotlin.math.max
import kotlin.math.sin

/**
 * The dashboard hero: one gauge with the quota arc outside, the 60 s throughput
 * trace inside and the power control at the centre.
 *
 * The arc and the trace are decorative; the state word, the rates and the
 * content description carry every fact, so nothing is encoded by colour or
 * drawing alone.
 */
@Composable
fun KumihoGauge(
    stateLabel: String,
    connected: Boolean,
    quotaFraction: Float?,
    samples: List<RateSample>,
    onToggle: () -> Unit,
    modifier: Modifier = Modifier,
    busy: Boolean = false,
    enabled: Boolean = true,
    size: Dp = 248.dp,
    pulseToken: Int = 0,
) {
    val colors = LocalKumihoColors.current
    val scheme = MaterialTheme.colorScheme
    val motion = rememberMotion()
    val description = stringResource(R.string.gauge_description, stateLabel)
    val interactive = enabled && !busy
    val lifecycleState by LocalLifecycleOwner.current.lifecycle.currentStateAsState()
    val resumed = lifecycleState == Lifecycle.State.RESUMED

    val quotaAngle by animateFloatAsState(
        targetValue = quotaSweep(quotaFraction ?: 0f),
        animationSpec = motion.gaugeSweep,
        label = "gaugeQuota",
    )

    val glowAlpha = if (connected && motion.glowEnabled && resumed) {
        val transition = rememberInfiniteTransition(label = "gaugeGlow")
        val alpha by transition.animateFloat(
            initialValue = 0.55f,
            targetValue = 1f,
            animationSpec = infiniteRepeatable(
                animation = tween(6_000, easing = FastOutSlowInEasing),
                repeatMode = RepeatMode.Reverse,
            ),
            label = "gaugeGlowAlpha",
        )
        alpha
    } else {
        1f
    }

    val pulse = remember { Animatable(1f) }
    LaunchedEffect(pulseToken) {
        if (pulseToken > 0 && motion.animationsEnabled) {
            pulse.snapTo(1f)
            pulse.animateTo(1.06f, tween(90))
            pulse.animateTo(1f, tween(90))
        }
    }

    val spin = if (busy && motion.animationsEnabled) {
        val transition = rememberInfiniteTransition(label = "gaugeBusy")
        val angle by transition.animateFloat(
            initialValue = 0f,
            targetValue = 360f,
            animationSpec = infiniteRepeatable(tween(1_400, easing = LinearEasing)),
            label = "gaugeSpin",
        )
        angle
    } else {
        0f
    }

    val container: Color = when {
        !enabled -> scheme.surfaceContainerHighest
        connected -> scheme.primary
        else -> scheme.primaryContainer
    }
    val content: Color = when {
        !enabled -> scheme.onSurfaceVariant
        connected -> scheme.onPrimary
        else -> scheme.onPrimaryContainer
    }

    Box(
        modifier = modifier
            .size(size)
            .clickable(enabled = interactive, role = Role.Button, onClick = onToggle)
            .semantics {
                contentDescription = description
                stateDescription = stateLabel
                role = Role.Button
            },
        contentAlignment = Alignment.Center,
    ) {
        Canvas(modifier = Modifier.fillMaxSize()) {
            val diameter = this.size.minDimension
            val centre = this.center
            val stroke = ARC_STROKE_DP.dp.toPx()
            val glowWidth = 20.dp.toPx()
            val arcRadius = diameter / 2f - glowWidth - stroke / 2f
            val arcTopLeft = Offset(centre.x - arcRadius, centre.y - arcRadius)
            val arcSize = androidx.compose.ui.geometry.Size(arcRadius * 2f, arcRadius * 2f)

            fun point(degrees: Float, radius: Float): Offset {
                val radians = Math.toRadians(degrees.toDouble())
                return Offset(
                    centre.x + radius * cos(radians).toFloat(),
                    centre.y + radius * sin(radians).toFloat(),
                )
            }

            // The glow sits outside the arc, on the page background only.
            if (connected) {
                if (motion.glowEnabled && resumed) {
                    drawCircle(
                        brush = Brush.radialGradient(
                            colors = listOf(
                                colors.glow.copy(alpha = glowAlpha),
                                Color.Transparent,
                            ),
                            center = centre,
                            radius = diameter / 2f,
                        ),
                        radius = diameter / 2f,
                        center = centre,
                    )
                } else {
                    drawCircle(
                        color = colors.glow.copy(alpha = 0.6f),
                        radius = arcRadius + glowWidth / 2f,
                        center = centre,
                        style = Stroke(glowWidth / 2f),
                    )
                }
            }

            drawArc(
                color = scheme.outlineVariant,
                startAngle = START_DEGREES,
                sweepAngle = 360f,
                useCenter = false,
                topLeft = arcTopLeft,
                size = arcSize,
                style = Stroke(stroke),
            )
            if (quotaFraction != null) {
                drawArc(
                    color = scheme.primary,
                    startAngle = START_DEGREES,
                    sweepAngle = quotaAngle,
                    useCenter = false,
                    topLeft = arcTopLeft,
                    size = arcSize,
                    style = Stroke(stroke, cap = StrokeCap.Round),
                )
            }
            if (busy) {
                rotate(degrees = spin, pivot = centre) {
                    drawArc(
                        color = colors.dataLine,
                        startAngle = START_DEGREES,
                        sweepAngle = 90f,
                        useCenter = false,
                        topLeft = arcTopLeft,
                        size = arcSize,
                        style = Stroke(stroke, cap = StrokeCap.Round),
                    )
                }
            }

            if (samples.isNotEmpty()) {
                val now = samples.last().atMillis
                val maxRate = samples.maxOf { max(it.down, it.up) }
                val halfBand = TRACE_BAND_DP.dp.toPx() / 2f
                val bandOuter = arcRadius - 6.dp.toPx()
                val downFloor = bandOuter - halfBand
                val upFloor = downFloor - halfBand
                val angles = samples.map {
                    START_DEGREES + sampleSweep(it.atMillis, now)
                }

                val downPath = Path()
                samples.forEachIndexed { i, sample ->
                    val p = point(
                        angles[i],
                        downFloor + radiusFraction(sample.down, maxRate) * halfBand,
                    )
                    if (i == 0) downPath.moveTo(p.x, p.y) else downPath.lineTo(p.x, p.y)
                }
                if (samples.size > 1) {
                    for (i in samples.indices.reversed()) {
                        val p = point(angles[i], downFloor)
                        downPath.lineTo(p.x, p.y)
                    }
                    downPath.close()
                    drawPath(downPath, color = colors.dataFill)
                } else {
                    drawCircle(
                        color = colors.dataFill,
                        radius = max(1f, radiusFraction(samples[0].down, maxRate) * halfBand),
                        center = point(angles[0], downFloor),
                    )
                }

                val upPath = Path()
                samples.forEachIndexed { i, sample ->
                    val p = point(
                        angles[i],
                        upFloor + radiusFraction(sample.up, maxRate) * halfBand,
                    )
                    if (i == 0) upPath.moveTo(p.x, p.y) else upPath.lineTo(p.x, p.y)
                }
                drawPath(
                    path = upPath,
                    color = colors.dataLine,
                    style = Stroke(width = 2.dp.toPx(), cap = StrokeCap.Round),
                )
            }
        }
        Box(
            modifier = Modifier
                .size(96.dp)
                .scale(pulse.value)
                .clip(PillShape)
                .background(container),
            contentAlignment = Alignment.Center,
        ) {
            if (busy) {
                CircularProgressIndicator(
                    color = content,
                    strokeWidth = 3.dp,
                    modifier = Modifier.size(32.dp),
                )
            } else {
                Icon(
                    painter = painterResource(R.drawable.ic_power),
                    contentDescription = null,
                    tint = content,
                    modifier = Modifier.size(32.dp),
                )
            }
        }
    }
}
