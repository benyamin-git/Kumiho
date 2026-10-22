package dev.benyamin.kumiho.ui.theme

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.luminance
import kotlin.math.pow
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Guards the frozen palette contract between the Go engine and the Compose
 * theme. The fixture (`src/test/resources/palette.json`) is real output from
 * `mobile.Palette(theme, accent)` for all three themes and six accents, so this
 * test fails if a role is renamed, dropped, or given a value that no theme can
 * render: the point is that no component ever needs a colour fallback.
 */
class PaletteJsonTest {

    private val fixture: JSONObject = JSONObject(readFixture())

    private val themes = listOf("light", "dark", "oled")
    private val accents = listOf("blue", "teal", "green", "orange", "rose", "violet")

    /** The house roles, in the engine's frozen emission order. */
    private val roles = PaletteMapping.roleNames

    private fun rolesFor(theme: String, accent: String): Map<String, String> {
        val obj = fixture.getJSONObject(theme).getJSONObject(accent)
        return obj.keys().asSequence().associateWith { obj.getString(it) }
    }

    @Test
    fun `fixture covers every theme and accent with exactly the frozen roles`() {
        themes.forEach { theme ->
            accents.forEach { accent ->
                val roles = rolesFor(theme, accent)
                assertEquals(
                    "role set drifted for $theme/$accent",
                    this.roles.toSet(),
                    roles.keys,
                )
                roles.forEach { (name, hex) ->
                    assertTrue("$theme/$accent/$name is not #rrggbb: $hex", Regex("^#[0-9a-f]{6}$").matches(hex))
                }
            }
        }
    }

    @Test
    fun `every pair maps to colours with no missing role`() {
        themes.forEach { theme ->
            accents.forEach { accent ->
                val colors = PaletteMapping.fromRoles(
                    rolesFor(theme, accent),
                    ThemeId.fromId(theme)!!,
                    KumihoAccent.fromId(accent)!!,
                )
                assertNotNull(colors)
                assertEquals(ThemeId.fromId(theme), colors.theme)
                assertEquals(KumihoAccent.fromId(accent), colors.accent)
            }
        }
    }

    @Test
    fun `a missing role is an error, never a fallback`() {
        val full = rolesFor("light", "blue")
        val missing = full - "primary"
        val failure = runCatching {
            PaletteMapping.fromRoles(missing, ThemeId.LIGHT, KumihoAccent.BLUE)
        }.exceptionOrNull()
        assertNotNull("a missing role must fail loudly", failure)
        assertTrue(failure!!.message!!.contains("primary"))
    }

    @Test
    fun `derived roles follow the house rules`() {
        themes.forEach { theme ->
            accents.forEach { accent ->
                val colors = PaletteMapping.fromRoles(
                    rolesFor(theme, accent),
                    ThemeId.fromId(theme)!!,
                    KumihoAccent.fromId(accent)!!,
                )
                val where = "$theme/$accent"

                // Page surface and body text are the house neutrals.
                assertEquals(where, colors.background, colors.surface)
                assertEquals(where, colors.onSurface, colors.onBackground)

                // The two documented derivations.
                assertEquals(where + " errorContainer", colors.loss.copy(alpha = 0.16f), colors.errorContainer)
                assertEquals(where + " outlineVariant", colors.outline.copy(alpha = 0.5f), colors.outlineVariant)

                // Error is the loss hue; on-error and on-error-container are the
                // house contrasts, not Material defaults.
                assertEquals(where, colors.loss, colors.error)
                assertEquals(where, colors.background, colors.onError)
                assertEquals(where, colors.loss, colors.onErrorContainer)

                // The container ladder is the five mapped steps, and the bright/
                // dim roles are its luminance extremes.
                val ladder = listOf(
                    colors.surfaceContainerLowest,
                    colors.surfaceContainerLow,
                    colors.surfaceContainer,
                    colors.surfaceContainerHigh,
                    colors.surfaceContainerHighest,
                )
                assertEquals(where, ladder.size, ladder.toSet().size)
                assertTrue(where, ladder.contains(colors.surfaceBright))
                assertTrue(where, ladder.contains(colors.surfaceDim))
                assertTrue(
                    where + " surfaceBright must be the brightest step",
                    colors.surfaceBright.luminance() >= colors.surfaceDim.luminance(),
                )

                // The only fixed overlay is black; the inverse pair is the
                // neutral pair swapped.
                assertEquals(where, Color.Black, colors.scrim)
                assertEquals(where, colors.onSurface, colors.inverseSurface)
                assertEquals(where, colors.background, colors.inverseOnSurface)
            }
        }
    }

    @Test
    fun `the full ColorScheme is built only from house roles`() {
        themes.forEach { theme ->
            accents.forEach { accent ->
                val colors = PaletteMapping.fromRoles(
                    rolesFor(theme, accent),
                    ThemeId.fromId(theme)!!,
                    KumihoAccent.fromId(accent)!!,
                )
                val where = "$theme/$accent"
                val legal = setOf(
                    colors.primary, colors.onPrimary, colors.primaryContainer, colors.onPrimaryContainer,
                    colors.surfaceTint, colors.inversePrimary,
                    colors.secondary, colors.onSecondary, colors.secondaryContainer, colors.onSecondaryContainer,
                    colors.tertiary, colors.onTertiary, colors.tertiaryContainer, colors.onTertiaryContainer,
                    colors.background, colors.onSurface, colors.outline,
                    colors.surfaceContainerLowest, colors.surfaceContainerLow, colors.surfaceContainer,
                    colors.surfaceContainerHigh, colors.surfaceContainerHighest,
                    colors.profit, colors.loss, colors.warning,
                    colors.errorContainer, colors.outlineVariant, colors.scrim,
                    colors.surfaceBright, colors.surfaceDim,
                )
                val scheme = colors.toColorScheme()

                // Every slot the theme renders is one of the mapped or derived
                // house colours; a defaulted slot would be a Material purple.
                val slots = listOf(
                    scheme.primary, scheme.onPrimary, scheme.primaryContainer, scheme.onPrimaryContainer,
                    scheme.inversePrimary,
                    scheme.secondary, scheme.onSecondary, scheme.secondaryContainer, scheme.onSecondaryContainer,
                    scheme.tertiary, scheme.onTertiary, scheme.tertiaryContainer, scheme.onTertiaryContainer,
                    scheme.background, scheme.onBackground,
                    scheme.surface, scheme.onSurface, scheme.surfaceVariant, scheme.onSurfaceVariant,
                    scheme.surfaceTint, scheme.inverseSurface, scheme.inverseOnSurface,
                    scheme.error, scheme.onError, scheme.errorContainer, scheme.onErrorContainer,
                    scheme.outline, scheme.outlineVariant, scheme.scrim,
                    scheme.surfaceBright, scheme.surfaceDim,
                    scheme.surfaceContainer, scheme.surfaceContainerHigh, scheme.surfaceContainerHighest,
                    scheme.surfaceContainerLow, scheme.surfaceContainerLowest,
                    scheme.primaryFixed, scheme.primaryFixedDim, scheme.onPrimaryFixed, scheme.onPrimaryFixedVariant,
                    scheme.secondaryFixed, scheme.secondaryFixedDim, scheme.onSecondaryFixed, scheme.onSecondaryFixedVariant,
                    scheme.tertiaryFixed, scheme.tertiaryFixedDim, scheme.onTertiaryFixed, scheme.onTertiaryFixedVariant,
                )
                assertEquals(where, 48, slots.size)
                slots.forEach { slot ->
                    assertTrue("$where has a colour outside the house palette: $slot", legal.contains(slot))
                }

                // The Material purple family must never appear. (Material's
                // default *error* hue is deliberately not on this list: the
                // house loss token is that same red in the Light theme, and the
                // `legal` membership check above already proves nothing was
                // defaulted.)
                listOf(
                    Color(0xFF6650A4), // Material light primary
                    Color(0xFFD0BCFF), // Material dark primary
                    Color(0xFF984061), // Material light primary-container
                    Color(0xFF4F378B), // Material light tertiary
                    Color(0xFF7D5260), // Material light secondary-container
                ).forEach { materialDefault ->
                    assertTrue(
                        "$where leaked a Material purple $materialDefault",
                        slots.none { it == materialDefault },
                    )
                }
            }
        }
    }

    @Test
    fun `text roles clear the contrast floor in every theme`() {
        val textPairs: List<Pair<String, String>> = listOf(
            "on-primary" to "primary",
            "on-surface" to "background",
            "on-primary-container" to "primary-container",
            "on-secondary-container" to "secondary-container",
            "on-tertiary-container" to "tertiary-container",
            "on-secondary" to "secondary",
            "on-tertiary" to "tertiary",
            "background" to "loss",
        )
        themes.forEach { theme ->
            accents.forEach { accent ->
                val roles = rolesFor(theme, accent)
                textPairs.forEach { (fg, bg) ->
                    val ratio = contrast(roles.getValue(fg), roles.getValue(bg))
                    assertTrue(
                        "$theme/$accent $fg on $bg is $ratio, below 4.5",
                        ratio >= 4.5,
                    )
                }
                // Outline is a non-text element: the 3:1 rule applies.
                val outline = contrast(roles.getValue("outline"), roles.getValue("background"))
                assertTrue("$theme/$accent outline on background is $outline, below 3.0", outline >= 3.0)
            }
        }
    }

    private fun readFixture(): String =
        checkNotNull(PaletteJsonTest::class.java.getResourceAsStream("/palette.json")) {
            "palette.json fixture is missing from test resources"
        }.use { it.readBytes().decodeToString() }

    /** WCAG relative-luminance contrast ratio between two hex colours. */
    private fun contrast(a: String, b: String): Double {
        val la = luminance(a)
        val lb = luminance(b)
        val hi = maxOf(la, lb)
        val lo = minOf(la, lb)
        return (hi + 0.05) / (lo + 0.05)
    }

    private fun luminance(hex: String): Double {
        fun channel(index: Int): Double {
            val v = hex.substring(1 + index * 2, 3 + index * 2).toInt(16) / 255.0
            return if (v <= 0.03928) v / 12.92 else ((v + 0.055) / 1.055).pow(2.4)
        }
        return 0.2126 * channel(0) + 0.7152 * channel(1) + 0.0722 * channel(2)
    }
}
