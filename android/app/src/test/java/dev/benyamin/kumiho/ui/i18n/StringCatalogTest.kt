package dev.benyamin.kumiho.ui.i18n

import java.io.File
import javax.xml.parsers.DocumentBuilderFactory
import org.junit.Assert.assertEquals
import org.junit.Assert.fail
import org.junit.Assert.assertTrue
import org.junit.Test
import org.w3c.dom.Element

/**
 * The English source is the contract. Persian may phrase a sentence
 * differently, but it may not drop a key, a plural quantity or a format token.
 */
class StringCatalogTest {

    private val english = parse("src/main/res/values/strings.xml")
    private val persian = parse("src/main/res/values-fa/strings.xml")

    @Test
    fun everyEnglishEntryHasAPersianEntry() {
        assertEquals(english.keys, persian.keys)
        assertEquals(
            english.filterValues { it.tag == "plurals" }.keys,
            persian.filterValues { it.tag == "plurals" }.keys,
        )
    }

    @Test
    fun formatTokensMatchInOrder() {
        english.keys.forEach { name ->
            assertEquals(
                "format tokens differ for $name",
                tokens(english.getValue(name).values),
                tokens(persian.getValue(name).values),
            )
        }
    }

    @Test
    fun pluralQuantitiesMatch() {
        english.filterValues { it.tag == "plurals" }.forEach { (name, entry) ->
            val source = entry.values.keys
            val translated = persian.getValue(name).values.keys
            assertEquals("plural quantities differ for $name", source, translated)
        }
    }

    @Test
    fun noPersianValueIsEmpty() {
        persian.forEach { (name, entry) ->
            entry.values.forEach { (quantity, value) ->
                assertTrue(
                    "empty Persian value for $name${if (quantity == null) "" else " ($quantity)"}",
                    value.isNotBlank(),
                )
            }
        }
    }

    @Test
    fun noAccidentalEnglishSentenceRemains() {
        val latinOnlyAllowed = setOf(
            "app_name",
            "tile_label",
            "notification_title",
            "notification_with_location",
            "notification_traffic",
            "dashboard_location_country_city",
            "dashboard_details_dns",
            "dashboard_details_ipv6",
            "locations_selection_separator",
            "settings_placeholder_relay_url",
            "settings_theme_oled",
            "settings_version_app",
            "settings_label_mtu",
            "settings_editor_title",
            "settings_option_cloudflare",
            "settings_option_google",
            "settings_option_quad9",
            "settings_option_sni",
            "diagnostics_report_meta",
            "diagnostics_report_host",
            "diagnostics_ports_value",
            "diagnostics_status_ok",
        )
        val asciiLetter = Regex("[A-Za-z]")
        val persianLetter = Regex("[\\u0600-\\u06FF]")
        persian.forEach { (name, entry) ->
            entry.values.forEach { (_, value) ->
                if (name !in latinOnlyAllowed && asciiLetter.containsMatchIn(value) &&
                    !persianLetter.containsMatchIn(value)
                ) {
                    fail("untranslated English value for $name: $value")
                }
            }
        }
    }

    private fun tokens(value: String): List<String> =
        FORMAT_TOKEN.findAll(value).map { it.value }.toList()

    private fun tokens(values: Map<String?, String>): Map<String?, List<String>> =
        values.mapValues { tokens(it.value) }

    private fun parse(path: String): Map<String, Entry> {
        val factory = DocumentBuilderFactory.newInstance().apply {
            isNamespaceAware = false
        }
        val document = factory.newDocumentBuilder().parse(File(path))
        val root = document.documentElement
        val entries = mutableMapOf<String, Entry>()
        for (index in 0 until root.childNodes.length) {
            val node = root.childNodes.item(index) as? Element ?: continue
            val name = node.getAttribute("name")
            when (node.tagName) {
                "string" -> entries[name] = Entry(
                    tag = "string",
                    values = mapOf(null to node.textContent),
                )
                "plurals" -> {
                    val items = mutableMapOf<String?, String>()
                    val children = node.childNodes
                    for (index in 0 until children.length) {
                        val item = children.item(index) as? Element ?: continue
                        if (item.tagName == "item") {
                            items[item.getAttribute("quantity")] = item.textContent
                        }
                    }
                    entries[name] = Entry("plurals", items)
                }
            }
        }
        return entries
    }

    private data class Entry(val tag: String, val values: Map<String?, String>)

    private fun Map<String, Entry>.filterValues(predicate: (Entry) -> Boolean): Map<String, Entry> =
        filter { (_, value) -> predicate(value) }

    private companion object {
        val FORMAT_TOKEN = Regex("%(?:\\d+\\$)?[-#+ 0,(<]*\\d*(?:\\.\\d+)?[a-zA-Z%]")
    }
}
