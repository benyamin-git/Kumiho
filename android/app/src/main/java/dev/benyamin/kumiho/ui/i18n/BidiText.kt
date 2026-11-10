package dev.benyamin.kumiho.ui.i18n

/**
 * Bidi isolation for Latin technical values inside Persian sentences.
 *
 * The GUI uses `<bdi>`; Compose has no equivalent element, so these are the
 * Unicode isolate controls the bidi algorithm already understands. The value
 * itself is never changed, only wrapped, so copied text remains byte-exact
 * apart from the invisible controls.
 */
object BidiText {
    const val LRI = "\u2066"
    const val PDI = "\u2069"

    fun isolateLtr(value: String): String =
        if (value.isEmpty()) value else "$LRI$value$PDI"
}
