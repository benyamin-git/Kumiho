package dev.benyamin.kumiho.ui.theme

import androidx.compose.material3.ColorScheme
import androidx.compose.runtime.Immutable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.luminance
import org.json.JSONObject

/** The three house themes. The stored id is exactly the Go `theme.Theme` value. */
enum class ThemeId(val id: String, val label: String) {
    LIGHT("light", "Light"),
    DARK("dark", "Dark"),
    OLED("oled", "OLED"),
    ;

    companion object {
        /** Resolves a stored id; null when absent or unknown. */
        fun fromId(id: String?): ThemeId? = entries.firstOrNull { it.id == id }
    }
}

/** The six house accents, blue by default. */
enum class KumihoAccent(val id: String, val label: String) {
    BLUE("blue", "Blue"),
    TEAL("teal", "Teal"),
    GREEN("green", "Green"),
    ORANGE("orange", "Orange"),
    ROSE("rose", "Rose"),
    VIOLET("violet", "Violet"),
    ;

    companion object {
        /** The house default accent. */
        val Default: KumihoAccent = BLUE

        /** Resolves a stored id; null when absent or unknown. */
        fun fromId(id: String?): KumihoAccent? = entries.firstOrNull { it.id == id }
    }
}

/**
 * The house palette for one theme and accent: every semantic role the app uses,
 * in Compose [Color] form. Built from the Go palette ([PaletteMapping]) with no
 * fallbacks, so a component can never read a role that was silently defaulted.
 *
 * Only roles the Go palette emits are constructor parameters; MD3 roles the
 * house map has no standalone token for are derived from those roles
 * ([errorContainer], [outlineVariant], the container ladder) rather than
 * invented. No raw hex lives outside [PaletteMapping].
 */
@Immutable
data class KumihoColors(
    val theme: ThemeId,
    val accent: KumihoAccent,
    // Primary family.
    val primary: Color,
    val onPrimary: Color,
    val primaryContainer: Color,
    val onPrimaryContainer: Color,
    val surfaceTint: Color,
    val inversePrimary: Color,
    // Secondary family.
    val secondary: Color,
    val onSecondary: Color,
    val secondaryContainer: Color,
    val onSecondaryContainer: Color,
    // Tertiary family.
    val tertiary: Color,
    val onTertiary: Color,
    val tertiaryContainer: Color,
    val onTertiaryContainer: Color,
    // Theme neutrals.
    val background: Color,
    val onSurface: Color,
    val outline: Color,
    // The five-step surface-container ladder, lowest to highest.
    val surfaceContainerLowest: Color,
    val surfaceContainerLow: Color,
    val surfaceContainer: Color,
    val surfaceContainerHigh: Color,
    val surfaceContainerHighest: Color,
    // Status colours, fixed by the house style and never tinted.
    val profit: Color,
    val loss: Color,
    val warning: Color,
) {
    /** The page surface is the house background. */
    val surface: Color get() = background

    /** Body text on the page surface. */
    val onBackground: Color get() = onSurface

    /** A subtle surface: the first raised step of the ladder. */
    val surfaceVariant: Color get() = surfaceContainerLow

    /** Secondary text; the house map has no muted on-surface token. */
    val onSurfaceVariant: Color get() = onSurface

    /** A faint outline for dividers and card borders. */
    val outlineVariant: Color get() = outline.copy(alpha = 0.5f)

    /** Material error is the house loss colour. */
    val error: Color get() = loss

    /** On-error text: the page colour contrasts with the loss hue in every theme. */
    val onError: Color get() = background

    /** A 16% loss wash; the house map has no standalone error container. */
    val errorContainer: Color get() = loss.copy(alpha = 0.16f)

    /** Error text on the error container. */
    val onErrorContainer: Color get() = loss

    /** The brightest ladder step, for bright surface roles. */
    val surfaceBright: Color get() = brightestSurface

    /** The dimmest ladder step, for dim surface roles. */
    val surfaceDim: Color get() = dimmestSurface

    val inverseSurface: Color get() = onSurface
    val inverseOnSurface: Color get() = background

    /** The one fixed overlay: a modal scrim is black in every theme. */
    val scrim: Color get() = Color.Black

    private val ladder: List<Color>
        get() = listOf(
            surfaceContainerLowest,
            surfaceContainerLow,
            surfaceContainer,
            surfaceContainerHigh,
            surfaceContainerHighest,
        )

    private val brightestSurface: Color get() = ladder.maxBy { it.luminance() }
    private val dimmestSurface: Color get() = ladder.minBy { it.luminance() }

    /** The full Material 3 scheme, every slot from a house role, no default. */
    fun toColorScheme(): ColorScheme = ColorScheme(
        primary = primary,
        onPrimary = onPrimary,
        primaryContainer = primaryContainer,
        onPrimaryContainer = onPrimaryContainer,
        inversePrimary = inversePrimary,
        secondary = secondary,
        onSecondary = onSecondary,
        secondaryContainer = secondaryContainer,
        onSecondaryContainer = onSecondaryContainer,
        tertiary = tertiary,
        onTertiary = onTertiary,
        tertiaryContainer = tertiaryContainer,
        onTertiaryContainer = onTertiaryContainer,
        background = background,
        onBackground = onBackground,
        surface = surface,
        onSurface = onSurface,
        surfaceVariant = surfaceVariant,
        onSurfaceVariant = onSurfaceVariant,
        surfaceTint = surfaceTint,
        inverseSurface = inverseSurface,
        inverseOnSurface = inverseOnSurface,
        error = error,
        onError = onError,
        errorContainer = errorContainer,
        onErrorContainer = onErrorContainer,
        outline = outline,
        outlineVariant = outlineVariant,
        scrim = scrim,
        surfaceBright = surfaceBright,
        surfaceDim = surfaceDim,
        surfaceContainer = surfaceContainer,
        surfaceContainerHigh = surfaceContainerHigh,
        surfaceContainerHighest = surfaceContainerHighest,
        surfaceContainerLow = surfaceContainerLow,
        surfaceContainerLowest = surfaceContainerLowest,
        primaryFixed = primary,
        primaryFixedDim = primaryContainer,
        onPrimaryFixed = onPrimary,
        onPrimaryFixedVariant = onPrimaryContainer,
        secondaryFixed = secondary,
        secondaryFixedDim = secondaryContainer,
        onSecondaryFixed = onSecondary,
        onSecondaryFixedVariant = onSecondaryContainer,
        tertiaryFixed = tertiary,
        tertiaryFixedDim = tertiaryContainer,
        onTertiaryFixed = onTertiary,
        onTertiaryFixedVariant = onTertiaryContainer,
    )
}

/**
 * Turns the Go palette JSON into [KumihoColors]. This is the only place that
 * knows the wire names: the keys are exactly the Go custom-property names
 * without the `--` prefix. Every role is required; a missing key is an error,
 * never a fallback.
 */
object PaletteMapping {
    /** Every role the Go palette emits, in its frozen emission order. */
    val roleNames: List<String> = listOf(
        "primary", "on-primary", "primary-container", "on-primary-container",
        "surface-tint", "inverse-primary",
        "secondary", "on-secondary", "secondary-container", "on-secondary-container",
        "tertiary", "on-tertiary", "tertiary-container", "on-tertiary-container",
        "background", "on-surface", "outline",
        "surface-container-1", "surface-container-2", "surface-container-3",
        "surface-container-4", "surface-container-5",
        "profit", "loss", "warning",
    )

    /** Maps a role map to [KumihoColors], requiring every role. */
    fun fromRoles(roles: Map<String, String>, theme: ThemeId, accent: KumihoAccent): KumihoColors {
        val missing = roleNames.filterNot(roles::containsKey)
        require(missing.isEmpty()) {
            "palette ${theme.id}/${accent.id} is missing roles: ${missing.joinToString()}"
        }
        fun role(name: String): Color = parseHex(roles.getValue(name))
        return KumihoColors(
            theme = theme,
            accent = accent,
            primary = role("primary"),
            onPrimary = role("on-primary"),
            primaryContainer = role("primary-container"),
            onPrimaryContainer = role("on-primary-container"),
            surfaceTint = role("surface-tint"),
            inversePrimary = role("inverse-primary"),
            secondary = role("secondary"),
            onSecondary = role("on-secondary"),
            secondaryContainer = role("secondary-container"),
            onSecondaryContainer = role("on-secondary-container"),
            tertiary = role("tertiary"),
            onTertiary = role("on-tertiary"),
            tertiaryContainer = role("tertiary-container"),
            onTertiaryContainer = role("on-tertiary-container"),
            background = role("background"),
            onSurface = role("on-surface"),
            outline = role("outline"),
            surfaceContainerLowest = role("surface-container-1"),
            surfaceContainerLow = role("surface-container-2"),
            surfaceContainer = role("surface-container-3"),
            surfaceContainerHigh = role("surface-container-4"),
            surfaceContainerHighest = role("surface-container-5"),
            profit = role("profit"),
            loss = role("loss"),
            warning = role("warning"),
        )
    }

    /** Maps the Go palette JSON object to [KumihoColors]. */
    fun fromJson(json: String, theme: ThemeId, accent: KumihoAccent): KumihoColors {
        val obj = JSONObject(json)
        val roles = obj.keys().asSequence().associateWith { obj.getString(it) }
        return fromRoles(roles, theme, accent)
    }

    /** Parses `#rrggbb` (or `#aarrggbb`) into an opaque [Color]. */
    fun parseHex(hex: String): Color {
        val digits = hex.removePrefix("#")
        require(digits.length == 6 || digits.length == 8) { "not a hex colour: $hex" }
        val value = digits.toLong(16)
        val argb = if (digits.length == 6) 0xFF000000L or value else value
        return Color(argb)
    }
}
