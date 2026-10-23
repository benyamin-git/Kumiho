package dev.benyamin.kumiho

import java.io.File
import javax.xml.parsers.DocumentBuilderFactory
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.w3c.dom.Element

/**
 * The Android manifest keeps global cleartext off; only the engine's loopback
 * callback is exempt. A custom URL scheme would be a wider attack surface, so
 * this test also pins the absence of an intent filter for one.
 */
class NetworkSecurityConfigTest {

    @Test
    fun `only loopback hosts may use cleartext`() {
        val root = parse("src/main/res/xml/network_security_config.xml").documentElement
        assertEquals("network-security-config", root.tagName)

        val base = root.childElements("base-config").single()
        assertEquals("false", base.getAttribute("cleartextTrafficPermitted"))

        val domains = root.childElements("domain-config")
        assertEquals("one cleartext exception block", 1, domains.size)
        assertEquals("true", domains.single().getAttribute("cleartextTrafficPermitted"))
        val hosts = domains.single().childElements("domain")
        assertEquals(listOf("127.0.0.1", "localhost"), hosts.map { it.textContent.trim() })
        assertTrue(hosts.all { it.getAttribute("includeSubdomains") == "false" })
    }

    @Test
    fun `the manifest points at the config and keeps cleartext off`() {
        val manifest = parse("src/main/AndroidManifest.xml").documentElement
        assertNotNull(manifest)
        val application = manifest.getElementsByTagName("application").item(0) as Element
        assertEquals("@xml/network_security_config", application.getAttribute("android:networkSecurityConfig"))
        assertEquals("false", application.getAttribute("android:usesCleartextTraffic"))
        assertFalse(
            "browser sign-in must not register a custom scheme",
            manifest.getElementsByTagName("intent-filter").let { filters ->
                (0 until filters.length).any { index ->
                    (filters.item(index) as Element).textContent.contains("kumiho://")
                }
            },
        )
    }

    private fun parse(path: String): org.w3c.dom.Document {
        val file = listOf(File(path), File("app/$path")).firstOrNull { it.isFile }
            ?: error("cannot find $path from ${File(".").absolutePath}")
        val factory = DocumentBuilderFactory.newInstance().apply {
            isNamespaceAware = false
            isExpandEntityReferences = false
        }
        return factory.newDocumentBuilder().parse(file)
    }

    private fun Element.childElements(tag: String): List<Element> {
        val nodes = childNodes
        return (0 until nodes.length).mapNotNull { index ->
            nodes.item(index).takeIf { it is Element && it.tagName == tag } as? Element
        }
    }
}
